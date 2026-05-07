// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package functional

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/pkg/client"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

// TestCPCToCSC tests CPC -> CSC routing with prefix transformation
// CPC publishes: sensor/temp -> CSC receives: cpc/{1|2}/sensor/temp
func TestCPCToCSC(t *testing.T) {
	clusters := getClusters()
	csc := findCluster(clusters, "CSC")
	if csc == nil {
		t.Fatal("CSC cluster not found")
	}

	testCases := []struct {
		name     string
		cpc      *Cluster
		pubTopic string
		subTopic string
	}{
		{
			name:     "CPC-1_to_CSC",
			cpc:      findCluster(clusters, "CPC-1"),
			pubTopic: "sensor/temp",
			subTopic: "cpc/1/sensor/temp",
		},
		{
			name:     "CPC-2_to_CSC",
			cpc:      findCluster(clusters, "CPC-2"),
			pubTopic: "sensor/temp",
			subTopic: "cpc/2/sensor/temp",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cpc == nil {
				t.Skip("CPC cluster not found")
			}
			testMessageFlow(t, *tc.cpc, *csc, tc.pubTopic, tc.subTopic, false)
		})
	}
}

// TestCSCToCPC tests CSC -> CPC routing with prefix stripping
// CSC publishes: cpc/{1|2}/command/x -> CPC receives: command/x
func TestCSCToCPC(t *testing.T) {
	clusters := getClusters()
	csc := findCluster(clusters, "CSC")
	if csc == nil {
		t.Fatal("CSC cluster not found")
	}

	testCases := []struct {
		name     string
		cpc      *Cluster
		pubTopic string
		subTopic string
	}{
		{
			name:     "CSC_to_CPC-1",
			cpc:      findCluster(clusters, "CPC-1"),
			pubTopic: "cpc/1/command/restart",
			subTopic: "command/restart",
		},
		{
			name:     "CSC_to_CPC-2",
			cpc:      findCluster(clusters, "CPC-2"),
			pubTopic: "cpc/2/command/restart",
			subTopic: "command/restart",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cpc == nil {
				t.Skip("CPC cluster not found")
			}
			testMessageFlow(t, *csc, *tc.cpc, tc.pubTopic, tc.subTopic, false)
		})
	}
}

// TestCSCBroadcast tests CSC -> All CPCs broadcast
// CSC publishes: broadcast/msg -> All CPCs receive: broadcast/msg
func TestCSCBroadcast(t *testing.T) {
	clusters := getClusters()
	csc := findCluster(clusters, "CSC")
	if csc == nil {
		t.Fatal("CSC cluster not found")
	}

	cpcs := []*Cluster{
		findCluster(clusters, "CPC-1"),
		findCluster(clusters, "CPC-2"),
	}

	topic := "broadcast/announcement"
	testID := uuid.New().String()[:8]
	message := fmt.Sprintf("broadcast-test-%s", testID)

	// Subscribe all CPCs
	var receivedCount int64
	receivedChans := make([]chan struct{}, 0, len(cpcs))

	for _, cpc := range cpcs {
		if cpc == nil {
			continue
		}
		receivedChan := make(chan struct{})
		receivedChans = append(receivedChans, receivedChan)

		sub, err := createSubscriber(*cpc, topic, func(c mqtt.Client, msg mqtt.Message) {
			atomic.AddInt64(&receivedCount, 1)
			close(receivedChan)
		})
		if err != nil {
			t.Fatalf("Failed to create %s subscriber: %v", cpc.name, err)
		}
		defer sub.Disconnect()
	}

	// Publish from CSC
	pub, err := createPublisher(*csc)
	if err != nil {
		t.Fatalf("Failed to create CSC publisher: %v", err)
	}
	defer pub.Disconnect()

	if err := pub.Publish(topic, []byte(message), 1, false); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for all CPCs to receive
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	for i, ch := range receivedChans {
		select {
		case <-ch:
			t.Logf("[OK] CPC-%d received broadcast", i+1)
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for CPC-%d to receive broadcast", i+1)
		}
	}

	if count := atomic.LoadInt64(&receivedCount); count != int64(len(receivedChans)) {
		t.Errorf("Expected %d CPCs to receive broadcast, got %d", len(receivedChans), count)
	}
}

// TestCPCIsolation tests that CPCs cannot communicate directly with each other
// CPC-1 publishes: sensor/temp -> CPC-2 should NOT receive (timeout expected)
func TestCPCIsolation(t *testing.T) {
	clusters := getClusters()

	testCases := []struct {
		name   string
		source *Cluster
		target *Cluster
	}{
		{
			name:   "CPC-1_isolated_from_CPC-2",
			source: findCluster(clusters, "CPC-1"),
			target: findCluster(clusters, "CPC-2"),
		},
		{
			name:   "CPC-2_isolated_from_CPC-1",
			source: findCluster(clusters, "CPC-2"),
			target: findCluster(clusters, "CPC-1"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.source == nil || tc.target == nil {
				t.Skip("CPC cluster not found")
			}
			topic := "sensor/temp"
			testMessageFlow(t, *tc.source, *tc.target, topic, topic, true)
		})
	}
}

// TestCSCToCPCRetainedMessages tests CSC -> CPC retained message routing with prefix stripping
// CSC publishes retained: cpc/{1|2}/command/x -> CPC receives retained: command/x
func TestCSCToCPCRetainedMessages(t *testing.T) {
	clusters := getClusters()
	csc := findCluster(clusters, "CSC")
	if csc == nil {
		t.Fatal("CSC cluster not found")
	}

	testCases := []struct {
		name     string
		cpc      *Cluster
		pubTopic string
		subTopic string
	}{
		{
			name:     "CSC_to_CPC-1",
			cpc:      findCluster(clusters, "CPC-1"),
			pubTopic: "cpc/1/command/restart",
			subTopic: "command/restart",
		},
		{
			name:     "CSC_to_CPC-2",
			cpc:      findCluster(clusters, "CPC-2"),
			pubTopic: "cpc/2/command/restart",
			subTopic: "command/restart",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cpc == nil {
				t.Skip("CPC cluster not found")
			}
			testRetainedMessageFlow(t, *csc, *tc.cpc, tc.pubTopic, tc.subTopic, false)
		})
	}
}

// TestCPCToCSCRetainedMessagesFailure tests that retained messages from CPC -> CSC do NOT work
// CPC publishes retained: sensor/temp -> CSC should NOT receive retained message (timeout expected)
func TestCPCToCSCRetainedMessagesFailure(t *testing.T) {
	clusters := getClusters()
	csc := findCluster(clusters, "CSC")
	if csc == nil {
		t.Fatal("CSC cluster not found")
	}

	testCases := []struct {
		name     string
		cpc      *Cluster
		pubTopic string
		subTopic string
	}{
		{
			name:     "CPC-1_to_CSC",
			cpc:      findCluster(clusters, "CPC-1"),
			pubTopic: "sensor/temp",
			subTopic: "cpc/1/sensor/temp",
		},
		{
			name:     "CPC-2_to_CSC",
			cpc:      findCluster(clusters, "CPC-2"),
			pubTopic: "sensor/temp",
			subTopic: "cpc/2/sensor/temp",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cpc == nil {
				t.Skip("CPC cluster not found")
			}
			testRetainedMessageFlow(t, *tc.cpc, *csc, tc.pubTopic, tc.subTopic, true)
		})
	}
}

// testMessageFlow is the core test helper for message routing
func testMessageFlow(t *testing.T, source, target Cluster, pubTopic, subTopic string, expectIsolated bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	testID := uuid.New().String()[:8]
	fullPubTopic := fmt.Sprintf("%s/%s", pubTopic, testID)
	fullSubTopic := fmt.Sprintf("%s/%s", subTopic, testID)
	message := fmt.Sprintf("test-%s-to-%s", source.name, target.name)

	t.Logf("DEBUG: Publishing on %s[%s], subscribing on %s[%s]", source.name, fullPubTopic, target.name, fullSubTopic)

	// Subscribe on target
	var received int64
	receivedChan := make(chan struct{})

	sub, err := createSubscriber(target, fullSubTopic, func(c mqtt.Client, msg mqtt.Message) {
		t.Logf("DEBUG: Received message on topic: %s, payload: %s", msg.Topic(), string(msg.Payload()))
		atomic.StoreInt64(&received, 1)
		close(receivedChan)
	})
	if err != nil {
		t.Fatalf("Failed to create %s subscriber: %v", target.name, err)
	}
	defer sub.Disconnect()

	// Publish from source
	pub, err := createPublisher(source)
	if err != nil {
		t.Fatalf("Failed to create %s publisher: %v", source.name, err)
	}
	defer pub.Disconnect()

	if err := pub.Publish(fullPubTopic, []byte(message), 1, false); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait and verify
	select {
	case <-receivedChan:
		if expectIsolated {
			t.Fatalf("SECURITY VIOLATION: Message reached %s from %s - should be isolated!", target.name, source.name)
		}
		t.Logf("[OK] Message routed: %s[%s] -> %s[%s]", source.name, pubTopic, target.name, subTopic)
	case <-ctx.Done():
		if expectIsolated {
			t.Logf("[OK] Isolation verified: %s -> %s blocked (as expected)", source.name, target.name)
		} else {
			t.Fatalf("Timeout: message not routed from %s[%s] to %s[%s]", source.name, pubTopic, target.name, subTopic)
		}
	}
}

// testRetainedMessageFlow tests retained message routing across clusters
func testRetainedMessageFlow(t *testing.T, source, target Cluster, pubTopic, subTopic string, expectFailure bool) {
	// Use shorter timeout for failure cases since we expect no message
	timeout := 10 * time.Second
	if expectFailure {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	testID := uuid.New().String()[:8]
	fullPubTopic := fmt.Sprintf("%s/%s", pubTopic, testID)
	fullSubTopic := fmt.Sprintf("%s/%s", subTopic, testID)
	message := fmt.Sprintf("retained-test-%s-to-%s", source.name, target.name)

	t.Logf("DEBUG: Publishing retained message on %s[%s], subscribing on %s[%s]", source.name, fullPubTopic, target.name, fullSubTopic)

	// Publish retained message on source cluster
	pub, err := createPublisher(source)
	if err != nil {
		t.Fatalf("Failed to create %s publisher: %v", source.name, err)
	}

	if err := pub.Publish(fullPubTopic, []byte(message), 0, true); err != nil {
		pub.Disconnect()
		t.Fatalf("Failed to publish retained message: %v", err)
	}
	pub.Disconnect()

	// Wait for retained message to be stored and propagated
	// Shorter wait for failure cases
	waitTime := 2 * time.Second
	if expectFailure {
		waitTime = 1 * time.Second
	}
	time.Sleep(waitTime)

	// Subscribe on target cluster (should receive retained message immediately)
	var received int64
	receivedChan := make(chan struct{})
	var closeOnce sync.Once

	sub, err := createSubscriber(target, fullSubTopic, func(c mqtt.Client, msg mqtt.Message) {
		t.Logf("DEBUG: Received message on topic: %s, payload: %s, retained: %v", msg.Topic(), string(msg.Payload()), msg.Retained())
		if msg.Retained() && string(msg.Payload()) == message {
			atomic.StoreInt64(&received, 1)
			closeOnce.Do(func() {
				close(receivedChan)
			})
		}
	})
	if err != nil {
		t.Fatalf("Failed to create %s subscriber: %v", target.name, err)
	}
	defer sub.Disconnect()

	// Give subscription time to be active before waiting for retained message
	time.Sleep(500 * time.Millisecond)

	// Wait for retained message
	select {
	case <-receivedChan:
		if expectFailure {
			t.Fatalf("UNEXPECTED: Retained message reached %s from %s - should not work!", target.name, source.name)
		}
		t.Logf("[OK] Retained message routed: %s[%s] -> %s[%s]", source.name, pubTopic, target.name, subTopic)
	case <-ctx.Done():
		if expectFailure {
			t.Logf("[OK] Retained message correctly blocked: %s -> %s (as expected)", source.name, target.name)
		} else {
			t.Fatalf("Timeout: retained message not routed from %s[%s] to %s[%s]", source.name, pubTopic, target.name, subTopic)
		}
	}
}

// Helper functions
func findCluster(clusters []Cluster, name string) *Cluster {
	for _, c := range clusters {
		if c.name == name {
			return &c
		}
	}
	return nil
}

func createSubscriber(cluster Cluster, topic string, handler mqtt.MessageHandler) (*client.Client, error) {
	cfg := client.Config{
		Broker:   cluster.broker,
		ClientID: fmt.Sprintf("fed-sub-%s-%s", cluster.name, uuid.New().String()),
	}

	sub, err := client.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	if err := sub.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", cluster.broker, err)
	}

	// Use QoS 0 for cross-account subscriptions (NATS MQTT limitation with account routing for now TODO: synadia to fix this)
	if err := sub.Subscribe(topic, 0, handler); err != nil {
		sub.Disconnect()
		return nil, fmt.Errorf("failed to subscribe to %s: %w", topic, err)
	}

	return sub, nil
}

func createPublisher(cluster Cluster) (*client.Client, error) {
	cfg := client.Config{
		Broker:   cluster.broker,
		ClientID: fmt.Sprintf("fed-pub-%s-%s", cluster.name, uuid.New().String()),
	}

	pub, err := client.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	if err := pub.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", cluster.broker, err)
	}

	return pub, nil
}
