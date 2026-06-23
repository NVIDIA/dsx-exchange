// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewHandlerToolsListJSONResponse(t *testing.T) {
	srv := Build(Config{})
	httpServer := httptest.NewServer(NewHandler(srv))
	defer httpServer.Close()

	resp := postJSONRPC(t, httpServer.URL, jsonRPCRequest(1, "tools/list", map[string]any{}))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tools/list status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("tools/list Content-Type = %q, want application/json", got)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read tools/list response: %v", err)
	}
	var out struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode tools/list response: %v\n%s", err, body)
	}
	names := map[string]bool{}
	for _, tool := range out.Result.Tools {
		names[tool.Name] = true
	}
	for _, name := range []string{toolDescribeTopic, toolFindTopics, toolReadRetained, toolSubscribe} {
		if !names[name] {
			t.Fatalf("tools/list missing %q; saw %#v", name, names)
		}
	}
}

func TestNewHandlerRejectsLongPollGET(t *testing.T) {
	srv := Build(Config{})
	httpServer := httptest.NewServer(NewHandler(srv))
	defer httpServer.Close()

	req, err := http.NewRequest(http.MethodGet, httpServer.URL, nil)
	if err != nil {
		t.Fatalf("build long-poll GET request: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("long-poll GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusBadRequest {
		t.Fatalf("long-poll GET status = %d, want non-success", resp.StatusCode)
	}
}

func TestNewMuxHealthEndpoints(t *testing.T) {
	httpServer := httptest.NewServer(NewMux(Config{}))
	defer httpServer.Close()

	for _, path := range []string{"/healthz/live", "/healthz/ready"} {
		resp, err := http.Get(httpServer.URL + path)
		if err != nil {
			t.Fatalf("GET %s failed: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("GET %s status = %d, want 204", path, resp.StatusCode)
		}
	}
}

func postJSONRPC(t *testing.T, url string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build JSON-RPC request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST JSON-RPC request failed: %v", err)
	}
	return resp
}

func jsonRPCRequest(id int, method string, params map[string]any) []byte {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		panic(err)
	}
	return body
}
