// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"bytes"
	"strconv"
	"time"

	"github.com/VictoriaMetrics/metrics"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// Collector collects broker capability metrics during benchmark execution
type Collector struct {
	// Scenario metadata
	ScenarioName string
	StartTime    time.Time
	EndTime      time.Time
	PublishStart time.Time
	PublishEnd   time.Time

	// Metrics set for this collector
	set *metrics.Set

	// Connection metrics
	totalConnections   *metrics.Counter
	successfulConns    *metrics.Counter
	failedConns        *metrics.Counter
	connectionRate     *metrics.Gauge
	maxConcurrentConns *metrics.Gauge
	currentConns       *metrics.Counter

	// Client counts
	NumPublishers  int64
	NumSubscribers int64
	NumTopics      int64
	MessageSize    int64

	// Message metrics
	publishedMessages   *metrics.Counter
	receivedMessages    *metrics.Counter
	failedPublishes     *metrics.Counter
	failedSubscriptions *metrics.Counter

	// Latency tracking
	latencySummary *metrics.Summary
}

// NewCollector creates a new metrics collector
func NewCollector(scenarioName string) *Collector {
	set := metrics.NewSet()

	return &Collector{
		ScenarioName:        scenarioName,
		StartTime:           time.Now(),
		set:                 set,
		totalConnections:    set.NewCounter("connections_total"),
		successfulConns:     set.NewCounter("connections_successful"),
		failedConns:         set.NewCounter("connections_failed"),
		connectionRate:      set.NewGauge("connection_rate", nil),
		maxConcurrentConns:  set.NewGauge("max_concurrent_connections", nil),
		currentConns:        set.NewCounter("current_connections"),
		publishedMessages:   set.NewCounter("messages_published"),
		receivedMessages:    set.NewCounter("messages_received"),
		failedPublishes:     set.NewCounter("messages_failed_publish"),
		failedSubscriptions: set.NewCounter("subscriptions_failed"),
		latencySummary:      set.NewSummary("latency_seconds"),
	}
}

// Start marks the beginning of the benchmark
func (c *Collector) Start() {
	c.StartTime = time.Now()
}

// End marks the end of the benchmark
func (c *Collector) End() {
	c.EndTime = time.Now()
}

// RecordConnection records a connection attempt
func (c *Collector) RecordConnection(success bool) {
	c.totalConnections.Inc()
	if success {
		c.successfulConns.Inc()
		c.currentConns.Inc()

		// Update max concurrent if needed
		current := float64(c.currentConns.Get())
		if current > c.maxConcurrentConns.Get() {
			c.maxConcurrentConns.Set(current)
		}
	} else {
		c.failedConns.Inc()
	}
}

// RecordDisconnection records a disconnection
func (c *Collector) RecordDisconnection() {
	c.currentConns.Add(-1)
}

// RecordPublish records a published message
func (c *Collector) RecordPublish(success bool) {
	if success {
		c.publishedMessages.Inc()
	} else {
		c.failedPublishes.Inc()
	}
}

// RecordReceive records a received message
func (c *Collector) RecordReceive() {
	c.receivedMessages.Inc()
}

// RecordLatency records message end-to-end latency
func (c *Collector) RecordLatency(latency time.Duration) {
	c.latencySummary.Update(latency.Seconds())
}

// RecordSubscription records a subscription attempt
func (c *Collector) RecordSubscription(success bool) {
	if !success {
		c.failedSubscriptions.Inc()
	}
}

// SetConnectionRate sets the connection rate
func (c *Collector) SetConnectionRate(rate float64) {
	c.connectionRate.Set(rate)
}

// GetDuration returns the total benchmark duration
func (c *Collector) GetDuration() time.Duration {
	if c.EndTime.IsZero() {
		return time.Since(c.StartTime)
	}
	return c.EndTime.Sub(c.StartTime)
}

// GetPublishRate returns messages published per second
func (c *Collector) GetPublishRate() float64 {
	// Use publish phase duration if available, otherwise total duration
	var duration float64
	if !c.PublishStart.IsZero() && !c.PublishEnd.IsZero() {
		duration = c.PublishEnd.Sub(c.PublishStart).Seconds()
	} else {
		duration = c.GetDuration().Seconds()
	}

	if duration == 0 {
		return 0
	}
	return float64(c.publishedMessages.Get()) / duration
}

// GetSubscribeRate returns messages received per second
func (c *Collector) GetSubscribeRate() float64 {
	// Use time from publish start to end (includes publishing + drain time)
	var duration float64
	if !c.PublishStart.IsZero() && !c.EndTime.IsZero() {
		duration = c.EndTime.Sub(c.PublishStart).Seconds()
	} else if !c.PublishStart.IsZero() {
		duration = time.Since(c.PublishStart).Seconds()
	} else {
		duration = c.GetDuration().Seconds()
	}

	if duration == 0 {
		return 0
	}
	return float64(c.receivedMessages.Get()) / duration
}

// GetSuccessRate returns the overall success rate
func (c *Collector) GetSuccessRate() float64 {
	published := c.publishedMessages.Get()
	failed := c.failedPublishes.Get()
	total := published + failed
	if total == 0 {
		return 0
	}
	return float64(published*100) / float64(total)
}

// GetLatencyStats returns latency statistics by parsing Summary output
func (c *Collector) GetLatencyStats() (LatencyStats, error) {
	// Marshal the metrics set to Prometheus format
	var buf bytes.Buffer
	c.set.WritePrometheus(&buf)

	// Parse using Prometheus expfmt parser with validation scheme
	parser := expfmt.NewTextParser(model.UTF8Validation)

	metricFamilies, err := parser.TextToMetricFamilies(&buf)
	if err != nil {
		return LatencyStats{}, err
	}

	stats := LatencyStats{}
	var sum float64

	// VictoriaMetrics outputs quantiles in latency_seconds, sum/count as separate metrics
	// Get count first from latency_seconds_count
	if family, ok := metricFamilies["latency_seconds_count"]; ok {
		for _, metric := range family.GetMetric() {
			stats.Count = int(metric.GetUntyped().GetValue())
		}
	}

	// Get sum from latency_seconds_sum
	if family, ok := metricFamilies["latency_seconds_sum"]; ok {
		for _, metric := range family.GetMetric() {
			sum = metric.GetUntyped().GetValue()
		}
	}

	// Calculate average from sum and count
	if stats.Count > 0 && sum > 0 {
		stats.Avg = time.Duration(sum / float64(stats.Count) * float64(time.Second))
	}

	// Get quantiles from latency_seconds
	if family, ok := metricFamilies["latency_seconds"]; ok {
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "quantile" {
					q, _ := strconv.ParseFloat(label.GetValue(), 64)
					value := metric.GetUntyped().GetValue()
					duration := time.Duration(value * float64(time.Second))

					switch q {
					case 0.5:
						stats.P50 = duration
					case 0.9:
						stats.P90 = duration
					case 0.97:
						stats.P97 = duration
					case 0.99:
						stats.P99 = duration
					case 1.0:
						stats.Max = duration
					}
				}
			}
		}
	}

	return stats, nil
}

// LatencyStats contains latency statistics
type LatencyStats struct {
	Count int
	Min   time.Duration
	Max   time.Duration
	Avg   time.Duration
	P50   time.Duration
	P90   time.Duration
	P97   time.Duration // VictoriaMetrics provides P97, not P95
	P99   time.Duration
}

// Snapshot returns the current metric values
type Snapshot struct {
	TotalConnections   uint64
	SuccessfulConns    uint64
	FailedConns        uint64
	ConnectionRate     float64
	MaxConcurrentConns float64
	PublishedMessages  uint64
	ReceivedMessages   uint64
	FailedPublishes    uint64
}

// GetSnapshot returns a snapshot of current metrics
func (c *Collector) GetSnapshot() Snapshot {
	return Snapshot{
		TotalConnections:   uint64(c.totalConnections.Get()),
		SuccessfulConns:    uint64(c.successfulConns.Get()),
		FailedConns:        uint64(c.failedConns.Get()),
		ConnectionRate:     c.connectionRate.Get(),
		MaxConcurrentConns: c.maxConcurrentConns.Get(),
		PublishedMessages:  uint64(c.publishedMessages.Get()),
		ReceivedMessages:   uint64(c.receivedMessages.Get()),
		FailedPublishes:    uint64(c.failedPublishes.Get()),
	}
}
