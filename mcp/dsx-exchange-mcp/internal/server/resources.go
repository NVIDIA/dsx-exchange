// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/specs"
)

func registerResources(s *mcp.Server) {
	available := specs.List()

	s.AddResource(
		&mcp.Resource{
			URI:         "dsx-exchange://specs/",
			Name:        "DSX Exchange spec index",
			Description: "Index of available AsyncAPI specs covering MQTT topics on the DSX Exchange event bus. Read individual specs at dsx-exchange://specs/<domain>.",
			MIMEType:    "application/json",
		},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			body, _ := json.Marshal(map[string]any{"domains": available})
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      req.Params.URI,
					MIMEType: "application/json",
					Text:     string(body),
				}},
			}, nil
		},
	)

	for _, d := range available {
		domain := d
		uri := "dsx-exchange://specs/" + domain
		s.AddResource(
			&mcp.Resource{
				URI:         uri,
				Name:        domain + " AsyncAPI spec",
				Description: fmt.Sprintf("AsyncAPI 3.x definition for the %s domain on DSX Exchange (MQTT topics, payloads, message metadata).", domain),
				MIMEType:    "application/yaml",
			},
			func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				body, err := specs.Read(domain)
				if err != nil {
					return nil, err
				}
				return &mcp.ReadResourceResult{
					Contents: []*mcp.ResourceContents{{
						URI:      req.Params.URI,
						MIMEType: mimeFor(req.Params.URI),
						Text:     string(body),
					}},
				}, nil
			},
		)
	}
}

func mimeFor(uri string) string {
	if strings.HasSuffix(uri, ".json") {
		return "application/json"
	}
	return "application/yaml"
}
