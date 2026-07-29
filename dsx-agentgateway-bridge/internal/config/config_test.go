// Copyright 2026 NVIDIA Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"strings"
	"testing"
	"time"
)

func TestRoleFromArgsUsesArg(t *testing.T) {
	t.Setenv(EnvBridgeRole, "leaf")

	got, err := RoleFromArgs([]string{"hub"})
	if err != nil {
		t.Fatalf("RoleFromArgs: %v", err)
	}
	if got != RoleHub {
		t.Fatalf("RoleFromArgs() = %q, want %q", got, RoleHub)
	}
}

func TestRoleFromArgsUsesEnvWhenArgMissing(t *testing.T) {
	t.Setenv(EnvBridgeRole, "leaf")

	got, err := RoleFromArgs(nil)
	if err != nil {
		t.Fatalf("RoleFromArgs: %v", err)
	}
	if got != RoleLeaf {
		t.Fatalf("RoleFromArgs() = %q, want %q", got, RoleLeaf)
	}
}

func TestRoleFromArgsRejectsMissingRole(t *testing.T) {
	t.Setenv(EnvBridgeRole, "")

	if _, err := RoleFromArgs(nil); err == nil {
		t.Fatalf("RoleFromArgs accepted missing role")
	}
}

func TestRoleFromArgsRejectsMultipleArgs(t *testing.T) {
	if _, err := RoleFromArgs([]string{"hub", "leaf"}); err == nil {
		t.Fatalf("RoleFromArgs accepted multiple args")
	}
}

func TestParseRoleAcceptsHub(t *testing.T) {
	got, err := ParseRole("hub")
	if err != nil {
		t.Fatalf("ParseRole: %v", err)
	}
	if got != RoleHub {
		t.Fatalf("ParseRole() = %q, want %q", got, RoleHub)
	}
}

func TestParseRoleAcceptsLeaf(t *testing.T) {
	got, err := ParseRole("leaf")
	if err != nil {
		t.Fatalf("ParseRole: %v", err)
	}
	if got != RoleLeaf {
		t.Fatalf("ParseRole() = %q, want %q", got, RoleLeaf)
	}
}

func TestParseRoleRejectsNonExactValues(t *testing.T) {
	for _, raw := range []string{"", " hub", "hub "} {
		if _, err := ParseRole(raw); err == nil {
			t.Fatalf("ParseRole(%q) succeeded", raw)
		}
	}
}

func TestLoadHubLoadsRuntimeConfig(t *testing.T) {
	setNoAuth(t)
	t.Setenv(EnvNATSURL, "nats://example:4222")
	t.Setenv(EnvSubjectPrefix, "dsx.test")
	t.Setenv(EnvRequestTimeout, "250ms")
	t.Setenv(EnvHTTPRequestTimeout, "5s")
	t.Setenv(EnvHTTPWriteTimeout, "750ms")

	cfg, err := LoadHub()
	if err != nil {
		t.Fatalf("LoadHub: %v", err)
	}
	if cfg.SubjectPrefix != "dsx.test" {
		t.Fatalf("SubjectPrefix = %q, want dsx.test", cfg.SubjectPrefix)
	}
	if cfg.Timeout.String() != "250ms" {
		t.Fatalf("Timeout = %s, want 250ms", cfg.Timeout)
	}
	if cfg.HTTPRequestTimeout != 5*time.Second {
		t.Fatalf("HTTPRequestTimeout = %s, want 5s", cfg.HTTPRequestTimeout)
	}
	if cfg.HTTPWriteTimeout != 750*time.Millisecond {
		t.Fatalf("HTTPWriteTimeout = %s, want 750ms", cfg.HTTPWriteTimeout)
	}
}

func TestLoadHubRequiresNATSURL(t *testing.T) {
	setNoAuth(t)
	t.Setenv(EnvNATSURL, "")

	if _, err := LoadHub(); err == nil {
		t.Fatalf("LoadHub accepted missing NATS URL")
	}
}

func TestLoadHubRejectsInvalidWriteTimeout(t *testing.T) {
	for _, value := range []string{"invalid", "0s", "-1s"} {
		t.Run(value, func(t *testing.T) {
			setNoAuth(t)
			t.Setenv(EnvNATSURL, "nats://example:4222")
			t.Setenv(EnvHTTPWriteTimeout, value)
			if _, err := LoadHub(); err == nil {
				t.Fatalf("LoadHub accepted HTTP write timeout %q", value)
			}
		})
	}
}

func TestLoadHubRejectsInvalidHTTPRequestTimeout(t *testing.T) {
	for _, value := range []string{"invalid", "0s", "-1s"} {
		t.Run(value, func(t *testing.T) {
			setNoAuth(t)
			t.Setenv(EnvNATSURL, "nats://example:4222")
			t.Setenv(EnvHTTPRequestTimeout, value)
			if _, err := LoadHub(); err == nil {
				t.Fatalf("LoadHub accepted HTTP request timeout %q", value)
			}
		})
	}
}

func TestLoadHubAndLeafRejectInvalidRequestTimeout(t *testing.T) {
	for _, value := range []string{"invalid", "0s", "-1s"} {
		t.Run(value, func(t *testing.T) {
			setNoAuth(t)
			t.Setenv(EnvNATSURL, "nats://example:4222")
			t.Setenv(EnvShardID, "cpc-1")
			t.Setenv(EnvLocalGatewayOrigin, "http://gateway")
			t.Setenv(EnvRequestTimeout, value)
			if _, err := LoadHub(); err == nil {
				t.Fatalf("LoadHub accepted request timeout %q", value)
			}
			if _, err := LoadLeaf(); err == nil {
				t.Fatalf("LoadLeaf accepted request timeout %q", value)
			}
		})
	}
}

func TestLoadLeafRequiresLocalGatewayOrigin(t *testing.T) {
	setNoAuth(t)
	t.Setenv(EnvNATSURL, "nats://example:4222")
	t.Setenv(EnvShardID, "cpc-1")
	t.Setenv(EnvLocalGatewayOrigin, "")

	if _, err := LoadLeaf(); err == nil {
		t.Fatalf("LoadLeaf accepted missing local gateway origin")
	}
}

func TestNormalizeLeafRejectsInvalidLocalGatewayOrigin(t *testing.T) {
	t.Parallel()

	for _, origin := range []string{
		"gateway", "http://", "http:gateway/mcp", "ftp://gateway/mcp",
		" http://gateway", "http://gateway ",
	} {
		if _, err := NormalizeLeaf(Leaf{
			NATSURL:            "nats://example:4222",
			ShardID:            "cpc-1",
			LocalGatewayOrigin: origin,
		}); err == nil {
			t.Fatalf("NormalizeLeaf accepted local gateway origin %q", origin)
		}
	}
}

func TestNormalizeHubAppliesDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := NormalizeHub(Hub{
		NATSURL: "nats://example:4222",
	})
	if err != nil {
		t.Fatalf("NormalizeHub: %v", err)
	}
	if cfg.ListenAddr != defaultHubListenAddr {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, defaultHubListenAddr)
	}
	if cfg.SubjectPrefix != defaultSubjectPrefix {
		t.Fatalf("SubjectPrefix = %q, want %q", cfg.SubjectPrefix, defaultSubjectPrefix)
	}
	if cfg.Timeout != defaultRequestTimeout {
		t.Fatalf("Timeout = %s, want %s", cfg.Timeout, defaultRequestTimeout)
	}
	if cfg.HTTPWriteTimeout != defaultHTTPWriteTimeout {
		t.Fatalf("HTTPWriteTimeout = %s, want %s", cfg.HTTPWriteTimeout, defaultHTTPWriteTimeout)
	}
	if cfg.HTTPRequestTimeout != defaultHTTPRequestTimeout {
		t.Fatalf("HTTPRequestTimeout = %s, want %s", cfg.HTTPRequestTimeout, defaultHTTPRequestTimeout)
	}
	if cfg.DiscoveryTimeout != defaultDiscoveryTimeout {
		t.Fatalf("DiscoveryTimeout = %s, want %s", cfg.DiscoveryTimeout, defaultDiscoveryTimeout)
	}
	if cfg.DiscoveryRefresh != defaultDiscoveryRefresh {
		t.Fatalf("DiscoveryRefresh = %s, want %s", cfg.DiscoveryRefresh, defaultDiscoveryRefresh)
	}
}

func TestNormalizeHubRequiresNATSURL(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeHub(Hub{}); err == nil {
		t.Fatalf("NormalizeHub accepted missing NATS URL")
	}
}

func TestNormalizeHubAndLeafRejectNegativeDurations(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		cfg  Hub
	}{
		{name: "hub request", cfg: Hub{Timeout: -time.Second}},
		{name: "HTTP request", cfg: Hub{HTTPRequestTimeout: -time.Second}},
		{name: "HTTP write", cfg: Hub{HTTPWriteTimeout: -time.Second}},
		{name: "discovery request", cfg: Hub{DiscoveryTimeout: -time.Second}},
		{name: "discovery refresh", cfg: Hub{DiscoveryRefresh: -time.Second}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.cfg.NATSURL = "nats://example:4222"
			if _, err := NormalizeHub(test.cfg); err == nil {
				t.Fatal("negative duration was accepted")
			}
		})
	}
	if _, err := NormalizeLeaf(Leaf{NATSURL: "nats://example:4222", ShardID: "cpc-1", LocalGatewayOrigin: "http://gateway", Timeout: -time.Second}); err == nil {
		t.Fatal("negative leaf request duration was accepted")
	}
}

func TestNormalizeHubPreservesConfiguredValues(t *testing.T) {
	t.Parallel()

	cfg, err := NormalizeHub(Hub{
		ListenAddr:         ":9999",
		NATSURL:            "nats://example:4222",
		SubjectPrefix:      "custom.bridge",
		Timeout:            time.Second,
		HTTPRequestTimeout: 3 * time.Second,
		HTTPWriteTimeout:   2 * time.Second,
		DiscoveryTimeout:   500 * time.Millisecond,
		DiscoveryRefresh:   750 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NormalizeHub: %v", err)
	}
	if cfg.ListenAddr != ":9999" || cfg.NATSURL != "nats://example:4222" || cfg.SubjectPrefix != "custom.bridge" || cfg.Timeout != time.Second || cfg.HTTPRequestTimeout != 3*time.Second || cfg.HTTPWriteTimeout != 2*time.Second || cfg.DiscoveryTimeout != 500*time.Millisecond || cfg.DiscoveryRefresh != 750*time.Millisecond {
		t.Fatalf("NormalizeHub changed configured values: %+v", cfg)
	}
}

func TestNormalizeLeafAppliesDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := NormalizeLeaf(Leaf{
		NATSURL:            "nats://example:4222",
		ShardID:            "cpc-1",
		LocalGatewayOrigin: "http://gateway",
	})
	if err != nil {
		t.Fatalf("NormalizeLeaf: %v", err)
	}
	if cfg.HealthAddr != defaultLeafHealthAddr {
		t.Fatalf("HealthAddr = %q, want %q", cfg.HealthAddr, defaultLeafHealthAddr)
	}
	if cfg.SubjectPrefix != defaultSubjectPrefix {
		t.Fatalf("SubjectPrefix = %q, want %q", cfg.SubjectPrefix, defaultSubjectPrefix)
	}
	if cfg.Timeout != defaultRequestTimeout {
		t.Fatalf("Timeout = %s, want %s", cfg.Timeout, defaultRequestTimeout)
	}
}

func TestNormalizeLeafRequiresNATSURL(t *testing.T) {
	t.Parallel()

	if _, err := NormalizeLeaf(Leaf{
		ShardID:            "cpc-1",
		LocalGatewayOrigin: "http://gateway",
	}); err == nil {
		t.Fatalf("NormalizeLeaf accepted missing NATS URL")
	}
}

func TestValidBridgeRoleAcceptsChartRoles(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{string(RoleHub), string(RoleLeaf)} {
		if !ValidBridgeRole(raw) {
			t.Fatalf("ValidBridgeRole(%q) rejected a chart role", raw)
		}
	}
}

func clearNATSEnv(t *testing.T) {
	t.Helper()
	names := []string{
		EnvNATSAuthMode,
		EnvNATSOAuthIssuer,
		EnvNATSOAuthClientID,
		EnvNATSOAuthClientIDFile,
		EnvNATSOAuthClientSecret,
		EnvNATSOAuthClientSecretFile,
		EnvNATSOAuthScope,
		EnvNATSTLSEnabled,
		EnvNATSTLSCAFile,
		EnvNATSTLSServerName,
		EnvNATSURL,
	}
	for _, name := range names {
		t.Setenv(name, "")
	}
}

func TestValidBridgeRoleRejectsNonExactValues(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", " hub", "hub ", "bridge-hub", "bridge-leaf", "agentgateway-bridge"} {
		if ValidBridgeRole(raw) {
			t.Fatalf("ValidBridgeRole(%q) accepted an invalid role", raw)
		}
	}
}

func TestValidShardIDAcceptsNATSSubjectTokenCharacters(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"cpc-1", "cpc_1", "cpc+1", "cpc,1"} {
		if !ValidShardID(raw) {
			t.Fatalf("ValidShardID rejected valid shard id %q", raw)
		}
	}
}

func TestValidShardIDRejectsInvalidNATSSubjectTokens(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "cpc.1", "cpc*1", "cpc>1", "cpc 1", "cpc\t1", "cpc\x001"} {
		if ValidShardID(raw) {
			t.Fatalf("ValidShardID accepted invalid shard id %q", raw)
		}
	}
}

func TestValidSubjectPrefixAcceptsDotSeparatedTokens(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		defaultSubjectPrefix,
		"tenant_A.bridge-1",
		"a",
	} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			if !ValidSubjectPrefix(raw) {
				t.Fatalf("ValidSubjectPrefix(%q) rejected a valid prefix", raw)
			}
		})
	}
}

func TestValidSubjectPrefixRejectsInvalidNATSSubjectTokens(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"",
		".dsx",
		"dsx.",
		"dsx..bridge",
		"dsx.*.bridge",
		"dsx.>",
		"dsx bridge",
		"dsx/bridge",
		"dsx,bridge",
		strings.Repeat("a", 256),
	} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			if ValidSubjectPrefix(raw) {
				t.Fatalf("ValidSubjectPrefix(%q) accepted an invalid prefix", raw)
			}
		})
	}
}
