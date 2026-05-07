// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package benchmark

import (
	"context"
	"fmt"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/metrics"
	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/report"
	"github.com/google/uuid"
)

// Scenario represents a benchmark scenario
type Scenario interface {
	Name() string
	Description() string
	Run(ctx context.Context, config *Config, collector *metrics.Collector) error
	Config() report.ScenarioConfig
}

// Runner executes benchmark scenarios
type Runner struct {
	config *Config
}

// NewRunner creates a new benchmark runner
func NewRunner(config *Config) *Runner {
	return &Runner{
		config: config,
	}
}

// Run executes a scenario and generates a report
func (r *Runner) Run(ctx context.Context, scenario Scenario) error {
	// Generate unique test run ID to avoid interference from previous runs
	r.config.TestRunID = uuid.New().String()

	fmt.Printf("Starting benchmark: %s\n", scenario.Name())
	fmt.Printf("Description: %s\n", scenario.Description())
	fmt.Printf("Broker: %s\n", r.config.BrokerURL)
	fmt.Printf("Test run ID: %s\n\n", r.config.TestRunID)

	// Create metrics collector
	collector := metrics.NewCollector(scenario.Name())
	collector.Start()

	// Run the scenario
	startTime := time.Now()
	if err := scenario.Run(ctx, r.config, collector); err != nil {
		return fmt.Errorf("scenario failed: %w", err)
	}

	collector.End()
	duration := time.Since(startTime)

	fmt.Printf("\nBenchmark completed in %v\n", duration)

	// Generate report
	rep, err := report.Generate(collector, r.config.BrokerURL, scenario.Config())
	if err != nil {
		return fmt.Errorf("failed to generate report: %w", err)
	}

	// Print to console
	rep.Print()

	// Save to files
	if err := rep.SaveJSON(r.config.ReportDir); err != nil {
		return fmt.Errorf("failed to save JSON report: %w", err)
	}

	if err := rep.SaveText(r.config.ReportDir); err != nil {
		return fmt.Errorf("failed to save text report: %w", err)
	}

	fmt.Printf("Reports saved to: %s\n", r.config.ReportDir)

	return nil
}
