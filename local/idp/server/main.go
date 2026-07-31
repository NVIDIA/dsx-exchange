// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"os"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const (
	defaultConfigPath     = "/etc/demoidp/config.json"
	defaultSigningKeyPath = "/etc/demoidp/signing-key/tls.key"
	defaultListenAddr     = ":5556"
	maxRequestBytes       = 1 << 20

	jwksPath  = "/jwks.json"
	tokenPath = "/token"
)

type config struct {
	Issuer     string   `json:"issuer"`
	KeyID      string   `json:"kid"`
	TTLSeconds int64    `json:"ttlSeconds"`
	Clients    []client `json:"clients"`
	Users      []user   `json:"users"`
}

type client struct {
	ID         string         `json:"id"`
	Secret     string         `json:"secret"`
	GrantType  string         `json:"grantType"`
	Audience   string         `json:"audience"`
	Audiences  []string       `json:"audiences"`
	TTLSeconds *int64         `json:"ttlSeconds,omitempty"`
	Claims     map[string]any `json:"claims"`
}

type user struct {
	Username string         `json:"username"`
	Password string         `json:"password"`
	Claims   map[string]any `json:"claims"`
}

type server struct {
	cfg        config
	signingKey *rsa.PrivateKey
	clients    map[string]client
	users      map[string]user
	now        func() time.Time
}

func main() {
	cfg, err := loadConfig(configPath())
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	signingKey, err := loadSigningKey(signingKeyPath())
	if err != nil {
		log.Fatalf("load signing key: %v", err)
	}
	s := newServer(cfg, signingKey, time.Now)

	srv := &http.Server{
		Addr:              listenAddr(),
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("demoidp issuer %s listening on %s", cfg.Issuer, srv.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
	}
}

func newServer(cfg config, signingKey *rsa.PrivateKey, now func() time.Time) *server {
	s := &server{
		cfg:        cfg,
		signingKey: signingKey,
		clients:    make(map[string]client, len(cfg.Clients)),
		users:      make(map[string]user, len(cfg.Users)),
		now:        now,
	}
	for _, c := range cfg.Clients {
		s.clients[c.ID] = c
	}
	for _, u := range cfg.Users {
		s.users[u.Username] = u
	}
	return s
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET "+jwksPath, s.handleJWKS)
	mux.HandleFunc("POST "+tokenPath, s.handleToken)
	return mux
}

func configPath() string {
	if path := os.Getenv("DEMOIDP_CONFIG"); path != "" {
		return path
	}
	return defaultConfigPath
}

func listenAddr() string {
	if addr := os.Getenv("DEMOIDP_LISTEN_ADDR"); addr != "" {
		return addr
	}
	return defaultListenAddr
}

func signingKeyPath() string {
	if path := os.Getenv("DEMOIDP_SIGNING_KEY"); path != "" {
		return path
	}
	return defaultSigningKeyPath
}

func loadConfig(path string) (config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

func loadSigningKey(path string) (*rsa.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signing key %s: %w", path, err)
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, fmt.Errorf("decode signing key PEM %s: no PEM block found", path)
	}
	key, pkcs1Err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if pkcs1Err == nil {
		return key, nil
	}
	parsed, pkcs8Err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if pkcs8Err != nil {
		return nil, fmt.Errorf("parse signing key %s as PKCS#1 or PKCS#8 private key: %w", path, errors.Join(
			fmt.Errorf("PKCS#1: %w", pkcs1Err),
			fmt.Errorf("PKCS#8: %w", pkcs8Err),
		))
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("parse signing key %s as RSA private key: parsed %T, not RSA", path, parsed)
	}
	return key, nil
}

func (s *server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	writeJSONContentType(w, http.StatusOK, "application/jwk-set+json", jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{
			{
				Key:       &s.signingKey.PublicKey,
				KeyID:     s.cfg.KeyID,
				Algorithm: string(jose.RS256),
				Use:       "sig",
			},
		},
	})
}

func (s *server) handleToken(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid form body")
		return
	}
	c, ok := s.clients[r.Form.Get("client_id")]
	if !ok || c.Secret != r.Form.Get("client_secret") {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "client authentication failed")
		return
	}
	grantType := c.GrantType
	if grantType == "" {
		grantType = "password"
	}
	if r.Form.Get("grant_type") != grantType {
		writeOAuthError(w, http.StatusBadRequest, "unauthorized_client", "client is not allowed to use this grant type")
		return
	}

	claims := c.Claims
	if grantType == "password" {
		u, ok := s.users[r.Form.Get("username")]
		if !ok || u.Password != r.Form.Get("password") {
			writeOAuthError(w, http.StatusUnauthorized, "invalid_grant", "user authentication failed")
			return
		}
		claims = u.Claims
	}

	token, expiresIn, err := s.mintJWT(c, claims, r.Form.Get("scope"))
	if err != nil {
		log.Printf("mint token: %v", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to mint token")
		return
	}
	response := map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   expiresIn,
		"scope":        r.Form.Get("scope"),
	}
	if grantType == "password" {
		response["id_token"] = token
	}
	writeOAuthToken(w, response)
}

func (s *server) mintJWT(c client, tokenClaims map[string]any, scope string) (string, int64, error) {
	now := s.now().UTC()
	expiresIn := s.cfg.TTLSeconds
	if c.TTLSeconds != nil {
		expiresIn = *c.TTLSeconds
	}
	var aud any = c.ID
	if c.Audience != "" {
		aud = c.Audience
	}
	if len(c.Audiences) > 0 {
		aud = c.Audiences
	}

	claims := maps.Clone(tokenClaims)
	if claims == nil {
		claims = map[string]any{}
	}
	claims["iss"] = s.cfg.Issuer
	claims["aud"] = aud
	claims["iat"] = now.Unix()
	claims["nbf"] = now.Add(-5 * time.Second).Unix()
	claims["exp"] = now.Add(time.Duration(expiresIn) * time.Second).Unix()
	if scope != "" {
		claims["scope"] = scope
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: s.signingKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", s.cfg.KeyID),
	)
	if err != nil {
		return "", 0, err
	}
	token, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		return "", 0, err
	}
	return token, expiresIn, nil
}

func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{
		"error":             code,
		"error_description": description,
	})
}

func writeOAuthToken(w http.ResponseWriter, v any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusOK, v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	writeJSONContentType(w, status, "application/json", v)
}

func writeJSONContentType(w http.ResponseWriter, status int, contentType string, v any) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		log.Printf("write response: %v", err)
	}
}
