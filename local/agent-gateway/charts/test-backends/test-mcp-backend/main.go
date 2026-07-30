// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	listenAddr = ":3001"
	staticURI  = "dsx://fixture/static"
)

func main() {
	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           newHandler(),
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("test-mcp-backend listening on %s backend=%s", listenAddr, backendID())
		errCh <- srv.ListenAndServe()
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		log.Printf("shutting down: %v", ctx.Err())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown failed: %v", err)
	}
}

func newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.Handle("/", newMCPHandler())
	return mux
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func newMCPHandler() http.Handler {
	s := server.NewMCPServer(
		backendID(),
		"0.1.0",
		server.WithToolCapabilities(false),
		server.WithPromptCapabilities(false),
		server.WithResourceCapabilities(false, false),
		server.WithRecovery(),
		server.WithResourceRecovery(),
	)

	s.AddTools(
		server.ServerTool{
			Tool: mcp.NewTool("echo",
				mcp.WithDescription("Return the supplied message."),
				mcp.WithString("message"),
			),
			Handler: echoTool,
		},
		server.ServerTool{
			Tool: mcp.NewTool("add",
				mcp.WithDescription("Return the sum of two numbers."),
				mcp.WithNumber("a"),
				mcp.WithNumber("b"),
			),
			Handler: addTool,
		},
		server.ServerTool{
			Tool:    fixtureTool("printEnv", "Return stable environment identity used by session-pinning tests."),
			Handler: printEnvTool,
		},
		server.ServerTool{
			Tool:    fixtureTool("getTinyImage", "Return a deterministic tiny image fixture."),
			Handler: staticTextTool("tiny-image:1x1"),
		},
		server.ServerTool{
			Tool:    fixtureTool("headers", "Return the HTTP request headers received by this MCP backend."),
			Handler: headersTool,
		},
	)
	for _, name := range []string{
		"longRunningOperation", "annotatedMessage", "structuredContent",
		"getResourceLinks", "sampleLLM", "getResourceReference",
	} {
		handler := staticTextTool(fmt.Sprintf("backend=%s tool=%s", backendID(), name))
		if name == "longRunningOperation" {
			handler = longRunningOperationTool
		}
		s.AddTool(fixtureTool(name, "Deterministic restricted-tool fixture."), handler)
	}

	s.AddPrompt(
		mcp.NewPrompt("simple_prompt", mcp.WithPromptDescription("Static prompt fixture for gateway prompt dispatch.")),
		func(context.Context, mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return mcp.NewGetPromptResult(
				"Static prompt fixture for gateway prompt dispatch.",
				[]mcp.PromptMessage{
					mcp.NewPromptMessage(
						mcp.RoleUser,
						mcp.NewTextContent(fmt.Sprintf("backend=%s prompt=simple_prompt", backendID())),
					),
				},
			), nil
		},
	)

	s.AddResource(
		mcp.NewResource(
			staticURI,
			"fixture-static",
			mcp.WithResourceDescription("Static resource fixture for gateway resource-list multiplexing."),
			mcp.WithMIMEType("text/plain"),
		),
		readStaticResource,
	)
	s.AddResourceTemplate(
		mcp.NewResourceTemplate(
			"dsx://fixture/{name}",
			"fixture-by-name",
			mcp.WithTemplateDescription("Template fixture for gateway resource-template multiplexing."),
			mcp.WithTemplateMIMEType("text/plain"),
		),
		func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			return nil, fmt.Errorf("unknown resource: %s", req.Params.URI)
		},
	)

	return logMCPPosts(server.NewStreamableHTTPServer(s, server.WithStateLess(true)))
}

func logMCPPosts(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if marker := r.Header.Get("X-Dsx-Test-Invocation"); marker != "" {
				log.Printf("Received MCP POST request %q", marker)
			} else {
				log.Print("Received MCP POST request")
			}
		}
		next.ServeHTTP(w, r)
	})
}

func fixtureTool(name, description string) mcp.Tool {
	return mcp.NewTool(name, mcp.WithDescription(description))
}

func echoTool(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(fmt.Sprint(req.GetArguments()["message"])), nil
}

func addTool(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return mcp.NewToolResultText(fmt.Sprintf("%g", req.GetFloat("a", 0)+req.GetFloat("b", 0))), nil
}

func printEnvTool(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	payload, err := json.Marshal(map[string]string{
		"BACKEND_ID": backendID(),
		"HOSTNAME":   os.Getenv("HOSTNAME"),
	})
	if err != nil {
		return nil, fmt.Errorf("encode environment: %w", err)
	}
	return mcp.NewToolResultText(string(payload)), nil
}

func staticTextTool(text string) server.ToolHandlerFunc {
	return func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return mcp.NewToolResultText(text), nil
	}
}

func longRunningOperationTool(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req.GetBool("bridge_stream", false) {
		mcpServer := server.ServerFromContext(ctx)
		if mcpServer == nil {
			return nil, fmt.Errorf("missing MCP server in request context")
		}
		if err := mcpServer.SendNotificationToClient(ctx, "notifications/progress", map[string]any{
			"progressToken": "bridge-stream-fixture",
			"progress":      0.5,
			"message":       fmt.Sprintf("backend=%s tool=longRunningOperation progress", backendID()),
		}); err != nil {
			return nil, fmt.Errorf("send progress notification: %w", err)
		}
		idle := time.Duration(req.GetInt("idle_before_result_ms", 0)) * time.Millisecond
		if markerDelay := time.Duration(req.GetInt("idle_marker_ms", 0)) * time.Millisecond; markerDelay > 0 {
			if markerDelay >= idle {
				return nil, fmt.Errorf("idle_marker_ms must be less than idle_before_result_ms")
			}
			if err := waitForDelay(ctx, markerDelay); err != nil {
				return nil, err
			}
			log.Printf("Idle MCP request %q", req.Header.Get("X-Dsx-Test-Invocation"))
			idle -= markerDelay
		}
		if err := waitForDelay(ctx, idle); err != nil {
			return nil, err
		}
		if req.GetBool("block_until_cancel", false) {
			<-ctx.Done()
			if marker := req.Header.Get("X-Dsx-Test-Invocation"); marker != "" {
				log.Printf("Cancelled MCP request %q", marker)
			}
			return nil, ctx.Err()
		}
	}
	return mcp.NewToolResultText(fmt.Sprintf("backend=%s tool=longRunningOperation", backendID())), nil
}

func waitForDelay(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func headersTool(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	payload, err := json.Marshal(flattenHeaders(req.Header))
	if err != nil {
		return nil, fmt.Errorf("encode headers: %w", err)
	}
	return mcp.NewToolResultText(string(payload)), nil
}

func readStaticResource(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      req.Params.URI,
			MIMEType: "text/plain",
			Text:     fmt.Sprintf("backend=%s resource=fixture-static", backendID()),
		},
	}, nil
}

func backendID() string {
	if v := os.Getenv("BACKEND_ID"); v != "" {
		return v
	}
	return "test-mcp-backend"
}

func flattenHeaders(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		if len(v) == 0 {
			out[k] = ""
			continue
		}
		out[k] = v[0]
	}
	out["x-dsx-backend-id"] = backendID()
	return out
}
