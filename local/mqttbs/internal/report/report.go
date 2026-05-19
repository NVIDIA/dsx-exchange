// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/metrics"
)

// Report contains benchmark results
type Report struct {
	Metadata Metadata             `json:"metadata"`
	Scenario ScenarioConfig       `json:"scenario"`
	Results  Results              `json:"results"`
	Latency  metrics.LatencyStats `json:"latency"`
}

// Metadata contains information about the benchmark execution
type Metadata struct {
	Timestamp   time.Time `json:"timestamp"`
	Duration    string    `json:"duration"`
	ToolVersion string    `json:"tool_version"`
	BrokerURL   string    `json:"broker_url"`
}

// ScenarioConfig describes the scenario configuration
type ScenarioConfig struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	NumPublishers  int64  `json:"num_publishers"`
	NumSubscribers int64  `json:"num_subscribers"`
	NumTopics      int64  `json:"num_topics"`
	MessageSize    int64  `json:"message_size"`
	QoS            byte   `json:"qos"`
}

// Results contains the benchmark results
type Results struct {
	Connections struct {
		Total         int64   `json:"total"`
		Successful    int64   `json:"successful"`
		Failed        int64   `json:"failed"`
		Rate          float64 `json:"rate_per_sec"`
		MaxConcurrent int64   `json:"max_concurrent"`
	} `json:"connections"`

	Messages struct {
		Published     int64   `json:"published"`
		Received      int64   `json:"received"`
		FailedPublish int64   `json:"failed_publish"`
		PublishRate   float64 `json:"publish_rate_per_sec"`
		SubscribeRate float64 `json:"subscribe_rate_per_sec"`
		SuccessRate   float64 `json:"success_rate_percent"`
	} `json:"messages"`
}

// Generate generates a report from metrics
func Generate(collector *metrics.Collector, brokerURL string, scenario ScenarioConfig) (*Report, error) {
	latencyStats, err := collector.GetLatencyStats()
	if err != nil {
		return nil, err
	}
	if collector.NumPublishers != 0 || collector.NumSubscribers != 0 || collector.NumTopics != 0 {
		scenario.NumPublishers = collector.NumPublishers
		scenario.NumSubscribers = collector.NumSubscribers
		scenario.NumTopics = collector.NumTopics
	}
	if collector.MessageSize != 0 {
		scenario.MessageSize = collector.MessageSize
	}

	report := &Report{
		Metadata: Metadata{
			Timestamp:   collector.StartTime,
			Duration:    collector.GetDuration().String(),
			ToolVersion: "1.0.0",
			BrokerURL:   brokerURL,
		},
		Scenario: scenario,
		Latency:  latencyStats,
	}

	// Get snapshot of metrics
	snap := collector.GetSnapshot()

	// Connection metrics
	report.Results.Connections.Total = int64(snap.TotalConnections)
	report.Results.Connections.Successful = int64(snap.SuccessfulConns)
	report.Results.Connections.Failed = int64(snap.FailedConns)
	report.Results.Connections.Rate = snap.ConnectionRate
	report.Results.Connections.MaxConcurrent = int64(snap.MaxConcurrentConns)

	// Message metrics
	report.Results.Messages.Published = int64(snap.PublishedMessages)
	report.Results.Messages.Received = int64(snap.ReceivedMessages)
	report.Results.Messages.FailedPublish = int64(snap.FailedPublishes)
	report.Results.Messages.PublishRate = collector.GetPublishRate()
	report.Results.Messages.SubscribeRate = collector.GetSubscribeRate()
	report.Results.Messages.SuccessRate = collector.GetSuccessRate()

	return report, nil
}

// SaveJSON saves the report as JSON
func (r *Report) SaveJSON(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create report directory: %w", err)
	}

	filename := fmt.Sprintf("report-%s-%s.json",
		r.Scenario.Name,
		r.Metadata.Timestamp.Format("20060102-150405"))

	path := filepath.Join(dir, filename)

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write report file: %w", err)
	}

	return nil
}

// SaveText saves the report as human-readable text
func (r *Report) SaveText(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create report directory: %w", err)
	}

	filename := fmt.Sprintf("report-%s-%s.txt",
		r.Scenario.Name,
		r.Metadata.Timestamp.Format("20060102-150405"))

	path := filepath.Join(dir, filename)

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create report file: %w", err)
	}
	defer f.Close()

	// Write report
	fmt.Fprintf(f, "MQTT Benchmark Suite Report\n")
	fmt.Fprintf(f, "============================\n\n")

	fmt.Fprintf(f, "Metadata\n")
	fmt.Fprintf(f, "--------\n")
	fmt.Fprintf(f, "Timestamp:    %s\n", r.Metadata.Timestamp.Format(time.RFC3339))
	fmt.Fprintf(f, "Duration:     %s\n", r.Metadata.Duration)
	fmt.Fprintf(f, "Tool Version: %s\n", r.Metadata.ToolVersion)
	fmt.Fprintf(f, "Broker URL:   %s\n\n", r.Metadata.BrokerURL)

	fmt.Fprintf(f, "Scenario Configuration\n")
	fmt.Fprintf(f, "---------------------\n")
	fmt.Fprintf(f, "Name:          %s\n", r.Scenario.Name)
	fmt.Fprintf(f, "Description:   %s\n", r.Scenario.Description)
	fmt.Fprintf(f, "Publishers:    %d\n", r.Scenario.NumPublishers)
	fmt.Fprintf(f, "Subscribers:   %d\n", r.Scenario.NumSubscribers)
	fmt.Fprintf(f, "Topics:        %d\n", r.Scenario.NumTopics)
	fmt.Fprintf(f, "Message Size:  %d bytes\n", r.Scenario.MessageSize)
	fmt.Fprintf(f, "QoS:           %d\n\n", r.Scenario.QoS)

	fmt.Fprintf(f, "Connection Results\n")
	fmt.Fprintf(f, "------------------\n")
	fmt.Fprintf(f, "Total Connections:     %d\n", r.Results.Connections.Total)
	fmt.Fprintf(f, "Successful:            %d\n", r.Results.Connections.Successful)
	fmt.Fprintf(f, "Failed:                %d\n", r.Results.Connections.Failed)
	fmt.Fprintf(f, "Connection Rate:       %.2f conn/sec\n", r.Results.Connections.Rate)
	fmt.Fprintf(f, "Max Concurrent:        %d\n\n", r.Results.Connections.MaxConcurrent)

	fmt.Fprintf(f, "Message Results\n")
	fmt.Fprintf(f, "---------------\n")
	fmt.Fprintf(f, "Published:             %d\n", r.Results.Messages.Published)
	fmt.Fprintf(f, "Received:              %d\n", r.Results.Messages.Received)
	fmt.Fprintf(f, "Failed Publishes:      %d\n", r.Results.Messages.FailedPublish)
	fmt.Fprintf(f, "Publish Rate:          %.2f msg/sec\n", r.Results.Messages.PublishRate)
	fmt.Fprintf(f, "Subscribe Rate:        %.2f msg/sec\n", r.Results.Messages.SubscribeRate)
	fmt.Fprintf(f, "Success Rate:          %.2f%%\n\n", r.Results.Messages.SuccessRate)

	if r.Latency.Count > 0 {
		fmt.Fprintf(f, "Latency Statistics\n")
		fmt.Fprintf(f, "------------------\n")
		fmt.Fprintf(f, "Sample Count:          %d\n", r.Latency.Count)
		fmt.Fprintf(f, "Min:                   %v\n", r.Latency.Min)
		fmt.Fprintf(f, "Max:                   %v\n", r.Latency.Max)
		fmt.Fprintf(f, "Average:               %v\n", r.Latency.Avg)
		fmt.Fprintf(f, "P50 (Median):          %v\n", r.Latency.P50)
		fmt.Fprintf(f, "P90:                   %v\n", r.Latency.P90)
		fmt.Fprintf(f, "P97:                   %v\n", r.Latency.P97)
		fmt.Fprintf(f, "P99:                   %v\n", r.Latency.P99)
	}

	return nil
}

// Print prints the report to stdout
func (r *Report) Print() {
	fmt.Printf("\n")
	fmt.Printf("MQTT Benchmark Suite Report\n")
	fmt.Printf("============================\n\n")

	fmt.Printf("Scenario: %s\n", r.Scenario.Name)
	fmt.Printf("Duration: %s\n", r.Metadata.Duration)
	fmt.Printf("Broker:   %s\n\n", r.Metadata.BrokerURL)

	fmt.Printf("Connection Results:\n")
	fmt.Printf("  Total:              %d\n", r.Results.Connections.Total)
	fmt.Printf("  Successful:         %d\n", r.Results.Connections.Successful)
	fmt.Printf("  Failed:             %d\n", r.Results.Connections.Failed)
	fmt.Printf("  Connection Rate:    %.2f conn/sec\n", r.Results.Connections.Rate)
	fmt.Printf("  Max Concurrent:     %d\n\n", r.Results.Connections.MaxConcurrent)

	fmt.Printf("Message Results:\n")
	fmt.Printf("  Published:          %d\n", r.Results.Messages.Published)
	fmt.Printf("  Received:           %d\n", r.Results.Messages.Received)
	fmt.Printf("  Publish Rate:       %.2f msg/sec\n", r.Results.Messages.PublishRate)
	fmt.Printf("  Subscribe Rate:     %.2f msg/sec\n", r.Results.Messages.SubscribeRate)
	fmt.Printf("  Success Rate:       %.2f%%\n\n", r.Results.Messages.SuccessRate)

	if r.Latency.Count > 0 {
		fmt.Printf("Latency:\n")
		fmt.Printf("  Samples:            %d\n", r.Latency.Count)
		fmt.Printf("  Average:            %v\n", r.Latency.Avg)
		fmt.Printf("  P50:                %v\n", r.Latency.P50)
		fmt.Printf("  P90:                %v\n", r.Latency.P90)
		fmt.Printf("  P97:                %v\n", r.Latency.P97)
		fmt.Printf("  P99:                %v\n", r.Latency.P99)
	}

	fmt.Printf("\n")
}
