// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey struct{}

// Caller is the request identity material the gateway passes through. The raw
// bearer is used only as the MQTT password; the x-mcp-* fields are audit labels
// emitted by the gateway's ext_authz path when present.
type Caller struct {
	Bearer    string
	SessionID string
	Tenant    string
	Issuer    string
	Subject   string
	SpiffeID  string
}

// Middleware extracts the caller bearer and gateway-projected identity headers
// from the HTTP request and stores them on the request context.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		caller := Caller{
			Bearer:    bearerFromHeader(r.Header.Get("Authorization")),
			SessionID: r.Header.Get("Mcp-Session-Id"),
			Tenant:    r.Header.Get("x-mcp-tenant"),
			Issuer:    r.Header.Get("x-mcp-issuer"),
			Subject:   r.Header.Get("x-mcp-sub"),
			SpiffeID:  r.Header.Get("x-mcp-spiffe-id"),
		}
		r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, caller))
		next.ServeHTTP(w, r)
	})
}

func bearerFromHeader(h string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(strings.ToLower(h), strings.ToLower(prefix)) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

// FromContext returns all caller identity material stored on ctx.
func FromContext(ctx context.Context) Caller {
	v, _ := ctx.Value(ctxKey{}).(Caller)
	return v
}

// Bearer returns the caller's bearer token from ctx, or "" if absent.
func Bearer(ctx context.Context) string {
	return FromContext(ctx).Bearer
}
