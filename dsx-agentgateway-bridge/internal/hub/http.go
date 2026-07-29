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

package hub

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/felixge/httpsnoop"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/NVIDIA/dsx-exchange/dsx-agentgateway-bridge/internal/health"
)

func withRequestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metrics := httpsnoop.CaptureMetrics(next, w, r)
		logger.InfoContext(r.Context(), "bridge request completed",
			"method", r.Method,
			"status", metrics.Code,
			"duration", metrics.Duration,
		)
	})
}

// withRequestTimeout bounds request lifetime without wrapping ResponseWriter,
// so streaming handlers retain Flusher and ResponseController support.
func withRequestTimeout(timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeAndFlush(w http.ResponseWriter, timeout time.Duration, status int, body []byte) (committed bool, err error) {
	controller := http.NewResponseController(w)
	deadlineSet := true
	if err := controller.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		if !errors.Is(err, http.ErrNotSupported) {
			return false, err
		}
		deadlineSet = false
	}

	if status != 0 {
		w.WriteHeader(status)
		committed = true
	}
	if len(body) != 0 {
		committed = true
		_, err = w.Write(body)
	}
	if err == nil {
		err = controller.Flush()
		if errors.Is(err, http.ErrNotSupported) {
			err = nil
		}
	}
	if deadlineSet {
		// The next write installs a fresh deadline before touching the socket.
		err = errors.Join(err, controller.SetWriteDeadline(time.Time{}))
	}
	return committed, err
}

func newHTTPServer(name, addr string, writeTimeout, requestTimeout time.Duration, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           otelhttp.NewHandler(withRequestTimeout(requestTimeout, h), name),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       60 * time.Second,
	}
}

func runHTTP(ctx context.Context, name, addr string, writeTimeout, requestTimeout time.Duration, h http.Handler) error {
	srv := newHTTPServer(name, addr, writeTimeout, requestTimeout, h)
	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "name", name, "addr", addr)
		errCh <- srv.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		slog.Info("shutting down", "name", name, "reason", ctx.Err())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
	health.Shutdown(srv)
	return nil
}
