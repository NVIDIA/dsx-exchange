// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package functional

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/pkg/client"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

type Cluster struct {
	name   string
	broker string
}

func getClusters() []Cluster {
	// Allow override via MQTT_BROKER for single broker testing
	if broker := os.Getenv("MQTT_BROKER"); broker != "" {
		return []Cluster{
			{name: "Single", broker: broker},
		}
	}

	// Allow override via MQTT_BROKERS for multiple brokers
	// Format: "CSC=tcp://host:port,CPC-1=tcp://host:port,CPC-2=tcp://host:port"
	if brokerList := os.Getenv("MQTT_BROKERS"); brokerList != "" {
		var customClusters []Cluster
		for _, entry := range strings.Split(brokerList, ",") {
			parts := strings.SplitN(entry, "=", 2)
			if len(parts) == 2 {
				customClusters = append(customClusters, Cluster{name: parts[0], broker: parts[1]})
			}
		}
		if len(customClusters) > 0 {
			return customClusters
		}
	}

	// Default: all clusters via Envoy Gateway
	return []Cluster{
		{name: "CSC", broker: "tcp://172.18.200.1:1883"},
		{name: "CPC-1", broker: "tcp://172.18.201.1:1883"},
		{name: "CPC-2", broker: "tcp://172.18.202.1:1883"},
	}
}

func TestMQTTPubSubQoS0(t *testing.T) {
	for _, cluster := range getClusters() {
		cluster := cluster
		t.Run(cluster.name, func(t *testing.T) {
			testMQTTPubSubQoS0(t, cluster.broker)
		})
	}
}

func testMQTTPubSubQoS0(t *testing.T, broker string) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	topic := fmt.Sprintf("test/qos0/%s", uuid.New().String())
	messageCount := 10

	var receivedCount int64
	receivedChan := make(chan struct{})

	// Create subscriber
	subCfg := client.Config{
		Broker:   broker,
		ClientID: fmt.Sprintf("sub-qos0-%s", uuid.New().String()),
	}
	sub, err := client.New(subCfg)
	if err != nil {
		t.Fatalf("Failed to create subscriber: %v", err)
	}

	if err := sub.Connect(); err != nil {
		t.Fatalf("Failed to connect subscriber: %v", err)
	}
	defer sub.Disconnect()

	// Subscribe with handler
	handler := func(c mqtt.Client, msg mqtt.Message) {
		if atomic.AddInt64(&receivedCount, 1) == int64(messageCount) {
			close(receivedChan)
		}
	}

	if err := sub.Subscribe(topic, 0, handler); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Give subscriber time to be ready
	time.Sleep(500 * time.Millisecond)

	// Create publisher
	pubCfg := client.Config{
		Broker:   broker,
		ClientID: fmt.Sprintf("pub-qos0-%s", uuid.New().String()),
	}
	pub, err := client.New(pubCfg)
	if err != nil {
		t.Fatalf("Failed to create publisher: %v", err)
	}

	if err := pub.Connect(); err != nil {
		t.Fatalf("Failed to connect publisher: %v", err)
	}
	defer pub.Disconnect()

	// Publish messages
	g, gctx := errgroup.WithContext(ctx)
	for i := 0; i < messageCount; i++ {
		i := i
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			payload := []byte(fmt.Sprintf("message-%d", i))
			return pub.Publish(topic, payload, 0, false)
		})
	}

	if err := g.Wait(); err != nil {
		t.Fatalf("Failed to publish messages: %v", err)
	}

	// Wait for all messages to be received
	select {
	case <-receivedChan:
		t.Logf("Successfully received %d messages", atomic.LoadInt64(&receivedCount))
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for messages. Received %d/%d", atomic.LoadInt64(&receivedCount), messageCount)
	}
}

func TestMQTTPubSubQoS1(t *testing.T) {
	for _, cluster := range getClusters() {
		t.Run(cluster.name, func(t *testing.T) {
			testMQTTPubSubQoS1(t, cluster.broker)
		})
	}
}

func testMQTTPubSubQoS1(t *testing.T, broker string) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	topic := fmt.Sprintf("test/qos1/%s", uuid.New().String())
	messageCount := 10

	var receivedCount int64
	receivedChan := make(chan struct{})

	// Create subscriber
	subCfg := client.Config{
		Broker:   broker,
		ClientID: fmt.Sprintf("sub-qos1-%s", uuid.New().String()),
	}
	sub, err := client.New(subCfg)
	if err != nil {
		t.Fatalf("Failed to create subscriber: %v", err)
	}

	if err := sub.Connect(); err != nil {
		t.Fatalf("Failed to connect subscriber: %v", err)
	}
	defer sub.Disconnect()

	// Subscribe with handler
	handler := func(c mqtt.Client, msg mqtt.Message) {
		if atomic.AddInt64(&receivedCount, 1) == int64(messageCount) {
			close(receivedChan)
		}
	}

	if err := sub.Subscribe(topic, 1, handler); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Create publisher
	pubCfg := client.Config{
		Broker:   broker,
		ClientID: fmt.Sprintf("pub-qos1-%s", uuid.New().String()),
	}
	pub, err := client.New(pubCfg)
	if err != nil {
		t.Fatalf("Failed to create publisher: %v", err)
	}

	if err := pub.Connect(); err != nil {
		t.Fatalf("Failed to connect publisher: %v", err)
	}
	defer pub.Disconnect()

	// Publish messages
	g, gctx := errgroup.WithContext(ctx)
	for i := 0; i < messageCount; i++ {
		i := i
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			payload := []byte(fmt.Sprintf("message-%d", i))
			return pub.Publish(topic, payload, 1, false)
		})
	}

	if err := g.Wait(); err != nil {
		t.Fatalf("Failed to publish messages: %v", err)
	}

	// Wait for all messages
	select {
	case <-receivedChan:
		t.Logf("Successfully received %d messages with QoS 1", atomic.LoadInt64(&receivedCount))
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for messages. Received %d/%d", atomic.LoadInt64(&receivedCount), messageCount)
	}
}

func TestMQTTRetainedMessages(t *testing.T) {
	for _, cluster := range getClusters() {
		cluster := cluster
		t.Run(cluster.name, func(t *testing.T) {
			testMQTTRetainedMessages(t, cluster.broker)
		})
	}
}

func testMQTTRetainedMessages(t *testing.T, broker string) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	topic := fmt.Sprintf("test/retained/%s", uuid.New().String())
	retainedPayload := []byte("retained-message")

	// Publish retained message
	pubCfg := client.Config{
		Broker:   broker,
		ClientID: fmt.Sprintf("pub-retained-%s", uuid.New().String()),
	}
	pub, err := client.New(pubCfg)
	if err != nil {
		t.Fatalf("Failed to create publisher: %v", err)
	}

	if err := pub.Connect(); err != nil {
		t.Fatalf("Failed to connect publisher: %v", err)
	}

	if err := pub.Publish(topic, retainedPayload, 0, true); err != nil {
		t.Fatalf("Failed to publish retained message: %v", err)
	}
	pub.Disconnect()

	// Wait for message to be stored
	time.Sleep(time.Second)

	// Subscribe after publishing (should receive retained message)
	receivedChan := make(chan struct{})

	subCfg := client.Config{
		Broker:   broker,
		ClientID: fmt.Sprintf("sub-retained-%s", uuid.New().String()),
	}
	sub, err := client.New(subCfg)
	if err != nil {
		t.Fatalf("Failed to create subscriber: %v", err)
	}

	if err := sub.Connect(); err != nil {
		t.Fatalf("Failed to connect subscriber: %v", err)
	}
	defer sub.Disconnect()

	handler := func(c mqtt.Client, msg mqtt.Message) {
		if msg.Retained() && string(msg.Payload()) == string(retainedPayload) {
			close(receivedChan)
		}
	}

	if err := sub.Subscribe(topic, 0, handler); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Wait for retained message
	select {
	case <-receivedChan:
		t.Log("Successfully received retained message")
	case <-ctx.Done():
		t.Fatal("Timeout waiting for retained message")
	}
}

func TestMQTTWildcards(t *testing.T) {
	for _, cluster := range getClusters() {
		cluster := cluster
		t.Run(cluster.name, func(t *testing.T) {
			testMQTTWildcards(t, cluster.broker)
		})
	}
}

func testMQTTWildcards(t *testing.T, broker string) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	var receivedCount int64
	receivedChan := make(chan struct{})

	baseTopicID := uuid.New().String()
	wildcardTopic := fmt.Sprintf("test/wildcard/%s/+/data", baseTopicID)

	// Create subscriber with wildcard
	subCfg := client.Config{
		Broker:   broker,
		ClientID: fmt.Sprintf("sub-wildcard-%s", uuid.New().String()),
	}
	sub, err := client.New(subCfg)
	if err != nil {
		t.Fatalf("Failed to create subscriber: %v", err)
	}

	if err := sub.Connect(); err != nil {
		t.Fatalf("Failed to connect subscriber: %v", err)
	}
	defer sub.Disconnect()

	handler := func(c mqtt.Client, msg mqtt.Message) {
		if atomic.AddInt64(&receivedCount, 1) == 3 {
			close(receivedChan)
		}
	}

	// Subscribe to wildcard topic
	if err := sub.Subscribe(wildcardTopic, 0, handler); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	// Create publisher
	pubCfg := client.Config{
		Broker:   broker,
		ClientID: fmt.Sprintf("pub-wildcard-%s", uuid.New().String()),
	}
	pub, err := client.New(pubCfg)
	if err != nil {
		t.Fatalf("Failed to create publisher: %v", err)
	}

	if err := pub.Connect(); err != nil {
		t.Fatalf("Failed to connect publisher: %v", err)
	}
	defer pub.Disconnect()

	// Publish to different topics matching wildcard
	topics := []string{
		fmt.Sprintf("test/wildcard/%s/sensor1/data", baseTopicID),
		fmt.Sprintf("test/wildcard/%s/sensor2/data", baseTopicID),
		fmt.Sprintf("test/wildcard/%s/sensor3/data", baseTopicID),
	}

	g, gctx := errgroup.WithContext(ctx)
	for _, topic := range topics {
		topic := topic
		g.Go(func() error {
			if gctx.Err() != nil {
				return gctx.Err()
			}
			return pub.Publish(topic, []byte("data"), 0, false)
		})
	}

	if err := g.Wait(); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for messages
	select {
	case <-receivedChan:
		t.Logf("Successfully received %d messages via wildcard subscription", atomic.LoadInt64(&receivedCount))
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for wildcard messages. Received %d/3", atomic.LoadInt64(&receivedCount))
	}
}
