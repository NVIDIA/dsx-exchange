// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	toolSubscribe          = "dsx_exchange_subscribe"
	toolReadRetained       = "dsx_exchange_read_retained"
	toolDescribeTopic      = "dsx_exchange_describe_topic"
	toolFindTopics         = "dsx_exchange_find_topics"
	toolStartSubscription  = "dsx_exchange_start_subscription"
	toolReadSubscription   = "dsx_exchange_read_subscription"
	toolStatusSubscription = "dsx_exchange_subscription_status"
	toolStopSubscription   = "dsx_exchange_stop_subscription"

	maxErrorSamples = 5
)

type config struct {
	endpoint           string
	bearer             string
	experiment         string
	experimentDetail   string
	scenario           string
	sessions           int
	sessionSweep       string
	backendReplicas    int
	stickySessionCheck string
	duration           time.Duration
	startupRamp        time.Duration
	pollInterval       time.Duration
	rateLimit          int
	gatewayRateLimit   int
	manifestName       string
	backendImageID     string
	loadImageID        string
	topic              string
	retainedTopic      string
	deniedTopic        string
	subscribeDuration  int
	maxMessages        int
	maxBytes           int
	watchTTL           int
	httpTimeout        time.Duration
	backendConnectS    int
	backendSubscribeS  int
	backendCollectMax  int
	backendWatchMax    int
	reportDir          string
}

type mcpClient struct {
	endpoint string
	bearer   string
	httpc    *http.Client
	nextID   int
	limiter  *rateLimiter
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolCallResult struct {
	IsError bool `json:"isError"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

type runReport struct {
	StartedAt                  time.Time                    `json:"started_at"`
	EndedAt                    time.Time                    `json:"ended_at"`
	DurationSeconds            float64                      `json:"duration_seconds"`
	ThroughputRPS              float64                      `json:"throughput_requests_per_second"`
	SuccessRate                float64                      `json:"success_rate_percent"`
	Endpoint                   string                       `json:"endpoint"`
	Experiment                 string                       `json:"experiment,omitempty"`
	ExperimentDetail           string                       `json:"experiment_detail,omitempty"`
	Scenario                   string                       `json:"scenario"`
	Sessions                   int                          `json:"sessions"`
	BackendReplicas            int                          `json:"backend_replicas,omitempty"`
	StickySessionCheck         string                       `json:"sticky_session_check,omitempty"`
	RateLimit                  int                          `json:"rate_limit_per_second,omitempty"`
	GatewayRateLimit           int                          `json:"gateway_rate_limit_rps,omitempty"`
	ManifestName               string                       `json:"manifest_name,omitempty"`
	BackendImageID             string                       `json:"backend_image_id,omitempty"`
	LoadImageID                string                       `json:"load_image_id,omitempty"`
	ExperimentConfigHash       string                       `json:"experiment_config_hash,omitempty"`
	TokenTTLSecondsAtStart     int                          `json:"token_ttl_seconds_at_start,omitempty"`
	Topic                      string                       `json:"topic"`
	RetainedTopic              string                       `json:"retained_topic"`
	DeniedTopic                string                       `json:"denied_topic,omitempty"`
	HTTPTimeoutSeconds         float64                      `json:"http_timeout_seconds"`
	StartupRampSeconds         float64                      `json:"startup_ramp_seconds,omitempty"`
	PollIntervalSeconds        float64                      `json:"poll_interval_seconds,omitempty"`
	SubscribeDurationS         int                          `json:"subscribe_duration_seconds"`
	MaxMessages                int                          `json:"max_messages"`
	MaxBytes                   int                          `json:"max_bytes"`
	WatchTTLS                  int                          `json:"watch_ttl_seconds"`
	BackendConnectS            int                          `json:"backend_mqtt_connect_timeout_seconds,omitempty"`
	BackendSubscribeS          int                          `json:"backend_mqtt_subscribe_timeout_seconds,omitempty"`
	BackendCollectMax          int                          `json:"backend_mqtt_collect_max_concurrent_per_pod,omitempty"`
	BackendWatchStartMax       int                          `json:"backend_mqtt_watch_start_max_concurrent_per_pod,omitempty"`
	TotalRequests              uint64                       `json:"total_requests"`
	Successes                  uint64                       `json:"successes"`
	Failures                   uint64                       `json:"failures"`
	ExpectedToolErrors         uint64                       `json:"expected_tool_errors"`
	InitializedSessions        uint64                       `json:"initialized_sessions"`
	StartedWatches             uint64                       `json:"started_watches"`
	StoppedWatches             uint64                       `json:"stopped_watches"`
	SessionNotFoundErrors      uint64                       `json:"session_not_found_errors,omitempty"`
	SubscriptionNotFoundErrors uint64                       `json:"subscription_not_found_errors,omitempty"`
	ByOperation                map[string]operationSnapshot `json:"by_operation"`
	Errors                     map[string]uint64            `json:"errors"`
	ErrorSamples               map[string][]string          `json:"error_samples,omitempty"`
}

type operationSnapshot struct {
	Phase           string            `json:"phase"`
	Count           uint64            `json:"count"`
	Successes       uint64            `json:"successes"`
	Failures        uint64            `json:"failures"`
	P50Milliseconds float64           `json:"p50_ms"`
	P95Milliseconds float64           `json:"p95_ms"`
	P99Milliseconds float64           `json:"p99_ms"`
	Errors          map[string]uint64 `json:"errors,omitempty"`
}

type operationStats struct {
	count     uint64
	successes uint64
	failures  uint64
	latencies []time.Duration
	errors    map[string]uint64
}

type recorder struct {
	mu                         sync.Mutex
	startedAt                  time.Time
	endpoint                   string
	experiment                 string
	experimentDetail           string
	scenario                   string
	sessions                   int
	backendReplicas            int
	stickySessionCheck         string
	rateLimit                  int
	gatewayRateLimit           int
	manifestName               string
	backendImageID             string
	loadImageID                string
	experimentConfigHash       string
	tokenTTLSecondsAtStart     int
	topic                      string
	retainedTopic              string
	deniedTopic                string
	httpTimeout                time.Duration
	startupRamp                time.Duration
	pollInterval               time.Duration
	subscribeDuration          int
	maxMessages                int
	maxBytes                   int
	watchTTL                   int
	backendConnectS            int
	backendSubscribeS          int
	backendCollectMax          int
	backendWatchStartMax       int
	totalRequests              uint64
	successes                  uint64
	failures                   uint64
	expectedToolErrors         uint64
	initializedSessions        uint64
	startedWatches             uint64
	stoppedWatches             uint64
	sessionNotFoundErrors      uint64
	subscriptionNotFoundErrors uint64
	byOperation                map[string]*operationStats
	errors                     map[string]uint64
	errorSamples               map[string][]string
}

type rateLimiter struct {
	ch <-chan struct{}
}

func main() {
	cfg := parseConfig()
	sessionCounts, err := parseSessionCounts(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(2)
	}

	reports := make([]runReport, 0, len(sessionCounts))
	failed := false
	for _, sessions := range sessionCounts {
		runCfg := cfg
		runCfg.sessions = sessions
		if err := validateConfig(runCfg); err != nil {
			fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
			os.Exit(2)
		}
		report := runLoad(runCfg)
		printTextReport(os.Stderr, report)
		reports = append(reports, report)
		if report.Failures > 0 {
			failed = true
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if len(reports) == 1 {
		err = enc.Encode(reports[0])
	} else {
		err = enc.Encode(reports)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "write report JSON: %v\n", err)
		os.Exit(1)
	}
	if cfg.reportDir != "" {
		if err := writeReports(cfg.reportDir, reports); err != nil {
			fmt.Fprintf(os.Stderr, "write report files: %v\n", err)
			os.Exit(1)
		}
	}
	if failed {
		os.Exit(1)
	}
}

func runLoad(cfg config) runReport {
	if err := validateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(2)
	}

	limiter := newRateLimiter(cfg.rateLimit)
	rec := &recorder{
		startedAt:              time.Now().UTC(),
		endpoint:               cfg.endpoint,
		experiment:             cfg.experiment,
		experimentDetail:       cfg.experimentDetail,
		scenario:               cfg.scenario,
		sessions:               cfg.sessions,
		backendReplicas:        cfg.backendReplicas,
		stickySessionCheck:     cfg.stickySessionCheck,
		rateLimit:              cfg.rateLimit,
		gatewayRateLimit:       cfg.gatewayRateLimit,
		manifestName:           cfg.manifestName,
		backendImageID:         cfg.backendImageID,
		loadImageID:            cfg.loadImageID,
		experimentConfigHash:   experimentConfigHash(cfg),
		tokenTTLSecondsAtStart: tokenTTLSeconds(cfg.bearer),
		topic:                  cfg.topic,
		retainedTopic:          cfg.retainedTopic,
		deniedTopic:            cfg.deniedTopic,
		httpTimeout:            cfg.httpTimeout,
		startupRamp:            cfg.startupRamp,
		pollInterval:           effectivePollInterval(cfg),
		subscribeDuration:      cfg.subscribeDuration,
		maxMessages:            cfg.maxMessages,
		maxBytes:               cfg.maxBytes,
		watchTTL:               cfg.watchTTL,
		backendConnectS:        cfg.backendConnectS,
		backendSubscribeS:      cfg.backendSubscribeS,
		backendCollectMax:      cfg.backendCollectMax,
		backendWatchStartMax:   cfg.backendWatchMax,
		byOperation:            map[string]*operationStats{},
		errors:                 map[string]uint64{},
		errorSamples:           map[string][]string{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.duration)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < cfg.sessions; i++ {
		wg.Add(1)
		go func(sessionIndex int) {
			defer wg.Done()
			if !waitStartupRamp(ctx, cfg.startupRamp, sessionIndex, cfg.sessions) {
				return
			}
			client := &mcpClient{
				endpoint: cfg.endpoint,
				bearer:   cfg.bearer,
				httpc:    &http.Client{Timeout: cfg.httpTimeout},
				limiter:  limiter,
			}
			runSession(ctx, cfg, rec, client, sessionIndex)
		}(i)
	}
	wg.Wait()

	report := rec.snapshot(time.Now().UTC())
	return report
}

func parseConfig() config {
	cfg := config{}
	flag.StringVar(&cfg.endpoint, "endpoint", env("DSX_EXCHANGE_MCP_URL", ""), "MCP endpoint URL")
	flag.StringVar(&cfg.bearer, "bearer", firstEnv("DSX_EXCHANGE_E2E_BEARER", "DSX_EXCHANGE_BEARER"), "Bearer token for MCP Authorization")
	flag.StringVar(&cfg.experiment, "experiment", env("DSX_EXCHANGE_MCP_LOAD_EXPERIMENT", ""), "experiment label recorded in JSON/CSV reports")
	flag.StringVar(&cfg.experimentDetail, "experiment-detail", env("DSX_EXCHANGE_MCP_LOAD_EXPERIMENT_DETAIL", ""), "free-form experiment detail recorded in JSON/CSV reports")
	flag.StringVar(&cfg.scenario, "scenario", env("DSX_EXCHANGE_MCP_LOAD_SCENARIO", "discovery"), "scenario: discovery, discovery-hold, schema-resources, bounded-read, mixed, mixed-stateless, or legacy watch/watch-hold/watch-status-hold/sticky-check")
	flag.IntVar(&cfg.sessions, "sessions", envInt("DSX_EXCHANGE_MCP_LOAD_SESSIONS", 50), "concurrent MCP sessions")
	flag.StringVar(&cfg.sessionSweep, "session-sweep", env("DSX_EXCHANGE_MCP_LOAD_SESSION_SWEEP", ""), "comma-separated concurrent MCP session counts; overrides -sessions when set")
	flag.IntVar(&cfg.backendReplicas, "backend-replicas", envInt("DSX_EXCHANGE_MCP_LOAD_BACKEND_REPLICAS", 0), "metadata: MCP backend replica count for this experiment")
	flag.StringVar(&cfg.stickySessionCheck, "sticky-session-check", env("DSX_EXCHANGE_MCP_LOAD_STICKY_SESSION_CHECK", ""), "metadata: sticky-session validation state such as not_run, planned, running, pass, or fail")
	flag.DurationVar(&cfg.duration, "duration", envDuration("DSX_EXCHANGE_MCP_LOAD_DURATION", time.Minute), "load-test duration")
	flag.DurationVar(&cfg.startupRamp, "startup-ramp", envDuration("DSX_EXCHANGE_MCP_LOAD_STARTUP_RAMP", 0), "spread MCP session startup across this duration; 0 starts all sessions immediately")
	flag.DurationVar(&cfg.pollInterval, "poll-interval", envDuration("DSX_EXCHANGE_MCP_LOAD_POLL_INTERVAL", 0), "override watch/status poll interval; 0 uses scenario default")
	flag.IntVar(&cfg.rateLimit, "rate-limit", envInt("DSX_EXCHANGE_MCP_LOAD_RATE_LIMIT", 0), "global request rate limit per second; 0 means unlimited")
	flag.IntVar(&cfg.gatewayRateLimit, "gateway-rate-limit-rps", envInt("DSX_EXCHANGE_MCP_LOAD_GATEWAY_RATE_LIMIT_RPS", -1), "metadata: configured gateway tenant rate limit in requests per second; -1 uses -rate-limit")
	flag.StringVar(&cfg.manifestName, "manifest-name", env("DSX_EXCHANGE_MCP_LOAD_MANIFEST_NAME", ""), "metadata: Kubernetes manifest or job name used for this run")
	flag.StringVar(&cfg.backendImageID, "backend-image-id", env("DSX_EXCHANGE_MCP_LOAD_BACKEND_IMAGE_ID", ""), "metadata: backend image ID or digest used for this run")
	flag.StringVar(&cfg.loadImageID, "load-image-id", env("DSX_EXCHANGE_MCP_LOAD_IMAGE_ID", ""), "metadata: load generator image ID or digest used for this run")
	flag.StringVar(&cfg.topic, "topic", env("DSX_EXCHANGE_E2E_ALLOWED_TOPIC", "BMS/v1/PUB/Value/Rack/RackPower/#"), "allowed live topic filter")
	flag.StringVar(&cfg.retainedTopic, "retained-topic", env("DSX_EXCHANGE_E2E_RETAINED_TOPIC", "BMS/v1/PUB/Metadata/Rack/RackPower/#"), "allowed retained metadata topic filter")
	flag.StringVar(&cfg.deniedTopic, "denied-topic", env("DSX_EXCHANGE_E2E_DENIED_TOPIC", ""), "optional denied topic filter; expected to return MCP tool error")
	flag.IntVar(&cfg.subscribeDuration, "subscribe-duration-s", envInt("DSX_EXCHANGE_MCP_LOAD_SUBSCRIBE_DURATION_S", 1), "bounded subscribe duration in seconds")
	flag.IntVar(&cfg.maxMessages, "max-messages", envInt("DSX_EXCHANGE_MCP_LOAD_MAX_MESSAGES", 10), "max messages per read/subscribe call")
	flag.IntVar(&cfg.maxBytes, "max-bytes", envInt("DSX_EXCHANGE_MCP_LOAD_MAX_BYTES", 32768), "max bytes per watch read")
	flag.IntVar(&cfg.watchTTL, "watch-ttl-s", envInt("DSX_EXCHANGE_MCP_LOAD_WATCH_TTL_S", 30), "watch TTL in seconds")
	flag.DurationVar(&cfg.httpTimeout, "http-timeout", envDuration("DSX_EXCHANGE_MCP_LOAD_HTTP_TIMEOUT", 30*time.Second), "HTTP request timeout")
	flag.IntVar(&cfg.backendConnectS, "backend-mqtt-connect-timeout-s", envInt("DSX_EXCHANGE_MCP_LOAD_BACKEND_MQTT_CONNECT_TIMEOUT_S", 0), "metadata: backend MQTT connect timeout in seconds")
	flag.IntVar(&cfg.backendSubscribeS, "backend-mqtt-subscribe-timeout-s", envInt("DSX_EXCHANGE_MCP_LOAD_BACKEND_MQTT_SUBSCRIBE_TIMEOUT_S", 0), "metadata: backend MQTT subscribe timeout in seconds")
	flag.IntVar(&cfg.backendCollectMax, "backend-mqtt-collect-max-concurrent-per-pod", envInt("DSX_EXCHANGE_MCP_LOAD_BACKEND_MQTT_COLLECT_MAX_CONCURRENT_PER_POD", 0), "metadata: backend bounded MQTT tool admission limit per pod")
	flag.IntVar(&cfg.backendWatchMax, "backend-mqtt-watch-start-max-concurrent-per-pod", envInt("DSX_EXCHANGE_MCP_LOAD_BACKEND_MQTT_WATCH_START_MAX_CONCURRENT_PER_POD", 0), "metadata: backend watch-start MQTT admission limit per pod")
	flag.StringVar(&cfg.reportDir, "report-dir", env("DSX_EXCHANGE_MCP_LOAD_REPORT_DIR", ""), "optional directory for JSON, text, and CSV reports")
	flag.Parse()
	cfg.endpoint = strings.TrimSpace(cfg.endpoint)
	cfg.bearer = strings.TrimSpace(cfg.bearer)
	cfg.experiment = strings.TrimSpace(cfg.experiment)
	cfg.experimentDetail = strings.TrimSpace(cfg.experimentDetail)
	cfg.scenario = strings.TrimSpace(cfg.scenario)
	cfg.stickySessionCheck = strings.TrimSpace(cfg.stickySessionCheck)
	cfg.manifestName = strings.TrimSpace(cfg.manifestName)
	cfg.backendImageID = strings.TrimSpace(cfg.backendImageID)
	cfg.loadImageID = strings.TrimSpace(cfg.loadImageID)
	cfg.topic = strings.TrimSpace(cfg.topic)
	cfg.retainedTopic = strings.TrimSpace(cfg.retainedTopic)
	cfg.deniedTopic = strings.TrimSpace(cfg.deniedTopic)
	if cfg.gatewayRateLimit < 0 {
		cfg.gatewayRateLimit = cfg.rateLimit
	}
	return cfg
}

func validateConfig(cfg config) error {
	if cfg.endpoint == "" {
		return errors.New("-endpoint or DSX_EXCHANGE_MCP_URL is required")
	}
	if cfg.bearer == "" {
		return errors.New("-bearer, DSX_EXCHANGE_E2E_BEARER, or DSX_EXCHANGE_BEARER is required")
	}
	if cfg.sessions <= 0 {
		return errors.New("-sessions must be greater than zero")
	}
	if cfg.duration <= 0 {
		return errors.New("-duration must be greater than zero")
	}
	if cfg.startupRamp < 0 {
		return errors.New("-startup-ramp must be zero or greater")
	}
	if cfg.pollInterval < 0 {
		return errors.New("-poll-interval must be zero or greater")
	}
	if cfg.gatewayRateLimit < 0 {
		return errors.New("-gateway-rate-limit-rps must be zero or greater")
	}
	switch cfg.scenario {
	case "discovery", "discovery-hold", "schema-resources", "bounded-read", "mixed-stateless", "watch", "watch-hold", "watch-status-hold", "sticky-check", "mixed":
	default:
		return fmt.Errorf("unknown scenario %q", cfg.scenario)
	}
	if cfg.backendReplicas < 0 {
		return errors.New("-backend-replicas must be zero or greater")
	}
	if cfg.topic == "" {
		return errors.New("-topic is required")
	}
	if cfg.retainedTopic == "" {
		return errors.New("-retained-topic is required")
	}
	if cfg.subscribeDuration <= 0 {
		return errors.New("-subscribe-duration-s must be greater than zero")
	}
	if cfg.maxMessages <= 0 {
		return errors.New("-max-messages must be greater than zero")
	}
	if cfg.maxBytes <= 0 {
		return errors.New("-max-bytes must be greater than zero")
	}
	if cfg.watchTTL <= 0 {
		return errors.New("-watch-ttl-s must be greater than zero")
	}
	if cfg.backendCollectMax < 0 {
		return errors.New("-backend-mqtt-collect-max-concurrent-per-pod must be zero or greater")
	}
	if cfg.backendWatchMax < 0 {
		return errors.New("-backend-mqtt-watch-start-max-concurrent-per-pod must be zero or greater")
	}
	return nil
}

func parseSessionCounts(cfg config) ([]int, error) {
	if strings.TrimSpace(cfg.sessionSweep) == "" {
		return []int{cfg.sessions}, nil
	}
	parts := strings.Split(cfg.sessionSweep, ",")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("-session-sweep contains invalid session count %q", part)
		}
		out = append(out, n)
	}
	if len(out) == 0 {
		return nil, errors.New("-session-sweep did not contain any session counts")
	}
	return out, nil
}

func effectivePollInterval(cfg config) time.Duration {
	if cfg.pollInterval > 0 {
		return cfg.pollInterval
	}
	switch cfg.scenario {
	case "sticky-check":
		return 250 * time.Millisecond
	case "watch-hold", "watch-status-hold":
		return time.Second
	default:
		return 0
	}
}

func waitStartupRamp(ctx context.Context, ramp time.Duration, sessionIndex, sessions int) bool {
	if ramp <= 0 || sessionIndex <= 0 || sessions <= 1 {
		return ctx.Err() == nil
	}
	delay := time.Duration(int64(ramp) * int64(sessionIndex) / int64(sessions))
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func experimentConfigHash(cfg config) string {
	normalized := map[string]any{
		"endpoint":                               cfg.endpoint,
		"experiment":                             cfg.experiment,
		"experiment_detail":                      cfg.experimentDetail,
		"scenario":                               cfg.scenario,
		"sessions":                               cfg.sessions,
		"session_sweep":                          cfg.sessionSweep,
		"backend_replicas":                       cfg.backendReplicas,
		"sticky_session_check":                   cfg.stickySessionCheck,
		"duration":                               cfg.duration.String(),
		"startup_ramp":                           cfg.startupRamp.String(),
		"poll_interval":                          effectivePollInterval(cfg).String(),
		"client_rate_limit_per_second":           cfg.rateLimit,
		"gateway_rate_limit_rps":                 cfg.gatewayRateLimit,
		"topic":                                  cfg.topic,
		"retained_topic":                         cfg.retainedTopic,
		"denied_topic":                           cfg.deniedTopic,
		"subscribe_duration_seconds":             cfg.subscribeDuration,
		"max_messages":                           cfg.maxMessages,
		"max_bytes":                              cfg.maxBytes,
		"watch_ttl_seconds":                      cfg.watchTTL,
		"http_timeout":                           cfg.httpTimeout.String(),
		"backend_mqtt_connect_timeout_seconds":   cfg.backendConnectS,
		"backend_mqtt_subscribe_timeout_seconds": cfg.backendSubscribeS,
		"backend_mqtt_collect_max_concurrent_per_pod":     cfg.backendCollectMax,
		"backend_mqtt_watch_start_max_concurrent_per_pod": cfg.backendWatchMax,
		"manifest_name":    cfg.manifestName,
		"backend_image_id": cfg.backendImageID,
		"load_image_id":    cfg.loadImageID,
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%x", sum)
}

func tokenTTLSeconds(bearer string) int {
	parts := strings.Split(strings.TrimSpace(bearer), ".")
	if len(parts) < 2 {
		return 0
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0
	}
	var claims struct {
		Exp float64 `json:"exp"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return 0
	}
	if claims.Exp <= 0 {
		return 0
	}
	ttl := int(claims.Exp - float64(time.Now().Unix()))
	if ttl < 0 {
		return 0
	}
	return ttl
}

func runSession(ctx context.Context, cfg config, rec *recorder, client *mcpClient, sessionIndex int) {
	var sessionID string
	if _, err := measure(ctx, rec, "initialize", func(ctx context.Context) error {
		var initErr error
		sessionID, initErr = client.initialize(ctx)
		return initErr
	}); err != nil {
		return
	}
	if sessionID != "" {
		rec.recordInitializedSession()
	}
	if _, err := measure(ctx, rec, "notifications_initialized", func(ctx context.Context) error {
		return client.initialized(ctx, sessionID)
	}); err != nil {
		return
	}

	var tools []string
	if _, err := measure(ctx, rec, "tools_list", func(ctx context.Context) error {
		var listErr error
		tools, listErr = client.listTools(ctx, sessionID)
		return listErr
	}); err != nil {
		return
	}
	names := resolveTools(tools)
	if err := names.require(cfg.scenario); err != nil {
		rec.recordError(err.Error())
		return
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(sessionIndex)))
	switch cfg.scenario {
	case "watch-hold":
		runWatchHold(ctx, cfg, rec, client, sessionID, names)
		return
	case "watch-status-hold":
		runWatchStatusHold(ctx, cfg, rec, client, sessionID, names)
		return
	case "sticky-check":
		runStickyCheck(ctx, cfg, rec, client, sessionID, names)
		return
	}
	for ctx.Err() == nil {
		switch cfg.scenario {
		case "discovery", "discovery-hold":
			runDiscovery(ctx, cfg, rec, client, sessionID, names)
		case "schema-resources":
			runSchemaResources(ctx, rec, client, sessionID)
		case "bounded-read":
			runBoundedRead(ctx, cfg, rec, client, sessionID, names)
		case "mixed-stateless":
			if rng.Intn(100) < 60 {
				runDiscovery(ctx, cfg, rec, client, sessionID, names)
			} else {
				runBoundedRead(ctx, cfg, rec, client, sessionID, names)
			}
		case "watch":
			runWatch(ctx, cfg, rec, client, sessionID, names)
		case "mixed":
			if rng.Intn(100) < 60 {
				runDiscovery(ctx, cfg, rec, client, sessionID, names)
			} else {
				runBoundedRead(ctx, cfg, rec, client, sessionID, names)
			}
		}
	}
}

func runSchemaResources(ctx context.Context, rec *recorder, client *mcpClient, sessionID string) {
	measure(ctx, rec, "resources_list", func(ctx context.Context) error {
		uris, err := client.listResources(ctx, sessionID)
		if err != nil {
			return err
		}
		if len(uris) == 0 {
			return errors.New("resources_list_empty")
		}
		return nil
	})
	measure(ctx, rec, "resources_read_index", func(ctx context.Context) error {
		return client.readResource(ctx, sessionID, "dsx-exchange://specs/")
	})
	measure(ctx, rec, "resources_read_bms", func(ctx context.Context) error {
		return client.readResource(ctx, sessionID, "dsx-exchange://specs/bms")
	})
}

func runDiscovery(ctx context.Context, cfg config, rec *recorder, client *mcpClient, sessionID string, names toolNames) {
	measure(ctx, rec, "find_topics", func(ctx context.Context) error {
		res, err := client.callTool(ctx, sessionID, names.find, map[string]any{
			"domain":     "bms",
			"query":      "RackPower",
			"role":       "value",
			"point_type": "RackPower",
			"limit":      20,
		})
		if err != nil {
			return err
		}
		if res.IsError {
			return fmt.Errorf("unexpected_tool_error:%s", compact(res.textSummary()))
		}
		return nil
	})
	measure(ctx, rec, "describe_topic", func(ctx context.Context) error {
		res, err := client.callTool(ctx, sessionID, names.describe, map[string]any{
			"topic_filter": cfg.topic,
		})
		if err != nil {
			return err
		}
		if res.IsError {
			return fmt.Errorf("unexpected_tool_error:%s", compact(res.textSummary()))
		}
		return nil
	})
}

func runBoundedRead(ctx context.Context, cfg config, rec *recorder, client *mcpClient, sessionID string, names toolNames) {
	measure(ctx, rec, "read_retained", func(ctx context.Context) error {
		res, err := client.callTool(ctx, sessionID, names.readRetained, map[string]any{
			"topic_filter": cfg.retainedTopic,
			"max_messages": cfg.maxMessages,
		})
		if err != nil {
			return err
		}
		if res.IsError {
			return fmt.Errorf("unexpected_tool_error:%s", compact(res.textSummary()))
		}
		return nil
	})
	measure(ctx, rec, "subscribe", func(ctx context.Context) error {
		res, err := client.callTool(ctx, sessionID, names.subscribe, map[string]any{
			"topic_filter":   cfg.topic,
			"max_messages":   cfg.maxMessages,
			"max_duration_s": cfg.subscribeDuration,
		})
		if err != nil {
			return err
		}
		if res.IsError {
			return fmt.Errorf("unexpected_tool_error:%s", compact(res.textSummary()))
		}
		return nil
	})
	if cfg.deniedTopic != "" {
		measure(ctx, rec, "subscribe_denied_expected", func(ctx context.Context) error {
			res, err := client.callTool(ctx, sessionID, names.subscribe, map[string]any{
				"topic_filter":   cfg.deniedTopic,
				"max_messages":   1,
				"max_duration_s": cfg.subscribeDuration,
			})
			if err != nil {
				return err
			}
			if !res.IsError {
				return errors.New("denied_topic_unexpected_success")
			}
			rec.recordExpectedToolError()
			return nil
		})
	}
}

func runWatch(ctx context.Context, cfg config, rec *recorder, client *mcpClient, sessionID string, names toolNames) {
	started, err := startSubscription(ctx, cfg, rec, client, sessionID, names)
	if err != nil {
		return
	}

	_, _ = statusSubscription(ctx, rec, client, sessionID, names, started.SubscriptionID)
	_, _ = readSubscription(ctx, cfg, rec, client, sessionID, names, started.SubscriptionID, started.Cursor)
	_ = stopSubscription(ctx, rec, client, sessionID, names, started.SubscriptionID)
}

func runWatchHold(ctx context.Context, cfg config, rec *recorder, client *mcpClient, sessionID string, names toolNames) {
	started, err := startSubscription(ctx, cfg, rec, client, sessionID, names)
	if err != nil {
		return
	}
	cursor := started.Cursor
	ticker := time.NewTicker(effectivePollInterval(cfg))
	defer ticker.Stop()

	for ctx.Err() == nil {
		_, _ = statusSubscription(ctx, rec, client, sessionID, names, started.SubscriptionID)
		if nextCursor, err := readSubscription(ctx, cfg, rec, client, sessionID, names, started.SubscriptionID, cursor); err == nil && nextCursor != "" {
			cursor = nextCursor
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), cfg.httpTimeout)
	defer cancel()
	_ = stopSubscription(cleanupCtx, rec, client, sessionID, names, started.SubscriptionID)
}

func runWatchStatusHold(ctx context.Context, cfg config, rec *recorder, client *mcpClient, sessionID string, names toolNames) {
	started, err := startSubscription(ctx, cfg, rec, client, sessionID, names)
	if err != nil {
		return
	}
	ticker := time.NewTicker(effectivePollInterval(cfg))
	defer ticker.Stop()

	for ctx.Err() == nil {
		_, _ = statusSubscription(ctx, rec, client, sessionID, names, started.SubscriptionID)
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), cfg.httpTimeout)
	defer cancel()
	_ = stopSubscription(cleanupCtx, rec, client, sessionID, names, started.SubscriptionID)
}

func runStickyCheck(ctx context.Context, cfg config, rec *recorder, client *mcpClient, sessionID string, names toolNames) {
	started, err := startSubscription(ctx, cfg, rec, client, sessionID, names)
	if err != nil {
		return
	}
	cursor := started.Cursor
	ticker := time.NewTicker(effectivePollInterval(cfg))
	defer ticker.Stop()

	for ctx.Err() == nil {
		_, _ = statusSubscription(ctx, rec, client, sessionID, names, started.SubscriptionID)
		if nextCursor, err := readSubscription(ctx, cfg, rec, client, sessionID, names, started.SubscriptionID, cursor); err == nil && nextCursor != "" {
			cursor = nextCursor
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}

	cleanupCtx, cancel := context.WithTimeout(context.Background(), cfg.httpTimeout)
	defer cancel()
	_ = stopSubscription(cleanupCtx, rec, client, sessionID, names, started.SubscriptionID)
}

type startedSubscription struct {
	SubscriptionID string `json:"subscription_id"`
	Cursor         string `json:"cursor"`
}

func startSubscription(ctx context.Context, cfg config, rec *recorder, client *mcpClient, sessionID string, names toolNames) (startedSubscription, error) {
	var out startedSubscription
	_, err := measure(ctx, rec, "start_subscription", func(ctx context.Context) error {
		res, err := client.callTool(ctx, sessionID, names.start, map[string]any{
			"topic_filter":        cfg.topic,
			"ttl_seconds":         cfg.watchTTL,
			"buffer_max_messages": cfg.maxMessages,
			"buffer_max_bytes":    cfg.maxBytes,
		})
		if err != nil {
			return err
		}
		if res.IsError {
			return fmt.Errorf("unexpected_tool_error:%s", compact(res.textSummary()))
		}
		if err := json.Unmarshal([]byte(res.lastText()), &out); err != nil {
			return fmt.Errorf("decode_start_subscription:%w", err)
		}
		if out.SubscriptionID == "" {
			return errors.New("start_subscription_missing_id")
		}
		return nil
	})
	if err != nil {
		return startedSubscription{}, err
	}
	rec.recordStartedWatch()
	return out, nil
}

func statusSubscription(ctx context.Context, rec *recorder, client *mcpClient, sessionID string, names toolNames, subscriptionID string) (string, error) {
	var status string
	_, err := measure(ctx, rec, "subscription_status", func(ctx context.Context) error {
		res, err := client.callTool(ctx, sessionID, names.status, map[string]any{
			"subscription_id": subscriptionID,
		})
		if err != nil {
			return err
		}
		if res.IsError {
			return fmt.Errorf("unexpected_tool_error:%s", compact(res.textSummary()))
		}
		var out struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(res.lastText()), &out); err != nil {
			return fmt.Errorf("decode_subscription_status:%w", err)
		}
		status = out.Status
		return nil
	})
	return status, err
}

func readSubscription(ctx context.Context, cfg config, rec *recorder, client *mcpClient, sessionID string, names toolNames, subscriptionID, cursor string) (string, error) {
	nextCursor := cursor
	_, err := measure(ctx, rec, "read_subscription", func(ctx context.Context) error {
		res, err := client.callTool(ctx, sessionID, names.read, map[string]any{
			"subscription_id": subscriptionID,
			"cursor":          cursor,
			"max_messages":    cfg.maxMessages,
			"max_bytes":       cfg.maxBytes,
		})
		if err != nil {
			return err
		}
		if res.IsError {
			return fmt.Errorf("unexpected_tool_error:%s", compact(res.textSummary()))
		}
		var out struct {
			NextCursor string `json:"next_cursor"`
		}
		if err := json.Unmarshal([]byte(res.lastText()), &out); err != nil {
			return fmt.Errorf("decode_read_subscription:%w", err)
		}
		if out.NextCursor != "" {
			nextCursor = out.NextCursor
		}
		return nil
	})
	return nextCursor, err
}

func stopSubscription(ctx context.Context, rec *recorder, client *mcpClient, sessionID string, names toolNames, subscriptionID string) error {
	_, err := measure(ctx, rec, "stop_subscription", func(ctx context.Context) error {
		res, err := client.callTool(ctx, sessionID, names.stop, map[string]any{
			"subscription_id": subscriptionID,
		})
		if err != nil {
			return err
		}
		if res.IsError {
			return fmt.Errorf("unexpected_tool_error:%s", compact(res.textSummary()))
		}
		return nil
	})
	if err == nil {
		rec.recordStoppedWatch()
	}
	return err
}

func measure(ctx context.Context, rec *recorder, operation string, fn func(context.Context) error) (string, error) {
	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		rec.record(operation, duration, false, err)
		return "", err
	}
	rec.record(operation, duration, true, nil)
	return "", nil
}

func (c *mcpClient) initialize(ctx context.Context) (string, error) {
	_, sessionID, err := c.request(ctx, "", "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "dsx-exchange-mcp-load",
			"version": "0.1.0",
		},
	})
	return sessionID, err
}

func (c *mcpClient) initialized(ctx context.Context, sessionID string) error {
	_, _, err := c.post(ctx, sessionID, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	return err
}

func (c *mcpClient) listTools(ctx context.Context, sessionID string) ([]string, error) {
	raw, _, err := c.request(ctx, sessionID, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var result struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode_tools_list:%w", err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names, nil
}

func (c *mcpClient) listResources(ctx context.Context, sessionID string) ([]string, error) {
	raw, _, err := c.request(ctx, sessionID, "resources/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var result struct {
		Resources []struct {
			URI string `json:"uri"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode_resources_list:%w", err)
	}
	uris := make([]string, 0, len(result.Resources))
	for _, resource := range result.Resources {
		if resource.URI != "" {
			uris = append(uris, resource.URI)
		}
	}
	return uris, nil
}

func (c *mcpClient) readResource(ctx context.Context, sessionID string, uri string) error {
	raw, _, err := c.request(ctx, sessionID, "resources/read", map[string]any{
		"uri": uri,
	})
	if err != nil {
		return err
	}
	var result struct {
		Contents []struct {
			URI      string `json:"uri"`
			Text     string `json:"text"`
			MIMEType string `json:"mimeType"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("decode_resources_read:%w", err)
	}
	if len(result.Contents) == 0 || result.Contents[0].Text == "" {
		return fmt.Errorf("resource_empty:%s", uri)
	}
	return nil
}

func (c *mcpClient) callTool(ctx context.Context, sessionID, name string, args map[string]any) (toolCallResult, error) {
	raw, _, err := c.request(ctx, sessionID, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return toolCallResult{}, err
	}
	var result toolCallResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return toolCallResult{}, fmt.Errorf("decode_tools_call:%w", err)
	}
	return result, nil
}

func (c *mcpClient) request(ctx context.Context, sessionID, method string, params map[string]any) (json.RawMessage, string, error) {
	c.nextID++
	resp, newSessionID, err := c.post(ctx, sessionID, map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, newSessionID, err
	}
	if resp.Error != nil {
		return nil, newSessionID, fmt.Errorf("jsonrpc_error_%d:%s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, newSessionID, nil
}

func (c *mcpClient) post(ctx context.Context, sessionID string, payload map[string]any) (rpcResponse, string, error) {
	if c.limiter != nil {
		if err := c.limiter.wait(ctx); err != nil {
			return rpcResponse{}, "", err
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return rpcResponse{}, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return rpcResponse{}, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	res, err := c.httpc.Do(req)
	if err != nil {
		return rpcResponse{}, "", fmt.Errorf("http_request:%w", err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return rpcResponse{}, res.Header.Get("Mcp-Session-Id"), fmt.Errorf("read_response:%w", err)
	}
	if res.StatusCode >= http.StatusBadRequest {
		return rpcResponse{}, res.Header.Get("Mcp-Session-Id"), fmt.Errorf("http_%d:%s", res.StatusCode, strings.TrimSpace(string(raw)))
	}

	data := lastMCPResponseData(raw)
	if len(data) == 0 {
		return rpcResponse{}, res.Header.Get("Mcp-Session-Id"), nil
	}
	var decoded rpcResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return rpcResponse{}, res.Header.Get("Mcp-Session-Id"), fmt.Errorf("decode_mcp_response:%w", err)
	}
	return decoded, res.Header.Get("Mcp-Session-Id"), nil
}

func lastMCPResponseData(body []byte) []byte {
	body = bytes.TrimSpace(body)
	if len(body) == 0 || bytes.HasPrefix(body, []byte("{")) {
		return body
	}
	var last []byte
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(data) == 0 || bytes.Equal(data, []byte("[DONE]")) {
			continue
		}
		last = append(last[:0], data...)
	}
	return last
}

func (r toolCallResult) textSummary() string {
	var texts []string
	for _, item := range r.Content {
		if item.Text != "" {
			texts = append(texts, item.Text)
		}
	}
	return strings.Join(texts, "\n")
}

func (r toolCallResult) lastText() string {
	for i := len(r.Content) - 1; i >= 0; i-- {
		if r.Content[i].Text != "" {
			return r.Content[i].Text
		}
	}
	return ""
}

type toolNames struct {
	subscribe    string
	readRetained string
	describe     string
	find         string
	start        string
	read         string
	status       string
	stop         string
}

func resolveTools(names []string) toolNames {
	return toolNames{
		subscribe:    chooseToolName(names, toolSubscribe),
		readRetained: chooseToolName(names, toolReadRetained),
		describe:     chooseToolName(names, toolDescribeTopic),
		find:         chooseToolName(names, toolFindTopics),
		start:        chooseToolName(names, toolStartSubscription),
		read:         chooseToolName(names, toolReadSubscription),
		status:       chooseToolName(names, toolStatusSubscription),
		stop:         chooseToolName(names, toolStopSubscription),
	}
}

func (n toolNames) require(scenario string) error {
	if scenario == "schema-resources" {
		return nil
	}
	if n.describe == "" {
		return errors.New("missing_tool_describe_topic")
	}
	if n.find == "" {
		return errors.New("missing_tool_find_topics")
	}
	if scenario == "bounded-read" || scenario == "mixed-stateless" || scenario == "mixed" {
		if n.readRetained == "" {
			return errors.New("missing_tool_read_retained")
		}
		if n.subscribe == "" {
			return errors.New("missing_tool_subscribe")
		}
	}
	if scenario == "watch" || scenario == "watch-hold" || scenario == "watch-status-hold" || scenario == "sticky-check" {
		if n.start == "" || n.status == "" || n.stop == "" {
			return errors.New("missing_watch_tool")
		}
	}
	if scenario == "watch" || scenario == "watch-hold" || scenario == "sticky-check" {
		if n.read == "" {
			return errors.New("missing_watch_read_tool")
		}
	}
	return nil
}

func chooseToolName(names []string, baseName string) string {
	for _, name := range names {
		if name == baseName {
			return name
		}
	}
	for _, name := range names {
		if strings.HasSuffix(name, "_"+baseName) {
			return name
		}
	}
	return ""
}

func (r *recorder) record(operation string, duration time.Duration, success bool, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.totalRequests++
	stats := r.byOperation[operation]
	if stats == nil {
		stats = &operationStats{}
		r.byOperation[operation] = stats
	}
	stats.count++
	stats.latencies = append(stats.latencies, duration)
	if success {
		r.successes++
		stats.successes++
		return
	}
	r.failures++
	stats.failures++
	if err != nil {
		code := classifyError(err)
		r.errors[code]++
		if stats.errors == nil {
			stats.errors = map[string]uint64{}
		}
		stats.errors[code]++
		r.recordStickyErrorCountersLocked(err.Error())
		r.recordErrorSampleLocked(code, err)
	}
}

func (r *recorder) recordError(code string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures++
	r.errors[code]++
	r.recordStickyErrorCountersLocked(code)
	r.recordErrorSampleLocked(code, errors.New(code))
}

func (r *recorder) recordStickyErrorCountersLocked(msg string) {
	msg = compact(msg)
	if strings.Contains(msg, "session not found") {
		r.sessionNotFoundErrors++
	}
	if strings.Contains(msg, "subscription_not_found") {
		r.subscriptionNotFoundErrors++
	}
}

func (r *recorder) recordErrorSampleLocked(code string, err error) {
	if err == nil {
		return
	}
	samples := r.errorSamples[code]
	if len(samples) >= maxErrorSamples {
		return
	}
	sample := compact(err.Error())
	if len(sample) > 300 {
		sample = sample[:300] + "..."
	}
	for _, existing := range samples {
		if existing == sample {
			return
		}
	}
	r.errorSamples[code] = append(samples, sample)
}

func (r *recorder) recordExpectedToolError() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expectedToolErrors++
}

func (r *recorder) recordInitializedSession() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initializedSessions++
}

func (r *recorder) recordStartedWatch() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startedWatches++
}

func (r *recorder) recordStoppedWatch() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stoppedWatches++
}

func (r *recorder) snapshot(endedAt time.Time) runReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	byOperation := map[string]operationSnapshot{}
	for op, stats := range r.byOperation {
		latencies := append([]time.Duration(nil), stats.latencies...)
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		byOperation[op] = operationSnapshot{
			Phase:           operationPhase(op),
			Count:           stats.count,
			Successes:       stats.successes,
			Failures:        stats.failures,
			P50Milliseconds: percentileMS(latencies, 0.50),
			P95Milliseconds: percentileMS(latencies, 0.95),
			P99Milliseconds: percentileMS(latencies, 0.99),
			Errors:          cloneErrors(stats.errors),
		}
	}
	durationSeconds := endedAt.Sub(r.startedAt).Seconds()
	throughput := 0.0
	if durationSeconds > 0 {
		throughput = float64(r.totalRequests) / durationSeconds
	}
	successRate := 0.0
	if r.totalRequests > 0 {
		successRate = float64(r.successes) / float64(r.totalRequests) * 100
	}
	return runReport{
		StartedAt:                  r.startedAt,
		EndedAt:                    endedAt,
		DurationSeconds:            durationSeconds,
		ThroughputRPS:              throughput,
		SuccessRate:                successRate,
		Endpoint:                   r.endpoint,
		Experiment:                 r.experiment,
		ExperimentDetail:           r.experimentDetail,
		Scenario:                   r.scenario,
		Sessions:                   r.sessions,
		BackendReplicas:            r.backendReplicas,
		StickySessionCheck:         r.stickySessionCheckResultLocked(),
		RateLimit:                  r.rateLimit,
		GatewayRateLimit:           r.gatewayRateLimit,
		ManifestName:               r.manifestName,
		BackendImageID:             r.backendImageID,
		LoadImageID:                r.loadImageID,
		ExperimentConfigHash:       r.experimentConfigHash,
		TokenTTLSecondsAtStart:     r.tokenTTLSecondsAtStart,
		Topic:                      r.topic,
		RetainedTopic:              r.retainedTopic,
		DeniedTopic:                r.deniedTopic,
		HTTPTimeoutSeconds:         r.httpTimeout.Seconds(),
		StartupRampSeconds:         r.startupRamp.Seconds(),
		PollIntervalSeconds:        r.pollInterval.Seconds(),
		SubscribeDurationS:         r.subscribeDuration,
		MaxMessages:                r.maxMessages,
		MaxBytes:                   r.maxBytes,
		WatchTTLS:                  r.watchTTL,
		BackendConnectS:            r.backendConnectS,
		BackendSubscribeS:          r.backendSubscribeS,
		BackendCollectMax:          r.backendCollectMax,
		BackendWatchStartMax:       r.backendWatchStartMax,
		TotalRequests:              r.totalRequests,
		Successes:                  r.successes,
		Failures:                   r.failures,
		ExpectedToolErrors:         r.expectedToolErrors,
		InitializedSessions:        r.initializedSessions,
		StartedWatches:             r.startedWatches,
		StoppedWatches:             r.stoppedWatches,
		SessionNotFoundErrors:      r.sessionNotFoundErrors,
		SubscriptionNotFoundErrors: r.subscriptionNotFoundErrors,
		ByOperation:                byOperation,
		Errors:                     cloneErrors(r.errors),
		ErrorSamples:               cloneErrorSamples(r.errorSamples),
	}
}

func (r *recorder) stickySessionCheckResultLocked() string {
	if r.scenario != "sticky-check" {
		return r.stickySessionCheck
	}
	if r.failures == 0 {
		return "pass"
	}
	return "fail"
}

func operationPhase(operation string) string {
	switch operation {
	case "initialize", "notifications_initialized", "tools_list", "start_subscription":
		return "startup"
	case "stop_subscription":
		return "cleanup"
	default:
		return "steady"
	}
}

func cloneErrors(in map[string]uint64) map[string]uint64 {
	out := make(map[string]uint64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneErrorSamples(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func percentileMS(latencies []time.Duration, p float64) float64 {
	if len(latencies) == 0 {
		return 0
	}
	if len(latencies) == 1 {
		return float64(latencies[0].Microseconds()) / 1000
	}
	idx := int(float64(len(latencies)-1) * p)
	return float64(latencies[idx].Microseconds()) / 1000
}

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	msg := compact(err.Error())
	switch {
	case strings.HasPrefix(msg, "http_"):
		return strings.SplitN(msg, ":", 2)[0]
	case strings.HasPrefix(msg, "jsonrpc_error_"):
		return strings.SplitN(msg, ":", 2)[0]
	case strings.HasPrefix(msg, "unexpected_tool_error:"):
		if code := classifyToolError(msg); code != "" {
			return "tool_error_" + code
		}
		return "unexpected_tool_error"
	case strings.HasPrefix(msg, "context deadline exceeded"):
		return "context_deadline"
	case strings.HasPrefix(msg, "context canceled"):
		return "context_canceled"
	case strings.HasPrefix(msg, "http_request:"):
		return "http_request"
	default:
		if len(msg) > 80 {
			return msg[:80]
		}
		return msg
	}
}

func classifyToolError(msg string) string {
	raw := strings.TrimPrefix(msg, "unexpected_tool_error:")
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return ""
	}
	return strings.TrimSpace(body.Error.Code)
}

func printTextReport(w io.Writer, report runReport) {
	fmt.Fprintf(w, "MCP load report\n")
	fmt.Fprintf(w, "  endpoint: %s\n", report.Endpoint)
	if report.Experiment != "" {
		fmt.Fprintf(w, "  experiment: %s\n", report.Experiment)
	}
	if report.ExperimentDetail != "" {
		fmt.Fprintf(w, "  experiment_detail: %s\n", report.ExperimentDetail)
	}
	fmt.Fprintf(w, "  scenario: %s\n", report.Scenario)
	fmt.Fprintf(w, "  sessions: %d\n", report.Sessions)
	if report.BackendReplicas > 0 || report.StickySessionCheck != "" {
		fmt.Fprintf(w, "  scale_metadata: backend_replicas=%d sticky_session_check=%s\n", report.BackendReplicas, report.StickySessionCheck)
	}
	fmt.Fprintf(w, "  reproducibility: config_hash=%s manifest=%s backend_image=%s load_image=%s token_ttl_start=%ds gateway_rps=%d\n",
		report.ExperimentConfigHash, report.ManifestName, report.BackendImageID, report.LoadImageID, report.TokenTTLSecondsAtStart, report.GatewayRateLimit)
	fmt.Fprintf(w, "  backend_mqtt_timeouts: connect=%ds subscribe=%ds http_timeout=%.1fs\n", report.BackendConnectS, report.BackendSubscribeS, report.HTTPTimeoutSeconds)
	fmt.Fprintf(w, "  backend_mqtt_admission: collect_max_per_pod=%d watch_start_max_per_pod=%d\n", report.BackendCollectMax, report.BackendWatchStartMax)
	fmt.Fprintf(w, "  workload_args: startup_ramp=%.1fs poll_interval=%.1fs subscribe_duration=%ds watch_ttl=%ds max_messages=%d max_bytes=%d\n", report.StartupRampSeconds, report.PollIntervalSeconds, report.SubscribeDurationS, report.WatchTTLS, report.MaxMessages, report.MaxBytes)
	fmt.Fprintf(w, "  duration: %.1fs\n", report.DurationSeconds)
	fmt.Fprintf(w, "  requests: %d success=%d failures=%d expected_tool_errors=%d throughput=%.2f/s success_rate=%.2f%%\n", report.TotalRequests, report.Successes, report.Failures, report.ExpectedToolErrors, report.ThroughputRPS, report.SuccessRate)
	fmt.Fprintf(w, "  initialized_sessions=%d started_watches=%d stopped_watches=%d\n", report.InitializedSessions, report.StartedWatches, report.StoppedWatches)
	fmt.Fprintf(w, "  sticky_errors: session_not_found=%d subscription_not_found=%d\n", report.SessionNotFoundErrors, report.SubscriptionNotFoundErrors)
	ops := make([]string, 0, len(report.ByOperation))
	for op := range report.ByOperation {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	for _, op := range ops {
		s := report.ByOperation[op]
		fmt.Fprintf(w, "  %-28s phase=%-7s count=%-6d ok=%-6d fail=%-4d p50=%7.2fms p95=%7.2fms p99=%7.2fms\n",
			op, s.Phase, s.Count, s.Successes, s.Failures, s.P50Milliseconds, s.P95Milliseconds, s.P99Milliseconds)
	}
	if len(report.Errors) > 0 {
		fmt.Fprintf(w, "  errors:\n")
		keys := make([]string, 0, len(report.Errors))
		for k := range report.Errors {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "    %s: %d\n", k, report.Errors[k])
			for _, sample := range report.ErrorSamples[k] {
				fmt.Fprintf(w, "      sample: %s\n", sample)
			}
		}
	}
}

func writeReports(dir string, reports []runReport) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	timestamp := time.Now().UTC().Format("20060102-150405")
	jsonPath := filepath.Join(dir, "dsx-exchange-mcp-load-"+timestamp+".json")
	textPath := filepath.Join(dir, "dsx-exchange-mcp-load-"+timestamp+".txt")
	csvPath := filepath.Join(dir, "dsx-exchange-mcp-load-"+timestamp+".csv")

	jsonFile, err := os.Create(jsonPath)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(jsonFile)
	enc.SetIndent("", "  ")
	if len(reports) == 1 {
		err = enc.Encode(reports[0])
	} else {
		err = enc.Encode(reports)
	}
	closeErr := jsonFile.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	textFile, err := os.Create(textPath)
	if err != nil {
		return err
	}
	for i, report := range reports {
		if i > 0 {
			fmt.Fprintln(textFile)
		}
		printTextReport(textFile, report)
	}
	if err := textFile.Close(); err != nil {
		return err
	}
	if err := writeCSVReport(csvPath, reports); err != nil {
		return err
	}
	bundleDir, err := writeReportBundle(dir, timestamp, reports)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "report files written: %s %s %s\n", jsonPath, textPath, csvPath)
	fmt.Fprintf(os.Stderr, "report bundle written: %s\n", bundleDir)
	return nil
}

func writeReportBundle(dir, timestamp string, reports []runReport) (string, error) {
	name := "dsx-exchange-mcp-load"
	if len(reports) > 0 && reports[0].Experiment != "" {
		name = reports[0].Experiment
	}
	bundleDir := filepath.Join(dir, sanitizeFilename(name)+"-"+timestamp)
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		return "", err
	}
	if err := writeJSONReport(filepath.Join(bundleDir, "report.json"), reports); err != nil {
		return "", err
	}
	if err := writeTextReport(filepath.Join(bundleDir, "report.txt"), reports); err != nil {
		return "", err
	}
	if err := writeCSVReport(filepath.Join(bundleDir, "report.csv"), reports); err != nil {
		return "", err
	}
	if err := writeConfigYAML(filepath.Join(bundleDir, "config.yaml"), reports); err != nil {
		return "", err
	}
	if err := writeSummaryMarkdown(filepath.Join(bundleDir, "summary.md"), reports); err != nil {
		return "", err
	}
	return bundleDir, nil
}

func writeJSONReport(path string, reports []runReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if len(reports) == 1 {
		err = enc.Encode(reports[0])
	} else {
		err = enc.Encode(reports)
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func writeTextReport(path string, reports []runReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	for i, report := range reports {
		if i > 0 {
			fmt.Fprintln(f)
		}
		printTextReport(f, report)
	}
	return f.Close()
}

func writeConfigYAML(path string, reports []runReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	fmt.Fprintln(f, "runs:")
	for _, report := range reports {
		fmt.Fprintf(f, "  - experiment: %q\n", report.Experiment)
		fmt.Fprintf(f, "    experiment_config_hash: %q\n", report.ExperimentConfigHash)
		fmt.Fprintf(f, "    manifest_name: %q\n", report.ManifestName)
		fmt.Fprintf(f, "    scenario: %q\n", report.Scenario)
		fmt.Fprintf(f, "    sessions: %d\n", report.Sessions)
		fmt.Fprintf(f, "    backend_replicas: %d\n", report.BackendReplicas)
		fmt.Fprintf(f, "    client_rate_limit_per_second: %d\n", report.RateLimit)
		fmt.Fprintf(f, "    gateway_rate_limit_rps: %d\n", report.GatewayRateLimit)
		fmt.Fprintf(f, "    duration_seconds: %.3f\n", report.DurationSeconds)
		fmt.Fprintf(f, "    startup_ramp_seconds: %.3f\n", report.StartupRampSeconds)
		fmt.Fprintf(f, "    poll_interval_seconds: %.3f\n", report.PollIntervalSeconds)
		fmt.Fprintf(f, "    http_timeout_seconds: %.3f\n", report.HTTPTimeoutSeconds)
		fmt.Fprintf(f, "    backend_mqtt_connect_timeout_seconds: %d\n", report.BackendConnectS)
		fmt.Fprintf(f, "    backend_mqtt_subscribe_timeout_seconds: %d\n", report.BackendSubscribeS)
		fmt.Fprintf(f, "    backend_mqtt_collect_max_concurrent_per_pod: %d\n", report.BackendCollectMax)
		fmt.Fprintf(f, "    backend_mqtt_watch_start_max_concurrent_per_pod: %d\n", report.BackendWatchStartMax)
		fmt.Fprintf(f, "    watch_ttl_seconds: %d\n", report.WatchTTLS)
		fmt.Fprintf(f, "    max_messages: %d\n", report.MaxMessages)
		fmt.Fprintf(f, "    max_bytes: %d\n", report.MaxBytes)
		fmt.Fprintf(f, "    token_ttl_seconds_at_start: %d\n", report.TokenTTLSecondsAtStart)
		fmt.Fprintf(f, "    backend_image_id: %q\n", report.BackendImageID)
		fmt.Fprintf(f, "    load_image_id: %q\n", report.LoadImageID)
		fmt.Fprintf(f, "    topic: %q\n", report.Topic)
		fmt.Fprintf(f, "    retained_topic: %q\n", report.RetainedTopic)
	}
	return f.Close()
}

func writeSummaryMarkdown(path string, reports []runReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	fmt.Fprintln(f, "# MCP Load Test Summary")
	fmt.Fprintln(f)
	fmt.Fprintln(f, "| Experiment | Scenario | Sessions | Replicas | Ramp | Poll | Success | Failures | Started Watches | Stopped Watches | Session 404 | Subscription Missing |")
	fmt.Fprintln(f, "|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	for _, report := range reports {
		fmt.Fprintf(f, "| %s | %s | %d | %d | %.0fs | %.3fs | %.2f%% | %d | %d | %d | %d | %d |\n",
			report.Experiment, report.Scenario, report.Sessions, report.BackendReplicas,
			report.StartupRampSeconds, report.PollIntervalSeconds, report.SuccessRate,
			report.Failures, report.StartedWatches, report.StoppedWatches,
			report.SessionNotFoundErrors, report.SubscriptionNotFoundErrors)
	}
	return f.Close()
}

func writeCSVReport(path string, reports []runReport) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	header := []string{
		"started_at",
		"ended_at",
		"experiment",
		"experiment_detail",
		"scenario",
		"sessions",
		"backend_replicas",
		"sticky_session_check",
		"rate_limit_per_second",
		"gateway_rate_limit_rps",
		"manifest_name",
		"backend_image_id",
		"load_image_id",
		"experiment_config_hash",
		"token_ttl_seconds_at_start",
		"endpoint",
		"topic",
		"retained_topic",
		"http_timeout_seconds",
		"startup_ramp_seconds",
		"poll_interval_seconds",
		"subscribe_duration_seconds",
		"max_messages",
		"max_bytes",
		"watch_ttl_seconds",
		"backend_mqtt_connect_timeout_seconds",
		"backend_mqtt_subscribe_timeout_seconds",
		"backend_mqtt_collect_max_concurrent_per_pod",
		"backend_mqtt_watch_start_max_concurrent_per_pod",
		"duration_seconds",
		"throughput_requests_per_second",
		"success_rate_percent",
		"total_requests",
		"successes",
		"failures",
		"expected_tool_errors",
		"initialized_sessions",
		"started_watches",
		"stopped_watches",
		"session_not_found_errors",
		"subscription_not_found_errors",
		"operation",
		"phase",
		"operation_count",
		"operation_successes",
		"operation_failures",
		"p50_ms",
		"p95_ms",
		"p99_ms",
		"operation_errors",
		"errors",
	}
	if err := w.Write(header); err != nil {
		_ = f.Close()
		return err
	}
	for _, report := range reports {
		ops := make([]string, 0, len(report.ByOperation))
		for op := range report.ByOperation {
			ops = append(ops, op)
		}
		sort.Strings(ops)
		for _, op := range ops {
			s := report.ByOperation[op]
			if err := w.Write([]string{
				report.StartedAt.Format(time.RFC3339Nano),
				report.EndedAt.Format(time.RFC3339Nano),
				report.Experiment,
				report.ExperimentDetail,
				report.Scenario,
				strconv.Itoa(report.Sessions),
				strconv.Itoa(report.BackendReplicas),
				report.StickySessionCheck,
				strconv.Itoa(report.RateLimit),
				strconv.Itoa(report.GatewayRateLimit),
				report.ManifestName,
				report.BackendImageID,
				report.LoadImageID,
				report.ExperimentConfigHash,
				strconv.Itoa(report.TokenTTLSecondsAtStart),
				report.Endpoint,
				report.Topic,
				report.RetainedTopic,
				formatFloat(report.HTTPTimeoutSeconds),
				formatFloat(report.StartupRampSeconds),
				formatFloat(report.PollIntervalSeconds),
				strconv.Itoa(report.SubscribeDurationS),
				strconv.Itoa(report.MaxMessages),
				strconv.Itoa(report.MaxBytes),
				strconv.Itoa(report.WatchTTLS),
				strconv.Itoa(report.BackendConnectS),
				strconv.Itoa(report.BackendSubscribeS),
				strconv.Itoa(report.BackendCollectMax),
				strconv.Itoa(report.BackendWatchStartMax),
				formatFloat(report.DurationSeconds),
				formatFloat(report.ThroughputRPS),
				formatFloat(report.SuccessRate),
				strconv.FormatUint(report.TotalRequests, 10),
				strconv.FormatUint(report.Successes, 10),
				strconv.FormatUint(report.Failures, 10),
				strconv.FormatUint(report.ExpectedToolErrors, 10),
				strconv.FormatUint(report.InitializedSessions, 10),
				strconv.FormatUint(report.StartedWatches, 10),
				strconv.FormatUint(report.StoppedWatches, 10),
				strconv.FormatUint(report.SessionNotFoundErrors, 10),
				strconv.FormatUint(report.SubscriptionNotFoundErrors, 10),
				op,
				s.Phase,
				strconv.FormatUint(s.Count, 10),
				strconv.FormatUint(s.Successes, 10),
				strconv.FormatUint(s.Failures, 10),
				formatFloat(s.P50Milliseconds),
				formatFloat(s.P95Milliseconds),
				formatFloat(s.P99Milliseconds),
				formatErrorSummary(s.Errors),
				formatErrorSummary(report.Errors),
			}); err != nil {
				_ = f.Close()
				return err
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 3, 64)
}

func sanitizeFilename(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "run"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "run"
	}
	return out
}

func formatErrorSummary(errorsByCode map[string]uint64) string {
	if len(errorsByCode) == 0 {
		return ""
	}
	keys := make([]string, 0, len(errorsByCode))
	for k := range errorsByCode {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+strconv.FormatUint(errorsByCode[k], 10))
	}
	return strings.Join(parts, ";")
}

func newRateLimiter(rate int) *rateLimiter {
	if rate <= 0 {
		return nil
	}
	ch := make(chan struct{}, rate)
	for i := 0; i < rate; i++ {
		ch <- struct{}{}
	}
	interval := time.Second / time.Duration(rate)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}()
	return &rateLimiter{ch: ch}
}

func (l *rateLimiter) wait(ctx context.Context) error {
	if l == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-l.ch:
		return nil
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return ""
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		var out int
		if _, err := fmt.Sscanf(v, "%d", &out); err == nil {
			return out
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if out, err := time.ParseDuration(v); err == nil {
			return out
		}
	}
	return fallback
}

func compact(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return s
}
