// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package schemaindex builds a lightweight, model-friendly index over embedded
// AsyncAPI channels. It intentionally extracts only the topic, payload, and
// relationship fields needed by DSX Exchange MCP schema tools; it is not a full
// AsyncAPI engine.
package schemaindex

import (
	"errors"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/schemas"
)

type Index struct {
	topics []Topic
}

type SearchOptions struct {
	Domain string
	Query  string
	// Role is a coarse topic role hint:
	// "metadata"/"value": for BMS channels following path convention
	// "event": fallback for channels not matching metadata/value
	Role string
	// ObjectType and PointType are BMS-oriented selectors.
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
		// ObjectType/PointType filtering intentionally follows the BMS topic
		// convention:
		//
		//   BMS/v1/{publisher}/{Value|Metadata}/{objectType}/{pointType}/{tagPath}
		//
		// This keeps BMS discovery ergonomic without pretending every AsyncAPI
		// schema has object/point semantics.
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
