// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package config

import "testing"

func TestBrokerURLDefaultsAreStandaloneLocalhost(t *testing.T) {
	t.Setenv("CSC_BROKER_URL", "")
	t.Setenv("CPC1_BROKER_URL", "")
	t.Setenv("CPC2_BROKER_URL", "")
	t.Setenv("KEYCLOAK_URL", "")

	cfg := DefaultConfig()
	if cfg.Broker.URL != "tcp://localhost:1883" {
		t.Fatalf("default broker URL = %q, want tcp://localhost:1883", cfg.Broker.URL)
	}

	if got := GetCSCBrokerURL(); got != "tcp://localhost:1883" {
		t.Fatalf("CSC broker default = %q, want tcp://localhost:1883", got)
	}
	if got := GetCPC1BrokerURL(); got != "tcp://localhost:1883" {
		t.Fatalf("CPC1 broker default = %q, want tcp://localhost:1883", got)
	}
	if got := GetCPC2BrokerURL(); got != "tcp://localhost:1883" {
		t.Fatalf("CPC2 broker default = %q, want tcp://localhost:1883", got)
	}
	if got := GetKeycloakURL(); got != "http://localhost:8080" {
		t.Fatalf("Keycloak URL default = %q, want http://localhost:8080", got)
	}
}

func TestBrokerURLEnvOverrides(t *testing.T) {
	t.Setenv("CSC_BROKER_URL", "tcp://172.18.200.1:1883")
	t.Setenv("CPC1_BROKER_URL", "tcp://172.18.201.1:1883")
	t.Setenv("CPC2_BROKER_URL", "tcp://172.18.202.1:1883")
	t.Setenv("KEYCLOAK_URL", "http://172.18.200.1")

	if got := GetCSCBrokerURL(); got != "tcp://172.18.200.1:1883" {
		t.Fatalf("CSC broker override = %q, want tcp://172.18.200.1:1883", got)
	}
	if got := GetCPC1BrokerURL(); got != "tcp://172.18.201.1:1883" {
		t.Fatalf("CPC1 broker override = %q, want tcp://172.18.201.1:1883", got)
	}
	if got := GetCPC2BrokerURL(); got != "tcp://172.18.202.1:1883" {
		t.Fatalf("CPC2 broker override = %q, want tcp://172.18.202.1:1883", got)
	}
	if got := GetKeycloakURL(); got != "http://172.18.200.1" {
		t.Fatalf("Keycloak URL override = %q, want http://172.18.200.1", got)
	}
}
