// Copyright 2026 NVIDIA Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/config"
	bridgemcp "github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/mcp"
)

const ShardIDArgument = "shard_id"

func callArgumentsMap(raw any) (map[string]any, error) {
	if raw == nil {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := bridgemcp.DecodeParams(raw, &args); err != nil {
		return nil, fmt.Errorf("invalid arguments: %w", err)
	}
	if args == nil {
		return map[string]any{}, nil
	}
	return args, nil
}

func consumeShardID(args map[string]any) (string, error) {
	raw, ok := args[ShardIDArgument]
	if !ok {
		return "", errors.New("missing shard_id")
	}
	shardID, ok := raw.(string)
	if !ok {
		return "", errors.New("shard_id must be a string")
	}
	if !config.ValidShardID(shardID) {
		return "", fmt.Errorf("invalid shard_id %q", shardID)
	}
	delete(args, ShardIDArgument)
	return shardID, nil
}

func consumeShardIDFromStrings(args map[string]string) (string, error) {
	shardID, ok := args[ShardIDArgument]
	if !ok {
		return "", errors.New("missing shard_id")
	}
	if !config.ValidShardID(shardID) {
		return "", fmt.Errorf("invalid shard_id %q", shardID)
	}
	delete(args, ShardIDArgument)
	return shardID, nil
}

func parsePromptGetParams(raw any) (mcp.GetPromptParams, error) {
	var params mcp.GetPromptParams
	if raw == nil {
		return params, errors.New("missing params")
	}
	if err := bridgemcp.DecodeParams(raw, &params); err != nil {
		return params, fmt.Errorf("invalid params: %w", err)
	}
	if params.Name == "" {
		return params, errors.New("missing prompt name")
	}
	if params.Arguments == nil {
		params.Arguments = map[string]string{}
	}
	return params, nil
}

func parseCompletionParams(raw any) (mcp.CompleteParams, error) {
	var params mcp.CompleteParams
	if raw == nil {
		return params, errors.New("missing params")
	}
	if err := bridgemcp.DecodeParams(raw, &params); err != nil {
		return params, fmt.Errorf("invalid params: %w", err)
	}
	if params.Context.Arguments == nil {
		params.Context.Arguments = map[string]string{}
	}
	return params, nil
}

func nilIfEmpty[V any](values map[string]V) map[string]V {
	if len(values) == 0 {
		return nil
	}
	return values
}

func toolsWithShardID(tools []mcp.Tool) []mcp.Tool {
	out := make([]mcp.Tool, 0, len(tools))
	for _, tool := range tools {
		// shard_id is reserved for hub routing. If a leaf already uses that name,
		// skip the tool rather than expose an ambiguous invocation contract.
		if tool.Name == BridgeListShardsToolName || toolUsesReservedShardID(tool) {
			continue
		}
		augmented, err := toolWithShardID(tool)
		if err != nil {
			continue
		}
		out = append(out, augmented)
	}
	return out
}

func toolWithShardID(tool mcp.Tool) (mcp.Tool, error) {
	if len(tool.RawInputSchema) != 0 {
		var schema map[string]any
		if err := json.Unmarshal(tool.RawInputSchema, &schema); err != nil {
			return tool, err
		}
		if schema["type"] == nil {
			schema["type"] = "object"
		}
		properties, _ := schema["properties"].(map[string]any)
		if properties == nil {
			properties = map[string]any{}
		}
		properties[ShardIDArgument] = shardIDSchemaProperty()
		schema["properties"] = properties
		schema["required"] = appendRequired(schema["required"], ShardIDArgument)
		raw, err := json.Marshal(schema)
		if err != nil {
			return tool, err
		}
		tool.RawInputSchema = raw
		return tool, nil
	}
	if tool.InputSchema.Type == "" {
		tool.InputSchema.Type = "object"
	}
	if tool.InputSchema.Properties == nil {
		tool.InputSchema.Properties = map[string]any{}
	}
	tool.InputSchema.Properties[ShardIDArgument] = shardIDSchemaProperty()
	if !slices.Contains(tool.InputSchema.Required, ShardIDArgument) {
		tool.InputSchema.Required = append(tool.InputSchema.Required, ShardIDArgument)
	}
	return tool, nil
}

func appendRequired(raw any, required string) []any {
	values, _ := raw.([]any)
	for _, value := range values {
		if value == required {
			return values
		}
	}
	return append(values, required)
}

func toolUsesReservedShardID(tool mcp.Tool) bool {
	if tool.InputSchema.Properties != nil {
		if _, ok := tool.InputSchema.Properties[ShardIDArgument]; ok {
			return true
		}
	}
	if len(tool.RawInputSchema) == 0 {
		return false
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(tool.RawInputSchema, &schema); err != nil {
		return false
	}
	_, ok := schema.Properties[ShardIDArgument]
	return ok
}

func promptsWithShardID(prompts []mcp.Prompt) []mcp.Prompt {
	out := make([]mcp.Prompt, 0, len(prompts))
	for _, prompt := range prompts {
		// Prompt arguments are the only place prompt/get can carry routing data.
		if promptUsesReservedShardID(prompt) {
			continue
		}
		out = append(out, promptWithShardID(prompt))
	}
	return out
}

func promptWithShardID(prompt mcp.Prompt) mcp.Prompt {
	prompt.Arguments = append(append([]mcp.PromptArgument{}, prompt.Arguments...), mcp.PromptArgument{
		Name:        ShardIDArgument,
		Description: "DSX shard id from dsx_bridge_list_shards.",
		Required:    true,
	})
	return prompt
}

func promptUsesReservedShardID(prompt mcp.Prompt) bool {
	for _, arg := range prompt.Arguments {
		if arg.Name == ShardIDArgument {
			return true
		}
	}
	return false
}

func shardIDSchemaProperty() map[string]any {
	return map[string]any{
		"type":        "string",
		"description": "DSX shard id from dsx_bridge_list_shards.",
	}
}
