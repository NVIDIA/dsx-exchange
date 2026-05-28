// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

func TestPermissionsManagerReloadsKubernetesConfigMapVolume(t *testing.T) {
	volumeDir := t.TempDir()
	permissionsFile := filepath.Join(volumeDir, "permissions.json")

	writeKubernetesConfigMapVersion(t, volumeDir, "2026_05_27_00_00_00.000000001", "APP1")

	pm, err := NewPermissionsManager(permissionsFile, testLogger())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pm.Close())
	})

	requireNoAuthAccount(t, pm, "APP1")

	writeKubernetesConfigMapVersion(t, volumeDir, "2026_05_27_00_00_01.000000002", "APP2")

	require.Eventually(t, func() bool {
		profile, ok := pm.GetNoAuthProfile()
		return ok && profile.Account == "APP2"
	}, 10*time.Second, 100*time.Millisecond)
}

func testLogger() *otelzap.Logger {
	return otelzap.New(zap.NewNop())
}

func writeKubernetesConfigMapVersion(t *testing.T, volumeDir, version, account string) {
	t.Helper()

	versionName := ".." + version
	versionDir := filepath.Join(volumeDir, versionName)
	require.NoError(t, os.Mkdir(versionDir, 0o755))
	writeNoAuthPermissions(t, filepath.Join(versionDir, "permissions.json"), account)

	tmpLink := filepath.Join(volumeDir, "..data_tmp")
	require.NoError(t, os.RemoveAll(tmpLink))
	require.NoError(t, os.Symlink(versionName, tmpLink))
	require.NoError(t, os.Rename(tmpLink, filepath.Join(volumeDir, "..data")))

	permissionsLink := filepath.Join(volumeDir, "permissions.json")
	if _, err := os.Lstat(permissionsLink); os.IsNotExist(err) {
		require.NoError(t, os.Symlink(filepath.Join("..data", "permissions.json"), permissionsLink))
	} else {
		require.NoError(t, err)
	}
}

func writeNoAuthPermissions(t *testing.T, path, account string) {
	t.Helper()

	data, err := json.Marshal(PermissionsConfig{
		NoAuth: &NoAuthEntry{
			Account: account,
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

func requireNoAuthAccount(t *testing.T, pm *PermissionsManager, account string) {
	t.Helper()

	profile, ok := pm.GetNoAuthProfile()
	require.True(t, ok)
	require.Equal(t, account, profile.Account)
}
