// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package benchmark

import (
	"time"
)

// Config contains benchmark configuration
type Config struct {
	BrokerURL string
	Username  string
	Password  string
	ReportDir string

	// MQTT settings
	KeepAlive    int
	CleanSession bool
	QoS          byte

	// Benchmark settings
	Duration    time.Duration
	PublishRate int    // Messages per second per publisher
	MessageSize int    // Message payload size in bytes
	TestRunID   string // Random UUID for unique topics and client IDs per test run

	// Scenario scale settings
	ConnectionClients int
	ConnectionRate    int
	FanOutSubscribers int
	P2PClients        int
	FanInPublishers   int
	FanInSubscribers  int
	FanInTopics       int

	// Timeouts
	ConnectTimeout time.Duration
	PublishTimeout time.Duration
}

// NewConfig creates a default configuration
func NewConfig() *Config {
	return &Config{
		BrokerURL:         "tcp://localhost:1883",
		ReportDir:         "./results",
		KeepAlive:         300,
		CleanSession:      true,
		QoS:               1,
		Duration:          1 * time.Minute,
		PublishRate:       1,  // Default: 1 message per second per publisher
		MessageSize:       16, // Default: 16 bytes
		ConnectionClients: 10000,
		ConnectionRate:    100,
		FanOutSubscribers: 1000,
		P2PClients:        1000,
		FanInPublishers:   1000,
		FanInSubscribers:  5,
		FanInTopics:       1000,
		ConnectTimeout:    10 * time.Second,
		PublishTimeout:    5 * time.Second,
	}
}
