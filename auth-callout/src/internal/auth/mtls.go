// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	natsjwt "github.com/nats-io/jwt/v2"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/config"
	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/metrics"
)

// MTLSAuthenticator handles mTLS client certificate authentication
type MTLSAuthenticator struct {
	pm          *config.PermissionsManager
	caPool      *x509.CertPool
	logger      *otelzap.Logger
	serviceName string
}

// NewMTLSAuthenticator creates a new mTLS authenticator
func NewMTLSAuthenticator(caPEM []byte, pm *config.PermissionsManager, logger *otelzap.Logger, serviceName string) (*MTLSAuthenticator, error) {
	caPool := x509.NewCertPool()
	if len(caPEM) > 0 {
		if !caPool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		logger.Info("mTLS authenticator initialized with CA certificate")
	} else {
		logger.Info("mTLS authenticator initialized without CA validation")
	}

	return &MTLSAuthenticator{
		pm:          pm,
		caPool:      caPool,
		logger:      logger,
		serviceName: serviceName,
	}, nil
}

// CanAuthenticate checks if mTLS credentials are present
func (m *MTLSAuthenticator) CanAuthenticate(rc *natsjwt.AuthorizationRequestClaims) bool {
	return rc.TLS != nil && len(rc.TLS.VerifiedChains) > 0
}

// Authenticate validates a client certificate and returns user profile
func (m *MTLSAuthenticator) Authenticate(ctx context.Context, certPEM string) (*config.UserProfile, error) {
	meter := metrics.GetMeter(m.serviceName)

	if counter, err := meter.Int64Counter("auth_mtls_attempts_total",
		metric.WithDescription("Total mTLS authentication attempts")); err == nil {
		counter.Add(ctx, 1, metric.WithAttributes(attribute.String("method", "mtls")))
	}

	// Parse the certificate
	cert, err := m.parseCertificate(certPEM)
	if err != nil {
		if counter, err := meter.Int64Counter("auth_mtls_failures_total",
			metric.WithDescription("Total mTLS authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "mtls"),
				attribute.String("reason", "parse_failed"),
			))
		}
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Validate against CA if configured
	if m.caPool != nil && len(m.caPool.Subjects()) > 0 {
		opts := x509.VerifyOptions{
			Roots: m.caPool,
			KeyUsages: []x509.ExtKeyUsage{
				x509.ExtKeyUsageClientAuth,
			},
		}
		if _, err := cert.Verify(opts); err != nil {
			if counter, err := meter.Int64Counter("auth_mtls_failures_total",
				metric.WithDescription("Total mTLS authentication failures")); err == nil {
				counter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("method", "mtls"),
					attribute.String("reason", "ca_validation_failed"),
				))
			}
			return nil, fmt.Errorf("certificate validation failed: %w", err)
		}
	}

	// Extract identity from certificate
	identity := m.extractIdentity(cert)
	if identity == "" {
		if counter, err := meter.Int64Counter("auth_mtls_failures_total",
			metric.WithDescription("Total mTLS authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "mtls"),
				attribute.String("reason", "no_identity"),
			))
		}
		return nil, fmt.Errorf("could not extract identity from certificate")
	}

	// Look up user profile in permissions config
	profile, ok := m.pm.GetMTLSProfile(identity)
	if !ok {
		if counter, err := meter.Int64Counter("auth_mtls_failures_total",
			metric.WithDescription("Total mTLS authentication failures")); err == nil {
			counter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("method", "mtls"),
				attribute.String("reason", "user_not_found"),
			))
		}
		return nil, fmt.Errorf("user not found in permissions config: %s", identity)
	}

	m.logger.Info("mTLS authentication successful",
		zap.String("identity", identity),
		zap.String("account", profile.Account),
	)
	return &profile, nil
}

// parseCertificate parses a PEM-encoded certificate
func (m *MTLSAuthenticator) parseCertificate(certPEM string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	return cert, nil
}

// extractIdentity extracts identity from certificate
// Priority: CN, then email, then first DNS SAN
func (m *MTLSAuthenticator) extractIdentity(cert *x509.Certificate) string {
	// Try Common Name first
	if cert.Subject.CommonName != "" {
		return formatDN("CN", cert.Subject.CommonName)
	}

	// Try email addresses
	if len(cert.EmailAddresses) > 0 {
		return cert.EmailAddresses[0]
	}

	// Try DNS names in SAN
	if len(cert.DNSNames) > 0 {
		return cert.DNSNames[0]
	}

	// Try other subject fields
	if len(cert.Subject.Organization) > 0 {
		return formatDN("O", cert.Subject.Organization[0])
	}

	if len(cert.Subject.OrganizationalUnit) > 0 {
		return formatDN("OU", cert.Subject.OrganizationalUnit[0])
	}

	return ""
}

// formatDN formats a distinguished name component
func formatDN(field, value string) string {
	return fmt.Sprintf("%s=%s", field, value)
}

// TryAuthenticate attempts mTLS authentication from auth request claims
func (m *MTLSAuthenticator) TryAuthenticate(ctx context.Context, rc *natsjwt.AuthorizationRequestClaims) (config.UserProfile, error) {
	// Check if TLS client certificate is present
	if rc.TLS == nil || len(rc.TLS.VerifiedChains) == 0 {
		return config.UserProfile{}, fmt.Errorf("no TLS client certificate provided")
	}

	// Get the first certificate in the first verified chain (the client cert)
	if len(rc.TLS.VerifiedChains[0]) == 0 {
		return config.UserProfile{}, fmt.Errorf("empty certificate chain")
	}

	certPEM := rc.TLS.VerifiedChains[0][0]

	profile, err := m.Authenticate(ctx, certPEM)
	if err != nil {
		return config.UserProfile{}, fmt.Errorf("mTLS authentication failed: %w", err)
	}

	return *profile, nil
}
