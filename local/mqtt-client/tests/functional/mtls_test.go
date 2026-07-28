// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package functional

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/pkg/client"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
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

type mtlsEndpoint struct {
	name    string
	broker  string
	certDir string
}

func getMTLSEndpoints() []mtlsEndpoint {
	if broker := os.Getenv("MQTT_MTLS_BROKER"); broker != "" {
		cert, _, _ := getMTLSCertPaths()
		return []mtlsEndpoint{{name: "configured", broker: broker, certDir: strings.TrimSuffix(cert, "/client.pem")}}
	}
	return []mtlsEndpoint{
		{name: "CSC", broker: "ssl://172.18.200.1:8883", certDir: "../../../nats/certs/csc"},
		{name: "CPC-1", broker: "ssl://172.18.201.1:8883", certDir: "../../../nats/certs/cpc-1"},
		{name: "CPC-2", broker: "ssl://172.18.202.1:8883", certDir: "../../../nats/certs/cpc-2"},
	}
}

func (endpoint mtlsEndpoint) config(clientID string) client.Config {
	return client.Config{
		Broker:   endpoint.broker,
		ClientID: clientID,
		TLS:      true,
		TLSCert:  endpoint.certDir + "/client.pem",
		TLSKey:   endpoint.certDir + "/client-key.pem",
		TLSCA:    endpoint.certDir + "/ca.pem",
	}
}

func (endpoint mtlsEndpoint) natsBroker() string {
	return strings.Replace(strings.Replace(endpoint.broker, "ssl://", "nats://", 1), ":8883", ":4222", 1)
}

func connectMTLSClient(t *testing.T, config client.Config) *client.Client {
	t.Helper()

	mqttClient, err := client.New(config)
	if err != nil {
		t.Fatalf("create mTLS client: %v", err)
	}
	if err := mqttClient.Connect(); err != nil {
		t.Fatalf("connect mTLS client: %v", err)
	}
	return mqttClient
}

func TestRestrictedMTLSMQTTQoS1(t *testing.T) {
	for _, endpoint := range getMTLSEndpoints() {
		t.Run(endpoint.name, func(t *testing.T) {
			topic := "test/mtls/qos1/" + uuid.NewString()
			received := make(chan mqtt.Message, 1)

			// Verify permitted QoS 1 traffic.
			subscriber := connectMTLSClient(t, endpoint.config("qos1-sub-"+uuid.NewString()))
			defer subscriber.Disconnect()
			if err := subscriber.Subscribe(topic, 1, func(_ mqtt.Client, message mqtt.Message) {
				received <- message
			}); err != nil {
				t.Fatalf("subscribe at QoS 1: %v", err)
			}

			publisher := connectMTLSClient(t, endpoint.config("qos1-pub-"+uuid.NewString()))
			defer publisher.Disconnect()
			payload := []byte("qos1")
			if err := publisher.Publish(topic, payload, 1, false); err != nil {
				t.Fatalf("publish at QoS 1: %v", err)
			}

			message := waitForMQTTMessage(t, received)
			if message.Qos() != 1 || string(message.Payload()) != string(payload) {
				t.Fatalf("received QoS %d payload %q", message.Qos(), message.Payload())
			}

			// Reject subjects outside test.>.
			forbiddenSubject := "forbidden." + uuid.NewString()
			observer, err := nats.Connect(endpoint.natsBroker())
			if err != nil {
				t.Fatalf("connect permission observer: %v", err)
			}
			defer observer.Close()
			observed, err := observer.SubscribeSync(forbiddenSubject)
			if err != nil {
				t.Fatalf("subscribe permission observer: %v", err)
			}
			if err := observer.Flush(); err != nil {
				t.Fatalf("flush permission observer: %v", err)
			}

			// MQTT 3.1.1 acknowledges QoS 1 publishes even when NATS denies the subject.
			if err := publisher.Publish(strings.ReplaceAll(forbiddenSubject, ".", "/"), nil, 1, false); err != nil {
				t.Fatalf("complete denied QoS 1 publish: %v", err)
			}
			if _, err := observed.NextMsg(500 * time.Millisecond); !errors.Is(err, nats.ErrTimeout) {
				t.Fatalf("wait for denied publish: %v", err)
			}
			if err := subscriber.Subscribe("forbidden/#", 1, nil); err == nil {
				t.Fatal("subscription outside the application permission succeeded")
			}
		})
	}
}

func TestRestrictedMTLSMQTTPersistentSession(t *testing.T) {
	for _, endpoint := range getMTLSEndpoints() {
		t.Run(endpoint.name, func(t *testing.T) {
			clientID := "persistent-" + uuid.NewString()
			topic := "test/mtls/persistent/" + uuid.NewString()
			config := endpoint.config(clientID)
			config.PersistentSession = true

			// Keep the subscription after disconnect.
			subscriber := connectMTLSClient(t, config)
			if err := subscriber.Subscribe(topic, 1, nil); err != nil {
				t.Fatalf("create persistent subscription: %v", err)
			}
			subscriber.Disconnect()

			// Queue a message while the subscriber is offline.
			publisher := connectMTLSClient(t, endpoint.config("persistent-pub-"+uuid.NewString()))
			if err := publisher.Publish(topic, []byte("stored while offline"), 1, false); err != nil {
				publisher.Disconnect()
				t.Fatalf("publish while subscriber is offline: %v", err)
			}
			publisher.Disconnect()

			// Resume the session with the same client ID.
			received := make(chan mqtt.Message, 1)
			config.MessageHandler = func(_ mqtt.Client, message mqtt.Message) {
				received <- message
			}
			reconnected := connectMTLSClient(t, config)
			defer reconnected.Disconnect()
			if message := waitForMQTTMessage(t, received); string(message.Payload()) != "stored while offline" {
				t.Fatalf("persistent payload %q", message.Payload())
			}
		})
	}
}

func TestRestrictedMTLSMQTTRetainedMessage(t *testing.T) {
	for _, endpoint := range getMTLSEndpoints() {
		t.Run(endpoint.name, func(t *testing.T) {
			topic := "test/mtls/retained/" + uuid.NewString()

			// Store the retained message.
			publisher := connectMTLSClient(t, endpoint.config("retained-pub-"+uuid.NewString()))
			defer publisher.Disconnect()
			defer publisher.Publish(topic, nil, 1, true)
			if err := publisher.Publish(topic, []byte("retained"), 1, true); err != nil {
				t.Fatalf("publish retained message: %v", err)
			}

			// Deliver it to a new subscriber.
			received := make(chan mqtt.Message, 1)
			subscriber := connectMTLSClient(t, endpoint.config("retained-sub-"+uuid.NewString()))
			defer subscriber.Disconnect()
			if err := subscriber.Subscribe(topic, 1, func(_ mqtt.Client, message mqtt.Message) {
				received <- message
			}); err != nil {
				t.Fatalf("subscribe to retained message: %v", err)
			}

			message := waitForMQTTMessage(t, received)
			if !message.Retained() || string(message.Payload()) != "retained" {
				t.Fatalf("received retained=%t payload=%q", message.Retained(), message.Payload())
			}
		})
	}
}

func waitForMQTTMessage(t *testing.T, messages <-chan mqtt.Message) mqtt.Message {
	t.Helper()

	select {
	case message := <-messages:
		return message
	case <-time.After(15 * time.Second):
		t.Fatal("timeout waiting for MQTT message")
		return nil
	}
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
