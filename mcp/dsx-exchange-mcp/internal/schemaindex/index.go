// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package schemaindex

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/schemas"
)

type Index struct {
	topics []Topic
}

type SearchOptions struct {
	Domain          string
	Query           string
	Role            string
	ObjectType      string
	PointType       string
	OperationAction string
	Limit           int
}

type Topic struct {
	Domain               string             `json:"domain"`
	SpecTitle            string             `json:"spec_title,omitempty"`
	SpecVersion          string             `json:"spec_version,omitempty"`
	Channel              string             `json:"channel"`
	Address              string             `json:"address"`
	TopicFilter          string             `json:"topic_filter"`
	Description          string             `json:"description,omitempty"`
	RetainedLiveBehavior string             `json:"retained_live_behavior,omitempty"`
	MatchedParameters    map[string]string  `json:"matched_parameters,omitempty"`
	Parameters           []ParameterSummary `json:"parameters,omitempty"`
	Messages             []MessageSummary   `json:"messages,omitempty"`
	Operations           []OperationSummary `json:"operations,omitempty"`
	RelatedTopics        []RelatedTopic     `json:"related_topics,omitempty"`
	Examples             []string           `json:"examples,omitempty"`
}

type ParameterSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type MessageSummary struct {
	Name        string       `json:"name"`
	Ref         string       `json:"ref,omitempty"`
	Title       string       `json:"title,omitempty"`
	Summary     string       `json:"summary,omitempty"`
	Description string       `json:"description,omitempty"`
	Payload     PayloadShape `json:"payload,omitempty"`
}

type PayloadShape struct {
	Ref        string            `json:"ref,omitempty"`
	Type       string            `json:"type,omitempty"`
	Required   []string          `json:"required,omitempty"`
	Properties []PropertySummary `json:"properties,omitempty"`
	AllOf      []string          `json:"all_of,omitempty"`
	OneOf      []string          `json:"one_of,omitempty"`
}

type PropertySummary struct {
	Name        string   `json:"name"`
	Type        string   `json:"type,omitempty"`
	Ref         string   `json:"ref,omitempty"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

type OperationSummary struct {
	Name        string `json:"name"`
	Action      string `json:"action,omitempty"`
	Summary     string `json:"summary,omitempty"`
	Description string `json:"description,omitempty"`
}

type RelatedTopic struct {
	Role        string `json:"role"`
	TopicFilter string `json:"topic_filter"`
}

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

var errMissingAsyncAPI = errors.New("missing asyncapi version")

var (
	defaultOnce sync.Once
	defaultIdx  *Index
	defaultErr  error
)

func Default() (*Index, error) {
	defaultOnce.Do(func() {
		defaultIdx, defaultErr = Load()
	})
	return defaultIdx, defaultErr
}

func Load() (*Index, error) {
	var topics []Topic
	err := fs.WalkDir(schemas.FS, "asyncapi", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := strings.ToLower(path.Ext(p))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil
		}
		body, err := schemas.FS.ReadFile(p)
		if err != nil {
			return err
		}
		domain := path.Base(path.Dir(p))
		doc, err := parseDocument(p, body)
		if err != nil {
			if errors.Is(err, errMissingAsyncAPI) {
				return nil
			}
			return err
		}
		topics = append(topics, docTopics(domain, doc)...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortTopics(topics)
	return &Index{topics: topics}, nil
}

func ParseDocumentForTest(domain string, body []byte) (*Index, error) {
	doc, err := parseDocument(domain+".yaml", body)
	if err != nil {
		return nil, err
	}
	topics := docTopics(domain, doc)
	sortTopics(topics)
	return &Index{topics: topics}, nil
}

func (idx *Index) Describe(topicFilter string) []Topic {
	topicFilter = strings.TrimSpace(topicFilter)
	if topicFilter == "" {
		return nil
	}
	var out []Topic
	for _, topic := range idx.topics {
		if !matchesAddress(topic.Address, topicFilter) && !filtersOverlap(topic.TopicFilter, topicFilter) {
			continue
		}
		topic.MatchedParameters = inferParameters(topic.Address, topicFilter)
		topic.RelatedTopics = relatedTopics(topic.Address, topic.MatchedParameters)
		topic.Examples = examples(topic)
		out = append(out, topic)
	}
	sortTopics(out)
	return out
}

func (idx *Index) Search(opts SearchOptions) []Topic {
	domain := strings.ToLower(strings.TrimSpace(opts.Domain))
	query := strings.ToLower(strings.TrimSpace(opts.Query))
	role := strings.ToLower(strings.TrimSpace(opts.Role))
	objectType := strings.TrimSpace(opts.ObjectType)
	pointType := strings.TrimSpace(opts.PointType)
	action := strings.ToLower(strings.TrimSpace(opts.OperationAction))
	limit := opts.Limit

	var out []Topic
	for _, topic := range idx.topics {
		if domain != "" && strings.ToLower(topic.Domain) != domain {
			continue
		}
		if role != "" && !matchesRole(topic, role) {
			continue
		}
		if objectType != "" && (!matchesAddressValue(topic.Address, "objectType", objectType) || !parameterAllows(topic.Parameters, "objectType", objectType)) {
			continue
		}
		if pointType != "" && (!matchesAddressValue(topic.Address, "pointType", pointType) || !parameterAllows(topic.Parameters, "pointType", pointType)) {
			continue
		}
		if action != "" && !matchesOperationAction(topic.Operations, action) {
			continue
		}
		if query != "" && !topicContains(topic, query) {
			continue
		}

		values := map[string]string{}
		if objectType != "" {
			values["objectType"] = objectType
		}
		if pointType != "" {
			values["pointType"] = pointType
		}
		topic.TopicFilter = addressToFilter(topic.Address, values)
		topic.MatchedParameters = valuesOrNil(values)
		topic.RelatedTopics = relatedTopics(topic.Address, topic.MatchedParameters)
		topic.Examples = examples(topic)
		out = append(out, topic)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	sortTopics(out)
	return out
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

func addressToFilter(address string, values map[string]string) string {
	parts := strings.Split(address, "/")
	for i, part := range parts {
		name, ok := placeholderName(part)
		if !ok {
			continue
		}
		if v := strings.TrimSpace(values[name]); v != "" {
			parts[i] = v
			continue
		}
		if i == len(parts)-1 && strings.Contains(strings.ToLower(name), "path") {
			parts[i] = "#"
		} else {
			parts[i] = "+"
		}
	}
	return strings.Join(parts, "/")
}

func matchesAddress(address, topic string) bool {
	addressParts := strings.Split(strings.Trim(address, "/"), "/")
	topicParts := strings.Split(strings.Trim(topic, "/"), "/")
	for i, part := range addressParts {
		if i >= len(topicParts) {
			return false
		}
		if name, ok := placeholderName(part); ok {
			if i == len(addressParts)-1 && strings.Contains(strings.ToLower(name), "path") {
				return true
			}
			continue
		}
		if topicParts[i] == "+" || topicParts[i] == "#" {
			continue
		}
		if part != topicParts[i] {
			return false
		}
	}
	return len(addressParts) == len(topicParts) || strings.HasSuffix(topic, "/#")
}

func filtersOverlap(a, b string) bool {
	ap := strings.Split(strings.Trim(a, "/"), "/")
	bp := strings.Split(strings.Trim(b, "/"), "/")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if ap[i] == "#" || bp[i] == "#" {
			return true
		}
		if ap[i] == "+" || bp[i] == "+" {
			continue
		}
		if ap[i] != bp[i] {
			return false
		}
	}
	return len(ap) == len(bp)
}

func matchesRole(topic Topic, role string) bool {
	switch role {
	case "metadata":
		return strings.Contains(topic.Address, "/Metadata/")
	case "value":
		return strings.Contains(topic.Address, "/Value/")
	case "event":
		return !strings.Contains(topic.Address, "/Metadata/") && !strings.Contains(topic.Address, "/Value/")
	default:
		return strings.Contains(strings.ToLower(topic.RetainedLiveBehavior), role) ||
			strings.Contains(strings.ToLower(topic.Address), role) ||
			strings.Contains(strings.ToLower(topic.Channel), role)
	}
}

func matchesAddressValue(address, name, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	parts := strings.Split(strings.Trim(address, "/"), "/")
	for i, part := range parts {
		placeholder, ok := placeholderName(part)
		if ok && placeholder == name {
			return true
		}
		if !ok && strings.EqualFold(part, value) {
			switch name {
			case "objectType":
				return i > 0 && (parts[i-1] == "Value" || parts[i-1] == "Metadata")
			case "pointType":
				return i > 1 && (parts[i-2] == "Value" || parts[i-2] == "Metadata")
			default:
				return true
			}
		}
	}
	return false
}

func parameterAllows(params []ParameterSummary, name, value string) bool {
	for _, param := range params {
		if param.Name != name {
			continue
		}
		if len(param.Enum) == 0 {
			return true
		}
		for _, allowed := range param.Enum {
			if strings.EqualFold(allowed, value) {
				return true
			}
		}
		return false
	}
	return true
}

func matchesOperationAction(ops []OperationSummary, action string) bool {
	for _, op := range ops {
		if strings.EqualFold(op.Action, action) {
			return true
		}
	}
	return false
}

func topicContains(topic Topic, query string) bool {
	if strings.Contains(strings.ToLower(topic.Domain), query) ||
		strings.Contains(strings.ToLower(topic.SpecTitle), query) ||
		strings.Contains(strings.ToLower(topic.Channel), query) ||
		strings.Contains(strings.ToLower(topic.Address), query) ||
		strings.Contains(strings.ToLower(topic.Description), query) ||
		strings.Contains(strings.ToLower(topic.RetainedLiveBehavior), query) {
		return true
	}
	for _, msg := range topic.Messages {
		if strings.Contains(strings.ToLower(msg.Name), query) ||
			strings.Contains(strings.ToLower(msg.Title), query) ||
			strings.Contains(strings.ToLower(msg.Summary), query) ||
			strings.Contains(strings.ToLower(msg.Description), query) ||
			strings.Contains(strings.ToLower(msg.Payload.Ref), query) {
			return true
		}
	}
	for _, op := range topic.Operations {
		if strings.Contains(strings.ToLower(op.Name), query) ||
			strings.Contains(strings.ToLower(op.Action), query) ||
			strings.Contains(strings.ToLower(op.Summary), query) ||
			strings.Contains(strings.ToLower(op.Description), query) {
			return true
		}
	}
	return false
}

func valuesOrNil(values map[string]string) map[string]string {
	clean := map[string]string{}
	for k, v := range values {
		if strings.TrimSpace(v) != "" {
			clean[k] = v
		}
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

func inferParameters(address, topic string) map[string]string {
	addressParts := strings.Split(strings.Trim(address, "/"), "/")
	topicParts := strings.Split(strings.Trim(topic, "/"), "/")
	out := map[string]string{}
	for i, part := range addressParts {
		name, ok := placeholderName(part)
		if !ok || i >= len(topicParts) {
			continue
		}
		if i == len(addressParts)-1 && strings.Contains(strings.ToLower(name), "path") {
			out[name] = strings.Join(topicParts[i:], "/")
			continue
		}
		out[name] = topicParts[i]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func relatedTopics(address string, values map[string]string) []RelatedTopic {
	switch {
	case strings.Contains(address, "/Value/"):
		return []RelatedTopic{{
			Role:        "metadata",
			TopicFilter: addressToFilter(strings.Replace(address, "/Value/", "/Metadata/", 1), values),
		}}
	case strings.Contains(address, "/Metadata/"):
		return []RelatedTopic{{
			Role:        "value",
			TopicFilter: addressToFilter(strings.Replace(address, "/Metadata/", "/Value/", 1), values),
		}}
	default:
		return nil
	}
}

func retainedLiveBehavior(address string) string {
	switch {
	case strings.Contains(address, "/Metadata/"):
		return "metadata channel; expected to be useful with dsx_exchange_read_retained before sampling related live values"
	case strings.Contains(address, "/Value/"):
		return "live value channel; use dsx_exchange_subscribe and read related metadata first when available"
	default:
		return "schema-defined channel; use the channel description and broker ACLs to decide whether retained reads or live subscription are appropriate"
	}
}

func examples(topic Topic) []string {
	out := []string{topic.TopicFilter}
	if len(topic.MatchedParameters) > 0 {
		filter := addressToFilter(topic.Address, topic.MatchedParameters)
		if filter != topic.TopicFilter {
			out = append(out, filter)
		}
	}
	return out
}

func placeholderName(part string) (string, bool) {
	if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") && len(part) > 2 {
		return part[1 : len(part)-1], true
	}
	return "", false
}

func refName(ref string) string {
	idx := strings.LastIndex(ref, "/")
	if idx < 0 || idx == len(ref)-1 {
		return ref
	}
	return ref[idx+1:]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func sortTopics(topics []Topic) {
	sort.Slice(topics, func(i, j int) bool {
		if topics[i].Domain != topics[j].Domain {
			return topics[i].Domain < topics[j].Domain
		}
		return topics[i].Channel < topics[j].Channel
	})
}

func mapValue(v any) map[string]any {
	switch typed := v.(type) {
	case map[string]any:
		return typed
	case map[any]any:
		out := map[string]any{}
		for k, v := range typed {
			if s, ok := k.(string); ok {
				out[s] = v
			}
		}
		return out
	default:
		return nil
	}
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func stringSlice(v any) []string {
	switch typed := v.(type) {
	case []string:
		return append([]string{}, typed...)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func refList(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		ref := stringValue(mapValue(item)["$ref"])
		if ref != "" {
			out = append(out, ref)
		}
	}
	return out
}
