// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package appconfig

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/knadh/koanf/maps"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/cobra"
	yamlv3 "go.yaml.in/yaml/v3"
)

const envPrefix = "AUTH_CALLOUT_"

// Manager loads service configuration from defaults, files, hot config, and env vars.
type Manager struct {
	defaultYAML string
	configPath  string
	k           *koanf.Koanf
}

// New creates a configuration manager using embedded default YAML.
func New(defaultYAML string) *Manager {
	return &Manager{
		defaultYAML: defaultYAML,
		k:           koanf.New("."),
	}
}

// BindFlags adds common config flags to the root command.
func (m *Manager) BindFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&m.configPath, "config", "", "path to service configuration YAML")
}

// Load reads and merges all configured sources.
func (m *Manager) Load() error {
	k := koanf.New(".")
	if err := k.Load(rawbytes.Provider([]byte(m.defaultYAML)), yaml.Parser()); err != nil {
		return fmt.Errorf("load defaults: %w", err)
	}

	configPath := firstNonEmpty(m.configPath, os.Getenv(envPrefix+"CONFIG"), os.Getenv(envPrefix+"CONFIG_FILE"))
	if configPath != "" {
		if err := loadOptionalFile(k, configPath, false); err != nil {
			return err
		}
	}

	if hotConfig := os.Getenv(envPrefix + "HOT_CONFIG"); hotConfig != "" {
		if err := loadOptionalFile(k, hotConfig, true); err != nil {
			return err
		}
	}

	if err := k.Load(env.Provider(envPrefix, ".", envKey), nil); err != nil {
		return fmt.Errorf("load environment: %w", err)
	}

	applyAliases(k)
	m.k = k
	return nil
}

// Unmarshal decodes the loaded configuration into target.
func (m *Manager) Unmarshal(target any) error {
	return m.k.Unmarshal("", target)
}

// ExportYAML returns the merged configuration as YAML.
func (m *Manager) ExportYAML() ([]byte, error) {
	return yamlv3.Marshal(maps.Unflatten(m.k.All(), "."))
}

func loadOptionalFile(k *koanf.Koanf, path string, optional bool) error {
	if _, err := os.Stat(path); err != nil {
		if optional && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat config file %q: %w", path, err)
	}
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return fmt.Errorf("load config file %q: %w", path, err)
	}
	return nil
}

func envKey(s string) string {
	key := strings.TrimPrefix(s, envPrefix)
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "__", "-")
	return strings.ReplaceAll(key, "_", ".")
}

func applyAliases(k *koanf.Koanf) {
	setString(k, "nats.url", "NATS_URL", envPrefix+"NATS_URL")
	setString(k, "nats.nkey-seed", "NATS_NKEY_SEED", envPrefix+"NATS_NKEY_SEED")
	setString(k, "nats.issuer-seed", "NATS_ISSUER_SEED", envPrefix+"NATS_ISSUER_SEED")
	setString(k, "nats.xkey-seed", "NATS_XKEY_SEED", envPrefix+"NATS_XKEY_SEED")
	setString(k, "jwks.url", "JWKS_URL", envPrefix+"JWKS_URL")
	setString(k, "jwks.issuer", "JWKS_ISSUER", envPrefix+"JWKS_ISSUER")
	setString(k, "jwks.audience", "JWKS_AUDIENCE", envPrefix+"JWKS_AUDIENCE")
	setStringSlice(k, "jwks.signing-algorithms", "JWKS_SIGNING_ALGORITHMS", envPrefix+"JWKS_SIGNING_ALGORITHMS")
	setString(k, "mtls.ca-path", "MTLS_CA_PATH", envPrefix+"MTLS_CA_PATH")
	setString(k, "permissions.file", "PERMISSIONS_FILE", envPrefix+"PERMISSIONS_FILE")
	setString(k, "observability.telemetry.service-name", envPrefix+"SERVICE_NAME")
	setInt(k, "host.port", envPrefix+"HOST_PORT", envPrefix+"SERVICE_SERVER_PORT")
}

func setString(k *koanf.Koanf, key string, names ...string) {
	if value := envValue(names...); value != "" {
		k.Set(key, value)
	}
}

func setStringSlice(k *koanf.Koanf, key string, names ...string) {
	if value := envValue(names...); value != "" {
		k.Set(key, strings.Split(value, ","))
	}
}

func setInt(k *koanf.Koanf, key string, names ...string) {
	value := envValue(names...)
	if value == "" {
		return
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		k.Set(key, value)
		return
	}
	k.Set(key, parsed)
}

func envValue(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
