// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/MicahParks/jwkset"
	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"

	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/config"
)

const testServiceName = "auth-callout-test"

func testLogger() *otelzap.Logger {
	zapLogger, _ := zap.NewDevelopment()
	return otelzap.New(zapLogger)
}

func TestValidateOAuth2SigningAlgorithms(t *testing.T) {
	tests := []struct {
		name      string
		input     []string
		expectErr string
	}{
		{
			name:      "rejects missing algorithms",
			expectErr: "OAuth2 signing algorithms are required",
		},
		{
			name:  "accepts configured algorithms",
			input: []string{"RS256", "ES256", "RS256"},
		},
		{
			name:      "rejects algorithms with whitespace",
			input:     []string{"RS256", " ES256"},
			expectErr: `unsupported OAuth2 signing algorithm " ES256"`,
		},
		{
			name:      "rejects empty algorithm",
			input:     []string{"RS256", ""},
			expectErr: `unsupported OAuth2 signing algorithm ""`,
		},
		{
			name:      "rejects unsupported algorithm",
			input:     []string{"HS256"},
			expectErr: `unsupported OAuth2 signing algorithm "HS256"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOAuth2SigningAlgorithms(tt.input)
			if tt.expectErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectErr)
				return
			}

			require.NoError(t, err)
		})
	}
}

// TestOAuth2Authentication tests OAuth2/JWKS authentication with mock server
func TestOAuth2Authentication(t *testing.T) {
	// Generate RSA key pair for JWT signing
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Create JWK Set with the private key
	jwkSet := jwkset.NewMemoryStorage()

	// Create JWK from the private key
	jwkOptions := jwkset.JWKOptions{
		Metadata: jwkset.JWKMetadataOptions{
			KID: "test-key-1",
		},
	}

	jwk, err := jwkset.NewJWKFromKey(privateKey, jwkOptions)
	require.NoError(t, err)

	// Add the key to the set
	err = jwkSet.KeyWrite(context.Background(), jwk)
	require.NoError(t, err)

	// Start mock JWKS server
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwks, err := jwkSet.JSONPublic(context.Background())
		if err != nil {
			http.Error(w, "Failed to get JWKS", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(jwks); err != nil {
			http.Error(w, "Failed to write JWKS", http.StatusInternalServerError)
			return
		}
	}))
	defer jwksServer.Close()

	// Create test permissions file
	permFile := createTestPermissionsFile(t)
	defer os.Remove(permFile)

	pm, err := config.NewPermissionsManager(permFile, testLogger())
	require.NoError(t, err)
	defer pm.Close()

	// Initialize OAuth2 authenticator with mock JWKS URL
	oauth2Auth, err := NewOAuth2Authenticator(
		jwksServer.URL,
		"https://auth.example.com/",
		"test-audience",
		[]string{gojwt.SigningMethodRS256.Alg()},
		pm,
		testLogger(),
		testServiceName,
	)
	require.NoError(t, err)
	defer oauth2Auth.Close()

	now := time.Now()

	tests := []struct {
		name           string
		claims         gojwt.MapClaims
		expectError    bool
		expectedAcct   string
		expectedErrMsg string
	}{
		{
			name: "valid token with scope as string",
			claims: gojwt.MapClaims{
				"iss":   "https://auth.example.com/",
				"sub":   "user@example.com",
				"aud":   "test-audience",
				"exp":   now.Add(1 * time.Hour).Unix(),
				"iat":   now.Unix(),
				"scope": "mqtt openid profile",
			},
			expectError:  false,
			expectedAcct: "APP1",
		},
		{
			name: "valid token with azp for service account",
			claims: gojwt.MapClaims{
				"iss":   "https://auth.example.com/",
				"sub":   "service-account-id-12345",
				"aud":   "test-audience",
				"exp":   now.Add(1 * time.Hour).Unix(),
				"iat":   now.Unix(),
				"scope": "mqtt",
				"azp":   "mqtt-client",
			},
			expectError:  false,
			expectedAcct: "APP2",
		},
		{
			name: "valid token with scopes as array",
			claims: gojwt.MapClaims{
				"iss":    "https://auth.example.com/",
				"sub":    "user@example.com",
				"aud":    "test-audience",
				"exp":    now.Add(1 * time.Hour).Unix(),
				"iat":    now.Unix(),
				"scopes": []string{"mqtt", "openid", "profile"},
			},
			expectError:  false,
			expectedAcct: "APP1",
		},
		{
			name: "token without mqtt scope (string)",
			claims: gojwt.MapClaims{
				"iss":   "https://auth.example.com/",
				"sub":   "user@example.com",
				"aud":   "test-audience",
				"exp":   now.Add(1 * time.Hour).Unix(),
				"iat":   now.Unix(),
				"scope": "openid profile",
			},
			expectError:    true,
			expectedErrMsg: "missing required scope: mqtt",
		},
		{
			name: "token without mqtt scope (array)",
			claims: gojwt.MapClaims{
				"iss":    "https://auth.example.com/",
				"sub":    "user@example.com",
				"aud":    "test-audience",
				"exp":    now.Add(1 * time.Hour).Unix(),
				"iat":    now.Unix(),
				"scopes": []string{"openid", "profile"},
			},
			expectError:    true,
			expectedErrMsg: "missing required scope: mqtt",
		},
		{
			name: "preference over scope string",
			claims: gojwt.MapClaims{
				"iss":    "https://auth.example.com/",
				"sub":    "user@example.com",
				"aud":    "test-audience",
				"exp":    now.Add(1 * time.Hour).Unix(),
				"iat":    now.Unix(),
				"scope":  "openid profile",
				"scopes": []string{"mqtt", "openid", "profile"},
			},
			expectError:    true,
			expectedErrMsg: "missing required scope: mqtt",
		},
		{
			name: "missing expiration fails",
			claims: gojwt.MapClaims{
				"iss":   "https://auth.example.com/",
				"sub":   "user@example.com",
				"aud":   "test-audience",
				"iat":   now.Unix(),
				"scope": "mqtt",
			},
			expectError:    true,
			expectedErrMsg: "token is missing required claim: exp claim is required",
		},
		{
			name: "wrong issuer fails",
			claims: gojwt.MapClaims{
				"iss":   "https://other.example.com/",
				"sub":   "user@example.com",
				"aud":   "test-audience",
				"exp":   now.Add(1 * time.Hour).Unix(),
				"iat":   now.Unix(),
				"scope": "mqtt",
			},
			expectError:    true,
			expectedErrMsg: "token has invalid issuer",
		},
		{
			name: "wrong audience fails",
			claims: gojwt.MapClaims{
				"iss":   "https://auth.example.com/",
				"sub":   "user@example.com",
				"aud":   "wrong-audience",
				"exp":   now.Add(1 * time.Hour).Unix(),
				"iat":   now.Unix(),
				"scope": "mqtt",
			},
			expectError:    true,
			expectedErrMsg: "token has invalid audience",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := gojwt.NewWithClaims(gojwt.SigningMethodRS256, tt.claims)
			token.Header["kid"] = "test-key-1"

			tokenString, err := token.SignedString(privateKey)
			require.NoError(t, err)

			profile, err := oauth2Auth.Authenticate(context.Background(), tokenString)

			if tt.expectError {
				assert.Error(t, err)
				if tt.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tt.expectedErrMsg)
				}
				t.Logf("Correctly rejected token: %v", err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, profile)
				assert.Equal(t, tt.expectedAcct, profile.Account)
				t.Logf("OAuth2 authentication successful for profile: %s (account: %s)", profile.Name, profile.Account)
			}
		})
	}
}

func TestOAuth2RejectsUnexpectedSigningMethod(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	jwkSet := jwkset.NewMemoryStorage()
	jwk, err := jwkset.NewJWKFromKey(privateKey, jwkset.JWKOptions{
		Metadata: jwkset.JWKMetadataOptions{
			KID: "test-key-1",
		},
	})
	require.NoError(t, err)
	require.NoError(t, jwkSet.KeyWrite(context.Background(), jwk))

	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwks, err := jwkSet.JSONPublic(context.Background())
		if err != nil {
			http.Error(w, "Failed to get JWKS", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(jwks); err != nil {
			http.Error(w, "Failed to write JWKS", http.StatusInternalServerError)
		}
	}))
	defer jwksServer.Close()

	permFile := createTestPermissionsFile(t)
	defer os.Remove(permFile)

	pm, err := config.NewPermissionsManager(permFile, testLogger())
	require.NoError(t, err)
	defer pm.Close()

	oauth2Auth, err := NewOAuth2Authenticator(
		jwksServer.URL,
		"https://auth.example.com/",
		"test-audience",
		[]string{gojwt.SigningMethodRS256.Alg()},
		pm,
		testLogger(),
		testServiceName,
	)
	require.NoError(t, err)
	defer oauth2Auth.Close()

	token := gojwt.NewWithClaims(gojwt.SigningMethodHS256, gojwt.MapClaims{
		"iss":   "https://auth.example.com/",
		"sub":   "user@example.com",
		"aud":   "test-audience",
		"exp":   time.Now().Add(1 * time.Hour).Unix(),
		"scope": "mqtt",
	})
	token.Header["kid"] = "test-key-1"
	tokenString, err := token.SignedString([]byte("secret"))
	require.NoError(t, err)

	_, err = oauth2Auth.Authenticate(context.Background(), tokenString)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signing method HS256 is invalid")
}

func TestOAuth2AllowsConfiguredES256SigningMethod(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	jwkSet := jwkset.NewMemoryStorage()
	jwk, err := jwkset.NewJWKFromKey(privateKey, jwkset.JWKOptions{
		Metadata: jwkset.JWKMetadataOptions{
			KID: "test-key-1",
		},
	})
	require.NoError(t, err)
	require.NoError(t, jwkSet.KeyWrite(context.Background(), jwk))

	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwks, err := jwkSet.JSONPublic(context.Background())
		if err != nil {
			http.Error(w, "Failed to get JWKS", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(jwks); err != nil {
			http.Error(w, "Failed to write JWKS", http.StatusInternalServerError)
		}
	}))
	defer jwksServer.Close()

	permFile := createTestPermissionsFile(t)
	defer os.Remove(permFile)

	pm, err := config.NewPermissionsManager(permFile, testLogger())
	require.NoError(t, err)
	defer pm.Close()

	oauth2Auth, err := NewOAuth2Authenticator(
		jwksServer.URL,
		"https://auth.example.com/",
		"test-audience",
		[]string{gojwt.SigningMethodRS256.Alg(), gojwt.SigningMethodES256.Alg()},
		pm,
		testLogger(),
		testServiceName,
	)
	require.NoError(t, err)
	defer oauth2Auth.Close()

	token := gojwt.NewWithClaims(gojwt.SigningMethodES256, gojwt.MapClaims{
		"iss":   "https://auth.example.com/",
		"sub":   "user@example.com",
		"aud":   "test-audience",
		"exp":   time.Now().Add(1 * time.Hour).Unix(),
		"scope": "mqtt",
	})
	token.Header["kid"] = "test-key-1"
	tokenString, err := token.SignedString(privateKey)
	require.NoError(t, err)

	profile, err := oauth2Auth.Authenticate(context.Background(), tokenString)
	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Equal(t, "APP1", profile.Account)
}

// TestOAuth2RequiredScope tests per-client required scope validation
func TestOAuth2RequiredScope(t *testing.T) {
	// Generate RSA key pair for JWT signing
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Create JWK Set with the private key
	jwkSet := jwkset.NewMemoryStorage()

	jwkOptions := jwkset.JWKOptions{
		Metadata: jwkset.JWKMetadataOptions{
			KID: "test-key-1",
		},
	}

	jwk, err := jwkset.NewJWKFromKey(privateKey, jwkOptions)
	require.NoError(t, err)

	err = jwkSet.KeyWrite(context.Background(), jwk)
	require.NoError(t, err)

	// Start mock JWKS server
	jwksServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jwks, err := jwkSet.JSONPublic(context.Background())
		if err != nil {
			http.Error(w, "Failed to get JWKS", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(jwks); err != nil {
			http.Error(w, "Failed to write JWKS", http.StatusInternalServerError)
			return
		}
	}))
	defer jwksServer.Close()

	// Create test permissions file with per-client required scopes
	testConfig := config.PermissionsConfig{
		OAuth2: map[string]*config.OAuth2Entry{
			"default-scope-client": {
				Subject: "default@example.com",
				Account: "DEFAULT",
				// RequiredScope not set - should default to "mqtt"
			},
			"custom-scope-client": {
				Azp:           "custom-client-id",
				Account:       "CUSTOM",
				RequiredScope: "nats:events",
			},
		},
	}

	data, err := json.MarshalIndent(testConfig, "", "  ")
	require.NoError(t, err)

	tmpFile, err := os.CreateTemp("", "permissions-scope-*.json")
	require.NoError(t, err)
	permFile := tmpFile.Name()
	defer os.Remove(permFile)

	_, err = tmpFile.Write(data)
	require.NoError(t, err)
	tmpFile.Close()

	pm, err := config.NewPermissionsManager(permFile, testLogger())
	require.NoError(t, err)
	defer pm.Close()

	oauth2Auth, err := NewOAuth2Authenticator(
		jwksServer.URL,
		"https://auth.example.com/",
		"test-audience",
		[]string{gojwt.SigningMethodRS256.Alg()},
		pm,
		testLogger(),
		testServiceName,
	)
	require.NoError(t, err)
	defer oauth2Auth.Close()

	now := time.Now()

	tests := []struct {
		name           string
		claims         gojwt.MapClaims
		expectError    bool
		expectedAcct   string
		expectedErrMsg string
	}{
		{
			name: "default scope client with mqtt scope succeeds",
			claims: gojwt.MapClaims{
				"iss":   "https://auth.example.com/",
				"sub":   "default@example.com",
				"aud":   "test-audience",
				"exp":   now.Add(1 * time.Hour).Unix(),
				"iat":   now.Unix(),
				"scope": "mqtt openid",
			},
			expectError:  false,
			expectedAcct: "DEFAULT",
		},
		{
			name: "default scope client without mqtt scope fails",
			claims: gojwt.MapClaims{
				"iss":   "https://auth.example.com/",
				"sub":   "default@example.com",
				"aud":   "test-audience",
				"exp":   now.Add(1 * time.Hour).Unix(),
				"iat":   now.Unix(),
				"scope": "openid profile",
			},
			expectError:    true,
			expectedErrMsg: "missing required scope: mqtt",
		},
		{
			name: "custom scope client with correct scope succeeds",
			claims: gojwt.MapClaims{
				"iss":   "https://auth.example.com/",
				"sub":   "some-service",
				"azp":   "custom-client-id",
				"aud":   "test-audience",
				"exp":   now.Add(1 * time.Hour).Unix(),
				"iat":   now.Unix(),
				"scope": "nats:events openid",
			},
			expectError:  false,
			expectedAcct: "CUSTOM",
		},
		{
			name: "custom scope client with wrong scope fails",
			claims: gojwt.MapClaims{
				"iss":   "https://auth.example.com/",
				"sub":   "some-service",
				"azp":   "custom-client-id",
				"aud":   "test-audience",
				"exp":   now.Add(1 * time.Hour).Unix(),
				"iat":   now.Unix(),
				"scope": "mqtt openid",
			},
			expectError:    true,
			expectedErrMsg: "missing required scope: nats:events",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := gojwt.NewWithClaims(gojwt.SigningMethodRS256, tt.claims)
			token.Header["kid"] = "test-key-1"

			tokenString, err := token.SignedString(privateKey)
			require.NoError(t, err)

			profile, err := oauth2Auth.Authenticate(context.Background(), tokenString)

			if tt.expectError {
				assert.Error(t, err)
				if tt.expectedErrMsg != "" {
					assert.Contains(t, err.Error(), tt.expectedErrMsg)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, profile)
				assert.Equal(t, tt.expectedAcct, profile.Account)
			}
		})
	}
}

// TestMTLSAuthentication tests mTLS client certificate authentication
func TestMTLSAuthentication(t *testing.T) {
	permFile := createTestPermissionsFile(t)
	defer os.Remove(permFile)

	pm, err := config.NewPermissionsManager(permFile, testLogger())
	require.NoError(t, err)
	defer pm.Close()

	caPEM, caKey := createTestCA(t)
	clientCertPEM := createClientCert(t, "device1", caPEM, caKey)

	mtlsAuth, err := NewMTLSAuthenticator(caPEM, pm, testLogger(), testServiceName)
	require.NoError(t, err)

	profile, err := mtlsAuth.Authenticate(context.Background(), clientCertPEM)
	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Equal(t, "device1", profile.Name)
	assert.Equal(t, "APP1", profile.Account)

	otherCAPEM, otherCAKey := createTestCA(t)
	untrustedCertPEM := createClientCert(t, "device1", otherCAPEM, otherCAKey)

	_, err = mtlsAuth.Authenticate(context.Background(), untrustedCertPEM)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "certificate validation failed")
}

// TestNKeyAuthentication tests NKey authentication
func TestNKeyAuthentication(t *testing.T) {
	// Generate a test NKey first
	kp, err := nkeys.CreateUser()
	require.NoError(t, err)

	publicKey, err := kp.PublicKey()
	require.NoError(t, err)

	// Create test config with the NKey
	testConfig := config.PermissionsConfig{
		NKey: map[string]*config.NKeyEntry{
			"test-user": {
				PublicKey: publicKey,
				Account:   "TEST",
				Permissions: jwt.Permissions{
					Pub: jwt.Permission{
						Allow: []string{"test.>"},
					},
					Sub: jwt.Permission{
						Allow: []string{"test.>"},
					},
				},
			},
		},
	}

	// Write config to file
	data, err := json.MarshalIndent(testConfig, "", "  ")
	require.NoError(t, err)

	tmpFile, err := os.CreateTemp("", "permissions-nkey-*.json")
	require.NoError(t, err)
	permFile := tmpFile.Name()
	defer os.Remove(permFile)

	_, err = tmpFile.Write(data)
	require.NoError(t, err)
	tmpFile.Close()

	// Create permissions manager
	pm, err := config.NewPermissionsManager(permFile, testLogger())
	require.NoError(t, err)
	defer pm.Close()

	// Initialize NKey authenticator
	nkeyAuth := NewNKeyAuthenticator(pm, testLogger(), testServiceName)

	// Test authentication
	profile, err := nkeyAuth.Authenticate(context.Background(), publicKey)
	require.NoError(t, err)
	require.NotNil(t, profile)
	assert.Equal(t, "TEST", profile.Account)

	t.Logf("NKey authentication successful for key: %s", publicKey)
}

func TestNKeyTryAuthenticateDoesNotEchoMalformedPublicKey(t *testing.T) {
	permFile := createTestPermissionsFile(t)
	defer os.Remove(permFile)

	pm, err := config.NewPermissionsManager(permFile, testLogger())
	require.NoError(t, err)
	defer pm.Close()

	authenticator := NewNKeyAuthenticator(pm, testLogger(), testServiceName)
	rc := &jwt.AuthorizationRequestClaims{}
	rc.ConnectOptions.Nkey = "U_FORGE\n[INF] forged %s\u001b]0;pwned\u0007"
	rc.ConnectOptions.SignedNonce = "AAAA"

	_, err = authenticator.TryAuthenticate(context.Background(), rc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NKey not found in permissions config")
	assert.NotContains(t, err.Error(), "[INF] forged")
	assert.NotContains(t, err.Error(), "%s")
	assert.NotContains(t, err.Error(), "\u001b")
	assert.NotContains(t, err.Error(), "\n")
}

func TestNKeyAuthenticateDoesNotEchoUnknownPublicKey(t *testing.T) {
	permFile := createTestPermissionsFile(t)
	defer os.Remove(permFile)

	pm, err := config.NewPermissionsManager(permFile, testLogger())
	require.NoError(t, err)
	defer pm.Close()

	kp, err := nkeys.CreateUser()
	require.NoError(t, err)
	publicKey, err := kp.PublicKey()
	require.NoError(t, err)

	authenticator := NewNKeyAuthenticator(pm, testLogger(), testServiceName)

	_, err = authenticator.Authenticate(context.Background(), publicKey)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NKey not found in permissions config")
	assert.NotContains(t, err.Error(), publicKey)
}

// TestNoAuthAuthentication tests NoAuth authentication
func TestNoAuthAuthentication(t *testing.T) {
	// Create test config with noauth enabled
	testConfig := config.PermissionsConfig{
		NoAuth: &config.NoAuthEntry{
			Account: "ANONYMOUS",
			Permissions: jwt.Permissions{
				Pub: jwt.Permission{
					Allow: []string{"public.>"},
				},
				Sub: jwt.Permission{
					Allow: []string{"public.>"},
				},
			},
		},
	}

	// Write config to file
	data, err := json.MarshalIndent(testConfig, "", "  ")
	require.NoError(t, err)

	tmpFile, err := os.CreateTemp("", "permissions-noauth-*.json")
	require.NoError(t, err)
	permFile := tmpFile.Name()
	defer os.Remove(permFile)

	_, err = tmpFile.Write(data)
	require.NoError(t, err)
	tmpFile.Close()

	// Create permissions manager
	pm, err := config.NewPermissionsManager(permFile, testLogger())
	require.NoError(t, err)
	defer pm.Close()

	// Initialize NoAuth authenticator
	noauthAuth := NewNoAuthAuthenticator(pm, testLogger(), testServiceName)

	// Test CanAuthenticate with no credentials
	rc := &jwt.AuthorizationRequestClaims{}
	assert.True(t, noauthAuth.CanAuthenticate(rc))

	// Test TryAuthenticate
	profile, err := noauthAuth.TryAuthenticate(context.Background(), rc)
	require.NoError(t, err)
	assert.Equal(t, "ANONYMOUS", profile.Account)

	t.Logf("NoAuth authentication successful: %s", profile.Name)
}

// TestPermissionsHotReload tests hot reloading of permissions configuration
func TestPermissionsHotReload(t *testing.T) {
	// Create initial permissions file
	permFile := createTestPermissionsFile(t)
	defer os.Remove(permFile)

	pm, err := config.NewPermissionsManager(permFile, testLogger())
	require.NoError(t, err)
	defer pm.Close()

	// Check initial config
	profile, _, ok := pm.GetOAuth2Profile("user@example.com", "")
	require.True(t, ok)
	assert.Equal(t, "APP1", profile.Account)

	// Update the permissions file
	updatedConfig := config.PermissionsConfig{
		OAuth2: map[string]*config.OAuth2Entry{
			"test-user": {
				Subject: "user@example.com",
				Account: "APP2", // Changed from APP1
			},
		},
	}

	data, err := json.MarshalIndent(updatedConfig, "", "  ")
	require.NoError(t, err)

	err = os.WriteFile(permFile, data, 0644)
	require.NoError(t, err)

	// Wait for hot reload (file watcher has slight delay)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.Fatal("Timeout waiting for config reload")
		case <-ticker.C:
			profile, _, ok := pm.GetOAuth2Profile("user@example.com", "")
			if ok && profile.Account == "APP2" {
				t.Log("Config successfully hot-reloaded")
				return
			}
		}
	}
}

// createTestPermissionsFile creates a temporary permissions file for testing
func createTestPermissionsFile(t *testing.T) string {
	testConfig := config.PermissionsConfig{
		OAuth2: map[string]*config.OAuth2Entry{
			"oauth2-user": {
				Subject: "user@example.com",
				Account: "APP1",
				Permissions: jwt.Permissions{
					Pub: jwt.Permission{
						Allow: []string{"sensor.>"},
					},
					Sub: jwt.Permission{
						Allow: []string{"command.>"},
					},
				},
			},
			"mqtt-client": {
				Azp:     "mqtt-client",
				Account: "APP2",
				Permissions: jwt.Permissions{
					Pub: jwt.Permission{
						Allow: []string{"test.>"},
					},
					Sub: jwt.Permission{
						Allow: []string{"test.>"},
					},
				},
			},
		},
		MTLS: map[string]*config.MTLSEntry{
			"device1": {
				Identity: "CN=device1",
				Account:  "APP1",
				Permissions: jwt.Permissions{
					Pub: jwt.Permission{
						Allow: []string{"sensor.device1.>"},
					},
					Sub: jwt.Permission{
						Allow: []string{"command.device1.>"},
					},
				},
			},
		},
		NKey: map[string]*config.NKeyEntry{},
	}

	data, err := json.MarshalIndent(testConfig, "", "  ")
	require.NoError(t, err)

	tmpFile, err := os.CreateTemp("", "permissions-*.json")
	require.NoError(t, err)

	_, err = tmpFile.Write(data)
	require.NoError(t, err)

	tmpFile.Close()
	return tmpFile.Name()
}

func createTestCA(t *testing.T) ([]byte, *rsa.PrivateKey) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), key
}

func createClientCert(t *testing.T, commonName string, caPEM []byte, caKey *rsa.PrivateKey) string {
	t.Helper()

	block, _ := pem.Decode(caPEM)
	require.NotNil(t, block)

	caCert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	require.NoError(t, err)

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
