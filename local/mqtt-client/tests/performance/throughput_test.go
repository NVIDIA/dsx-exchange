// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package performance

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HdrHistogram/hdrhistogram-go"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/pkg/client"
	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/pkg/config"
	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/pkg/metrics"
)

// TestConfig holds configuration for performance tests
type TestConfig struct {
	PublisherBroker  string
	SubscriberBroker string
	Topic            string
	SubscriberTopic  string
	QoS              byte
	Retained         bool
	Pairs            int
	MessageSize      int
	Duration         time.Duration
	WarmupDuration   time.Duration
	Username         string
	Password         string
}

// TestResult holds performance test results
type TestResult struct {
	Duration      float64
	MessagesTotal int64
	Throughput    float64
	LatencyP50    float64
	LatencyP95    float64
	LatencyP99    float64
	LatencyMean   float64
	SuccessRate   float64
}

// TestReport contains full test information for reporting
type TestReport struct {
	TestName         string     `json:"test_name"`
	Timestamp        time.Time  `json:"timestamp"`
	Config           TestConfig `json:"config"`
	Result           TestResult `json:"result"`
	TargetThroughput float64    `json:"target_throughput"`
	Passed           bool       `json:"passed"`
}

// Global test reports collector
var (
	testReports   []TestReport
	testReportsMu sync.Mutex
)

// TestStats tracks statistics during test execution
type testStats struct {
	publishedCount atomic.Int64
	receivedCount  atomic.Int64
	pairHists      []*hdrhistogram.Histogram // One histogram per pair, preallocated
	startTime      time.Time
	endTime        time.Time
}

// MessageHeaderSize is the size of the timestamp header in bytes
const MessageHeaderSize = 8 // 8 bytes: Unix nano timestamp

// Default test configuration values.
var (
	DefaultWarmupDuration = envDuration("PERF_TEST_WARMUP", 2*time.Second)
	DefaultDuration       = envDuration("PERF_TEST_DURATION", 10*time.Second)
	DefaultMessageSize    = envInt("PERF_TEST_MESSAGE_SIZE", 1024)
	DefaultPairs          = envInt("PERF_TEST_PAIRS", 8)
	DefaultPublishDelay   = envDuration("PERF_PUBLISH_DELAY", 0)
)

const ReportsDir = "reports"

func envInt(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		panic(fmt.Sprintf("invalid %s=%q: must be a positive integer", name, raw))
	}

	return value
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}

	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		panic(fmt.Sprintf("invalid %s=%q: must be a positive duration", name, raw))
	}

	return value
}

func envFloat(name string, fallback float64) float64 {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < 0 {
		panic(fmt.Sprintf("invalid %s=%q: must be a non-negative number", name, raw))
	}

	return value
}

func qos0TargetThroughput() float64 {
	return envFloat("PERF_TARGET_QOS0", 0)
}

func qos1TargetThroughput() float64 {
	return envFloat("PERF_TARGET_QOS1", 0)
}

func effectiveMinSuccessRate() float64 {
	return envFloat("PERF_MIN_SUCCESS_RATE", 99.0)
}

func subscriberQoS(cfg TestConfig) byte {
	if cfg.PublisherBroker != cfg.SubscriberBroker {
		// Cross-account MQTT subscriptions are currently limited to QoS 0 by NATS.
		return 0
	}

	return cfg.QoS
}

// addTestReport adds a test report to the global collection
func addTestReport(report TestReport) {
	testReportsMu.Lock()
	defer testReportsMu.Unlock()
	testReports = append(testReports, report)
}

// generateReports writes all collected test reports to files
func generateReports() error {
	testReportsMu.Lock()
	defer testReportsMu.Unlock()

	if len(testReports) == 0 {
		return nil
	}

	// Create reports directory
	if err := os.MkdirAll(ReportsDir, 0755); err != nil {
		return fmt.Errorf("failed to create reports directory: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")

	// Generate JSON report
	if err := generateJSONReport(timestamp); err != nil {
		return fmt.Errorf("failed to generate JSON report: %w", err)
	}

	// Generate text summary report
	if err := generateTextReport(timestamp); err != nil {
		return fmt.Errorf("failed to generate text report: %w", err)
	}

	return nil
}

// generateJSONReport writes a JSON report file
func generateJSONReport(timestamp string) error {
	filename := filepath.Join(ReportsDir, fmt.Sprintf("perf-report-%s.json", timestamp))
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(testReports); err != nil {
		return err
	}

	fmt.Printf("\nJSON report written to: %s\n", filename)
	return nil
}

// generateTextReport writes a human-readable text report
func generateTextReport(timestamp string) error {
	filename := filepath.Join(ReportsDir, fmt.Sprintf("perf-report-%s.txt", timestamp))
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	fmt.Fprintf(file, "Performance Test Report\n")
	fmt.Fprintf(file, "Generated: %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(file, "===========================================\n\n")

	passedCount := 0
	failedCount := 0

	for _, report := range testReports {
		if report.Passed {
			passedCount++
		} else {
			failedCount++
		}

		status := "PASS"
		if !report.Passed {
			status = "FAIL"
		}

		fmt.Fprintf(file, "Test: %s [%s]\n", report.TestName, status)
		fmt.Fprintf(file, "Timestamp: %s\n", report.Timestamp.Format(time.RFC3339))
		fmt.Fprintf(file, "-------------------------------------------\n")
		fmt.Fprintf(file, "Configuration:\n")
		fmt.Fprintf(file, "  Publisher:   %s\n", report.Config.PublisherBroker)
		fmt.Fprintf(file, "  Subscriber:  %s\n", report.Config.SubscriberBroker)
		fmt.Fprintf(file, "  Topic:       %s\n", report.Config.Topic)
		if report.Config.SubscriberTopic != "" {
			fmt.Fprintf(file, "  Subscriber Topic: %s\n", report.Config.SubscriberTopic)
		}
		fmt.Fprintf(file, "  QoS:         %d\n", report.Config.QoS)
		fmt.Fprintf(file, "  Retained:    %v\n", report.Config.Retained)
		fmt.Fprintf(file, "  Pairs:       %d\n", report.Config.Pairs)
		fmt.Fprintf(file, "  Message Size: %d bytes\n", report.Config.MessageSize)
		fmt.Fprintf(file, "  Duration:    %s\n", report.Config.Duration)
		fmt.Fprintf(file, "\nResults:\n")
		fmt.Fprintf(file, "  Duration:       %.2fs\n", report.Result.Duration)
		fmt.Fprintf(file, "  Messages:       %d\n", report.Result.MessagesTotal)
		if report.TargetThroughput > 0 {
			fmt.Fprintf(file, "  Throughput:     %.2f msg/s (target: %.0f msg/s)\n",
				report.Result.Throughput, report.TargetThroughput)
		} else {
			fmt.Fprintf(file, "  Throughput:     %.2f msg/s\n", report.Result.Throughput)
		}
		fmt.Fprintf(file, "  Latency p50:    %.2f ms\n", report.Result.LatencyP50)
		fmt.Fprintf(file, "  Latency p95:    %.2f ms\n", report.Result.LatencyP95)
		fmt.Fprintf(file, "  Latency p99:    %.2f ms\n", report.Result.LatencyP99)
		fmt.Fprintf(file, "  Latency mean:   %.2f ms\n", report.Result.LatencyMean)
		fmt.Fprintf(file, "  Success Rate:   %.2f%%\n", report.Result.SuccessRate)
		fmt.Fprintf(file, "\n")
	}

	fmt.Fprintf(file, "===========================================\n")
	fmt.Fprintf(file, "Summary:\n")
	fmt.Fprintf(file, "  Total Tests: %d\n", len(testReports))
	fmt.Fprintf(file, "  Passed:      %d\n", passedCount)
	fmt.Fprintf(file, "  Failed:      %d\n", failedCount)
	fmt.Fprintf(file, "===========================================\n")

	fmt.Printf("Text report written to: %s\n", filename)
	fmt.Printf("\nTest Summary: %d passed, %d failed out of %d total\n",
		passedCount, failedCount, len(testReports))

	return nil
}

// RunThroughputTest executes a throughput performance test
func runThroughputTest(t *testing.T, cfg TestConfig) TestResult {
	t.Helper()

	// Validate message size
	if cfg.MessageSize < MessageHeaderSize {
		t.Fatalf("Message size must be at least %d bytes for header", MessageHeaderSize)
	}

	t.Logf("Starting test: %d pairs, duration %s", cfg.Pairs, cfg.Duration)
	t.Logf("Broker(s): pub=%s, sub=%s", cfg.PublisherBroker, cfg.SubscriberBroker)
	if DefaultPublishDelay > 0 {
		t.Logf("Publish delay: %s", DefaultPublishDelay)
	}

	// Preallocate histograms for all pairs
	pairHists := make([]*hdrhistogram.Histogram, cfg.Pairs)
	for i := 0; i < cfg.Pairs; i++ {
		// Track latencies from 1 microsecond to 5 seconds with 3 significant digits
		pairHists[i] = hdrhistogram.New(1, 5000000, 3)
	}

	stats := &testStats{
		startTime: time.Now(),
		pairHists: pairHists,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Channel to signal when to start publishing
	startPublishing := make(chan struct{})

	// Start publisher-subscriber pairs
	t.Logf("Starting %d publisher-subscriber pairs...", cfg.Pairs)
	g, gCtx := errgroup.WithContext(ctx)

	for i := 0; i < cfg.Pairs; i++ {
		pairID := i
		pairHist := pairHists[i]
		g.Go(func() error {
			return runTestPair(gCtx, pairID, cfg, stats, pairHist, startPublishing, t)
		})
	}

	// Warmup period to let subscribers connect and subscribe
	t.Logf("Warmup period: %s", cfg.WarmupDuration)
	time.Sleep(cfg.WarmupDuration)
	t.Logf("Warmup complete, starting measurement...")

	measurementStart := time.Now()

	// Signal all pairs to start publishing
	close(startPublishing)

	// Run for specified duration
	t.Logf("Running for duration: %s", cfg.Duration)
	time.Sleep(cfg.Duration)
	stats.endTime = time.Now()
	cancel()

	// Wait for all pairs to shutdown
	t.Logf("Shutting down publisher-subscriber pairs...")
	if err := g.Wait(); err != nil {
		t.Errorf("Error during test execution: %v", err)
	}
	t.Logf("Test complete")

	// Calculate results
	duration := stats.endTime.Sub(measurementStart).Seconds()
	received := stats.receivedCount.Load()
	published := stats.publishedCount.Load()

	result := TestResult{
		Duration:      duration,
		MessagesTotal: received,
		Throughput:    float64(received) / duration,
	}

	// Calculate success rate safely
	if published > 0 {
		result.SuccessRate = float64(received) / float64(published) * 100
	} else {
		result.SuccessRate = 0
	}

	// Merge all per-pair histograms
	var mergedHist *hdrhistogram.Histogram
	for i, hist := range stats.pairHists {
		if i == 0 {
			mergedHist = hist
		} else {
			mergedHist.Merge(hist)
		}
	}

	// Extract latency statistics from merged histogram
	if mergedHist != nil && mergedHist.TotalCount() > 0 {
		// Convert from microseconds to milliseconds
		result.LatencyP50 = float64(mergedHist.ValueAtQuantile(50.0)) / 1000.0
		result.LatencyP95 = float64(mergedHist.ValueAtQuantile(95.0)) / 1000.0
		result.LatencyP99 = float64(mergedHist.ValueAtQuantile(99.0)) / 1000.0
		result.LatencyMean = mergedHist.Mean() / 1000.0
	}

	return result
}

func runTestPair(ctx context.Context, id int, cfg TestConfig, stats *testStats, pairHist *hdrhistogram.Histogram, startPublishing <-chan struct{}, t *testing.T) error {
	topicSuffix := fmt.Sprintf("%d/%s", id, uuid.New().String())
	pubTopic := fmt.Sprintf("%s/%s", cfg.Topic, topicSuffix)
	subTopicBase := cfg.SubscriberTopic
	if subTopicBase == "" {
		subTopicBase = cfg.Topic
	}
	subTopic := fmt.Sprintf("%s/%s", subTopicBase, topicSuffix)
	t.Logf("Pair %d: Starting", id)

	// Local mutex to protect histogram (MQTT client may invoke handler concurrently)
	var histMu sync.Mutex

	// Atomic counter for in-flight messages: incremented on publish, decremented on receive
	var inFlight atomic.Int64
	var publishingDone atomic.Bool
	allReceived := make(chan struct{})
	var allReceivedOnce sync.Once

	// Per-pair counters for reporting
	var pairPublished atomic.Int64
	var pairReceived atomic.Int64

	// Create publisher client
	pubClientID := fmt.Sprintf("perf-pub-%d-%s", id, uuid.New().String())
	pubClientCfg := client.Config{
		Broker:   cfg.PublisherBroker,
		ClientID: pubClientID,
		Username: cfg.Username,
		Password: cfg.Password,
		QoS:      cfg.QoS,
		TLS:      false,
	}

	t.Logf("Pair %d: Creating publisher client...", id)
	pubClient, err := client.New(pubClientCfg)
	if err != nil {
		return fmt.Errorf("pair %d: failed to create publisher client: %w", id, err)
	}

	// Create subscriber client
	subClientID := fmt.Sprintf("perf-sub-%d-%s", id, uuid.New().String())
	subClientCfg := client.Config{
		Broker:   cfg.SubscriberBroker,
		ClientID: subClientID,
		Username: cfg.Username,
		Password: cfg.Password,
		QoS:      subscriberQoS(cfg),
		TLS:      false,
	}

	t.Logf("Pair %d: Creating subscriber client...", id)
	subClient, err := client.New(subClientCfg)
	if err != nil {
		return fmt.Errorf("pair %d: failed to create subscriber client: %w", id, err)
	}

	// Connect both clients
	t.Logf("Pair %d: Connecting publisher to %s...", id, cfg.PublisherBroker)
	if err := pubClient.Connect(); err != nil {
		return fmt.Errorf("pair %d: failed to connect publisher: %w", id, err)
	}
	defer pubClient.Disconnect()

	t.Logf("Pair %d: Connecting subscriber to %s...", id, cfg.SubscriberBroker)
	if err := subClient.Connect(); err != nil {
		return fmt.Errorf("pair %d: failed to connect subscriber: %w", id, err)
	}
	defer subClient.Disconnect()

	t.Logf("Pair %d: Both clients connected successfully", id)

	// Subscribe before publishing
	handler := func(client mqtt.Client, msg mqtt.Message) {
		stats.receivedCount.Add(1)
		pairReceived.Add(1)

		// Decrement in-flight counter and close channel when it reaches 0 (only if publishing is done)
		if inFlight.Add(-1) == 0 && publishingDone.Load() {
			allReceivedOnce.Do(func() {
				close(allReceived)
			})
		}

		// Extract header for latency calculation
		if len(msg.Payload()) >= MessageHeaderSize {
			publishTimeNano := int64(binary.LittleEndian.Uint64(msg.Payload()[0:8]))
			publishTime := time.Unix(0, publishTimeNano)

			// Calculate latency
			latencyDuration := time.Since(publishTime)
			latencySeconds := latencyDuration.Seconds()
			latencyMicros := latencyDuration.Microseconds()

			// Record to per-pair histogram (protected by local mutex)
			histMu.Lock()
			if err := pairHist.RecordValue(latencyMicros); err != nil {
				// Value out of range, skip
			}
			histMu.Unlock()

			// Record to Prometheus summary for observability
			metrics.RecordEndToEndLatency(
				cfg.PublisherBroker,
				cfg.SubscriberBroker,
				subTopic,
				strconv.Itoa(int(cfg.QoS)),
				strconv.FormatBool(cfg.Retained),
				"", // federation flag - empty for now
				latencySeconds,
			)
		}
	}

	t.Logf("Pair %d: Subscribing to topic '%s'...", id, subTopic)
	if err := subClient.Subscribe(subTopic, subscriberQoS(cfg), handler); err != nil {
		return fmt.Errorf("pair %d: failed to subscribe: %w", id, err)
	}
	t.Logf("Pair %d: Subscribed successfully", id)

	// Wait for signal to start publishing
	select {
	case <-startPublishing:
		t.Logf("Pair %d: Starting to publish...", id)
	case <-ctx.Done():
		t.Logf("Pair %d: Context cancelled before publishing started", id)
		return nil
	}

	// Precompute payload buffer
	payload := make([]byte, cfg.MessageSize)
	// Fill with pattern
	for i := MessageHeaderSize; i < len(payload); i++ {
		payload[i] = byte(i % 256)
	}

	// Publish loop
	var sequence uint32
	for {
		select {
		case <-ctx.Done():
			// Publishing stopped, wait for all messages to be received
			published := pairPublished.Load()
			t.Logf("Pair %d: Publisher stopped, sent %d messages, waiting for subscriber...", id, published)

			// Mark publishing as done
			publishingDone.Store(true)
			if inFlight.Load() == 0 {
				allReceivedOnce.Do(func() {
					close(allReceived)
				})
			}

			// Wait for channel to close (when in-flight reaches 0)
			ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			select {
			case <-allReceived:
				break
			case <-ctx.Done():
				return fmt.Errorf("pair %d: timed out waiting for all messages to be received", id)
			}

			received := pairReceived.Load()
			t.Logf("Pair %d: All messages received (%d/%d)", id, received, published)
			return nil
		default:
			// Increment in-flight counter
			inFlight.Add(1)

			// Encode timestamp into payload (only field needed for latency)
			binary.LittleEndian.PutUint64(payload[0:8], uint64(time.Now().UnixNano()))

			if err := pubClient.Publish(pubTopic, payload, cfg.QoS, cfg.Retained); err != nil {
				// Decrement on error and signal if zero
				if inFlight.Add(-1) == 0 {
					allReceivedOnce.Do(func() {
						close(allReceived)
					})
				}
				return fmt.Errorf("pair %d: publish failed at sequence %d: %w", id, sequence, err)
			}

			stats.publishedCount.Add(1)
			pairPublished.Add(1)
			sequence++

			if DefaultPublishDelay > 0 {
				select {
				case <-ctx.Done():
				case <-time.After(DefaultPublishDelay):
				}
			}
		}
	}
}

func logResults(t *testing.T, testName string, result TestResult, targetThroughput float64, cfg TestConfig, passed bool) {
	t.Helper()

	t.Logf("=== %s ===", testName)
	t.Logf("Duration:       %.2fs", result.Duration)
	t.Logf("Messages:       %d", result.MessagesTotal)
	if targetThroughput > 0 {
		t.Logf("Throughput:     %.2f msg/s (target: %.0f msg/s)", result.Throughput, targetThroughput)
	} else {
		t.Logf("Throughput:     %.2f msg/s", result.Throughput)
	}
	t.Logf("Latency p50:    %.2f ms", result.LatencyP50)
	t.Logf("Latency p95:    %.2f ms", result.LatencyP95)
	t.Logf("Latency p99:    %.2f ms", result.LatencyP99)
	t.Logf("Latency mean:   %.2f ms", result.LatencyMean)
	t.Logf("Success Rate:   %.2f%%", result.SuccessRate)

	// Save report for later
	report := TestReport{
		TestName:         testName,
		Timestamp:        time.Now(),
		Config:           cfg,
		Result:           result,
		TargetThroughput: targetThroughput,
		Passed:           passed,
	}
	addTestReport(report)
}

func assertThroughput(t *testing.T, result TestResult, targetThroughput float64, tolerance float64) bool {
	t.Helper()

	passed := true
	minSuccessRate := effectiveMinSuccessRate()

	if targetThroughput > 0 && result.Throughput < targetThroughput*tolerance {
		t.Errorf("Throughput %.2f msg/s is below target %.0f msg/s (with %.0f%% tolerance)",
			result.Throughput, targetThroughput, tolerance*100)
		passed = false
	}

	if result.SuccessRate < minSuccessRate {
		t.Errorf("Success rate %.2f%% is below %.2f%%", result.SuccessRate, minSuccessRate)
		passed = false
	}

	return passed
}

func getCSCBroker() string {
	return config.GetCSCBrokerURL()
}

func getCPC1Broker() string {
	return config.GetCPC1BrokerURL()
}

// TestMain runs all tests and generates reports at the end
func TestMain(m *testing.M) {
	// Run all tests
	exitCode := m.Run()

	// Generate reports
	if err := generateReports(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate reports: %v\n", err)
	}

	os.Exit(exitCode)
}

// Local tests - single cluster
func TestThroughputQoS0_Local(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	broker := getCSCBroker()

	cfg := TestConfig{
		PublisherBroker:  broker,
		SubscriberBroker: broker,
		Topic:            "perf/qos0-local",
		QoS:              0,
		Retained:         false,
		Pairs:            DefaultPairs,
		Duration:         DefaultDuration,
		MessageSize:      DefaultMessageSize,
		WarmupDuration:   DefaultWarmupDuration,
	}

	result := runThroughputTest(t, cfg)
	targetThroughput := qos0TargetThroughput()
	passed := assertThroughput(t, result, targetThroughput, 1.0)
	logResults(t, "Local QoS 0", result, targetThroughput, cfg, passed)
}

func TestThroughputQoS0Retained_Local(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	broker := getCSCBroker()

	cfg := TestConfig{
		PublisherBroker:  broker,
		SubscriberBroker: broker,
		Topic:            "perf/qos0-retained-local",
		QoS:              0,
		Retained:         true,
		Pairs:            DefaultPairs,
		Duration:         DefaultDuration,
		MessageSize:      DefaultMessageSize,
		WarmupDuration:   DefaultWarmupDuration, // Uses default duration and message size
	}

	result := runThroughputTest(t, cfg)
	targetThroughput := qos1TargetThroughput()
	passed := assertThroughput(t, result, targetThroughput, 1.0)
	logResults(t, "Local QoS 0 + Retained", result, targetThroughput, cfg, passed)
}

func TestThroughputQoS1_Local(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	broker := getCSCBroker()

	cfg := TestConfig{
		PublisherBroker:  broker,
		SubscriberBroker: broker,
		Topic:            "perf/qos1-local",
		QoS:              1,
		Retained:         false,
		Pairs:            DefaultPairs,
		Duration:         DefaultDuration,
		MessageSize:      DefaultMessageSize,
		WarmupDuration:   DefaultWarmupDuration, // Uses default duration and message size
	}

	result := runThroughputTest(t, cfg)
	targetThroughput := qos1TargetThroughput()
	passed := assertThroughput(t, result, targetThroughput, 1.0)
	logResults(t, "Local QoS 1", result, targetThroughput, cfg, passed)
}

func TestThroughputQoS1Retained_Local(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	broker := getCSCBroker()

	cfg := TestConfig{
		PublisherBroker:  broker,
		SubscriberBroker: broker,
		Topic:            "perf/qos1-retained-local",
		QoS:              1,
		Retained:         true,
		Pairs:            DefaultPairs,
		Duration:         DefaultDuration,
		MessageSize:      DefaultMessageSize,
		WarmupDuration:   DefaultWarmupDuration,
	}

	result := runThroughputTest(t, cfg)
	targetThroughput := qos1TargetThroughput()
	passed := assertThroughput(t, result, targetThroughput, 1.0)
	logResults(t, "Local QoS 1 + Retained", result, targetThroughput, cfg, passed)
}

// Federation tests - CPC → CSC
func TestThroughputQoS0_CPCtoCSC(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	cpc1, csc := getCPC1Broker(), getCSCBroker()

	cfg := TestConfig{
		PublisherBroker:  cpc1,
		SubscriberBroker: csc,
		Topic:            "sensor/perf/qos0-c2c",
		SubscriberTopic:  "cpc/1/sensor/perf/qos0-c2c",
		QoS:              0,
		Retained:         false,
		Pairs:            DefaultPairs,
		Duration:         DefaultDuration,
		MessageSize:      DefaultMessageSize,
		WarmupDuration:   DefaultWarmupDuration,
	}

	result := runThroughputTest(t, cfg)
	targetThroughput := qos0TargetThroughput()
	passed := assertThroughput(t, result, targetThroughput, 0.80)
	logResults(t, "Federation CPC→CSC QoS 0", result, targetThroughput, cfg, passed)
}

func TestThroughputQoS0Retained_CPCtoCSC(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	cpc1, csc := getCPC1Broker(), getCSCBroker()

	cfg := TestConfig{
		PublisherBroker:  cpc1,
		SubscriberBroker: csc,
		Topic:            "sensor/perf/qos0-retained-c2c",
		SubscriberTopic:  "cpc/1/sensor/perf/qos0-retained-c2c",
		QoS:              0,
		Retained:         true,
		Pairs:            DefaultPairs,
		Duration:         DefaultDuration,
		MessageSize:      DefaultMessageSize,
		WarmupDuration:   DefaultWarmupDuration,
	}

	result := runThroughputTest(t, cfg)
	targetThroughput := qos1TargetThroughput()
	passed := assertThroughput(t, result, targetThroughput, 0.80)
	logResults(t, "Federation CPC→CSC QoS 0 + Retained", result, targetThroughput, cfg, passed)
}

func TestThroughputQoS1_CPCtoCSC(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	cpc1, csc := getCPC1Broker(), getCSCBroker()

	cfg := TestConfig{
		PublisherBroker:  cpc1,
		SubscriberBroker: csc,
		Topic:            "sensor/perf/qos1-c2c",
		SubscriberTopic:  "cpc/1/sensor/perf/qos1-c2c",
		QoS:              1,
		Retained:         false,
		Pairs:            DefaultPairs,
		Duration:         DefaultDuration,
		MessageSize:      DefaultMessageSize,
		WarmupDuration:   DefaultWarmupDuration,
	}

	result := runThroughputTest(t, cfg)
	targetThroughput := qos1TargetThroughput()
	passed := assertThroughput(t, result, targetThroughput, 0.80)
	logResults(t, "Federation CPC→CSC QoS 1", result, targetThroughput, cfg, passed)
}

func TestThroughputQoS1Retained_CPCtoCSC(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	cpc1, csc := getCPC1Broker(), getCSCBroker()

	cfg := TestConfig{
		PublisherBroker:  cpc1,
		SubscriberBroker: csc,
		Topic:            "sensor/perf/qos1-retained-c2c",
		SubscriberTopic:  "cpc/1/sensor/perf/qos1-retained-c2c",
		QoS:              1,
		Retained:         true,
		Pairs:            DefaultPairs,
		Duration:         DefaultDuration,
		MessageSize:      DefaultMessageSize,
		WarmupDuration:   DefaultWarmupDuration,
	}

	result := runThroughputTest(t, cfg)
	targetThroughput := qos1TargetThroughput()
	passed := assertThroughput(t, result, targetThroughput, 0.80)
	logResults(t, "Federation CPC→CSC QoS 1 + Retained", result, targetThroughput, cfg, passed)
}

// Federation tests - CSC → CPC
func TestThroughputQoS0_CSCtoCPC(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	csc, cpc1 := getCSCBroker(), getCPC1Broker()

	cfg := TestConfig{
		PublisherBroker:  csc,
		SubscriberBroker: cpc1,
		Topic:            "cpc/1/command/perf/qos0-c2p",
		SubscriberTopic:  "command/perf/qos0-c2p",
		QoS:              0,
		Retained:         false,
		Pairs:            DefaultPairs,
		Duration:         DefaultDuration,
		MessageSize:      DefaultMessageSize,
		WarmupDuration:   DefaultWarmupDuration,
	}

	result := runThroughputTest(t, cfg)
	targetThroughput := qos0TargetThroughput()
	passed := assertThroughput(t, result, targetThroughput, 0.80)
	logResults(t, "Federation CSC→CPC QoS 0", result, targetThroughput, cfg, passed)
}

func TestThroughputQoS0Retained_CSCtoCPC(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	csc, cpc1 := getCSCBroker(), getCPC1Broker()

	cfg := TestConfig{
		PublisherBroker:  csc,
		SubscriberBroker: cpc1,
		Topic:            "cpc/1/command/perf/qos0-retained-c2p",
		SubscriberTopic:  "command/perf/qos0-retained-c2p",
		QoS:              0,
		Retained:         true,
		Pairs:            DefaultPairs,
		Duration:         DefaultDuration,
		MessageSize:      DefaultMessageSize,
		WarmupDuration:   DefaultWarmupDuration,
	}

	result := runThroughputTest(t, cfg)
	targetThroughput := qos1TargetThroughput()
	passed := assertThroughput(t, result, targetThroughput, 0.80)
	logResults(t, "Federation CSC→CPC QoS 0 + Retained", result, targetThroughput, cfg, passed)
}

func TestThroughputQoS1_CSCtoCPC(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	csc, cpc1 := getCSCBroker(), getCPC1Broker()

	cfg := TestConfig{
		PublisherBroker:  csc,
		SubscriberBroker: cpc1,
		Topic:            "cpc/1/command/perf/qos1-c2p",
		SubscriberTopic:  "command/perf/qos1-c2p",
		QoS:              1,
		Retained:         false,
		Pairs:            DefaultPairs,
		Duration:         DefaultDuration,
		MessageSize:      DefaultMessageSize,
		WarmupDuration:   DefaultWarmupDuration,
	}

	result := runThroughputTest(t, cfg)
	targetThroughput := qos1TargetThroughput()
	passed := assertThroughput(t, result, targetThroughput, 0.80)
	logResults(t, "Federation CSC→CPC QoS 1", result, targetThroughput, cfg, passed)
}

func TestThroughputQoS1Retained_CSCtoCPC(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}
	csc, cpc1 := getCSCBroker(), getCPC1Broker()

	cfg := TestConfig{
		PublisherBroker:  csc,
		SubscriberBroker: cpc1,
		Topic:            "cpc/1/command/perf/qos1-retained-c2p",
		SubscriberTopic:  "command/perf/qos1-retained-c2p",
		QoS:              1,
		Retained:         true,
		Pairs:            DefaultPairs,
		Duration:         DefaultDuration,
		MessageSize:      DefaultMessageSize,
		WarmupDuration:   DefaultWarmupDuration,
	}

	result := runThroughputTest(t, cfg)
	targetThroughput := qos1TargetThroughput()
	passed := assertThroughput(t, result, targetThroughput, 0.80)
	logResults(t, "Federation CSC→CPC QoS 1 + Retained", result, targetThroughput, cfg, passed)
}

// TODO: TestConnectionScaling - Test connection scaling.

// TODO: TestMessageSizes - Test various message sizes.
