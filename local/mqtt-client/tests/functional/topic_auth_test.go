// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package functional

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/pkg/auth"
	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/pkg/client"
	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/pkg/config"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
)

// TestTopicAuthorization verifies MQTT topic-based authorization works correctly.
//
// MQTT 3.1.1 Authorization Behavior:
//
// 1. Subscribe Authorization:
//   - NATS correctly returns SUBACK 0x80 (failure) when subscription is denied
//   - eclipse/paho.mqtt.golang receives the code but token.Error() returns nil
//   - Workaround: client.Subscribe() manually checks SUBACK codes via (*mqtt.SubscribeToken).Result()
//
// 2. Publish Authorization:
//   - Per MQTT 3.1.1 spec [MQTT-3.3.5-2]: Server MUST either acknowledge per QoS rules OR close connection
//   - NATS chooses to acknowledge (send PUBACK for QoS 1) and silently drop unauthorized messages
//   - No client-side error indication - must verify via message delivery
func TestTopicAuthorization(t *testing.T) {
	for _, cluster := range getClusters() {
		cluster := cluster
		t.Run(cluster.name, func(t *testing.T) {
			t.Parallel()
			testTopicAuthorization(t, cluster.broker, cluster.name)
		})
	}
}

func testTopicAuthorization(t *testing.T, broker string, clusterName string) {
	testID := uuid.New().String()

	// Get OAuth2 tokens from Keycloak using client credentials
	// All clusters use the consolidated Keycloak in CSC
	keycloakURL := config.GetKeycloakURL()
	fullAccessClientID := "mqtt-client"
	fullAccessSecret := "mqtt-client-secret"
	pubOnlyClientID := "mqtt-publisher"
	pubOnlySecret := "mqtt-publisher-secret"
	subOnlyClientID := "mqtt-subscriber"
	subOnlySecret := "mqtt-subscriber-secret"

	fullAccessToken, err := auth.GetKeycloakToken(keycloakURL, fullAccessClientID, fullAccessSecret)
	if err != nil {
		t.Fatalf("Failed to get OAuth2 token: %v", err)
	}

	pubOnlyToken, err := auth.GetKeycloakToken(keycloakURL, pubOnlyClientID, pubOnlySecret)
	if err != nil {
		t.Fatalf("Failed to get pub-only OAuth2 token: %v", err)
	}

	subOnlyToken, err := auth.GetKeycloakToken(keycloakURL, subOnlyClientID, subOnlySecret)
	if err != nil {
		t.Fatalf("Failed to get sub-only OAuth2 token: %v", err)
	}

	log.Printf("Successfully obtained OAuth2 tokens for %s", clusterName)

	testCases := []struct {
		name         string
		pubTopic     string
		subPattern   string
		pubToken     string // Token to use for publishing
		subToken     string // Token to use for subscribing
		canPublish   bool
		canSubscribe bool
	}{
		// Full access user tests
		{"FullAccess_Simple", fmt.Sprintf("test/data/%s", testID), fmt.Sprintf("test/data/%s", testID), fullAccessToken, fullAccessToken, true, true},
		{"FullAccess_Nested", fmt.Sprintf("test/a/b/c/%s", testID), fmt.Sprintf("test/a/b/c/%s", testID), fullAccessToken, fullAccessToken, true, true},
		{"FullAccess_MultiLevel", fmt.Sprintf("test/%s/x/y", testID), fmt.Sprintf("test/%s/#", testID), fullAccessToken, fullAccessToken, true, true},
		{"FullAccess_SingleLevel", fmt.Sprintf("test/%s/s1/data", testID), fmt.Sprintf("test/%s/+/data", testID), fullAccessToken, fullAccessToken, true, true},
		{"FullAccess_Mixed", fmt.Sprintf("test/%s/d/s/ok", testID), fmt.Sprintf("test/%s/+/s/#", testID), fullAccessToken, fullAccessToken, true, true},

		// Denied topics for all users
		{"FullAccess_Admin", "admin/config", "admin/config", fullAccessToken, fullAccessToken, false, false},
		{"FullAccess_System", "system/metrics", "system/+", fullAccessToken, fullAccessToken, false, false},
		{"FullAccess_Sys", "$SYS/broker/stats", "$SYS/#", fullAccessToken, fullAccessToken, false, false},
		{"FullAccess_WrongPrefix", "testing/foo", "testing/#", fullAccessToken, fullAccessToken, false, false},
		{"FullAccess_RootWildcard", "any/topic", "#", fullAccessToken, fullAccessToken, false, false},

		// Pub-only user tests (can publish but not subscribe)
		// Use fullAccessToken for subscriber to verify messages are published
		{"PubOnly_CanPublish", fmt.Sprintf("test/pub/%s", testID), fmt.Sprintf("test/pub/%s", testID), pubOnlyToken, fullAccessToken, true, true},
		{"PubOnly_CannotSubscribe", fmt.Sprintf("test/nopub/%s", testID), fmt.Sprintf("test/nopub/%s", testID), fullAccessToken, pubOnlyToken, true, false},

		// Sub-only user tests (can subscribe but not publish)
		// Use fullAccessToken for publisher to verify subscriptions work
		{"SubOnly_CanSubscribe", fmt.Sprintf("test/sub/%s", testID), fmt.Sprintf("test/sub/%s", testID), fullAccessToken, subOnlyToken, true, true},
		{"SubOnly_CannotPublish", fmt.Sprintf("test/nosub/%s", testID), fmt.Sprintf("test/nosub/%s", testID), subOnlyToken, fullAccessToken, false, true},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {

			var receivedCount int64
			doneChan := make(chan struct{})

			// Subscriber with specified token
			sub, err := client.New(client.Config{
				Broker:   broker,
				ClientID: fmt.Sprintf("sub-%s", uuid.New().String()),
				Username: "oauthtoken",
				Password: tc.subToken,
			})
			if err != nil {
				t.Fatalf("Failed to create subscriber: %v", err)
			}
			if err := sub.Connect(); err != nil {
				t.Fatalf("Failed to connect subscriber: %v", err)
			}
			defer sub.Disconnect()

			handler := func(c mqtt.Client, msg mqtt.Message) {
				if atomic.AddInt64(&receivedCount, 1) == 1 {
					close(doneChan)
				}
			}

			// Subscribe - NATS returns SUBACK 0x80 for unauthorized, client wrapper detects it
			subErr := sub.Subscribe(tc.subPattern, 0, handler)
			if subErr != nil {
				t.Logf("Subscribe returned error: %v (canSubscribe=%v)", subErr, tc.canSubscribe)
			} else {
				t.Logf("Subscribe succeeded (canSubscribe=%v)", tc.canSubscribe)
			}
			if subErr != nil && tc.canSubscribe {
				t.Fatalf("Subscribe failed but should be allowed: %v", subErr)
			}
			if subErr == nil && !tc.canSubscribe {
				t.Fatalf("Subscribe succeeded but should have returned error (authorization denied)")
			}

			time.Sleep(100 * time.Millisecond)

			// Publisher with specified token
			pub, err := client.New(client.Config{
				Broker:   broker,
				ClientID: fmt.Sprintf("pub-%s", uuid.New().String()),
				Username: "oauthtoken",
				Password: tc.pubToken,
			})
			if err != nil {
				t.Fatalf("Failed to create publisher: %v", err)
			}
			if err := pub.Connect(); err != nil {
				t.Fatalf("Failed to connect publisher: %v", err)
			}
			defer pub.Disconnect()

			// Publish - per MQTT-3.3.5-2, server either acknowledges or closes connection on authorization failure
			pubErr := pub.Publish(tc.pubTopic, []byte("test"), 0, false)
			if pubErr != nil {
				t.Logf("Publish returned error: %v (canPublish=%v)", pubErr, tc.canPublish)
			} else {
				t.Logf("Publish succeeded (canPublish=%v)", tc.canPublish)
			}
			if pubErr != nil && tc.canPublish {
				t.Fatalf("Publish failed but should be allowed: %v", pubErr)
			}
			if pubErr == nil && !tc.canPublish {
				// NATS chooses to acknowledge, so no error is returned here
				t.Logf("Publish succeeded per MQTT-3.3.5-2 (NATS acknowledges unauthorized publishes)")
			}

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			// Verify message delivery
			shouldReceive := tc.canPublish && tc.canSubscribe
			if shouldReceive {
				select {
				case <-doneChan:
					// Success
				case <-ctx.Done():
					t.Fatalf("Timeout - expected message delivery")
				}
			} else {
				select {
				case <-doneChan:
					t.Fatalf("Unexpected message delivery (canPublish=%v, canSubscribe=%v)",
						tc.canPublish, tc.canSubscribe)
				case <-time.After(1 * time.Second):
					// Success - no message
				}
			}
		})
	}
}
