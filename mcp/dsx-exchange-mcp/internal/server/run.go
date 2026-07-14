// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/internal/auth"
)

// NewHandler wraps the MCP server in the streamable-HTTP transport used by the
// standalone binary and tests. Stateless mode keeps requests independent, and
// JSONResponse forces request/response clients to receive one JSON body instead
// of a server-sent-events stream.
func NewHandler(srv *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{
			Stateless:    true,
			JSONResponse: true,
		},
	)
}

// NewMux wires the public HTTP surface for the MCP backend.
func NewMux(cfg Config) http.Handler {
	srv := Build(cfg)
	mux := http.NewServeMux()
	mux.Handle("/mcp", auth.Middleware(NewHandler(srv)))
	mux.HandleFunc("/healthz/live", healthOK)
	mux.HandleFunc("/healthz/ready", healthOK)
	return mux
}

// Run serves the configured MCP backend until http.Server exits.
func Run(addr string, cfg Config) error {
	return http.ListenAndServe(addr, NewMux(cfg))
}

func healthOK(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}
