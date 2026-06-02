// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package benchmark

import "testing"

func TestNewConfigDefaultBrokerIsStandaloneLocalhost(t *testing.T) {
	cfg := NewConfig()
	if cfg.BrokerURL != "tcp://localhost:1883" {
		t.Fatalf("default broker URL = %q, want tcp://localhost:1883", cfg.BrokerURL)
	}
}
