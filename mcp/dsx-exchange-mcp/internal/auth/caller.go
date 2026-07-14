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

// Middleware extracts the caller bearer and identity headers
// from the HTTP request and stores them on the request context.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(WithCaller(r.Context(), CallerFromHeaders(r.Header)))
		next.ServeHTTP(w, r)
	})
}

// CallerFromHeaders extracts caller identity material from gateway-projected
// HTTP headers.
func CallerFromHeaders(h http.Header) Caller {
	return Caller{
		Bearer:    bearerFromHeader(h.Get("Authorization")),
		SessionID: h.Get("Mcp-Session-Id"),
		Tenant:    h.Get("x-mcp-tenant"),
		Issuer:    h.Get("x-mcp-issuer"),
		Subject:   h.Get("x-mcp-sub"),
		SpiffeID:  h.Get("x-mcp-spiffe-id"),
	}
}

// WithCaller stores caller identity material on ctx.
func WithCaller(ctx context.Context, caller Caller) context.Context {
	return context.WithValue(ctx, ctxKey{}, caller)
}

// WithSessionID returns a context whose caller includes sessionID. Other caller
// fields already present on ctx are preserved.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	caller := FromContext(ctx)
	caller.SessionID = sessionID
	return WithCaller(ctx, caller)
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
