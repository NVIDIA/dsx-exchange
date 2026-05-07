// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"net/http"
)

// HealthHandler handles health check requests at the "/healthz" endpoint.
// Returns HTTP 200 OK with "OK" body if the service is healthy.
func (s *Service) HealthHandler(w http.ResponseWriter, r *http.Request) {
	// Check NATS connection health
	if s.natsConn != nil && !s.natsConn.IsConnected() {
		http.Error(w, "NATS connection lost", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte("OK"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
