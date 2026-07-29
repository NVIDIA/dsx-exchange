// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package health

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBridgeHealthLivenessDoesNotDependOnNATS(t *testing.T) {
	t.Parallel()

	ready := false
	mux := http.NewServeMux()
	Register(mux, func() bool { return ready })

	live := httptest.NewRecorder()
	mux.ServeHTTP(live, httptest.NewRequest(http.MethodGet, "/livez", nil))
	if live.Code != http.StatusNoContent {
		t.Fatalf("/livez status = %d, want %d", live.Code, http.StatusNoContent)
	}
}

func TestBridgeHealthReadinessUsesPredicateWhenProvided(t *testing.T) {
	t.Parallel()

	ready := false
	mux := http.NewServeMux()
	Register(mux, func() bool { return ready })

	notReady := httptest.NewRecorder()
	mux.ServeHTTP(notReady, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if notReady.Code != http.StatusServiceUnavailable {
		t.Fatalf("/readyz not ready status = %d, want %d", notReady.Code, http.StatusServiceUnavailable)
	}

	ready = true
	isReady := httptest.NewRecorder()
	mux.ServeHTTP(isReady, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if isReady.Code != http.StatusNoContent {
		t.Fatalf("/readyz ready status = %d, want %d", isReady.Code, http.StatusNoContent)
	}
}

func TestStartHealthHTTPReportsBindFailure(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen occupied port: %v", err)
	}
	defer ln.Close()

	if srv, err := Start(ln.Addr().String(), nil); err == nil {
		Shutdown(srv)
		t.Fatalf("Start succeeded on occupied port")
	}
}
