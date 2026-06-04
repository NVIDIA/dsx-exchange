// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mqttbus

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestDeployedBusE2EAllowedTopic(t *testing.T) {
	if os.Getenv("RUN_EXCHANGE_E2E_DEPLOYED_BUS") != "1" {
		t.Skip("set RUN_EXCHANGE_E2E_DEPLOYED_BUS=1 to run deployed-bus e2e")
	}
	brokerURL := requiredEnv(t, "DSX_EXCHANGE_MQTT_URL")
	bearer := requiredEnv(t, "DSX_EXCHANGE_E2E_BEARER")
	topic := requiredEnv(t, "DSX_EXCHANGE_E2E_ALLOWED_TOPIC")

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	res, err := Collect(ctx, Config{
		BrokerURL: brokerURL,
		Username:  envOrDefault("DSX_EXCHANGE_MQTT_USERNAME", DefaultUsername),
		TLS: TLSConfig{
			CAFile:     os.Getenv("DSX_EXCHANGE_MQTT_CA_FILE"),
			ServerName: os.Getenv("DSX_EXCHANGE_MQTT_SERVER_NAME"),
		},
		MaxResultBytes: 1048576,
	}, bearer, topic, 1, 10*time.Second, false)
	if err != nil {
		t.Fatalf("deployed bus subscribe failed with code %q: %v", ErrorCode(err), err)
	}
	if res.StoppedReason == "" {
		t.Fatalf("stopped reason was empty")
	}
}

func TestDeployedBusE2EDeniedTopic(t *testing.T) {
	if os.Getenv("RUN_EXCHANGE_E2E_DEPLOYED_BUS") != "1" {
		t.Skip("set RUN_EXCHANGE_E2E_DEPLOYED_BUS=1 to run deployed-bus e2e")
	}
	brokerURL := requiredEnv(t, "DSX_EXCHANGE_MQTT_URL")
	bearer := requiredEnv(t, "DSX_EXCHANGE_E2E_BEARER")
	topic := requiredEnv(t, "DSX_EXCHANGE_E2E_DENIED_TOPIC")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err := Collect(ctx, Config{
		BrokerURL: brokerURL,
		Username:  envOrDefault("DSX_EXCHANGE_MQTT_USERNAME", DefaultUsername),
		TLS: TLSConfig{
			CAFile:     os.Getenv("DSX_EXCHANGE_MQTT_CA_FILE"),
			ServerName: os.Getenv("DSX_EXCHANGE_MQTT_SERVER_NAME"),
		},
		MaxResultBytes: 1048576,
	}, bearer, topic, 1, 5*time.Second, false)
	if err == nil {
		t.Fatalf("denied topic unexpectedly succeeded")
	}
	switch ErrorCode(err) {
	case CodeTopicACLDenied, CodeMQTTAuthorizationFailed, CodeMQTTAuthFailed, CodeMQTTSubscribeFailed:
	default:
		t.Fatalf("denied topic returned code %q, want auth/subscription failure: %v", ErrorCode(err), err)
	}
}

func requiredEnv(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		t.Fatalf("%s is required", key)
	}
	return v
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
