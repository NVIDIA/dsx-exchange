// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"golang.org/x/oauth2"
)

func TestNATSOptionsSetsClientNameAndReconnectPolicy(t *testing.T) {
	setNoAuth(t)
	opts, err := natsOptionsFromEnv(t, "test-client")
	if err != nil {
		t.Fatalf("NATSOptions: %v", err)
	}
	cfg, err := applyNATSOptions(opts)
	if err != nil {
		t.Fatalf("apply NATS options: %v", err)
	}
	if cfg.Name != "test-client" {
		t.Fatalf("Name=%q, want test-client", cfg.Name)
	}
	if cfg.MaxReconnect != -1 {
		t.Fatalf("MaxReconnect=%d, want -1", cfg.MaxReconnect)
	}
	if !cfg.RetryOnFailedConnect {
		t.Fatalf("RetryOnFailedConnect=false, want true")
	}
	if cfg.ReconnectWait != time.Second {
		t.Fatalf("ReconnectWait=%s, want 1s", cfg.ReconnectWait)
	}
}

func TestNATSOptionsRequiresExplicitAuthMode(t *testing.T) {
	clearNATSEnv(t)

	if _, err := natsOptionsFromEnv(t, "test-client"); err == nil {
		t.Fatalf("NATSOptions accepted missing auth mode")
	}
}

func TestNATSOptionsAcceptsExplicitNoAuth(t *testing.T) {
	setNoAuth(t)

	if _, err := natsOptionsFromEnv(t, "test-client"); err != nil {
		t.Fatalf("NATSOptions rejected noauth: %v", err)
	}
}

func TestNATSOptionsUsesNATSOAuthToken(t *testing.T) {
	clearNATSEnv(t)
	server, requests := newOAuthIssuerServer(t, 3600)
	setOAuthEnv(t, server.URL+"/issuer", "bridge-client", "bridge-secret", "nats:bridge")

	opts, err := natsOptionsFromEnv(t, "test-client")
	if err != nil {
		t.Fatalf("NATSOptions: %v", err)
	}
	cfg, err := applyNATSOptions(opts)
	if err != nil {
		t.Fatalf("apply NATS options: %v", err)
	}
	if cfg.User != "" || cfg.Password != "" {
		t.Fatalf("static NATS credentials = %q/%q, want none", cfg.User, cfg.Password)
	}
	if cfg.UserInfo != nil {
		t.Fatalf("UserInfo handler is set")
	}
	if cfg.Token != "" {
		t.Fatalf("static NATS token=%q, want none", cfg.Token)
	}
	if cfg.TokenHandler == nil {
		t.Fatalf("TokenHandler is nil")
	}
	if token := cfg.TokenHandler(); token != "access-token-1" {
		t.Fatalf("TokenHandler token=%q, want access-token-1", token)
	}
	if len(*requests) != 1 {
		t.Fatalf("token requests=%d, want 1", len(*requests))
	}
	if got := (*requests)[0]; got.username != "bridge-client" || got.password != "bridge-secret" || got.scope != "nats:bridge" {
		t.Fatalf("token request=%#v", got)
	}
}

func TestNATSOptionsRejectsUnknownAuthMode(t *testing.T) {
	clearNATSEnv(t)
	t.Setenv(EnvNATSAuthMode, "token")

	if _, err := natsOptionsFromEnv(t, "test-client"); err == nil {
		t.Fatalf("NATSOptions accepted an unsupported auth mode")
	}
}

func TestNATSOAuthTokenHandlerUsesOAuth2Cache(t *testing.T) {
	clearNATSEnv(t)
	server, requests := newOAuthIssuerServer(t, 3600)
	setOAuthEnv(t, server.URL+"/issuer", "bridge-client", "bridge-secret", "nats:bridge")

	cfg, err := loadNATSConfig()
	if err != nil {
		t.Fatalf("loadNATSConfig: %v", err)
	}
	source, err := natsOAuthTokenSource(cfg.OAuth)
	if err != nil {
		t.Fatalf("natsOAuthTokenSource: %v", err)
	}
	handler := natsOAuthTokenHandler(source)

	if got := handler(); got != "access-token-1" {
		t.Fatalf("first token=%q, want access-token-1", got)
	}
	if got := handler(); got != "access-token-1" {
		t.Fatalf("cached token=%q, want access-token-1", got)
	}
	if len(*requests) != 1 {
		t.Fatalf("token requests=%d, want 1", len(*requests))
	}
}

func TestNATSOAuthTokenSourceReloadsOAuthClientCredentialFilesOnRefresh(t *testing.T) {
	clearNATSEnv(t)
	server, requests := newOAuthIssuerServer(t, 1)
	clientIDFile := writeTextFile(t, "client-id", "bridge-client-a")
	clientSecretFile := writeTextFile(t, "client-secret", "bridge-secret-a")
	t.Setenv(EnvNATSAuthMode, string(NATSAuthModeOAuth))
	t.Setenv(EnvNATSOAuthIssuer, server.URL+"/issuer")
	t.Setenv(EnvNATSOAuthClientIDFile, clientIDFile)
	t.Setenv(EnvNATSOAuthClientSecretFile, clientSecretFile)
	t.Setenv(EnvNATSOAuthScope, "nats:bridge")

	cfg, err := loadNATSConfig()
	if err != nil {
		t.Fatalf("loadNATSConfig: %v", err)
	}
	source, err := natsOAuthTokenSource(cfg.OAuth)
	if err != nil {
		t.Fatalf("natsOAuthTokenSource: %v", err)
	}

	first, err := source.Token()
	if err != nil {
		t.Fatalf("first Token: %v", err)
	}
	if first.AccessToken != "access-token-1" {
		t.Fatalf("first token=%q, want access-token-1", first.AccessToken)
	}

	writeExistingTextFile(t, clientIDFile, "bridge-client-b")
	writeExistingTextFile(t, clientSecretFile, "bridge-secret-b")
	refreshed, err := source.Token()
	if err != nil {
		t.Fatalf("refreshed Token: %v", err)
	}
	if refreshed.AccessToken != "access-token-2" {
		t.Fatalf("refreshed token=%q, want access-token-2", refreshed.AccessToken)
	}

	if len(*requests) != 2 {
		t.Fatalf("token requests=%d, want 2", len(*requests))
	}
	if got := (*requests)[0]; got.issuer != server.URL+"/issuer" || got.username != "bridge-client-a" || got.password != "bridge-secret-a" || got.scope != "nats:bridge" {
		t.Fatalf("first token request=%#v", got)
	}
	if got := (*requests)[1]; got.issuer != server.URL+"/issuer" || got.username != "bridge-client-b" || got.password != "bridge-secret-b" || got.scope != "nats:bridge" {
		t.Fatalf("second token request=%#v", got)
	}
}

func TestNATSOAuthTokenHandlerReturnsRejectedTokenOnFetchFailure(t *testing.T) {
	handler := natsOAuthTokenHandler(errorTokenSource{})

	if got := handler(); got != natsOAuthRejectedToken {
		t.Fatalf("token=%q, want %q", got, natsOAuthRejectedToken)
	}
}

func TestNATSOptionsRejectsMissingOAuthClientConfig(t *testing.T) {
	tests := []struct {
		name string
		set  func(t *testing.T)
	}{
		{
			name: "issuer",
			set: func(t *testing.T) {
				t.Setenv(EnvNATSOAuthClientID, "bridge-client")
				t.Setenv(EnvNATSOAuthClientSecret, "bridge-secret")
				t.Setenv(EnvNATSOAuthScope, "nats:bridge")
			},
		},
		{
			name: "client ID",
			set: func(t *testing.T) {
				t.Setenv(EnvNATSOAuthIssuer, "https://issuer.example.test")
				t.Setenv(EnvNATSOAuthClientSecret, "bridge-secret")
				t.Setenv(EnvNATSOAuthScope, "nats:bridge")
			},
		},
		{
			name: "client secret",
			set: func(t *testing.T) {
				t.Setenv(EnvNATSOAuthIssuer, "https://issuer.example.test")
				t.Setenv(EnvNATSOAuthClientID, "bridge-client")
				t.Setenv(EnvNATSOAuthScope, "nats:bridge")
			},
		},
		{
			name: "scope",
			set: func(t *testing.T) {
				t.Setenv(EnvNATSOAuthIssuer, "https://issuer.example.test")
				t.Setenv(EnvNATSOAuthClientID, "bridge-client")
				t.Setenv(EnvNATSOAuthClientSecret, "bridge-secret")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearNATSEnv(t)
			t.Setenv(EnvNATSAuthMode, string(NATSAuthModeOAuth))
			tt.set(t)

			if _, err := natsOptionsFromEnv(t, "test-client"); err == nil {
				t.Fatalf("NATSOptions accepted missing OAuth %s", tt.name)
			}
		})
	}
}

func TestNATSConfigRejectsUnreadableOAuthClientFile(t *testing.T) {
	clearNATSEnv(t)
	t.Setenv(EnvNATSAuthMode, string(NATSAuthModeOAuth))
	t.Setenv(EnvNATSOAuthIssuer, "https://issuer.example.test")
	t.Setenv(EnvNATSOAuthClientIDFile, t.TempDir())
	t.Setenv(EnvNATSOAuthClientSecret, "bridge-secret")
	t.Setenv(EnvNATSOAuthScope, "nats:bridge")

	if _, err := natsOptionsFromEnv(t, "test-client"); err == nil {
		t.Fatalf("NATSOptions accepted an unreadable OAuth client ID path")
	}
}

func TestNATSConfigEnablesTLSWhenConfigured(t *testing.T) {
	setNoAuth(t)
	t.Setenv(EnvNATSTLSEnabled, "true")
	t.Setenv(EnvNATSTLSServerName, "nats.example.test")

	opts, err := natsOptionsFromEnv(t, "test-client")
	if err != nil {
		t.Fatalf("NATSOptions: %v", err)
	}
	cfg, err := applyNATSOptions(opts)
	if err != nil {
		t.Fatalf("apply NATS options: %v", err)
	}
	if !cfg.Secure {
		t.Fatalf("Secure=false, want true")
	}
	if cfg.TLSConfig == nil {
		t.Fatalf("TLSConfig is nil")
	}
	if cfg.TLSConfig.ServerName != "nats.example.test" {
		t.Fatalf("TLSConfig.ServerName=%q, want nats.example.test", cfg.TLSConfig.ServerName)
	}
}

func TestNATSConfigRejectsInvalidTLSFlag(t *testing.T) {
	setNoAuth(t)
	t.Setenv(EnvNATSTLSEnabled, "yes")

	if _, err := natsOptionsFromEnv(t, "test-client"); err == nil {
		t.Fatalf("NATSOptions accepted an invalid TLS flag")
	}
}

func TestNATSConfigAcceptsCAFile(t *testing.T) {
	setNoAuth(t)
	caFile := writeTestCAFile(t)
	t.Setenv(EnvNATSTLSCAFile, caFile)

	opts, err := natsOptionsFromEnv(t, "test-client")
	if err != nil {
		t.Fatalf("NATSOptions: %v", err)
	}
	cfg, err := applyNATSOptions(opts)
	if err != nil {
		t.Fatalf("apply NATS options: %v", err)
	}
	if !cfg.Secure {
		t.Fatalf("Secure=false, want true")
	}
	if cfg.RootCAsCB == nil {
		t.Fatalf("RootCAsCB is nil")
	}
	pool, err := cfg.RootCAsCB()
	if err != nil {
		t.Fatalf("RootCAsCB: %v", err)
	}
	if pool == nil {
		t.Fatalf("RootCAsCB returned nil pool")
	}
}

func TestNATSConfigRejectsMissingCAFile(t *testing.T) {
	setNoAuth(t)
	t.Setenv(EnvNATSTLSCAFile, filepath.Join(t.TempDir(), "missing-ca.pem"))

	if _, err := natsOptionsFromEnv(t, "test-client"); err == nil {
		t.Fatalf("NATSOptions accepted a missing CA file")
	}
}

func setNoAuth(t *testing.T) {
	t.Helper()
	clearNATSEnv(t)
	t.Setenv(EnvNATSAuthMode, string(NATSAuthModeNoAuth))
}

func setOAuthEnv(t *testing.T, issuer, clientID, clientSecret, scope string) {
	t.Helper()
	t.Setenv(EnvNATSAuthMode, string(NATSAuthModeOAuth))
	t.Setenv(EnvNATSOAuthIssuer, issuer)
	t.Setenv(EnvNATSOAuthClientID, clientID)
	t.Setenv(EnvNATSOAuthClientSecret, clientSecret)
	t.Setenv(EnvNATSOAuthScope, scope)
}

func natsOptionsFromEnv(t *testing.T, name string) ([]nats.Option, error) {
	t.Helper()
	cfg, err := loadNATSConfig()
	if err != nil {
		return nil, err
	}
	return NATSOptions(name, cfg)
}

type oauthRequest struct {
	issuer   string
	username string
	password string
	scope    string
}

type errorTokenSource struct{}

func (errorTokenSource) Token() (*oauth2.Token, error) {
	return nil, errors.New("token endpoint unavailable")
}

func newOAuthIssuerServer(t *testing.T, expiresIn int) (*httptest.Server, *[]oauthRequest) {
	t.Helper()
	requests := []oauthRequest{}
	lastIssuer := ""
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"keys":[]}`)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("token endpoint method=%s, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse token request form: %v", err)
		}
		username, password, _ := r.BasicAuth()
		requests = append(requests, oauthRequest{
			issuer:   lastIssuer,
			username: username,
			password: password,
			scope:    r.Form.Get("scope"),
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"access-token-%d","token_type":"Bearer","expires_in":%d}`, len(requests), expiresIn)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/.well-known/openid-configuration") {
			http.NotFound(w, r)
			return
		}
		issuerPath := strings.TrimSuffix(r.URL.Path, "/.well-known/openid-configuration")
		lastIssuer = server.URL + issuerPath
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{
			"issuer": %q,
			"authorization_endpoint": %q,
			"token_endpoint": %q,
			"jwks_uri": %q,
			"response_types_supported": ["code"],
			"subject_types_supported": ["public"],
			"id_token_signing_alg_values_supported": ["RS256"]
		}`, lastIssuer, server.URL+"/authorize", server.URL+"/token", server.URL+"/jwks")
	})

	return server, &requests
}

func writeTextFile(t *testing.T, name, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	writeExistingTextFile(t, path, value)
	return path
}

func writeExistingTextFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func applyNATSOptions(opts []nats.Option) (nats.Options, error) {
	cfg := nats.GetDefaultOptions()
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func writeTestCAFile(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test CA key: %v", err)
	}
	cert := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "unit-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create test CA: %v", err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write test CA: %v", err)
	}
	return path
}
