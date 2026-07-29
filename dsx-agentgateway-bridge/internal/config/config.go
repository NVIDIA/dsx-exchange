// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/nats-io/nats.go"

	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/shardbus"
)

const (
	EnvBridgeRole                = "BRIDGE_ROLE"
	EnvHealthAddr                = "HEALTH_ADDR"
	EnvHTTPRequestTimeout        = "HTTP_REQUEST_TIMEOUT"
	EnvHTTPWriteTimeout          = "HTTP_WRITE_TIMEOUT"
	EnvShardID                   = "SHARD_ID"
	EnvLocalGatewayOrigin        = "LOCAL_GATEWAY_ORIGIN"
	EnvNATSAuthMode              = "NATS_AUTH_MODE"
	EnvNATSOAuthIssuer           = "NATS_OAUTH_ISSUER"
	EnvNATSOAuthClientID         = "NATS_OAUTH_CLIENT_ID"
	EnvNATSOAuthClientIDFile     = "NATS_OAUTH_CLIENT_ID_FILE"
	EnvNATSOAuthClientSecret     = "NATS_OAUTH_CLIENT_SECRET"
	EnvNATSOAuthClientSecretFile = "NATS_OAUTH_CLIENT_SECRET_FILE"
	EnvNATSOAuthScope            = "NATS_OAUTH_SCOPE"
	EnvNATSTLSCAFile             = "NATS_TLS_CA_FILE"
	EnvNATSTLSEnabled            = "NATS_TLS_ENABLED"
	EnvNATSTLSServerName         = "NATS_TLS_SERVER_NAME"
	EnvNATSURL                   = "NATS_URL"
	EnvRequestTimeout            = "REQUEST_TIMEOUT"
	EnvSubjectPrefix             = "SUBJECT_PREFIX"

	NATSAuthModeNoAuth NATSAuthMode = "noauth"
	NATSAuthModeOAuth  NATSAuthMode = "oauth"

	RoleHub  Role = "hub"
	RoleLeaf Role = "leaf"

	defaultSubjectPrefix = "dsx.agentgateway.bridge.v1"

	defaultHubListenAddr      = ":3001"
	defaultLeafHealthAddr     = ":3001"
	defaultRequestTimeout     = 3 * time.Second
	defaultDiscoveryTimeout   = 2 * time.Second
	defaultDiscoveryRefresh   = 30 * time.Second
	defaultHTTPRequestTimeout = 5 * time.Minute
	defaultHTTPWriteTimeout   = 30 * time.Second
)

type Role string

type NATSAuthMode string

type NATSConfig struct {
	AuthMode NATSAuthMode
	OAuth    NATSOAuthConfig
	TLS      NATSTLSConfig
}

type NATSOAuthConfig struct {
	Issuer           string
	ClientID         string
	ClientIDFile     string
	ClientSecret     string
	ClientSecretFile string
	Scope            string
}

type NATSOAuthSettings struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	Scope        string
}

type NATSTLSConfig struct {
	Enabled    bool
	ServerName string
	CAFile     string
}

type Hub struct {
	ListenAddr         string
	NATSURL            string
	NATSOptions        []nats.Option
	SubjectPrefix      string
	Timeout            time.Duration
	HTTPRequestTimeout time.Duration
	HTTPWriteTimeout   time.Duration
	DiscoveryTimeout   time.Duration
	DiscoveryRefresh   time.Duration
	Bus                shardbus.Requester
	Ready              func() bool
}

type Leaf struct {
	HealthAddr         string
	NATSURL            string
	NATSOptions        []nats.Option
	SubjectPrefix      string
	ShardID            string
	LocalGatewayOrigin string
	Timeout            time.Duration
}

func NormalizeHub(cfg Hub) (Hub, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = defaultHubListenAddr
	}
	if cfg.NATSURL == "" {
		return cfg, fmt.Errorf("%s is required", EnvNATSURL)
	}
	if cfg.SubjectPrefix == "" {
		cfg.SubjectPrefix = defaultSubjectPrefix
	}
	if !ValidSubjectPrefix(cfg.SubjectPrefix) {
		return cfg, fmt.Errorf("invalid %s %q", EnvSubjectPrefix, cfg.SubjectPrefix)
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultRequestTimeout
	} else if cfg.Timeout < 0 {
		return cfg, fmt.Errorf("%s must be positive", EnvRequestTimeout)
	}
	if cfg.HTTPRequestTimeout == 0 {
		cfg.HTTPRequestTimeout = defaultHTTPRequestTimeout
	} else if cfg.HTTPRequestTimeout < 0 {
		return cfg, fmt.Errorf("%s must be positive", EnvHTTPRequestTimeout)
	}
	if cfg.HTTPWriteTimeout == 0 {
		cfg.HTTPWriteTimeout = defaultHTTPWriteTimeout
	} else if cfg.HTTPWriteTimeout < 0 {
		return cfg, fmt.Errorf("%s must be positive", EnvHTTPWriteTimeout)
	}
	if cfg.DiscoveryTimeout == 0 {
		cfg.DiscoveryTimeout = defaultDiscoveryTimeout
	} else if cfg.DiscoveryTimeout < 0 {
		return cfg, errors.New("discovery timeout must be positive")
	}
	if cfg.DiscoveryRefresh == 0 {
		cfg.DiscoveryRefresh = defaultDiscoveryRefresh
	} else if cfg.DiscoveryRefresh < 0 {
		return cfg, errors.New("discovery refresh must be positive")
	}
	return cfg, nil
}

func NormalizeLeaf(cfg Leaf) (Leaf, error) {
	if cfg.HealthAddr == "" {
		cfg.HealthAddr = defaultLeafHealthAddr
	}
	if cfg.NATSURL == "" {
		return cfg, fmt.Errorf("%s is required", EnvNATSURL)
	}
	if cfg.SubjectPrefix == "" {
		cfg.SubjectPrefix = defaultSubjectPrefix
	}
	if !ValidSubjectPrefix(cfg.SubjectPrefix) {
		return cfg, fmt.Errorf("invalid %s %q", EnvSubjectPrefix, cfg.SubjectPrefix)
	}
	if !ValidShardID(cfg.ShardID) {
		return cfg, fmt.Errorf("invalid %s %q", EnvShardID, cfg.ShardID)
	}
	if cfg.LocalGatewayOrigin == "" {
		return cfg, fmt.Errorf("%s is required", EnvLocalGatewayOrigin)
	}
	gatewayURL, err := url.Parse(cfg.LocalGatewayOrigin)
	if err != nil || gatewayURL.Opaque != "" || gatewayURL.Host == "" ||
		(gatewayURL.Scheme != "http" && gatewayURL.Scheme != "https") {
		return cfg, fmt.Errorf("%s must be an absolute HTTP or HTTPS URL", EnvLocalGatewayOrigin)
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultRequestTimeout
	} else if cfg.Timeout < 0 {
		return cfg, fmt.Errorf("%s must be positive", EnvRequestTimeout)
	}
	return cfg, nil
}

func ValidBridgeRole(raw string) bool {
	return raw == string(RoleHub) || raw == string(RoleLeaf)
}

func ValidShardID(id string) bool {
	return id != "" && !strings.ContainsAny(id, ".*>\x00") && !strings.ContainsFunc(id, unicode.IsSpace)
}

func ValidSubjectPrefix(prefix string) bool {
	// Keep prefixes as literal NATS subject tokens. Do not trim or normalize:
	// a configured typo should fail instead of silently changing routing.
	if prefix == "" || len(prefix) >= 256 || strings.HasPrefix(prefix, ".") || strings.HasSuffix(prefix, ".") || strings.Contains(prefix, "..") {
		return false
	}
	for token := range strings.SplitSeq(prefix, ".") {
		if token == "" {
			return false
		}
		for _, r := range token {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
				continue
			}
			return false
		}
	}
	return true
}

func positiveDurationEnv(key string) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q is not a duration: %w", key, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return d, nil
}

func loadNATSConfig() (NATSConfig, error) {
	tlsEnabled, err := natsTLSEnabled()
	if err != nil {
		return NATSConfig{}, err
	}
	cfg := NATSConfig{
		AuthMode: NATSAuthMode(os.Getenv(EnvNATSAuthMode)),
		OAuth: NATSOAuthConfig{
			Issuer:           os.Getenv(EnvNATSOAuthIssuer),
			ClientID:         os.Getenv(EnvNATSOAuthClientID),
			ClientIDFile:     os.Getenv(EnvNATSOAuthClientIDFile),
			ClientSecret:     os.Getenv(EnvNATSOAuthClientSecret),
			ClientSecretFile: os.Getenv(EnvNATSOAuthClientSecretFile),
			Scope:            os.Getenv(EnvNATSOAuthScope),
		},
		TLS: NATSTLSConfig{
			Enabled:    tlsEnabled,
			ServerName: os.Getenv(EnvNATSTLSServerName),
			CAFile:     os.Getenv(EnvNATSTLSCAFile),
		},
	}
	switch cfg.AuthMode {
	case NATSAuthModeNoAuth:
	case NATSAuthModeOAuth:
		if _, err := cfg.OAuth.CurrentSettings(); err != nil {
			return NATSConfig{}, err
		}
	default:
		return NATSConfig{}, fmt.Errorf("%s must be exactly %q or %q", EnvNATSAuthMode, NATSAuthModeNoAuth, NATSAuthModeOAuth)
	}
	if cfg.TLS.CAFile != "" {
		if _, err := readRequiredFile(EnvNATSTLSCAFile, cfg.TLS.CAFile); err != nil {
			return NATSConfig{}, err
		}
	}
	return cfg, nil
}

func (cfg NATSOAuthConfig) CurrentSettings() (NATSOAuthSettings, error) {
	issuer, err := requiredConfigValue(EnvNATSOAuthIssuer, cfg.Issuer)
	if err != nil {
		return NATSOAuthSettings{}, err
	}
	clientID, err := requiredReloadableConfigValue(EnvNATSOAuthClientID, EnvNATSOAuthClientIDFile, cfg.ClientID, cfg.ClientIDFile)
	if err != nil {
		return NATSOAuthSettings{}, err
	}
	clientSecret, err := requiredReloadableConfigValue(EnvNATSOAuthClientSecret, EnvNATSOAuthClientSecretFile, cfg.ClientSecret, cfg.ClientSecretFile)
	if err != nil {
		return NATSOAuthSettings{}, err
	}
	scope, err := requiredConfigValue(EnvNATSOAuthScope, cfg.Scope)
	if err != nil {
		return NATSOAuthSettings{}, err
	}
	return NATSOAuthSettings{
		Issuer:       issuer,
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scope:        scope,
	}, nil
}

func requiredConfigValue(name, value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("%s=%s requires %s", EnvNATSAuthMode, NATSAuthModeOAuth, name)
	}
	return value, nil
}

func requiredReloadableConfigValue(name, fileName, value, path string) (string, error) {
	if path != "" {
		b, err := readRequiredFile(fileName, path)
		if err != nil {
			return "", err
		}
		fileValue := string(b)
		if fileValue != strings.TrimSpace(fileValue) {
			return "", fmt.Errorf("%s %s has surrounding whitespace", fileName, path)
		}
		return fileValue, nil
	}
	if value == "" {
		return "", fmt.Errorf("%s=%s requires %s or %s", EnvNATSAuthMode, NATSAuthModeOAuth, name, fileName)
	}
	return value, nil
}

func natsTLSEnabled() (bool, error) {
	raw := os.Getenv(EnvNATSTLSEnabled)
	switch raw {
	case "":
		return false, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be exactly %q or %q", EnvNATSTLSEnabled, "true", "false")
	}
}

func readRequiredFile(name, path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s %s: %w", name, path, err)
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("%s %s is empty", name, path)
	}
	return b, nil
}

func RoleFromArgs(args []string) (Role, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("usage: bridge [hub|leaf]")
	}
	if len(args) == 1 {
		return ParseRole(args[0])
	}
	return ParseRole(os.Getenv(EnvBridgeRole))
}

func ParseRole(raw string) (Role, error) {
	if !ValidBridgeRole(raw) {
		return "", fmt.Errorf("bridge role must be exactly %q or %q", RoleHub, RoleLeaf)
	}
	return Role(raw), nil
}

func LoadHub() (Hub, error) {
	timeout, err := positiveDurationEnv(EnvRequestTimeout)
	if err != nil {
		return Hub{}, fmt.Errorf("%s is invalid: %w", EnvRequestTimeout, err)
	}
	httpWriteTimeout, err := positiveDurationEnv(EnvHTTPWriteTimeout)
	if err != nil {
		return Hub{}, fmt.Errorf("%s is invalid: %w", EnvHTTPWriteTimeout, err)
	}
	httpRequestTimeout, err := positiveDurationEnv(EnvHTTPRequestTimeout)
	if err != nil {
		return Hub{}, fmt.Errorf("%s is invalid: %w", EnvHTTPRequestTimeout, err)
	}
	natsConfig, err := loadNATSConfig()
	if err != nil {
		return Hub{}, fmt.Errorf("load NATS config: %w", err)
	}
	opts, err := NATSOptions("dsx-agentgateway-bridge-hub", natsConfig)
	if err != nil {
		return Hub{}, fmt.Errorf("configure NATS client: %w", err)
	}
	return NormalizeHub(Hub{
		NATSURL:            os.Getenv(EnvNATSURL),
		NATSOptions:        opts,
		SubjectPrefix:      os.Getenv(EnvSubjectPrefix),
		Timeout:            timeout,
		HTTPRequestTimeout: httpRequestTimeout,
		HTTPWriteTimeout:   httpWriteTimeout,
	})
}

func LoadLeaf() (Leaf, error) {
	timeout, err := positiveDurationEnv(EnvRequestTimeout)
	if err != nil {
		return Leaf{}, fmt.Errorf("%s is invalid: %w", EnvRequestTimeout, err)
	}
	natsConfig, err := loadNATSConfig()
	if err != nil {
		return Leaf{}, fmt.Errorf("load NATS config: %w", err)
	}
	opts, err := NATSOptions("dsx-agentgateway-bridge-leaf", natsConfig)
	if err != nil {
		return Leaf{}, fmt.Errorf("configure NATS client: %w", err)
	}
	return NormalizeLeaf(Leaf{
		HealthAddr:         os.Getenv(EnvHealthAddr),
		NATSURL:            os.Getenv(EnvNATSURL),
		NATSOptions:        opts,
		SubjectPrefix:      os.Getenv(EnvSubjectPrefix),
		ShardID:            os.Getenv(EnvShardID),
		LocalGatewayOrigin: os.Getenv(EnvLocalGatewayOrigin),
		Timeout:            timeout,
	})
}
