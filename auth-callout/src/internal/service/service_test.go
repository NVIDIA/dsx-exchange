// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/synadia-io/callout.go"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

func TestAuthorizationServiceStartsBeforeInitialNATSConnect(t *testing.T) {
	natsServer := runNATSServer(t)
	dialer := &gatedDialer{ready: make(chan struct{})}
	initialConnectCh := make(chan struct{}, 1)

	opts, err := newTestService().buildNATSOptions(initialConnectCh)
	if err != nil {
		t.Fatalf("build NATS options: %v", err)
	}
	opts = append(opts,
		nats.SetCustomDialer(dialer),
		nats.Timeout(50*time.Millisecond),
		nats.ReconnectWait(50*time.Millisecond),
	)

	natsConn, err := nats.Connect(natsServer.ClientURL(), opts...)
	if err != nil {
		t.Fatalf("connect while NATS dial is blocked: %v", err)
	}
	t.Cleanup(natsConn.Close)
	if natsConn.IsConnected() {
		t.Fatal("NATS connection is connected, want reconnecting")
	}
	authService, err := callout.NewAuthorizationService(
		natsConn,
		callout.Name("auth-callout"),
		callout.Authorizer(func(*jwt.AuthorizationRequest) (string, error) {
			return "", nil
		}),
		callout.ResponseSigner(func(*jwt.AuthorizationResponseClaims) (string, error) {
			return "", nil
		}),
		callout.Logger(noopNATSLogger{}),
	)
	if err != nil {
		t.Fatalf("create authorization service while NATS reconnects: %v", err)
	}
	t.Cleanup(func() { _ = authService.Stop() })

	if natsConn.NumSubscriptions() == 0 {
		t.Fatal("authorization service did not register NATS subscriptions")
	}

	close(dialer.ready)
	select {
	case <-initialConnectCh:
	case <-time.After(2 * time.Second):
		t.Fatal("NATS connection did not recover after dialer was unblocked")
	}
	if !natsConn.IsConnected() {
		t.Fatal("NATS connection is not connected after initial connect signal")
	}
}

func TestLivezHandlerDoesNotRequireNATSConnection(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/livez", nil)

	livezHandler(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "OK" {
		t.Fatalf("body = %q, want %q", body, "OK")
	}
}

func TestHealthHandlerRequiresNATSConnection(t *testing.T) {
	natsConn := newReconnectingNATSConn(t)

	service := &Service{natsConn: natsConn}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	service.HealthHandler(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
}

func newReconnectingNATSConn(t *testing.T) *nats.Conn {
	t.Helper()

	opts, err := newTestService().buildNATSOptions(make(chan struct{}, 1))
	if err != nil {
		t.Fatalf("build NATS options: %v", err)
	}
	opts = append(opts,
		nats.SetCustomDialer(failingDialer{}),
		nats.Timeout(50*time.Millisecond),
		nats.ReconnectWait(50*time.Millisecond),
	)

	natsConn, err := nats.Connect("nats://unavailable.test:4222", opts...)
	if err != nil {
		t.Fatalf("connect to unavailable NATS with retry enabled: %v", err)
	}
	t.Cleanup(natsConn.Close)

	return natsConn
}

func runNATSServer(t *testing.T) *natsserver.Server {
	t.Helper()

	natsServer, err := natsserver.NewServer(&natsserver.Options{
		Host:   "127.0.0.1",
		Port:   -1,
		NoLog:  true,
		NoSigs: true,
	})
	if err != nil {
		t.Fatalf("create NATS server: %v", err)
	}
	natsServer.Start()
	if !natsServer.ReadyForConnections(2 * time.Second) {
		natsServer.Shutdown()
		t.Fatal("NATS server did not become ready")
	}
	t.Cleanup(natsServer.Shutdown)

	return natsServer
}

func newTestService() *Service {
	return &Service{
		logger: otelzap.New(zap.NewNop()),
	}
}

type failingDialer struct{}

func (failingDialer) Dial(string, string) (net.Conn, error) {
	return nil, errors.New("dial failed")
}

type gatedDialer struct {
	ready chan struct{}
}

func (d *gatedDialer) Dial(network, address string) (net.Conn, error) {
	select {
	case <-d.ready:
		return (&net.Dialer{}).Dial(network, address)
	default:
		return nil, errors.New("dial blocked")
	}
}

type noopNATSLogger struct{}

func (noopNATSLogger) Noticef(string, ...any) {}
func (noopNATSLogger) Warnf(string, ...any)   {}
func (noopNATSLogger) Fatalf(string, ...any)  {}
func (noopNATSLogger) Errorf(string, ...any)  {}
func (noopNATSLogger) Debugf(string, ...any)  {}
func (noopNATSLogger) Tracef(string, ...any)  {}
