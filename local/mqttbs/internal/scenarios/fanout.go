// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package scenarios

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/benchmark"
	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/metrics"
	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/report"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// FanOut1K implements the singlenode-fanout-1-1K-1-1K scenario
// 1 publisher sends to 1,000 subscribers at 1 msg/sec
type FanOut1K struct{}

func (s *FanOut1K) Name() string {
	return "fanout-1k"
}

func (s *FanOut1K) Description() string {
	return "1 publisher → 1,000 subscribers, 1 msg/sec"
}

func (s *FanOut1K) Config() report.ScenarioConfig {
	return report.ScenarioConfig{
		Name:           s.Name(),
		Description:    s.Description(),
		NumPublishers:  1,
		NumSubscribers: 1000,
		NumTopics:      1,
		MessageSize:    16,
		QoS:            1,
	}
}

func (s *FanOut1K) Run(ctx context.Context, config *benchmark.Config, collector *metrics.Collector) error {
	numSubscribers := config.FanOutSubscribers
	duration := config.Duration
	msgRate := config.PublishRate
	messageSize := config.MessageSize
	topic := fmt.Sprintf("test/%s/fanout", config.TestRunID)

	collector.NumPublishers = 1
	collector.NumSubscribers = int64(numSubscribers)
	collector.NumTopics = 1
	collector.MessageSize = int64(messageSize)

	fmt.Printf("Connecting %d subscribers...\n", numSubscribers)

	connStart := time.Now()

	// Connect subscribers
	subscribers := make([]mqtt.Client, numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		clientID := fmt.Sprintf("fanout-sub-%s-%d", config.TestRunID, i)

		opts := mqtt.NewClientOptions().
			AddBroker(config.BrokerURL).
			SetClientID(clientID).
			SetKeepAlive(time.Duration(config.KeepAlive) * time.Second).
			SetCleanSession(config.CleanSession).
			SetConnectTimeout(config.ConnectTimeout).
			SetAutoReconnect(false).
			SetProtocolVersion(4) // MQTT 3.1.1

		if config.Username != "" {
			opts.SetUsername(config.Username)
			opts.SetPassword(config.Password)
		}

		// Message handler
		opts.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
			collector.RecordReceive()

			// Extract timestamp from payload for latency calculation
			if len(msg.Payload()) >= 8 {
				sentTimeNano := int64(binary.BigEndian.Uint64(msg.Payload()[:8]))
				sentTime := time.Unix(0, sentTimeNano)
				latency := time.Since(sentTime)
				collector.RecordLatency(latency)
			}
		})

		client := mqtt.NewClient(opts)
		token := client.Connect()
		token.Wait()

		if token.Error() != nil {
			collector.RecordConnection(false)
			return fmt.Errorf("subscriber %d failed to connect: %w", i, token.Error())
		}

		collector.RecordConnection(true)
		subscribers[i] = client

		// Subscribe to topic
		token = client.Subscribe(topic, config.QoS, nil)
		token.Wait()

		if token.Error() != nil {
			collector.RecordSubscription(false)
			return fmt.Errorf("subscriber %d failed to subscribe: %w", i, token.Error())
		}

		collector.RecordSubscription(true)

		if (i+1)%100 == 0 {
			fmt.Printf("  Subscribers connected: %d/%d\n", i+1, numSubscribers)
		}
	}

	fmt.Printf("\nAll subscribers connected and subscribed\n")
	fmt.Printf("Connecting publisher...\n")

	// Connect publisher
	pubOpts := mqtt.NewClientOptions().
		AddBroker(config.BrokerURL).
		SetClientID(fmt.Sprintf("fanout-pub-%s", config.TestRunID)).
		SetKeepAlive(time.Duration(config.KeepAlive) * time.Second).
		SetCleanSession(config.CleanSession).
		SetConnectTimeout(config.ConnectTimeout).
		SetAutoReconnect(false).
		SetProtocolVersion(4) // MQTT 3.1.1

	if config.Username != "" {
		pubOpts.SetUsername(config.Username)
		pubOpts.SetPassword(config.Password)
	}

	publisher := mqtt.NewClient(pubOpts)
	token := publisher.Connect()
	token.Wait()

	if token.Error() != nil {
		collector.RecordConnection(false)
		return fmt.Errorf("publisher failed to connect: %w", token.Error())
	}

	collector.RecordConnection(true)

	// Calculate connection rate
	connDuration := time.Since(connStart)
	totalConns := numSubscribers + 1
	connRate := float64(totalConns) / connDuration.Seconds()
	collector.SetConnectionRate(connRate)

	fmt.Printf("Publisher connected\n")
	fmt.Printf("Connection rate: %.2f conn/sec\n\n", connRate)

	// Start publishing
	fmt.Printf("Publishing messages at %d msg/sec for %v...\n", msgRate, duration)

	// Mark publish phase start
	collector.PublishStart = time.Now()

	ticker := time.NewTicker(time.Second / time.Duration(msgRate))
	defer ticker.Stop()

	endTime := time.Now().Add(duration)
	messageCount := 0

	done := false
	for !done {
		select {
		case <-ctx.Done():
			fmt.Printf("\nShutdown requested\n")
			done = true
		case <-ticker.C:
			if time.Now().After(endTime) {
				done = true
			} else {
				// Create payload with timestamp for latency measurement
				payload := make([]byte, messageSize)
				binary.BigEndian.PutUint64(payload[:8], uint64(time.Now().UnixNano()))

				token := publisher.Publish(topic, config.QoS, false, payload)
				token.Wait()

				if token.Error() != nil {
					collector.RecordPublish(false)
				} else {
					collector.RecordPublish(true)
					messageCount++
				}

				if messageCount%100 == 0 {
					fmt.Printf("  Published: %d messages\n", messageCount)
				}
			}
		}
	}

	// Mark publish phase end
	collector.PublishEnd = time.Now()

	fmt.Printf("\nPublishing completed: %d messages\n", messageCount)

	// Allow time for in-flight messages to be delivered
	fmt.Printf("Waiting for message delivery...\n")
	snap := collector.GetSnapshot()
	drainStart := time.Now()
	drainTimeout := 5 * time.Second
	lastReceived := snap.ReceivedMessages

	for time.Since(drainStart) < drainTimeout {
		time.Sleep(100 * time.Millisecond)
		snap = collector.GetSnapshot()
		if snap.ReceivedMessages > lastReceived {
			lastReceived = snap.ReceivedMessages
			drainStart = time.Now() // Reset timer if still receiving
		}
		// For fanout, expected = published * numSubscribers
		expectedTotal := snap.PublishedMessages * uint64(numSubscribers)
		if snap.ReceivedMessages >= expectedTotal {
			break
		}
	}

	fmt.Printf("  Final: Published=%d, Received=%d\n", snap.PublishedMessages, snap.ReceivedMessages)
	fmt.Printf("Disconnecting clients...\n")

	// Disconnect publisher
	if publisher.IsConnected() {
		publisher.Disconnect(250)
		collector.RecordDisconnection()
	}

	// Disconnect subscribers
	for i, sub := range subscribers {
		if sub != nil && sub.IsConnected() {
			sub.Disconnect(250)
			collector.RecordDisconnection()
		}
		if (i+1)%100 == 0 {
			fmt.Printf("  Disconnected: %d/%d\n", i+1, numSubscribers)
		}
	}

	fmt.Printf("All clients disconnected\n")

	return nil
}
