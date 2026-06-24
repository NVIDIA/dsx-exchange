// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/require"

	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/appconfig"
)

func TestRunServerDoesNotLogConfiguredNATSSeeds(t *testing.T) {
	permissionsFile := t.TempDir() + "/permissions.json"
	require.NoError(t, os.WriteFile(permissionsFile, []byte(`{}`), 0o600))

	natsKey := mustSeed(t, nkeys.CreateUser)
	issuerKey := mustSeed(t, nkeys.CreateAccount)
	xKey := mustSeed(t, nkeys.CreateCurveKeys)

	t.Setenv("AUTH_CALLOUT_NATS_NKEY_SEED", natsKey)
	t.Setenv("AUTH_CALLOUT_NATS_ISSUER_SEED", issuerKey)
	t.Setenv("AUTH_CALLOUT_NATS_XKEY_SEED", xKey)
	// Use malformed syntax so startup fails synchronously; valid unavailable URLs now reconnect.
	t.Setenv("AUTH_CALLOUT_NATS_URL", "nats://%zz")
	t.Setenv("AUTH_CALLOUT_PERMISSIONS_FILE", permissionsFile)
	t.Setenv("AUTH_CALLOUT_OBSERVABILITY_METRICS_ENABLED", "false")
	t.Setenv("AUTH_CALLOUT_OBSERVABILITY_TRACING_ENABLED", "false")

	logs := captureStderr(t, func() {
		globalConfig = appconfig.New(defaultConfigYAML)
		err := runServer(nil, nil)
		require.ErrorContains(t, err, "error connecting to NATS")
	})

	for _, seed := range []string{natsKey, issuerKey, xKey} {
		require.NotContains(t, logs, seed)
	}
}

func TestDefaultConfigIncludesJWKSSigningAlgorithms(t *testing.T) {
	// Load overlays env and config-file sources after defaultConfigYAML; clear
	// those inputs so this test only proves the embedded app defaults.
	t.Setenv("AUTH_CALLOUT_CONFIG", "")
	t.Setenv("AUTH_CALLOUT_CONFIG_FILE", "")
	t.Setenv("AUTH_CALLOUT_HOT_CONFIG", "")
	t.Setenv("AUTH_CALLOUT_JWKS_SIGNING_ALGORITHMS", "")
	t.Setenv("JWKS_SIGNING_ALGORITHMS", "")

	manager := appconfig.New(defaultConfigYAML)
	require.NoError(t, manager.Load())

	type jwksConfig struct {
		JWKS struct {
			SigningAlgorithms []string `koanf:"signing-algorithms"`
		} `koanf:"jwks"`
	}
	var config jwksConfig
	require.NoError(t, manager.Unmarshal(&config))
	require.Equal(t, []string{"RS256"}, config.JWKS.SigningAlgorithms)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	originalStderr := os.Stderr
	reader, writer, err := os.Pipe()
	require.NoError(t, err)

	os.Stderr = writer
	defer func() {
		os.Stderr = originalStderr
	}()

	fn()
	require.NoError(t, writer.Close())

	output, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())

	return string(output)
}

func mustSeed(t *testing.T, create func() (nkeys.KeyPair, error)) string {
	t.Helper()

	keyPair, err := create()
	require.NoError(t, err)

	seed, err := keyPair.Seed()
	require.NoError(t, err)

	return strings.Clone(string(seed))
}
