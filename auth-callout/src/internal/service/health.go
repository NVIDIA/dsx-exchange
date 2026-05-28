// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"net/http"
)

func livezHandler(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("OK"))
}

// HealthHandler handles health check requests at the "/healthz" endpoint.
func (s *Service) HealthHandler(w http.ResponseWriter, _ *http.Request) {
	// Check NATS connection health
	if s.natsConn != nil && !s.natsConn.IsConnected() {
		http.Error(w, "NATS connection lost", http.StatusServiceUnavailable)
		return
	}

	_, _ = w.Write([]byte("OK"))
}
