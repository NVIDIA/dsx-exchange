// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/base64"
	"fmt"

	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/config"
	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/metrics"
)

// NKeyAuthenticator handles NKey-based authentication
type NKeyAuthenticator struct {
	pm          *config.PermissionsManager
	logger      *otelzap.Logger
	serviceName string
}

// NewNKeyAuthenticator creates a new NKey authenticator
func NewNKeyAuthenticator(pm *config.PermissionsManager, logger *otelzap.Logger, serviceName string) *NKeyAuthenticator {
	logger.Info("NKey authenticator initialized")

	return &NKeyAuthenticator{
		pm:          pm,
		logger:      logger,
		serviceName: serviceName,
	}
}

// CanAuthenticate checks if NKey credentials are present
func (n *NKeyAuthenticator) CanAuthenticate(rc *natsjwt.AuthorizationRequestClaims) bool {
	return rc.ConnectOptions.Nkey != ""
}

// Authenticate validates an NKey and returns user profile
func (n *NKeyAuthenticator) Authenticate(ctx context.Context, publicKey string) (*config.UserProfile, error) {
	meter := metrics.GetMeter(n.serviceName)

	if counter, err := meter.Int64Counter("auth_nkey_attempts_total",
		metric.WithDescription("Total NKey authentication attempts")); err == nil {
		counter.Add(ctx, 1, metric.WithAttributes(attribute.String("method", "nkey")))
	}

	// Validate the NKey format
	if err := n.validateNKey(publicKey); err != nil {
		if counter, err := meter.Int64Counter("auth_nkey_failures_total",
			metric.WithDescription("Total NKey authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "nkey"),
				attribute.String("reason", "invalid_format"),
			))
		}
		return nil, fmt.Errorf("invalid NKey: %w", err)
	}

	// Look up user profile in permissions config
	profile, ok := n.pm.GetNKeyProfile(publicKey)
	if !ok {
		if counter, err := meter.Int64Counter("auth_nkey_failures_total",
			metric.WithDescription("Total NKey authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "nkey"),
				attribute.String("reason", "user_not_found"),
			))
		}
		return nil, fmt.Errorf("NKey not found in permissions config")
	}

	n.logger.Info("NKey authentication successful",
		zap.String("public_key", publicKey),
		zap.String("account", profile.Account),
	)
	return &profile, nil
}

// validateNKey checks if the provided string is a valid NKey public key
func (n *NKeyAuthenticator) validateNKey(publicKey string) error {
	// Try to parse the public key
	kp, err := nkeys.FromPublicKey(publicKey)
	if err != nil {
		return fmt.Errorf("failed to parse NKey: %w", err)
	}

	// Verify it's a user key (starts with 'U')
	if publicKey[0] != 'U' {
		return fmt.Errorf("NKey must be a user key (start with 'U'), got: %c", publicKey[0])
	}

	// Verify we can get the public key back (validates the key)
	pubKey, err := kp.PublicKey()
	if err != nil {
		return fmt.Errorf("invalid NKey public key: %w", err)
	}

	if pubKey != publicKey {
		return fmt.Errorf("NKey public key mismatch")
	}

	return nil
}

// TryAuthenticate attempts NKey authentication from auth request claims
func (n *NKeyAuthenticator) TryAuthenticate(ctx context.Context, rc *natsjwt.AuthorizationRequestClaims) (config.UserProfile, error) {
	meter := metrics.GetMeter(n.serviceName)

	// Get the user NKey from the connect options
	userNKey := rc.ConnectOptions.Nkey
	if userNKey == "" {
		return config.UserProfile{}, fmt.Errorf("no NKey provided")
	}

	// Check if this NKey is in our allowed list
	profile, ok := n.pm.GetNKeyProfile(userNKey)
	if !ok {
		if counter, err := meter.Int64Counter("auth_nkey_failures_total",
			metric.WithDescription("Total NKey authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "nkey"),
				attribute.String("reason", "user_not_found"),
			))
		}
		return config.UserProfile{}, fmt.Errorf("NKey not found in permissions config")
	}

	// Verify the signature
	signedNonce := rc.ConnectOptions.SignedNonce
	if signedNonce == "" {
		if counter, err := meter.Int64Counter("auth_nkey_failures_total",
			metric.WithDescription("Total NKey authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "nkey"),
				attribute.String("reason", "no_signature"),
			))
		}
		return config.UserProfile{}, fmt.Errorf("no signed nonce provided")
	}

	// Parse the public key
	kp, err := nkeys.FromPublicKey(userNKey)
	if err != nil {
		if counter, err := meter.Int64Counter("auth_nkey_failures_total",
			metric.WithDescription("Total NKey authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "nkey"),
				attribute.String("reason", "invalid_public_key"),
			))
		}
		return config.UserProfile{}, fmt.Errorf("invalid public key: %w", err)
	}

	// Decode the signature from base64
	signature, err := base64.RawURLEncoding.DecodeString(signedNonce)
	if err != nil {
		if counter, err := meter.Int64Counter("auth_nkey_failures_total",
			metric.WithDescription("Total NKey authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "nkey"),
				attribute.String("reason", "signature_decode_failed"),
			))
		}
		return config.UserProfile{}, fmt.Errorf("failed to decode signature: %w", err)
	}

	// Get the nonce from client information
	nonce := []byte(rc.ClientInformation.Nonce)

	// Verify the signature against the nonce
	if err := kp.Verify(nonce, signature); err != nil {
		if counter, err := meter.Int64Counter("auth_nkey_failures_total",
			metric.WithDescription("Total NKey authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "nkey"),
				attribute.String("reason", "signature_verification_failed"),
			))
		}
		return config.UserProfile{}, fmt.Errorf("signature verification failed: %w", err)
	}

	n.logger.Info("NKey authentication successful",
		zap.String("name", profile.Name),
		zap.String("account", profile.Account),
	)
	return profile, nil
}
