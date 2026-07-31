// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetOIDCTokenUsesLocalIDPEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse token form: %v", err)
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if got := r.Form.Get("grant_type"); got != "client_credentials" {
			t.Errorf("grant_type = %q, want client_credentials", got)
		}
		if got := r.Form.Get("client_id"); got != "mqtt-client" {
			t.Errorf("client_id = %q, want mqtt-client", got)
		}
		if got := r.Form.Get("client_secret"); got != "mqtt-client-secret" {
			t.Errorf("client_secret = %q, want mqtt-client-secret", got)
		}
		if got := r.Form.Get("scope"); got != "mqtt" {
			t.Errorf("scope = %q, want mqtt", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}); err != nil {
			t.Errorf("encode token response: %v", err)
		}
	}))
	defer server.Close()

	token, err := GetOIDCTokenContext(t.Context(), server.URL, "mqtt-client", "mqtt-client-secret")
	if err != nil {
		t.Fatalf("get token: %v", err)
	}
	if token != "token" {
		t.Fatalf("token = %q, want token", token)
	}

	_, err = GetOIDCTokenContext(t.Context(), server.URL+"/", "mqtt-client", "mqtt-client-secret")
	if err == nil {
		t.Fatal("expected trailing-slash IDP URL to fail")
	}
	if !strings.Contains(err.Error(), "must not end with /") {
		t.Fatalf("unexpected trailing-slash error: %v", err)
	}
}
