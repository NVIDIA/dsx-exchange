// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package functional

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/pkg/client"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

// getMTLSBrokerURL returns the mTLS broker URL from environment or default
func getMTLSBrokerURL() string {
	if url := os.Getenv("MQTT_MTLS_BROKER"); url != "" {
		return url
	}
	// Default: use CSC's mTLS port via Envoy Gateway
	return "ssl://172.18.200.1:8883"
}

func getTCPBrokerURL() string {
	if url := os.Getenv("MQTT_TCP_BROKER"); url != "" {
		return url
	}
	return "tcp://172.18.200.1:1883"
}

// getMTLSCertPaths returns paths to mTLS certificates
// These should be extracted from the Kubernetes secrets or generated locally
func getMTLSCertPaths() (cert, key, ca string) {
	certDir := os.Getenv("MTLS_CERT_DIR")
	if certDir == "" {
		certDir = "../../../nats/certs/csc" // Default to local certs (from mqtt-client/tests/functional)
	}
	return fmt.Sprintf("%s/client.pem", certDir),
		fmt.Sprintf("%s/client-key.pem", certDir),
		fmt.Sprintf("%s/ca.pem", certDir)
}

func TestMTLSConnection(t *testing.T) {
	broker := getMTLSBrokerURL()
	cert, key, ca := getMTLSCertPaths()

	// Fail test if certificates don't exist
	if _, err := os.Stat(cert); os.IsNotExist(err) {
		t.Fatalf("mTLS client certificate not found at %s", cert)
	}
	if _, err := os.Stat(key); os.IsNotExist(err) {
		t.Fatalf("mTLS client key not found at %s", key)
	}
	if _, err := os.Stat(ca); os.IsNotExist(err) {
		t.Fatalf("mTLS CA certificate not found at %s", ca)
	}

	t.Run("ConnectWithMTLS", func(t *testing.T) {
		cfg := client.Config{
			Broker:      broker,
			ClientID:    fmt.Sprintf("mtls-test-%s", uuid.New().String()),
			TLS:         true,
			TLSCert:     cert,
			TLSKey:      key,
			TLSCA:       ca,
			TLSInsecure: false, // Proper certificate validation required
		}

		c, err := client.New(cfg)
		if err != nil {
			t.Fatalf("Failed to create mTLS client: %v", err)
		}

		if err := c.Connect(); err != nil {
			t.Fatalf("Failed to connect with mTLS: %v", err)
		}
		defer c.Disconnect()

		if !c.IsConnected() {
			t.Fatal("Client should be connected")
		}

		t.Log("Successfully connected to MQTT broker with mTLS")
	})

	t.Run("ConnectWithoutMTLSShouldFail", func(t *testing.T) {
		// Attempt to connect without client certificate (should fail)
		cfg := client.Config{
			Broker:      broker,
			ClientID:    fmt.Sprintf("no-mtls-test-%s", uuid.New().String()),
			TLS:         true,
			TLSCA:       ca,
			TLSInsecure: false, // Proper certificate validation
			// No TLSCert or TLSKey - should fail
		}

		c, err := client.New(cfg)
		if err != nil {
			t.Fatalf("Failed to create TLS client: %v", err)
		}

		connectErr := c.Connect()
		if connectErr == nil {
			if c.IsConnected() {
				c.Disconnect()
				t.Fatal("Connection without client certificate should have failed (mTLS required)")
			}
			t.Fatal("Connection succeeded but client reports not connected - unexpected state")
		}

		t.Logf("Correctly rejected connection without client certificate: %v", connectErr)
	})

	t.Run("ConnectWithoutTLSShouldFail", func(t *testing.T) {
		// Attempt to connect without TLS at all (should fail)
		cfg := client.Config{
			Broker:   broker,
			ClientID: fmt.Sprintf("no-tls-test-%s", uuid.New().String()),
			TLS:      false,
			// No TLS configuration - should fail
		}

		c, err := client.New(cfg)
		if err != nil {
			t.Fatalf("Failed to create non-TLS client: %v", err)
		}

		connectErr := c.Connect()
		if connectErr == nil {
			if c.IsConnected() {
				c.Disconnect()
				t.Fatal("Connection without TLS should have failed (TLS required on mTLS port)")
			}
			t.Fatal("Connection succeeded but client reports not connected - unexpected state")
		}

		t.Logf("Correctly rejected connection without TLS: %v", connectErr)
	})
}

func TestMTLSPubSub(t *testing.T) {
	broker := getMTLSBrokerURL()
	cert, key, ca := getMTLSCertPaths()

	// Fail test if certificates don't exist
	if _, err := os.Stat(cert); os.IsNotExist(err) {
		t.Fatalf("mTLS client certificate not found at %s", cert)
	}
	if _, err := os.Stat(key); os.IsNotExist(err) {
		t.Fatalf("mTLS client key not found at %s", key)
	}
	if _, err := os.Stat(ca); os.IsNotExist(err) {
		t.Fatalf("mTLS CA certificate not found at %s", ca)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	topic := fmt.Sprintf("test/mtls/%s", uuid.New().String())
	messageCount := 10

	var receivedCount int64
	receivedChan := make(chan struct{})

	// Create mTLS subscriber
	subCfg := client.Config{
		Broker:      broker,
		ClientID:    fmt.Sprintf("mtls-sub-%s", uuid.New().String()),
		TLS:         true,
		TLSCert:     cert,
		TLSKey:      key,
		TLSCA:       ca,
		TLSInsecure: false, // Proper certificate validation required
	}
	sub, err := client.New(subCfg)
	if err != nil {
		t.Fatalf("Failed to create mTLS subscriber: %v", err)
	}

	if err := sub.Connect(); err != nil {
		t.Fatalf("Failed to connect mTLS subscriber: %v", err)
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

	// Create mTLS publisher
	pubCfg := client.Config{
		Broker:      broker,
		ClientID:    fmt.Sprintf("mtls-pub-%s", uuid.New().String()),
		TLS:         true,
		TLSCert:     cert,
		TLSKey:      key,
		TLSCA:       ca,
		TLSInsecure: false, // Proper certificate validation required
	}
	pub, err := client.New(pubCfg)
	if err != nil {
		t.Fatalf("Failed to create mTLS publisher: %v", err)
	}

	if err := pub.Connect(); err != nil {
		t.Fatalf("Failed to connect mTLS publisher: %v", err)
	}
	defer pub.Disconnect()

	// Publish messages
	for i := 0; i < messageCount; i++ {
		payload := []byte(fmt.Sprintf("mtls-message-%d", i))
		if err := pub.Publish(topic, payload, 0, false); err != nil {
			t.Fatalf("Failed to publish message %d: %v", i, err)
		}
	}

	// Wait for all messages to be received
	select {
	case <-receivedChan:
		t.Logf("Successfully received %d messages via mTLS", atomic.LoadInt64(&receivedCount))
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for mTLS messages. Received %d/%d", atomic.LoadInt64(&receivedCount), messageCount)
	}
}

// TestMTLSToTCPRouting tests that messages published via mTLS are received by TCP clients
func TestMTLSToTCPRouting(t *testing.T) {
	mtlsBroker := getMTLSBrokerURL()
	tcpBroker := getTCPBrokerURL()
	cert, key, ca := getMTLSCertPaths()

	// Fail test if certificates don't exist
	if _, err := os.Stat(cert); os.IsNotExist(err) {
		t.Fatalf("mTLS client certificate not found at %s", cert)
	}
	if _, err := os.Stat(key); os.IsNotExist(err) {
		t.Fatalf("mTLS client key not found at %s", key)
	}
	if _, err := os.Stat(ca); os.IsNotExist(err) {
		t.Fatalf("mTLS CA certificate not found at %s", ca)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	topic := fmt.Sprintf("test/mtls-to-tcp/%s", uuid.New().String())
	messageCount := 5

	var receivedCount int64
	receivedChan := make(chan struct{})

	// Create TCP subscriber (listening on core NATS)
	subCfg := client.Config{
		Broker:   tcpBroker,
		ClientID: fmt.Sprintf("tcp-sub-%s", uuid.New().String()),
	}
	sub, err := client.New(subCfg)
	if err != nil {
		t.Fatalf("Failed to create TCP subscriber: %v", err)
	}

	if err := sub.Connect(); err != nil {
		t.Fatalf("Failed to connect TCP subscriber: %v", err)
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

	// Create mTLS publisher
	pubCfg := client.Config{
		Broker:      mtlsBroker,
		ClientID:    fmt.Sprintf("mtls-pub-%s", uuid.New().String()),
		TLS:         true,
		TLSCert:     cert,
		TLSKey:      key,
		TLSCA:       ca,
		TLSInsecure: false, // Proper certificate validation required
	}
	pub, err := client.New(pubCfg)
	if err != nil {
		t.Fatalf("Failed to create mTLS publisher: %v", err)
	}

	if err := pub.Connect(); err != nil {
		t.Fatalf("Failed to connect mTLS publisher: %v", err)
	}
	defer pub.Disconnect()

	// Publish messages from mTLS client
	for i := 0; i < messageCount; i++ {
		payload := []byte(fmt.Sprintf("cross-transport-message-%d", i))
		if err := pub.Publish(topic, payload, 0, false); err != nil {
			t.Fatalf("Failed to publish message %d: %v", i, err)
		}
	}

	// Wait for TCP subscriber to receive mTLS messages
	select {
	case <-receivedChan:
		t.Logf("Successfully routed %d messages from mTLS to TCP clients", atomic.LoadInt64(&receivedCount))
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for cross-transport messages. Received %d/%d", atomic.LoadInt64(&receivedCount), messageCount)
	}
}

// TestTCPToMTLSRouting tests that messages published via TCP are received by mTLS clients
func TestTCPToMTLSRouting(t *testing.T) {
	mtlsBroker := getMTLSBrokerURL()
	tcpBroker := getTCPBrokerURL()
	cert, key, ca := getMTLSCertPaths()

	// Fail test if certificates don't exist
	if _, err := os.Stat(cert); os.IsNotExist(err) {
		t.Fatalf("mTLS client certificate not found at %s", cert)
	}
	if _, err := os.Stat(key); os.IsNotExist(err) {
		t.Fatalf("mTLS client key not found at %s", key)
	}
	if _, err := os.Stat(ca); os.IsNotExist(err) {
		t.Fatalf("mTLS CA certificate not found at %s", ca)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	topic := fmt.Sprintf("test/tcp-to-mtls/%s", uuid.New().String())
	messageCount := 5

	var receivedCount int64
	receivedChan := make(chan struct{})

	// Create mTLS subscriber
	subCfg := client.Config{
		Broker:      mtlsBroker,
		ClientID:    fmt.Sprintf("mtls-sub-%s", uuid.New().String()),
		TLS:         true,
		TLSCert:     cert,
		TLSKey:      key,
		TLSCA:       ca,
		TLSInsecure: false, // Proper certificate validation required
	}
	sub, err := client.New(subCfg)
	if err != nil {
		t.Fatalf("Failed to create mTLS subscriber: %v", err)
	}

	if err := sub.Connect(); err != nil {
		t.Fatalf("Failed to connect mTLS subscriber: %v", err)
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

	// Create TCP publisher
	pubCfg := client.Config{
		Broker:   tcpBroker,
		ClientID: fmt.Sprintf("tcp-pub-%s", uuid.New().String()),
	}
	pub, err := client.New(pubCfg)
	if err != nil {
		t.Fatalf("Failed to create TCP publisher: %v", err)
	}

	if err := pub.Connect(); err != nil {
		t.Fatalf("Failed to connect TCP publisher: %v", err)
	}
	defer pub.Disconnect()

	// Publish messages from TCP client
	for i := 0; i < messageCount; i++ {
		payload := []byte(fmt.Sprintf("cross-transport-message-%d", i))
		if err := pub.Publish(topic, payload, 0, false); err != nil {
			t.Fatalf("Failed to publish message %d: %v", i, err)
		}
	}

	// Wait for mTLS subscriber to receive TCP messages
	select {
	case <-receivedChan:
		t.Logf("Successfully routed %d messages from TCP to mTLS clients", atomic.LoadInt64(&receivedCount))
	case <-ctx.Done():
		t.Fatalf("Timeout waiting for cross-transport messages. Received %d/%d", atomic.LoadInt64(&receivedCount), messageCount)
	}
}
