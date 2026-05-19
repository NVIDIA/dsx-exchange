// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package scenarios

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/benchmark"
	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/metrics"
	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/report"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// FanIn1K implements the singlenode-sharedsub-1K-5-1K-1K scenario
// 1,000 publishers send to 5 subscribers using shared subscription
type FanIn1K struct{}

func (s *FanIn1K) Name() string {
	return "fanin-1k"
}

func (s *FanIn1K) Description() string {
	return "1,000 publishers → 5 subscribers, 1,000 topics, 1 msg/sec per publisher"
}

func (s *FanIn1K) Config() report.ScenarioConfig {
	return report.ScenarioConfig{
		Name:           s.Name(),
		Description:    s.Description(),
		NumPublishers:  1000,
		NumSubscribers: 5,
		NumTopics:      1000,
		MessageSize:    16,
		QoS:            1,
	}
}

func (s *FanIn1K) Run(ctx context.Context, config *benchmark.Config, collector *metrics.Collector) error {
	numPublishers := config.FanInPublishers
	numSubscribers := config.FanInSubscribers
	numTopics := config.FanInTopics
	duration := config.Duration
	msgRate := config.PublishRate
	messageSize := config.MessageSize

	collector.NumPublishers = int64(numPublishers)
	collector.NumSubscribers = int64(numSubscribers)
	collector.NumTopics = int64(numTopics)
	collector.MessageSize = int64(messageSize)

	fmt.Printf("Connecting %d subscribers...\n", numSubscribers)

	var wg sync.WaitGroup
	subscribers := make([]mqtt.Client, numSubscribers)
	connStart := time.Now()

	// MQTT 3.1.1 does not support shared subscriptions
	// All subscribers will receive all messages
	subscribeTopic := fmt.Sprintf("test/%s/#", config.TestRunID)

	// Connect subscribers with shared subscription
	for i := 0; i < numSubscribers; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		clientID := fmt.Sprintf("fanin-sub-%s-%d", config.TestRunID, i)

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

		// Subscribe to topic wildcard
		token = client.Subscribe(subscribeTopic, config.QoS, nil)
		token.Wait()

		if token.Error() != nil {
			collector.RecordSubscription(false)
			return fmt.Errorf("subscriber %d failed to subscribe to %s: %w", i, subscribeTopic, token.Error())
		}

		collector.RecordSubscription(true)

		if i == 0 {
			fmt.Printf("  Subscriber %d connected and subscribed to %s\n", i+1, subscribeTopic)
		}
	}

	fmt.Printf("\nAll subscribers connected\n")
	fmt.Printf("Note: MQTT 3.1.1 does not support shared subscriptions - all subscribers will receive all messages\n")
	fmt.Printf("Connecting %d publishers...\n", numPublishers)

	publishers := make([]mqtt.Client, numPublishers)

	// Connect publishers
	for i := 0; i < numPublishers; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		clientID := fmt.Sprintf("fanin-pub-%s-%d", config.TestRunID, i)

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

		client := mqtt.NewClient(opts)
		token := client.Connect()
		token.Wait()

		if token.Error() != nil {
			collector.RecordConnection(false)
			return fmt.Errorf("publisher %d failed to connect: %w", i, token.Error())
		}

		collector.RecordConnection(true)
		publishers[i] = client

		if (i+1)%100 == 0 {
			fmt.Printf("  Publishers connected: %d/%d\n", i+1, numPublishers)
		}
	}

	fmt.Printf("\nAll publishers connected\n")

	// Calculate connection rate
	connDuration := time.Since(connStart)
	totalConns := numSubscribers + numPublishers
	connRate := float64(totalConns) / connDuration.Seconds()
	collector.SetConnectionRate(connRate)
	fmt.Printf("Connection rate: %.2f conn/sec\n", connRate)

	fmt.Printf("Publishing messages at %d msg/sec per publisher for %v...\n\n", msgRate, duration)

	// Mark publish phase start
	collector.PublishStart = time.Now()

	// Start publishing from all publishers
	publishCtx, publishCancel := context.WithCancel(ctx)
	defer publishCancel()

	for i := 0; i < numPublishers; i++ {
		wg.Add(1)
		go func(pubIdx int) {
			defer wg.Done()

			// Each publisher publishes to a deterministic topic from the configured set.
			topic := fmt.Sprintf("test/%s/%d", config.TestRunID, (pubIdx%numTopics)+1)
			ticker := time.NewTicker(time.Second / time.Duration(msgRate))
			defer ticker.Stop()

			endTime := time.Now().Add(duration)
			messageCount := 0

			for {
				select {
				case <-publishCtx.Done():
					return
				case <-ticker.C:
					if time.Now().After(endTime) {
						return
					}

					// Create payload with timestamp for latency measurement
					payload := make([]byte, messageSize)
					binary.BigEndian.PutUint64(payload[:8], uint64(time.Now().UnixNano()))

					token := publishers[pubIdx].Publish(topic, config.QoS, false, payload)
					token.Wait()

					if token.Error() != nil {
						collector.RecordPublish(false)
					} else {
						collector.RecordPublish(true)
						messageCount++
					}
				}
			}
		}(i)
	}

	// Progress monitoring
	progressTicker := time.NewTicker(10 * time.Second)
	defer progressTicker.Stop()

	publishStart := time.Now()
	endTime := publishStart.Add(duration)

	done := false
	for !done {
		select {
		case <-ctx.Done():
			fmt.Printf("\nShutdown requested\n")
			publishCancel()
			wg.Wait()
			done = true
		case <-progressTicker.C:
			if time.Now().After(endTime) {
				publishCancel()
				wg.Wait()
				done = true
			} else {
				elapsed := time.Since(publishStart)
				remaining := duration - elapsed
				snap := collector.GetSnapshot()
				fmt.Printf("  Progress: Published=%d, Received=%d, Elapsed=%v, Remaining=%v\n",
					snap.PublishedMessages, snap.ReceivedMessages,
					elapsed.Round(time.Second), remaining.Round(time.Second))
			}
		}
	}

	// Mark publish phase end
	collector.PublishEnd = time.Now()

	fmt.Printf("\nPublishing completed\n")

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
		// Break early if we've received all published messages
		if snap.ReceivedMessages >= snap.PublishedMessages {
			break
		}
	}

	fmt.Printf("  Final: Published=%d, Received=%d\n", snap.PublishedMessages, snap.ReceivedMessages)
	fmt.Printf("Disconnecting clients...\n")

	// Disconnect publishers
	for i, pub := range publishers {
		if pub != nil && pub.IsConnected() {
			pub.Disconnect(250)
			collector.RecordDisconnection()
		}
		if (i+1)%100 == 0 {
			fmt.Printf("  Publishers disconnected: %d/%d\n", i+1, numPublishers)
		}
	}

	// Disconnect subscribers
	for _, sub := range subscribers {
		if sub != nil && sub.IsConnected() {
			sub.Disconnect(250)
			collector.RecordDisconnection()
		}
	}
	fmt.Printf("  Subscribers disconnected: %d/%d\n", numSubscribers, numSubscribers)

	fmt.Printf("All clients disconnected\n")

	return nil
}
