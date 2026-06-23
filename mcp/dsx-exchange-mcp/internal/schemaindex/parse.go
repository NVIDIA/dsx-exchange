// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package schemaindex

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type document struct {
	AsyncAPI   string               `yaml:"asyncapi"`
	Info       info                 `yaml:"info"`
	Channels   map[string]channel   `yaml:"channels"`
	Operations map[string]operation `yaml:"operations"`
	Components components           `yaml:"components"`
}

type info struct {
	Title       string `yaml:"title"`
	Version     string `yaml:"version"`
	Description string `yaml:"description"`
}

type channel struct {
	Ref         string                `yaml:"$ref"`
	Address     string                `yaml:"address"`
	Description string                `yaml:"description"`
	Parameters  map[string]parameter  `yaml:"parameters"`
	Messages    map[string]messageRef `yaml:"messages"`
}

type parameter struct {
	Ref         string   `yaml:"$ref"`
	Description string   `yaml:"description"`
	Enum        []string `yaml:"enum"`
}

type messageRef struct {
	Ref         string         `yaml:"$ref"`
	Name        string         `yaml:"name"`
	Title       string         `yaml:"title"`
	Summary     string         `yaml:"summary"`
	Description string         `yaml:"description"`
	Payload     map[string]any `yaml:"payload"`
}

type message struct {
	Name        string         `yaml:"name"`
	Title       string         `yaml:"title"`
	Summary     string         `yaml:"summary"`
	Description string         `yaml:"description"`
	Payload     map[string]any `yaml:"payload"`
}

type operation struct {
	Action      string      `yaml:"action"`
	Summary     string      `yaml:"summary"`
	Description string      `yaml:"description"`
	Channel     reference   `yaml:"channel"`
	Messages    []reference `yaml:"messages"`
}

type reference struct {
	Ref string `yaml:"$ref"`
}

type components struct {
	Messages   map[string]message        `yaml:"messages"`
	Parameters map[string]parameter      `yaml:"parameters"`
	Schemas    map[string]map[string]any `yaml:"schemas"`
}

func parseDocument(name string, body []byte) (document, error) {
	var doc document
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return document{}, fmt.Errorf("parse %s: %w", name, err)
	}
	if strings.TrimSpace(doc.AsyncAPI) == "" {
		return document{}, fmt.Errorf("parse %s: %w", name, errMissingAsyncAPI)
	}
	return doc, nil
}

// docTopics flattens the AsyncAPI channel map into the compact Topic records
// returned by MCP schema tools.
func docTopics(domain string, doc document) []Topic {
	names := make([]string, 0, len(doc.Channels))
	for name := range doc.Channels {
		names = append(names, name)
	}
	sort.Strings(names)

	topics := make([]Topic, 0, len(names))
	for _, name := range names {
		ch := doc.Channels[name]
		if ch.Address == "" {
			continue
		}
		topics = append(topics, Topic{
			Domain:               domain,
			SpecTitle:            doc.Info.Title,
			SpecVersion:          doc.Info.Version,
			Channel:              name,
			Address:              ch.Address,
			TopicFilter:          addressToFilter(ch.Address, nil),
			Description:          strings.TrimSpace(ch.Description),
			RetainedLiveBehavior: retainedLiveBehavior(ch.Address),
			Parameters:           summarizeParameters(ch.Parameters, doc.Components.Parameters),
			Messages:             summarizeMessages(ch.Messages, doc.Components),
			Operations:           summarizeOperations(name, doc.Operations),
		})
	}
	return topics
}

func summarizeParameters(params map[string]parameter, components map[string]parameter) []ParameterSummary {
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]ParameterSummary, 0, len(names))
	for _, name := range names {
		p := params[name]
		if p.Ref != "" {
			if resolved, ok := components[refName(p.Ref)]; ok {
				p = resolved
			}
		}
		out = append(out, ParameterSummary{
			Name:        name,
			Description: strings.TrimSpace(p.Description),
			Enum:        append([]string{}, p.Enum...),
		})
	}
	return out
}

func summarizeMessages(refs map[string]messageRef, components components) []MessageSummary {
	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]MessageSummary, 0, len(names))
	for _, name := range names {
		ref := refs[name]
		msg := message{
			Name:        ref.Name,
			Title:       ref.Title,
			Summary:     ref.Summary,
			Description: ref.Description,
			Payload:     ref.Payload,
		}
		if ref.Ref != "" {
			if resolved, ok := components.Messages[refName(ref.Ref)]; ok {
				msg = resolved
			}
		}
		out = append(out, MessageSummary{
			Name:        firstNonEmpty(msg.Name, name),
			Ref:         ref.Ref,
			Title:       msg.Title,
			Summary:     msg.Summary,
			Description: strings.TrimSpace(msg.Description),
			Payload:     summarizePayload(msg.Payload, components.Schemas),
		})
	}
	return out
}

func summarizeOperations(channelName string, operations map[string]operation) []OperationSummary {
	var out []OperationSummary
	for name, op := range operations {
		if refName(op.Channel.Ref) != channelName {
			continue
		}
		out = append(out, OperationSummary{
			Name:        name,
			Action:      op.Action,
			Summary:     op.Summary,
			Description: strings.TrimSpace(op.Description),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func summarizePayload(payload map[string]any, schemas map[string]map[string]any) PayloadShape {
	if len(payload) == 0 {
		return PayloadShape{}
	}
	if ref, _ := payload["$ref"].(string); ref != "" {
		shape := summarizeSchema(schemas[refName(ref)])
		shape.Ref = ref
		return shape
	}
	return summarizeSchema(payload)
}

func summarizeSchema(schema map[string]any) PayloadShape {
	if len(schema) == 0 {
		return PayloadShape{}
	}
	shape := PayloadShape{
		Type:     stringValue(schema["type"]),
		Required: stringSlice(schema["required"]),
		AllOf:    refList(schema["allOf"]),
		OneOf:    refList(schema["oneOf"]),
	}
	props := mapValue(schema["properties"])
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		prop := mapValue(props[name])
		shape.Properties = append(shape.Properties, PropertySummary{
			Name:        name,
			Type:        stringValue(prop["type"]),
			Ref:         stringValue(prop["$ref"]),
			Description: strings.TrimSpace(stringValue(prop["description"])),
			Enum:        stringSlice(prop["enum"]),
		})
	}
	return shape
}
