// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/benchmark"
	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/scenarios"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "mqttbs",
		Short: "MQTT Benchmark Suite - broker-agnostic MQTT benchmarking tool",
		Long: `MQTT Benchmark Suite (mqttbs) implements the Open MQTT Benchmark Suite
specification for standardized MQTT broker performance testing.`,
	}

	// Add subcommands
	rootCmd.AddCommand(runCmd())
	rootCmd.AddCommand(listCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runCmd() *cobra.Command {
	var (
		brokerURL         string
		username          string
		password          string
		reportDir         string
		duration          time.Duration
		publishRate       int
		messageSize       int
		connectionClients int
		connectionRate    int
		fanOutSubscribers int
		p2pClients        int
		fanInPublishers   int
		fanInSubscribers  int
		fanInTopics       int
	)
	defaultConfig := benchmark.NewConfig()

	cmd := &cobra.Command{
		Use:   "run [scenario]",
		Short: "Run a benchmark scenario",
		Long: `Run a specific benchmark scenario or suite.

Available scenarios:
  connection-10k  - 1,000 concurrent connections
  fanout-1k       - 1 publisher → 1,000 subscribers
  p2p-1k          - 1,000 publishers ↔ 1,000 subscribers
  fanin-1k        - 1,000 publishers → 5 subscribers (shared)
  basic-suite     - Run all Basic scenarios

Examples:
  mqttbs run connection-10k --broker tcp://localhost:1883
  mqttbs run basic-suite --broker tcp://192.168.1.100:1883`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create and validate configuration
			config := benchmark.NewConfig()
			config.BrokerURL = brokerURL
			config.Username = username
			config.Password = password
			config.ReportDir = reportDir
			config.PublishRate = publishRate
			config.MessageSize = messageSize
			config.Duration = duration
			config.ConnectionClients = connectionClients
			config.ConnectionRate = connectionRate
			config.FanOutSubscribers = fanOutSubscribers
			config.P2PClients = p2pClients
			config.FanInPublishers = fanInPublishers
			config.FanInSubscribers = fanInSubscribers
			config.FanInTopics = fanInTopics

			// Validate message size
			if messageSize < 8 {
				return fmt.Errorf("message size must be at least 8 bytes")
			}
			if connectionClients <= 0 || connectionRate <= 0 || fanOutSubscribers <= 0 ||
				p2pClients <= 0 || fanInPublishers <= 0 || fanInSubscribers <= 0 || fanInTopics <= 0 {
				return fmt.Errorf("scenario scale values must be positive")
			}

			return runScenario(cmd, args[0], config)
		},
	}

	cmd.Flags().StringVar(&brokerURL, "broker", "tcp://localhost:1883", "MQTT broker URL")
	cmd.Flags().StringVar(&username, "username", "", "MQTT username (optional)")
	cmd.Flags().StringVar(&password, "password", "", "MQTT password (optional)")
	cmd.Flags().StringVar(&reportDir, "report-dir", "./results", "Directory for reports")
	cmd.Flags().DurationVar(&duration, "duration", 1*time.Minute, "Scenario duration (e.g. 30s, 1m, 5m)")
	cmd.Flags().IntVar(&publishRate, "publish-rate", 1, "Messages per second per publisher")
	cmd.Flags().IntVar(&messageSize, "message-size", 16, "Message payload size in bytes")
	cmd.Flags().IntVar(&connectionClients, "connection-clients", defaultConfig.ConnectionClients, "Number of clients for connection scenario")
	cmd.Flags().IntVar(&connectionRate, "connection-rate", defaultConfig.ConnectionRate, "Connections per second for connection scenario")
	cmd.Flags().IntVar(&fanOutSubscribers, "fanout-subscribers", defaultConfig.FanOutSubscribers, "Number of subscribers for fanout scenario")
	cmd.Flags().IntVar(&p2pClients, "p2p-clients", defaultConfig.P2PClients, "Number of publisher/subscriber pairs for point-to-point scenario")
	cmd.Flags().IntVar(&fanInPublishers, "fanin-publishers", defaultConfig.FanInPublishers, "Number of publishers for fan-in scenario")
	cmd.Flags().IntVar(&fanInSubscribers, "fanin-subscribers", defaultConfig.FanInSubscribers, "Number of subscribers for fan-in scenario")
	cmd.Flags().IntVar(&fanInTopics, "fanin-topics", defaultConfig.FanInTopics, "Number of topics for fan-in scenario")

	return cmd
}

func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available benchmark scenarios",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Available MQTT Benchmark Scenarios:")
			fmt.Println()
			fmt.Println("Basic Set (Small-scale):")
			fmt.Println("  connection-10k   10,000 clients connect within 100 seconds")
			fmt.Println("  fanout-1k        1 publisher → 1,000 subscribers, 1 msg/sec")
			fmt.Println("  p2p-1k           1,000 publishers ↔ 1,000 subscribers, 1 msg/sec each")
			fmt.Println("  fanin-1k         1,000 publishers → 5 subscribers, 1 msg/sec each")
			fmt.Println()
			fmt.Println("Suites:")
			fmt.Println("  basic-suite      Run all Basic scenarios sequentially")
			fmt.Println()
		},
	}

	return cmd
}

func runScenario(cmd *cobra.Command, scenarioName string, config *benchmark.Config) error {
	// Create context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nReceived interrupt signal, shutting down gracefully...")
		cancel()
	}()

	// Create runner
	runner := benchmark.NewRunner(config)

	// Select and run scenario
	switch scenarioName {
	case "connection-10k":
		return runner.Run(ctx, &scenarios.Connection10K{})

	case "fanout-1k":
		return runner.Run(ctx, &scenarios.FanOut1K{})

	case "p2p-1k":
		return runner.Run(ctx, &scenarios.PointToPoint1K{})

	case "fanin-1k":
		return runner.Run(ctx, &scenarios.FanIn1K{})

	case "basic-suite":
		return runBasicSuite(ctx, runner)

	default:
		return fmt.Errorf("unknown scenario: %s (run 'mqttbs list' to see available scenarios)", scenarioName)
	}
}

func runBasicSuite(ctx context.Context, runner *benchmark.Runner) error {
	scenarios := []benchmark.Scenario{
		&scenarios.Connection10K{},
		&scenarios.FanOut1K{},
		&scenarios.PointToPoint1K{},
		&scenarios.FanIn1K{},
	}

	fmt.Println("===========================================")
	fmt.Println("Running Basic Suite (4 scenarios)")
	fmt.Println("===========================================")
	fmt.Println()

	for i, scenario := range scenarios {
		fmt.Printf("\n[%d/%d] Starting: %s\n", i+1, len(scenarios), scenario.Name())
		fmt.Println("-------------------------------------------")

		if err := runner.Run(ctx, scenario); err != nil {
			if ctx.Err() != nil {
				fmt.Println("\nSuite interrupted by user")
				return nil
			}
			return fmt.Errorf("scenario %s failed: %w", scenario.Name(), err)
		}

		if i < len(scenarios)-1 {
			fmt.Println("\n===========================================")
		}
	}

	fmt.Println("\n===========================================")
	fmt.Println("Basic Suite Completed Successfully")
	fmt.Println("===========================================")

	return nil
}
