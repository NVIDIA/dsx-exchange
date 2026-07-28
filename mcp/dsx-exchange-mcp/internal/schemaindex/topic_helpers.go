// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package schemaindex

import (
	"sort"
	"strings"
)

// relatedTopics identifies the corresponding BMS Metadata or Value channel for a given topic.
// This is only for BMS schemas that use the "/Value/" and "/Metadata/" convention to link live value
// and metadata channels. For all other schemas, no relationship is assumed unless they adopt this pattern.
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

// retainedLiveBehavior describes the BMS Metadata/Value convention when the
// address contains those path segments. For other schemas it falls back to a
// generic schema-channel note because retention/live semantics are domain
// specific and are not inferred from full AsyncAPI semantics here.
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
