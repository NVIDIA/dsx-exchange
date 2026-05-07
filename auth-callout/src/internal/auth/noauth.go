// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"

	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/config"
	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/metrics"
)

// NoAuthAuthenticator handles no-authentication (anonymous) access
type NoAuthAuthenticator struct {
	pm          *config.PermissionsManager
	logger      *otelzap.Logger
	serviceName string
}

// NewNoAuthAuthenticator creates a new no-auth authenticator
func NewNoAuthAuthenticator(pm *config.PermissionsManager, logger *otelzap.Logger, serviceName string) *NoAuthAuthenticator {
	logger.Info("NoAuth authenticator initialized")

	return &NoAuthAuthenticator{
		pm:          pm,
		logger:      logger,
		serviceName: serviceName,
	}
}

// CanAuthenticate checks if no credentials are present (noauth case)
func (n *NoAuthAuthenticator) CanAuthenticate(rc *natsjwt.AuthorizationRequestClaims) bool {
	hasOAuth2 := rc.ConnectOptions.Token != ""
	hasMTLS := rc.TLS != nil && len(rc.TLS.VerifiedChains) > 0
	hasNKey := rc.ConnectOptions.Nkey != ""
	return !hasOAuth2 && !hasMTLS && !hasNKey
}

// TryAuthenticate attempts no-auth authentication
// Only succeeds if no other credentials are provided and noauth is configured
func (n *NoAuthAuthenticator) TryAuthenticate(ctx context.Context, rc *natsjwt.AuthorizationRequestClaims) (config.UserProfile, error) {
	meter := metrics.GetMeter(n.serviceName)

	if counter, err := meter.Int64Counter("auth_noauth_attempts_total",
		metric.WithDescription("Total NoAuth authentication attempts")); err == nil {
		counter.Add(ctx, 1, metric.WithAttributes(attribute.String("method", "noauth")))
	}

	// Check if any credentials were provided
	hasOAuth2 := rc.ConnectOptions.Token != ""
	hasMTLS := rc.TLS != nil && len(rc.TLS.VerifiedChains) > 0
	hasNKey := rc.ConnectOptions.Nkey != ""

	// If any credentials were provided, don't use noauth
	if hasOAuth2 || hasMTLS || hasNKey {
		if counter, err := meter.Int64Counter("auth_noauth_failures_total",
			metric.WithDescription("Total NoAuth authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "noauth"),
				attribute.String("reason", "credentials_provided"),
			))
		}
		return config.UserProfile{}, fmt.Errorf("credentials provided but noauth was attempted")
	}

	// Get noauth profile
	profile, ok := n.pm.GetNoAuthProfile()
	if !ok {
		if counter, err := meter.Int64Counter("auth_noauth_failures_total",
			metric.WithDescription("Total NoAuth authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "noauth"),
				attribute.String("reason", "not_configured"),
			))
		}
		return config.UserProfile{}, fmt.Errorf("noauth not configured")
	}

	n.logger.Info("NoAuth authentication successful",
		zap.String("name", profile.Name),
		zap.String("account", profile.Account),
	)
	return profile, nil
}
