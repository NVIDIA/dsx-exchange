// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	gorillaHandlers "github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/synadia-io/callout.go"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/auth"
	perms "github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/config"
	obsconfig "github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/config"
	obslogging "github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/logging"
	obsmetrics "github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/metrics"
	obstracing "github.com/NVIDIA/dsx-exchange/auth-callout/src/internal/observability/tracing"
)

const (
	natsClientName         = "auth-callout-service"
	natsStartupConnectWait = 2 * time.Second
	natsMaxReconnects      = -1 // Keep retrying while readiness reports NATS unavailable.
	httpReadHeaderTimeout  = 5 * time.Second
	httpReadTimeout        = 10 * time.Second
	httpWriteTimeout       = 10 * time.Second
	httpIdleTimeout        = 120 * time.Second
)

// Service wraps HTTP server with graceful shutdown
type Service struct {
	config         ServiceConfig
	logger         *otelzap.Logger
	server         *http.Server
	rootRouter     *mux.Router
	natsConn       *nats.Conn
	authService    *callout.AuthorizationService
	permManager    *perms.PermissionsManager
	authenticators []auth.Authenticator
	issuerKeyPair  nkeys.KeyPair
	issuerPubKey   string
	curveKeyPair   nkeys.KeyPair
}

// ServiceConfig holds all configuration for the service
type ServiceConfig struct {
	HostConfig    HostConfig                    `koanf:"host"`
	Observability obsconfig.ObservabilityConfig `koanf:"observability"`
	NATS          NATSConfig                    `koanf:"nats"`
	JWKS          JWKSConfig                    `koanf:"jwks"`
	MTLS          MTLSConfig                    `koanf:"mtls"`
	Permissions   PermissionsFileConfig         `koanf:"permissions"`
}

type HostConfig struct {
	Port int    `koanf:"port"`
	Name string `koanf:"name"`
}

// NATSConfig contains NATS connection and auth callout configuration
type NATSConfig struct {
	URL        string `koanf:"url"`
	NKeySeed   string `koanf:"nkey-seed"`   // NATS connection NKey seed (from Vault)
	IssuerSeed string `koanf:"issuer-seed"` // Issuer account signing key seed (from Vault)
	XKeySeed   string `koanf:"xkey-seed"`   // XKey seed for encryption (from Vault, optional)
}

// JWKSConfig contains OAuth2/JWKS configuration
type JWKSConfig struct {
	URL               string   `koanf:"url"`
	Issuer            string   `koanf:"issuer"`
	Audience          string   `koanf:"audience"`
	SigningAlgorithms []string `koanf:"signing-algorithms"`
}

// MTLSConfig contains mTLS configuration
type MTLSConfig struct {
	CAPath string `koanf:"ca-path"`
}

// PermissionsFileConfig contains the path to the permissions file
type PermissionsFileConfig struct {
	File string `koanf:"file"`
}

// New creates a new service instance with the provided configuration and logger.
// It initializes the authenticators, permissions manager, and sets up
// hot reload support if configured. Returns a configured Service ready to run.
func New(config ServiceConfig, logger *otelzap.Logger) *Service {
	serviceName := config.Observability.Telemetry.ServiceName

	// Parse issuer account signing key (required)
	if config.NATS.IssuerSeed == "" {
		logger.Fatal("issuer seed is required")
	}
	issuerKeyPair, err := nkeys.FromSeed([]byte(config.NATS.IssuerSeed))
	if err != nil {
		logger.Fatal("error parsing issuer seed", zap.Error(err))
	}

	issuerPubKey, err := issuerKeyPair.PublicKey()
	if err != nil {
		logger.Fatal("error getting issuer public key", zap.Error(err))
	}
	logger.Info("Issuer public key loaded", zap.String("public_key", issuerPubKey))

	// Parse XKey seed if present (optional, for encryption)
	var curveKeyPair nkeys.KeyPair
	if config.NATS.XKeySeed != "" {
		curveKeyPair, err = nkeys.FromSeed([]byte(config.NATS.XKeySeed))
		if err != nil {
			logger.Fatal("error parsing xkey seed", zap.Error(err))
		}
		logger.Info("XKey encryption enabled")
	}

	// Initialize permissions manager
	pm, err := perms.NewPermissionsManager(config.Permissions.File, logger)
	if err != nil {
		logger.Fatal("error initializing permissions manager", zap.Error(err))
	}

	// Build list of authenticators in priority order
	var authenticators []auth.Authenticator

	// OAuth2 authenticator
	if config.JWKS.URL != "" {
		oauth2Auth, err := auth.NewOAuth2Authenticator(config.JWKS.URL, config.JWKS.Issuer, config.JWKS.Audience, config.JWKS.SigningAlgorithms, pm, logger, serviceName)
		if err != nil {
			logger.Fatal("error initializing OAuth2 authenticator", zap.Error(err))
		}
		authenticators = append(authenticators, oauth2Auth)
		logger.Info("OAuth2 authenticator enabled", zap.String("jwks_url", config.JWKS.URL))
	}

	// mTLS authenticator
	if config.MTLS.CAPath != "" {
		caPEM, err := os.ReadFile(config.MTLS.CAPath)
		if err != nil {
			logger.Fatal("error reading CA certificate", zap.Error(err))
		}
		mtlsAuth, err := auth.NewMTLSAuthenticator(caPEM, pm, logger, serviceName)
		if err != nil {
			logger.Fatal("error initializing mTLS authenticator", zap.Error(err))
		}
		authenticators = append(authenticators, mtlsAuth)
		logger.Info("mTLS authenticator enabled", zap.String("ca_path", config.MTLS.CAPath))
	}

	// NKey authenticator
	nkeyAuth := auth.NewNKeyAuthenticator(pm, logger, serviceName)
	authenticators = append(authenticators, nkeyAuth)

	// NoAuth authenticator (fallback)
	noauthAuth := auth.NewNoAuthAuthenticator(pm, logger, serviceName)
	authenticators = append(authenticators, noauthAuth)

	// Setup HTTP router for health/metrics
	rootRouter := mux.NewRouter()

	// Attach recovery handler
	zapErrorLogger := obslogging.NewLoggerWithZapWriter(logger.Logger)
	recoveryHandler := gorillaHandlers.RecoveryHandler(
		gorillaHandlers.PrintRecoveryStack(true),
		gorillaHandlers.RecoveryLogger(zapErrorLogger),
	)(rootRouter)

	srv := newHTTPServer(
		fmt.Sprintf(":%d", config.HostConfig.Port),
		recoveryHandler,
		logger.Logger,
	)

	return &Service{
		config:         config,
		logger:         logger,
		server:         srv,
		rootRouter:     rootRouter,
		permManager:    pm,
		authenticators: authenticators,
		issuerKeyPair:  issuerKeyPair,
		issuerPubKey:   issuerPubKey,
		curveKeyPair:   curveKeyPair,
	}
}

func newHTTPServer(addr string, handler http.Handler, logger *zap.Logger) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ErrorLog:          obslogging.NewLoggerWithZapWriter(logger),
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       httpIdleTimeout,
	}
}

// Run starts the HTTP server and handles graceful shutdown on SIGINT or SIGTERM signals.
// It sets up routes including health checks and authentication middleware.
// The service will wait for interrupt signals and perform a graceful shutdown with a 5-second timeout.
func (s *Service) Run() error {
	// =================================================
	// Put unauthenticated health routes here.
	// =================================================
	s.rootRouter.HandleFunc("/livez", livezHandler)
	s.rootRouter.HandleFunc("/healthz", s.HealthHandler)

	initialConnectCh := make(chan struct{}, 1)
	opts, err := s.buildNATSOptions(initialConnectCh)
	if err != nil {
		return err
	}

	nc, err := nats.Connect(s.config.NATS.URL, opts...)
	if err != nil {
		return fmt.Errorf("error connecting to NATS: %w", err)
	}
	s.natsConn = nc
	if !nc.IsConnected() {
		select {
		case <-initialConnectCh:
		case <-time.After(natsStartupConnectWait):
		}
	}
	if nc.IsConnected() {
		s.logger.Info("Connected to NATS", zap.String("url", s.config.NATS.URL))
	} else {
		s.logger.Warn(
			"NATS still connecting after startup check",
			zap.String("url", s.config.NATS.URL),
			zap.Duration("timeout", natsStartupConnectWait),
		)
	}

	// Create authorization handler
	authorizerFn := func(req *jwt.AuthorizationRequest) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		ctx = obslogging.AttachLoggerToContext(ctx, s.logger)
		return s.handleAuthRequest(ctx, req)
	}

	// Configure callout service options
	calloutOpts := []callout.Option{
		callout.Name("auth-callout"),
		callout.Authorizer(authorizerFn),
		callout.ResponseSignerKey(s.issuerKeyPair),
		callout.ResponseSignerIssuer(s.issuerPubKey),
	}

	// Add encryption key if provided
	if s.curveKeyPair != nil {
		calloutOpts = append(calloutOpts, callout.EncryptionKey(s.curveKeyPair))
	}

	// Create and start the authorization service
	authService, err := callout.NewAuthorizationService(nc, calloutOpts...)
	if err != nil {
		return fmt.Errorf("error creating authorization service: %w", err)
	}
	s.authService = authService
	s.logger.Info("Auth callout service started")

	// Run our server in a goroutine
	go func() {
		log := s.logger.Sugar()
		log.Warnf("service started on %s", s.server.Addr)
		if err := s.server.ListenAndServe(); err != nil {
			if !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("unable to listen and serve: %s\n", err.Error())
			}
		}
	}()

	// Handle interrupts
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	// Graceful shutdown
	s.logger.Warn("service is shutting down...")

	// Stop auth callout service
	if err := s.authService.Stop(); err != nil {
		s.logger.Error("error stopping auth callout service", zap.Error(err))
	}

	// Drain NATS connection
	if err := s.natsConn.Drain(); err != nil {
		s.logger.Error("error draining NATS connection", zap.Error(err))
	}

	// Close permissions manager
	if err := s.permManager.Close(); err != nil {
		s.logger.Error("error closing permissions manager", zap.Error(err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		errMsg := fmt.Sprintf("service shutdown failed: %s", err.Error())
		s.logger.Error(errMsg, zap.Error(err))
	} else {
		s.logger.Warn("service exited properly")
	}

	s.logger.Warn("service shut down successfully")
	return nil
}

func (s *Service) buildNATSOptions(initialConnectCh chan<- struct{}) ([]nats.Option, error) {
	opts := []nats.Option{
		nats.Name(natsClientName),
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(natsMaxReconnects),
		nats.ConnectHandler(func(_ *nats.Conn) {
			select {
			case initialConnectCh <- struct{}{}:
			default:
			}
		}),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				s.logger.Warn("NATS disconnected", zap.String("url", s.config.NATS.URL), zap.Error(err))
			}
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) {
			s.logger.Info("Connected to NATS", zap.String("url", s.config.NATS.URL))
		}),
		nats.ReconnectErrHandler(func(_ *nats.Conn, err error) {
			s.logger.Debug("NATS reconnect failed", zap.String("url", s.config.NATS.URL), zap.Error(err))
		}),
	}

	if s.config.NATS.NKeySeed == "" {
		return opts, nil
	}

	kp, err := nkeys.FromSeed([]byte(s.config.NATS.NKeySeed))
	if err != nil {
		return nil, fmt.Errorf("error loading NATS NKey: %w", err)
	}
	pubKey, err := kp.PublicKey()
	if err != nil {
		return nil, fmt.Errorf("error getting NATS NKey public key: %w", err)
	}

	return append(opts, nats.Nkey(pubKey, kp.Sign)), nil
}

// handleAuthRequest processes NATS authorization requests
func (s *Service) handleAuthRequest(ctx context.Context, req *jwt.AuthorizationRequest) (string, error) {
	ctx, endTrace := obstracing.LevelInfo(ctx)
	defer endTrace()

	logger := obslogging.GetLogger(ctx)
	meter := obsmetrics.GetMeter(s.config.Observability.Telemetry.ServiceName)

	if counter, err := meter.Int64Counter("auth_requests_total",
		metric.WithDescription("Total NATS auth callout requests")); err == nil {
		counter.Add(ctx, 1)
	}
	start := time.Now()
	defer func() {
		if histogram, err := meter.Float64Histogram("auth_request_duration_seconds",
			metric.WithDescription("Duration of NATS auth callout requests"),
			metric.WithUnit("s")); err == nil {
			histogram.Record(ctx, time.Since(start).Seconds())
		}
	}()

	logger.DebugContext(ctx, "Received auth request",
		zap.String("server", req.Server.Name),
		zap.String("client_host", req.ClientInformation.Host),
		zap.String("username", req.ConnectOptions.Username),
	)

	// Create authorization request claims wrapper
	rc := &jwt.AuthorizationRequestClaims{
		AuthorizationRequest: *req,
	}
	rc.UserNkey = req.UserNkey

	// Try each authenticator in priority order
	var userProfile perms.UserProfile
	var matched bool

	for _, authenticator := range s.authenticators {
		if authenticator.CanAuthenticate(rc) {
			matched = true
			var err error
			userProfile, err = authenticator.TryAuthenticate(ctx, rc)
			if err == nil {
				break
			}
			// Auth method matched but failed - reject immediately
			if counter, err := meter.Int64Counter("auth_errors_total",
				metric.WithDescription("Total NATS auth callout errors")); err == nil {
				counter.Add(ctx, 1)
			}
			return "", fmt.Errorf("authentication failed: %w", err)
		}
	}

	if !matched {
		if counter, err := meter.Int64Counter("auth_errors_total",
			metric.WithDescription("Total NATS auth callout errors")); err == nil {
			counter.Add(ctx, 1)
		}
		return "", fmt.Errorf("no valid authenticator available for provided credentials")
	}

	logger.InfoContext(ctx, "Authentication successful",
		zap.String("user", userProfile.Name),
		zap.String("account", userProfile.Account),
	)

	// Prepare user JWT
	uc := jwt.NewUserClaims(req.UserNkey)
	uc.Name = userProfile.Name

	// Centralized mode: set audience to account name
	uc.Audience = userProfile.Account

	// Set the associated permissions if present
	uc.Permissions = userProfile.Permissions

	// Set unlimited limits for JetStream operations
	uc.Subs = -1           // Unlimited subscriptions
	uc.Data = -1           // Unlimited data
	uc.Limits.Payload = -1 // Unlimited payload

	// Validate the claims
	vr := jwt.CreateValidationResults()
	uc.Validate(vr)
	if len(vr.Warnings()) > 0 {
		logger.WarnContext(ctx, "Warnings validating claims", zap.Any("warnings", vr.Warnings()))
	}
	if len(vr.Errors()) > 0 {
		if counter, err := meter.Int64Counter("auth_errors_total",
			metric.WithDescription("Total NATS auth callout errors")); err == nil {
			counter.Add(ctx, 1)
		}
		return "", fmt.Errorf("error validating claims: %v", vr.Errors())
	}

	// Sign with issuer key
	ejwt, err := uc.Encode(s.issuerKeyPair)
	if err != nil {
		if counter, err := meter.Int64Counter("auth_errors_total",
			metric.WithDescription("Total NATS auth callout errors")); err == nil {
			counter.Add(ctx, 1)
		}
		return "", fmt.Errorf("error signing user JWT: %v", err)
	}

	return ejwt, nil
}
