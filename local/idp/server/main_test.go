// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func TestTokenGrants(t *testing.T) {
	signingKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	s := newServer(config{
		Issuer:     "https://idp.example.com",
		KeyID:      "test-key",
		TTLSeconds: 3600,
		Clients: []client{
			{
				ID:        "service",
				Secret:    "service-secret",
				GrantType: "client_credentials",
				Audiences: []string{"gateway", "other"},
				Claims:    map[string]any{"sub": "service", "azp": "service"},
			},
			{
				ID:       "human",
				Secret:   "human-secret",
				Audience: "gateway",
			},
		},
		Users: []user{{
			Username: "operator",
			Password: "operator-password",
			Claims:   map[string]any{"sub": "operator", "email": "operator@example.com"},
		}},
	}, signingKey, func() time.Time { return now })

	tests := []struct {
		name        string
		form        url.Values
		audience    []string
		subject     string
		customClaim string
		wantIDToken bool
	}{
		{
			name: "client credentials",
			form: url.Values{
				"grant_type":    {"client_credentials"},
				"client_id":     {"service"},
				"client_secret": {"service-secret"},
				"scope":         {"mqtt"},
			},
			audience:    []string{"gateway", "other"},
			subject:     "service",
			customClaim: "azp",
		},
		{
			name: "password",
			form: url.Values{
				"grant_type":    {"password"},
				"client_id":     {"human"},
				"client_secret": {"human-secret"},
				"username":      {"operator"},
				"password":      {"operator-password"},
				"scope":         {"mqtt"},
			},
			audience:    []string{"gateway"},
			subject:     "operator",
			customClaim: "email",
			wantIDToken: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(tt.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			s.routes().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
			}
			var response struct {
				AccessToken string `json:"access_token"`
				IDToken     string `json:"id_token"`
				ExpiresIn   int64  `json:"expires_in"`
				Scope       string `json:"scope"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
				t.Fatalf("decode token response: %v", err)
			}
			if response.ExpiresIn != 3600 || response.Scope != "mqtt" {
				t.Fatalf("token metadata = expires_in:%d scope:%q", response.ExpiresIn, response.Scope)
			}
			if (response.IDToken != "") != tt.wantIDToken {
				t.Fatalf("id_token presence = %t, want %t", response.IDToken != "", tt.wantIDToken)
			}

			token, err := jwt.ParseSigned(response.AccessToken, []jose.SignatureAlgorithm{jose.RS256})
			if err != nil {
				t.Fatalf("parse access token: %v", err)
			}
			if len(token.Headers) != 1 {
				t.Fatalf("token headers = %d, want 1", len(token.Headers))
			}
			jwksResponse := httptest.NewRecorder()
			s.routes().ServeHTTP(jwksResponse, httptest.NewRequest(http.MethodGet, jwksPath, nil))
			if jwksResponse.Code != http.StatusOK {
				t.Fatalf("JWKS status = %d, want %d", jwksResponse.Code, http.StatusOK)
			}
			var jwks jose.JSONWebKeySet
			if err := json.NewDecoder(jwksResponse.Body).Decode(&jwks); err != nil {
				t.Fatalf("decode JWKS: %v", err)
			}
			keys := jwks.Key(token.Headers[0].KeyID)
			if len(keys) != 1 {
				t.Fatalf("JWKS keys for kid %q = %d, want 1", token.Headers[0].KeyID, len(keys))
			}
			var registered jwt.Claims
			var private map[string]any
			if err := token.Claims(keys[0].Key, &registered, &private); err != nil {
				t.Fatalf("verify access token: %v", err)
			}
			if registered.Issuer != "https://idp.example.com" || registered.Subject != tt.subject {
				t.Fatalf("claims = issuer:%q subject:%q", registered.Issuer, registered.Subject)
			}
			if !slices.Equal([]string(registered.Audience), tt.audience) {
				t.Fatalf("audience = %v, want %v", registered.Audience, tt.audience)
			}
			if !registered.Expiry.Time().Equal(now.Add(time.Hour)) {
				t.Fatalf("expiry = %s, want %s", registered.Expiry.Time(), now.Add(time.Hour))
			}
			if private[tt.customClaim] == nil || private["scope"] != "mqtt" {
				t.Fatalf("private claims = %v", private)
			}
		})
	}
}

func TestTokenRejectsInvalidCredentials(t *testing.T) {
	signingKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	s := newServer(config{
		Clients: []client{
			{ID: "service", Secret: "service-secret", GrantType: "client_credentials"},
			{ID: "human", Secret: "human-secret"},
		},
		Users: []user{{Username: "operator", Password: "operator-password"}},
	}, signingKey, time.Now)

	tests := []struct {
		name       string
		form       url.Values
		wantStatus int
		wantError  string
	}{
		{
			name: "client secret",
			form: url.Values{
				"grant_type":    {"client_credentials"},
				"client_id":     {"service"},
				"client_secret": {"wrong"},
			},
			wantStatus: http.StatusUnauthorized,
			wantError:  "invalid_client",
		},
		{
			name: "password",
			form: url.Values{
				"grant_type":    {"password"},
				"client_id":     {"human"},
				"client_secret": {"human-secret"},
				"username":      {"operator"},
				"password":      {"wrong"},
			},
			wantStatus: http.StatusUnauthorized,
			wantError:  "invalid_grant",
		},
		{
			name: "grant type",
			form: url.Values{
				"grant_type":    {"password"},
				"client_id":     {"service"},
				"client_secret": {"service-secret"},
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "unauthorized_client",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(tt.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			s.routes().ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			var response struct {
				Error string `json:"error"`
			}
			if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if response.Error != tt.wantError {
				t.Fatalf("error = %q, want %q", response.Error, tt.wantError)
			}
		})
	}
}
