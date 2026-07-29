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
