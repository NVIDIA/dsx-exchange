// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the complete configuration
type Config struct {
	Broker     BrokerConfig     `yaml:"broker"`
	Publisher  PublisherConfig  `yaml:"publisher"`
	Subscriber SubscriberConfig `yaml:"subscriber"`
	Benchmark  BenchmarkConfig  `yaml:"benchmark"`
}

// BrokerConfig contains broker connection details
type BrokerConfig struct {
	URL             string `yaml:"url"`
	SubscriberURL   string `yaml:"subscriber_url"` // For federation testing
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	ClientIDPrefix  string `yaml:"client_id_prefix"`
	TLS             bool   `yaml:"tls"`
	InsecureSkipTLS bool   `yaml:"insecure_skip_tls"`
}

// PublisherConfig contains publisher settings
type PublisherConfig struct {
	Topic       string `yaml:"topic"`
	QoS         byte   `yaml:"qos"`
	Retain      bool   `yaml:"retain"`
	Rate        int    `yaml:"rate"`         // messages per second
	MessageSize int    `yaml:"message_size"` // bytes
}

// SubscriberConfig contains subscriber settings
type SubscriberConfig struct {
	Topics []string `yaml:"topics"`
	QoS    byte     `yaml:"qos"`
}

// BenchmarkConfig contains benchmark test settings
type BenchmarkConfig struct {
	Publishers     int           `yaml:"publishers"`
	Subscribers    int           `yaml:"subscribers"`
	Duration       time.Duration `yaml:"duration"`
	MessageCount   int           `yaml:"message_count"`
	MessageSize    int           `yaml:"message_size"`
	QoS            byte          `yaml:"qos"`
	Retain         bool          `yaml:"retain"`
	ReportInterval time.Duration `yaml:"report_interval"`
	WarmupDuration time.Duration `yaml:"warmup_duration"`
}

// LoadFromFile loads configuration from a YAML file
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Broker.URL == "" {
		return fmt.Errorf("broker URL is required")
	}

	if c.Benchmark.Publishers < 0 {
		return fmt.Errorf("publishers must be >= 0")
	}

	if c.Benchmark.Subscribers < 0 {
		return fmt.Errorf("subscribers must be >= 0")
	}

	if c.Benchmark.QoS > 2 {
		return fmt.Errorf("QoS must be 0, 1, or 2")
	}

	if c.Benchmark.MessageSize <= 0 {
		c.Benchmark.MessageSize = 1024 // default 1KB
	}

	if c.Benchmark.Duration == 0 && c.Benchmark.MessageCount == 0 {
		return fmt.Errorf("either duration or message_count must be specified")
	}

	if c.Benchmark.ReportInterval == 0 {
		c.Benchmark.ReportInterval = 5 * time.Second
	}

	if c.Benchmark.WarmupDuration == 0 {
		c.Benchmark.WarmupDuration = 5 * time.Second
	}

	return nil
}

// DefaultConfig returns a configuration with default values
func DefaultConfig() *Config {
	return &Config{
		Broker: BrokerConfig{
			URL:            "tcp://localhost:1883",
			ClientIDPrefix: "mqtt-test",
			TLS:            false,
		},
		Publisher: PublisherConfig{
			Topic:       "test/messages",
			QoS:         0,
			Retain:      false,
			Rate:        100,
			MessageSize: 1024,
		},
		Subscriber: SubscriberConfig{
			Topics: []string{"test/messages"},
			QoS:    0,
		},
		Benchmark: BenchmarkConfig{
			Publishers:     10,
			Subscribers:    5,
			Duration:       60 * time.Second,
			MessageCount:   0,
			MessageSize:    1024,
			QoS:            0,
			Retain:         false,
			ReportInterval: 5 * time.Second,
			WarmupDuration: 5 * time.Second,
		},
	}
}

// GetBrokerURL returns the broker URL from environment or config
func GetBrokerURL(envVar, configURL string) string {
	if url := os.Getenv(envVar); url != "" {
		return url
	}
	return configURL
}

// GetCSCBrokerURL returns CSC broker URL
func GetCSCBrokerURL() string {
	return GetBrokerURL("CSC_BROKER_URL", "tcp://172.18.200.1:1883")
}

// GetCPC1BrokerURL returns CPC1 broker URL
func GetCPC1BrokerURL() string {
	return GetBrokerURL("CPC1_BROKER_URL", "tcp://172.18.201.1:1883")
}

// GetCPC2BrokerURL returns CPC2 broker URL
func GetCPC2BrokerURL() string {
	return GetBrokerURL("CPC2_BROKER_URL", "tcp://172.18.202.1:1883")
}

// GetKeycloakURL returns Keycloak URL (consolidated in CSC cluster)
func GetKeycloakURL() string {
	return GetBrokerURL("KEYCLOAK_URL", "http://172.18.200.1")
}
