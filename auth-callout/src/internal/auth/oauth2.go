// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/config"
	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/metrics"
)

// OAuth2Authenticator handles OAuth2/JWKS-based authentication
type OAuth2Authenticator struct {
	jwks              keyfunc.Keyfunc
	pm                *config.PermissionsManager
	issuer            string
	audience          string
	signingAlgorithms []string
	logger            *otelzap.Logger
	serviceName       string
	cancel            context.CancelFunc
}

var supportedOAuth2SigningAlgorithms = map[string]struct{}{
	jwt.SigningMethodRS256.Alg(): {},
	jwt.SigningMethodRS384.Alg(): {},
	jwt.SigningMethodRS512.Alg(): {},
	jwt.SigningMethodES256.Alg(): {},
	jwt.SigningMethodES384.Alg(): {},
	jwt.SigningMethodES512.Alg(): {},
	jwt.SigningMethodPS256.Alg(): {},
	jwt.SigningMethodPS384.Alg(): {},
	jwt.SigningMethodPS512.Alg(): {},
	jwt.SigningMethodEdDSA.Alg(): {},
}

// NewOAuth2Authenticator creates a new OAuth2 authenticator
func NewOAuth2Authenticator(jwksURL string, issuer string, audience string, signingAlgorithms []string, pm *config.PermissionsManager, logger *otelzap.Logger, serviceName string) (*OAuth2Authenticator, error) {
	if issuer == "" {
		return nil, fmt.Errorf("OAuth2 issuer is required")
	}
	if audience == "" {
		return nil, fmt.Errorf("OAuth2 audience is required")
	}
	if err := validateOAuth2SigningAlgorithms(signingAlgorithms); err != nil {
		return nil, err
	}

	// Create JWKS client with automatic refresh - context controls lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	k, err := keyfunc.NewDefaultCtx(ctx, []string{jwksURL})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create JWKS client: %w", err)
	}

	logger.Info("OAuth2 authenticator initialized",
		zap.String("jwks_url", jwksURL),
		zap.Strings("signing_algorithms", signingAlgorithms),
	)

	return &OAuth2Authenticator{
		jwks:              k,
		pm:                pm,
		issuer:            issuer,
		audience:          audience,
		signingAlgorithms: signingAlgorithms,
		logger:            logger,
		serviceName:       serviceName,
		cancel:            cancel,
	}, nil
}

func validateOAuth2SigningAlgorithms(configured []string) error {
	if len(configured) == 0 {
		return fmt.Errorf("OAuth2 signing algorithms are required")
	}

	for _, algorithm := range configured {
		if _, ok := supportedOAuth2SigningAlgorithms[algorithm]; !ok {
			return fmt.Errorf("unsupported OAuth2 signing algorithm %q", algorithm)
		}
	}
	return nil
}

// CanAuthenticate checks if OAuth2 credentials are present
func (o *OAuth2Authenticator) CanAuthenticate(rc *natsjwt.AuthorizationRequestClaims) bool {
	return rc.ConnectOptions.Token != "" || (rc.ConnectOptions.Username == "oauthtoken" && rc.ConnectOptions.Password != "")
}

// Claims represents the expected JWT claims structure
type Claims struct {
	jwt.RegisteredClaims
	Scope  string   `json:"scope"`
	Scopes []string `json:"scopes"`
	Azp    string   `json:"azp"`
}

// Authenticate validates an OAuth2 token and returns user profile
func (o *OAuth2Authenticator) Authenticate(ctx context.Context, token string) (*config.UserProfile, error) {
	meter := metrics.GetMeter(o.serviceName)

	if counter, err := meter.Int64Counter("auth_oauth2_attempts_total",
		metric.WithDescription("Total OAuth2 authentication attempts")); err == nil {
		counter.Add(ctx, 1, metric.WithAttributes(attribute.String("method", "oauth2")))
	}

	// Parse and validate the JWT token
	parsed, err := jwt.ParseWithClaims(
		token,
		&Claims{},
		o.jwks.Keyfunc,
		jwt.WithValidMethods(o.signingAlgorithms),
		jwt.WithExpirationRequired(),
		jwt.WithIssuer(o.issuer),
		jwt.WithAudience(o.audience),
	)
	if err != nil {
		if counter, err := meter.Int64Counter("auth_oauth2_failures_total",
			metric.WithDescription("Total OAuth2 authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "oauth2"),
				attribute.String("reason", "parse_failed"),
			))
		}
		return nil, fmt.Errorf("failed to parse JWT: %w", err)
	}

	if !parsed.Valid {
		if counter, err := meter.Int64Counter("auth_oauth2_failures_total",
			metric.WithDescription("Total OAuth2 authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "oauth2"),
				attribute.String("reason", "invalid_token"),
			))
		}
		return nil, fmt.Errorf("invalid JWT token")
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok {
		if counter, err := meter.Int64Counter("auth_oauth2_failures_total",
			metric.WithDescription("Total OAuth2 authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "oauth2"),
				attribute.String("reason", "invalid_claims"),
			))
		}
		return nil, fmt.Errorf("invalid claims type")
	}

	// Look up user profile in permissions config using both subject and azp
	profile, requiredScope, ok := o.pm.GetOAuth2Profile(claims.Subject, claims.Azp)
	if !ok {
		if counter, err := meter.Int64Counter("auth_oauth2_failures_total",
			metric.WithDescription("Total OAuth2 authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "oauth2"),
				attribute.String("reason", "user_not_found"),
			))
		}
		return nil, fmt.Errorf("user not found in permissions config: sub=%s, azp=%s", claims.Subject, claims.Azp)
	}

	// Validate scope - must contain the client's required scope
	if !o.hasScope(claims.Scope, claims.Scopes, requiredScope) {
		if counter, err := meter.Int64Counter("auth_oauth2_failures_total",
			metric.WithDescription("Total OAuth2 authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "oauth2"),
				attribute.String("reason", "missing_scope"),
			))
		}
		return nil, fmt.Errorf("missing required scope: %s", requiredScope)
	}

	o.logger.Info("OAuth2 authentication successful",
		zap.String("subject", claims.Subject),
		zap.String("azp", claims.Azp),
		zap.String("account", profile.Account),
	)
	return &profile, nil
}

// hasScope checks if the scope string contains the required scope
func (o *OAuth2Authenticator) hasScope(scopeStr string, scopes []string, required string) bool {
	if scopeStr != "" {
		scopes := strings.Fields(scopeStr)
		return slices.Contains(scopes, required)
	}

	return slices.Contains(scopes, required)
}

// TryAuthenticate attempts OAuth2 authentication from auth request claims
func (o *OAuth2Authenticator) TryAuthenticate(ctx context.Context, rc *natsjwt.AuthorizationRequestClaims) (config.UserProfile, error) {
	// OAuth2 tokens are typically passed in the password field or as a token
	token := rc.ConnectOptions.Token
	if token == "" && rc.ConnectOptions.Username == "oauthtoken" {
		// Try password field as fallback
		token = rc.ConnectOptions.Password
	}

	if token == "" {
		return config.UserProfile{}, fmt.Errorf("no OAuth2 token provided")
	}

	// Check if it looks like a JWT (three dot-separated parts)
	if strings.Count(token, ".") != 2 {
		return config.UserProfile{}, fmt.Errorf("invalid token format")
	}

	profile, err := o.Authenticate(ctx, token)
	if err != nil {
		return config.UserProfile{}, fmt.Errorf("OAuth2 authentication failed: %w", err)
	}

	return *profile, nil
}

// Close cleans up resources
func (o *OAuth2Authenticator) Close() {
	if o.cancel != nil {
		o.cancel()
	}
}
