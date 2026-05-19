// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package scenarios

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/benchmark"
	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/metrics"
	"github.com/NVIDIA/dsx-exchange/local/mqttbs/internal/report"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Connection10K implements the singlenode-conn-tcp-10K-100 scenario
// 1,000 clients connect at 100 connections/sec and hold for 30 minutes
type Connection10K struct{}

func (s *Connection10K) Name() string {
	return "connection-10k"
}

func (s *Connection10K) Description() string {
	return "10,000 clients connect within 100 seconds"
}

func (s *Connection10K) Config() report.ScenarioConfig {
	return report.ScenarioConfig{
		Name:           s.Name(),
		Description:    s.Description(),
		NumPublishers:  0,
		NumSubscribers: 0,
		NumTopics:      0,
		MessageSize:    0,
		QoS:            1,
	}
}

func (s *Connection10K) Run(ctx context.Context, config *benchmark.Config, collector *metrics.Collector) error {
	numClients := config.ConnectionClients
	connRate := config.ConnectionRate

	holdDuration := config.Duration
	collector.NumPublishers = 0
	collector.NumSubscribers = int64(numClients)
	collector.NumTopics = 0
	collector.MessageSize = 0

	fmt.Printf("Connecting %d clients at %d conn/sec...\n", numClients, connRate)

	var wg sync.WaitGroup
	clients := make([]mqtt.Client, numClients)
	ticker := time.NewTicker(time.Second / time.Duration(connRate))
	defer ticker.Stop()

	connStart := time.Now()

	// Connect clients at controlled rate
	for i := 0; i < numClients; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			wg.Add(1)
			go func(clientID int) {
				defer wg.Done()

				opts := mqtt.NewClientOptions().
					AddBroker(config.BrokerURL).
					SetClientID(fmt.Sprintf("conn-%s-%d", config.TestRunID, clientID)).
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
					// Log first few errors
					snap := collector.GetSnapshot()
					if snap.FailedConns < 5 {
						fmt.Printf("  Connection error for client %d: %v\n", clientID, token.Error())
					}
					return
				}

				collector.RecordConnection(true)
				clients[clientID] = client
			}(i)
		}

		// Progress indicator every 100 connections
		if (i+1)%100 == 0 {
			fmt.Printf("  Connected: %d/%d\n", i+1, numClients)
		}
	}

	// Wait for all connection attempts to complete
	wg.Wait()

	connDuration := time.Since(connStart)
	snap := collector.GetSnapshot()
	actualConnRate := float64(snap.SuccessfulConns) / connDuration.Seconds()
	collector.SetConnectionRate(actualConnRate)

	fmt.Printf("\nAll connection attempts completed\n")
	fmt.Printf("  Successful: %d\n", snap.SuccessfulConns)
	fmt.Printf("  Failed: %d\n", snap.FailedConns)
	fmt.Printf("  Actual rate: %.2f conn/sec\n", actualConnRate)
	fmt.Printf("\nHolding connections for %v...\n", holdDuration)

	// Hold connections for the specified duration
	holdTimer := time.NewTimer(holdDuration)
	defer holdTimer.Stop()

	select {
	case <-ctx.Done():
		fmt.Printf("\nShutdown requested, disconnecting clients...\n")
	case <-holdTimer.C:
		fmt.Printf("\nHold duration completed, disconnecting clients...\n")
	}

	// Disconnect all clients
	disconnectStart := time.Now()
	for i, client := range clients {
		if client != nil && client.IsConnected() {
			client.Disconnect(250)
			collector.RecordDisconnection()
		}
		if (i+1)%100 == 0 {
			fmt.Printf("  Disconnected: %d/%d\n", i+1, numClients)
		}
	}

	fmt.Printf("All clients disconnected in %v\n", time.Since(disconnectStart))

	return nil
}
