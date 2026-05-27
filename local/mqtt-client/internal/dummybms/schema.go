// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package dummybms

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

const (
	DefaultBMSSchemaPath = "../../schema/schema/bms/bms.yaml"
	bmsSchemaResource    = "https://dsx.local/schema/bms.yaml"
)

type BMSSchema struct {
	spec     map[string]any
	compiler *jsonschema.Compiler
	compiled map[string]*jsonschema.Schema
}

func LoadBMSSchema(path string) (*BMSSchema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read BMS schema %q: %w", path, err)
	}

	var spec map[string]any
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to parse BMS schema %q: %w", path, err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource(bmsSchemaResource, spec); err != nil {
		return nil, fmt.Errorf("failed to add BMS schema resource: %w", err)
	}

	return &BMSSchema{
		spec:     spec,
		compiler: compiler,
		compiled: make(map[string]*jsonschema.Schema),
	}, nil
}

func (s *BMSSchema) ValidateMessage(topic string, payload []byte) error {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("payload must be JSON: %w", err)
	}

	channels, ok := asMap(s.spec["channels"])
	if !ok {
		return fmt.Errorf("BMS schema has no channels")
	}

	var matchedChannels int
	var validationErrors []string
	for channelName, channelNode := range channels {
		channel, ok := asMap(channelNode)
		if !ok {
			continue
		}
		address, ok := asString(channel["address"])
		if !ok {
			continue
		}

		params, ok := matchTopicAddress(address, topic)
		if !ok {
			continue
		}
		if err := validateChannelParams(channel, params); err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", channelName, err))
			continue
		}
		matchedChannels++

		if err := s.validateChannelMessages(channel, params, value); err == nil {
			return nil
		} else {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", channelName, err))
		}
	}

	if matchedChannels == 0 {
		return fmt.Errorf("topic %q does not match any BMS schema channel: %s", topic, joinErrors(validationErrors))
	}
	return fmt.Errorf("payload for topic %q does not match BMS schema: %s", topic, joinErrors(validationErrors))
}

func (s *BMSSchema) validateChannelMessages(channel map[string]any, params map[string]string, value any) error {
	messages, ok := asMap(channel["messages"])
	if !ok {
		return fmt.Errorf("channel has no messages")
	}

	var validationErrors []string
	for name, messageNode := range messages {
		location, err := messagePayloadSchemaLocation(messageNode)
		if err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		schema, err := s.compile(location)
		if err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if err := schema.Validate(value); err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if err := validatePayloadTopicParameters(params, value); err != nil {
			validationErrors = append(validationErrors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		return nil
	}

	return fmt.Errorf("no channel message matched: %s", joinErrors(validationErrors))
}

func validatePayloadTopicParameters(params map[string]string, value any) error {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}

	for name, want := range params {
		value, ok := object[name]
		if !ok {
			continue
		}
		got, ok := value.(string)
		if !ok {
			return fmt.Errorf("payload %s must be a string to match topic parameter", name)
		}
		if got != want {
			return fmt.Errorf("payload %s %q does not match topic parameter %q", name, got, want)
		}
	}
	return nil
}

func (s *BMSSchema) compile(location string) (*jsonschema.Schema, error) {
	schema, ok := s.compiled[location]
	if ok {
		return schema, nil
	}

	schema, err := s.compiler.Compile(location)
	if err != nil {
		return nil, fmt.Errorf("failed to compile %s: %w", location, err)
	}
	s.compiled[location] = schema
	return schema, nil
}

func messagePayloadSchemaLocation(messageNode any) (string, error) {
	message, ok := asMap(messageNode)
	if !ok {
		return "", fmt.Errorf("message node is not an object")
	}
	ref, ok := asString(message["$ref"])
	if !ok {
		return "", fmt.Errorf("message node has no $ref")
	}
	if !strings.HasPrefix(ref, "#/components/messages/") {
		return "", fmt.Errorf("unsupported message ref %q", ref)
	}
	return bmsSchemaResource + "#" + strings.TrimPrefix(ref, "#") + "/payload", nil
}

func matchTopicAddress(address string, topic string) (map[string]string, bool) {
	addressParts := strings.Split(address, "/")
	topicParts := strings.Split(topic, "/")
	params := make(map[string]string)

	for _, topicPart := range topicParts {
		if topicPart == "" {
			return nil, false
		}
		if containsMQTTWildcard(topicPart) {
			return nil, false
		}
	}

	for i, addressPart := range addressParts {
		if i >= len(topicParts) {
			return nil, false
		}
		if isAddressParam(addressPart) {
			paramName := strings.TrimSuffix(strings.TrimPrefix(addressPart, "{"), "}")
			if i == len(addressParts)-1 {
				params[paramName] = strings.Join(topicParts[i:], "/")
				return params, params[paramName] != ""
			}
			params[paramName] = topicParts[i]
			continue
		}
		if addressPart != topicParts[i] {
			return nil, false
		}
	}

	return params, len(topicParts) == len(addressParts)
}

func validateChannelParams(channel map[string]any, params map[string]string) error {
	parameters, ok := asMap(channel["parameters"])
	if !ok {
		return nil
	}

	for name, value := range params {
		parameter, ok := asMap(parameters[name])
		if !ok {
			continue
		}
		enumValues, ok := asSlice(parameter["enum"])
		if !ok {
			continue
		}
		if !enumContains(enumValues, value) {
			return fmt.Errorf("topic parameter %s=%q is not in enum", name, value)
		}
	}
	return nil
}

func enumContains(enumValues []any, value string) bool {
	for _, enumValue := range enumValues {
		if enumString, ok := enumValue.(string); ok && enumString == value {
			return true
		}
	}
	return false
}

func isAddressParam(value string) bool {
	return strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}")
}

func containsMQTTWildcard(value string) bool {
	return strings.ContainsAny(value, "+#")
}

func asMap(value any) (map[string]any, bool) {
	object, ok := value.(map[string]any)
	return object, ok
}

func asSlice(value any) ([]any, bool) {
	slice, ok := value.([]any)
	return slice, ok
}

func asString(value any) (string, bool) {
	stringValue, ok := value.(string)
	return stringValue, ok
}

func joinErrors(validationErrors []string) string {
	if len(validationErrors) == 0 {
		return "no details"
	}
	if len(validationErrors) > 3 {
		validationErrors = append(validationErrors[:3], fmt.Sprintf("... %d more", len(validationErrors)-3))
	}
	return strings.Join(validationErrors, "; ")
}
