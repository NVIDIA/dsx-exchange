// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddlewareStoresCaller(t *testing.T) {
	var got Caller
	next := Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = FromContext(r.Context())
	}))

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer token-123")
	req.Header.Set("x-mcp-tenant", "tenant-a")
	req.Header.Set("x-mcp-issuer", "https://issuer")
	req.Header.Set("x-mcp-sub", "tenant-a/agent")
	req.Header.Set("x-mcp-spiffe-id", "spiffe://tenant-a/agent/tenant-a%2Fagent")

	next.ServeHTTP(httptest.NewRecorder(), req)

	if got.Bearer != "token-123" {
		t.Fatalf("Bearer = %q, want token-123", got.Bearer)
	}
	if got.Tenant != "tenant-a" || got.Issuer != "https://issuer" || got.Subject != "tenant-a/agent" {
		t.Fatalf("caller identity not propagated: %+v", got)
	}
	if got.SpiffeID == "" {
		t.Fatalf("SpiffeID was not propagated")
	}
}

func TestBearerSchemeIsCaseInsensitive(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "bearer token-123")

	var got string
	Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = Bearer(r.Context())
	})).ServeHTTP(httptest.NewRecorder(), req)

	if got != "token-123" {
		t.Fatalf("Bearer = %q, want token-123", got)
	}
}
