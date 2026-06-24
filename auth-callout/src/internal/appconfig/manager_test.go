// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package appconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadAllowsNATSSeedsInHotConfigAndEnvironment(t *testing.T) {
	hotConfigPath := writeConfigFile(t, "config-secrets.yaml", `
nats:
  nkey-seed: SUFAKE
  issuer-seed: SAFAKE
  xkey-seed: SXFAKE
`)
	t.Setenv("AUTH_CALLOUT_HOT_CONFIG", hotConfigPath)
	t.Setenv("AUTH_CALLOUT_NATS_ISSUER_SEED", "SAFROMENV")

	manager := New("")
	require.NoError(t, manager.Load())

	type natsConfig struct {
		NATS struct {
			NKeySeed   string `koanf:"nkey-seed"`
			IssuerSeed string `koanf:"issuer-seed"`
			XKeySeed   string `koanf:"xkey-seed"`
		} `koanf:"nats"`
	}
	var config natsConfig
	require.NoError(t, manager.Unmarshal(&config))
	require.Equal(t, "SUFAKE", config.NATS.NKeySeed)
	require.Equal(t, "SAFROMENV", config.NATS.IssuerSeed)
	require.Equal(t, "SXFAKE", config.NATS.XKeySeed)
}

func TestLoadAllowsJWKSAlgorithmsInEnvironment(t *testing.T) {
	t.Setenv("AUTH_CALLOUT_JWKS_SIGNING_ALGORITHMS", "RS256,ES256")

	manager := New("")
	require.NoError(t, manager.Load())

	type jwksConfig struct {
		JWKS struct {
			SigningAlgorithms []string `koanf:"signing-algorithms"`
		} `koanf:"jwks"`
	}
	var config jwksConfig
	require.NoError(t, manager.Unmarshal(&config))
	require.Equal(t, []string{"RS256", "ES256"}, config.JWKS.SigningAlgorithms)
}

func writeConfigFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
