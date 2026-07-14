// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package schemaindex

import "strings"

// addressToFilter turns an AsyncAPI channel address into an MQTT topic filter.
// Example: "BMS/v1/{publisher}/Value/{objectType}/{pointType}/{tagPath}" -> "BMS/v1/+/Value/+/+/#"
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

// matchesAddress checks whether a concrete topic or topic filter fits an
// AsyncAPI address. It is schema-oriented, not a full MQTT broker matcher.
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

// filtersOverlap handles broad MQTT filters such as BMS/v1/PUB/Value/# that do
// not directly fit one channel address but still overlap generated schema
// filters.
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
		// BMS exposes metadata as a first-class path segment. Other schemas only
		// match this role if they choose the same convention.
		return strings.Contains(topic.Address, "/Metadata/")
	case "value":
		// BMS exposes live values as a first-class path segment. Other schemas only
		// match this role if they choose the same convention.
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
				// BMS object types are the path level immediately after
				// /Value/ or /Metadata/.
				return i > 0 && (parts[i-1] == "Value" || parts[i-1] == "Metadata")
			case "pointType":
				// BMS point types are the path level immediately after the
				// object type.
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

// inferParameters extracts placeholder values from a topic that matched an
// AsyncAPI address, so callers see why a schema channel matched their filter.
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
