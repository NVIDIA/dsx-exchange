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

package config

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

const natsOAuthRejectedToken = "invalid-oauth-token"

// reloadingNATSOAuthSource reads env/file settings only when the OAuth2 cache
// needs a fresh token, so Secret-backed file updates are picked up on refresh.
type reloadingNATSOAuthSource struct {
	ctx    context.Context
	config NATSOAuthConfig
}

func (s reloadingNATSOAuthSource) Token() (*oauth2.Token, error) {
	settings, err := s.config.CurrentSettings()
	if err != nil {
		return nil, err
	}
	provider, err := oidc.NewProvider(s.ctx, settings.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC issuer: %w", err)
	}
	return (&clientcredentials.Config{
		ClientID:     settings.ClientID,
		ClientSecret: settings.ClientSecret,
		TokenURL:     provider.Endpoint().TokenURL,
		Scopes:       []string{settings.Scope},
	}).TokenSource(s.ctx).Token()
}

func NATSOptions(name string, config NATSConfig) ([]nats.Option, error) {
	opts := []nats.Option{
		nats.Name(name),
		nats.MaxReconnects(-1),
		nats.RetryOnFailedConnect(true),
		nats.ReconnectWait(time.Second),
		nats.PingInterval(20 * time.Second),
	}

	mode := config.AuthMode
	if mode != NATSAuthModeNoAuth && mode != NATSAuthModeOAuth {
		return nil, fmt.Errorf("%s must be exactly %q or %q", EnvNATSAuthMode, NATSAuthModeNoAuth, NATSAuthModeOAuth)
	}
	switch mode {
	case NATSAuthModeNoAuth:
	case NATSAuthModeOAuth:
		source, err := natsOAuthTokenSource(config.OAuth)
		if err != nil {
			return nil, err
		}
		opts = append(opts, nats.TokenHandler(natsOAuthTokenHandler(source)))
	}

	// CA roots and an explicit server name both imply TLS. The boolean
	// exists for the no-file case where the server already has public trust.
	tlsEnabled := config.TLS.Enabled || config.TLS.CAFile != "" || config.TLS.ServerName != ""
	if tlsEnabled {
		opts = append(opts, nats.Secure(&tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: config.TLS.ServerName,
		}))
	}
	if config.TLS.CAFile != "" {
		if _, err := readRequiredFile(EnvNATSTLSCAFile, config.TLS.CAFile); err != nil {
			return nil, err
		}
		opts = append(opts, nats.RootCAs(config.TLS.CAFile))
	}
	return opts, nil
}

func natsOAuthTokenSource(config NATSOAuthConfig) (oauth2.TokenSource, error) {
	if _, err := config.CurrentSettings(); err != nil {
		return nil, err
	}
	// oauth2 obtains its token-request client from this context value.
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{
		Timeout:   10 * time.Second,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	})
	return oauth2.ReuseTokenSource(nil, reloadingNATSOAuthSource{ctx: ctx, config: config}), nil
}

func natsOAuthTokenHandler(source oauth2.TokenSource) nats.AuthTokenHandler {
	return func() string {
		token, err := source.Token()
		if err != nil || !token.Valid() {
			return natsOAuthRejectedToken
		}
		return token.AccessToken
	}
}
