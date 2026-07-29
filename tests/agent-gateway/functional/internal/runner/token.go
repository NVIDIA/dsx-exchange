// Copyright 2026 NVIDIA Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

// Package runner — JWT minting through the local demo issuer harness.
// Tokens are minted via the Kubernetes Service proxy and cached per
// tenant so a suite of tests shares one token instead of re-minting.
package runner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sync/singleflight"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const demoIssuerNamespace = "dsx-identity"

// MintToken mints a JWT through the local demo issuer's OAuth token
// endpoint. The runner reads fixture credentials from the live Secret
// and posts through the Kubernetes Service proxy, so functional tests do
// not shell out to kubectl or the k6 minting script.
func MintToken(t *testing.T, caller string) string {
	t.Helper()
	return MintTokenClient(t, caller, "")
}

// MintTokenClient mints a JWT for `caller` using a named OAuth
// client (e.g. `wrong-audience`). Empty falls back to the
// default. Retries up to 5x on transient failures such as an IdP Pod
// still warming up.
func MintTokenClient(t *testing.T, caller, clientID string) string {
	t.Helper()
	if clientID == "" {
		return cachedMintToken(t, caller)
	}
	return mintTokenUncached(t, caller, clientID)
}

type tokenCacheKey struct {
	tenant   string
	clientID string
}

type cachedToken struct {
	value     string
	expiresAt time.Time
}

var (
	tokenCacheMu sync.Mutex
	tokenCache   = map[tokenCacheKey]cachedToken{}
	tokenFlights singleflight.Group
)

func cachedMintToken(t *testing.T, tenant string) string {
	t.Helper()
	key := tokenCacheKey{tenant: tenant}
	now := time.Now()

	tokenCacheMu.Lock()
	if cached, ok := tokenCache[key]; ok && now.Before(cached.expiresAt.Add(-1*time.Minute)) {
		tokenCacheMu.Unlock()
		return cached.value
	}
	tokenCacheMu.Unlock()

	v, err, _ := tokenFlights.Do(tenant, func() (any, error) {
		tok, err := MintTokenWithClient(context.Background(), K8s(t), tenant, "")
		if err != nil {
			return "", err
		}
		expiresAt, parseErr := tokenExpiresAt(tok)
		if parseErr != nil {
			return "", parseErr
		}
		tokenCacheMu.Lock()
		tokenCache[key] = cachedToken{value: tok, expiresAt: expiresAt}
		tokenCacheMu.Unlock()
		return tok, nil
	})

	if err != nil {
		t.Fatalf("mint token for %s failed: %v", tenant, err)
	}
	return v.(string)
}

func mintTokenUncached(t *testing.T, tenant, clientID string) string {
	t.Helper()
	tok, err := MintTokenWithClient(context.Background(), K8s(t), tenant, clientID)
	if err != nil {
		t.Fatalf("mint token for %s client %q failed: %v", tenant, clientID, err)
	}
	return tok
}

type demoIssuerCaller struct {
	service         string
	username        string
	grantType       string
	defaultClientID string
}

type demoIssuerConfig struct {
	Clients map[string]string
	Users   map[string]string
}

type demoIssuerConfigFile struct {
	Clients []struct {
		ID     string `json:"id"`
		Secret string `json:"secret"`
	} `json:"clients"`
	Users []struct {
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"users"`
}

// MintTokenWithClient mints a local demo-issuer JWT through the Kubernetes
// Service proxy. It is shared by functional tests and host-side perf setup.
func MintTokenWithClient(ctx context.Context, k8s kubernetes.Interface, caller, clientID string) (string, error) {
	spec, err := demoCaller(caller)
	if err != nil {
		return "", err
	}
	if clientID == "" {
		clientID = spec.defaultClientID
	}
	if clientID == "" {
		clientID = "dsx-agent-gateway"
	}
	var lastErr error
	for attempt := 1; attempt <= 5; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		tok, err := mintTokenOnce(attemptCtx, k8s, spec, clientID)
		cancel()
		if err == nil {
			return tok, nil
		}
		lastErr = err
		if attempt < 5 {
			_ = waitForDelay(ctx, 500*time.Millisecond)
		}
	}
	return "", fmt.Errorf("after 5 retries: %w", lastErr)
}

// MintExpiredToken asks the live demo issuer to sign an already-expired
// token through the expired-token client.
func MintExpiredToken(t *testing.T, caller string) string {
	t.Helper()
	return mintTokenUncached(t, caller, "expired-token")
}

func mintTokenOnce(ctx context.Context, k8s kubernetes.Interface, spec demoIssuerCaller, clientID string) (string, error) {
	cfg, err := demoConfig(ctx, k8s, spec.service)
	if err != nil {
		return "", fmt.Errorf("load demo issuer config service=%s client=%s: %w", spec.service, clientID, err)
	}
	clientSecret, ok := cfg.Clients[clientID]
	if !ok {
		return "", fmt.Errorf("demo issuer config service=%s missing client %q", spec.service, clientID)
	}
	grantType := spec.grantType
	if grantType == "" {
		grantType = "password"
	}
	form := url.Values{
		"grant_type":    {grantType},
		"scope":         {"openid email profile offline_access"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	if grantType == "password" {
		password, ok := cfg.Users[spec.username]
		if !ok {
			return "", fmt.Errorf("demo issuer config service=%s missing user %q", spec.service, spec.username)
		}
		form.Set("username", spec.username)
		form.Set("password", password)
	}

	body, err := k8s.CoreV1().RESTClient().Post().
		Namespace(demoIssuerNamespace).
		Resource("services").
		Name("http:"+spec.service+":http").
		SubResource("proxy").
		Suffix("token").
		SetHeader("Content-Type", "application/x-www-form-urlencoded").
		Body([]byte(form.Encode())).
		DoRaw(ctx)
	if err != nil {
		return "", fmt.Errorf("POST demo issuer token service=%s client=%s: %w", spec.service, clientID, err)
	}
	var out struct {
		IDToken     string `json:"id_token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse demo issuer token response service=%s client=%s: %w", spec.service, clientID, err)
	}
	if out.IDToken != "" {
		return out.IDToken, nil
	}
	if out.AccessToken != "" {
		return out.AccessToken, nil
	}
	return "", fmt.Errorf("demo issuer token response service=%s client=%s missing id_token/access_token: %s", spec.service, clientID, body)
}

func demoCaller(caller string) (demoIssuerCaller, error) {
	switch caller {
	case "operator":
		return demoIssuerCaller{service: "human-oidc", username: "operator@local"}, nil
	case "viewer":
		return demoIssuerCaller{service: "human-oidc", username: "viewer@local"}, nil
	case "service-agent":
		return demoIssuerCaller{service: "service-oidc", grantType: "client_credentials", defaultClientID: "service-agent"}, nil
	case "tenant-a":
		return demoIssuerCaller{service: "svid-issuer", username: "tenant-a@local"}, nil
	case "tenant-b":
		return demoIssuerCaller{service: "svid-issuer", username: "tenant-b@local"}, nil
	case "tenant-c":
		return demoIssuerCaller{service: "svid-issuer", username: "tenant-c@local"}, nil
	case "tenant-test":
		return demoIssuerCaller{service: "svid-issuer", username: "tenant-test@local"}, nil
	case "tenant-test-b":
		return demoIssuerCaller{service: "svid-issuer", username: "tenant-test-b@local"}, nil
	case "bad-svid":
		return demoIssuerCaller{service: "svid-issuer", username: "bad-svid@local"}, nil
	case "wrong-key-svid":
		return demoIssuerCaller{service: "svid-wrong-key", username: "tenant-a@local"}, nil
	case "unconfigured-issuer":
		return demoIssuerCaller{service: "unconfigured-issuer", username: "tenant-a@local"}, nil
	default:
		return demoIssuerCaller{}, fmt.Errorf("unknown caller %q", caller)
	}
}

func demoConfig(ctx context.Context, k8s kubernetes.Interface, service string) (demoIssuerConfig, error) {
	secretName := service + "-config"
	secret, err := k8s.CoreV1().Secrets(demoIssuerNamespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return demoIssuerConfig{}, fmt.Errorf("get demo issuer Secret %s/%s: %w", demoIssuerNamespace, secretName, err)
	}
	raw := secret.Data["config.json"]
	if len(raw) == 0 {
		return demoIssuerConfig{}, fmt.Errorf("demo issuer Secret %s/%s missing config.json", demoIssuerNamespace, secretName)
	}
	var file demoIssuerConfigFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return demoIssuerConfig{}, fmt.Errorf("parse demo issuer Secret %s/%s config.json: %w", demoIssuerNamespace, secretName, err)
	}
	cfg := demoIssuerConfig{
		Clients: make(map[string]string, len(file.Clients)),
		Users:   make(map[string]string, len(file.Users)),
	}
	for _, c := range file.Clients {
		cfg.Clients[c.ID] = c.Secret
	}
	for _, u := range file.Users {
		cfg.Users[u.Username] = u.Password
	}
	return cfg, nil
}

func tokenExpiresAt(tok string) (time.Time, error) {
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return time.Time{}, fmt.Errorf("token has %d JWT parts, want at least 2", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}, fmt.Errorf("decode token payload: %w", err)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return time.Time{}, fmt.Errorf("parse token payload: %w", err)
	}
	if claims.Exp == 0 {
		return time.Time{}, fmt.Errorf("token payload missing exp")
	}
	return time.Unix(claims.Exp, 0), nil
}
