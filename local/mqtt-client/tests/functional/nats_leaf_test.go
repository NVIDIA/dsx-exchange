// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package functional

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/pkg/auth"
	"github.com/NVIDIA/dsx-exchange/local/mqtt-client/pkg/config"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

const launchLayerAccount = "LaunchLayer"

type natsCluster struct {
	name   string
	broker string
}

type launchLayerEndpoint struct {
	cluster natsCluster
	account string
}

func TestLaunchLayerLeafNodeRoutesNATSMessages(t *testing.T) {
	clusters := getNATSClusters()
	csc := findNATSCluster(clusters, "CSC")
	if csc == nil {
		t.Fatal("CSC NATS cluster not found")
	}

	tests := []struct {
		name   string
		source launchLayerEndpoint
		target launchLayerEndpoint
	}{
		{
			name:   "CPC-1 to CSC",
			source: launchLayerCPC(t, clusters, "1"),
			target: launchLayerCSC(t, *csc),
		},
		{
			name:   "CPC-2 to CSC",
			source: launchLayerCPC(t, clusters, "2"),
			target: launchLayerCSC(t, *csc),
		},
		{
			name:   "CSC to CPC-1",
			source: launchLayerCSC(t, *csc),
			target: launchLayerCPC(t, clusters, "1"),
		},
		{
			name:   "CSC to CPC-2",
			source: launchLayerCSC(t, *csc),
			target: launchLayerCPC(t, clusters, "2"),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			testNATSMessageFlow(t, tc.source, tc.target)
		})
	}
}

func TestLaunchLayerJetStreamStoresLeafMessages(t *testing.T) {
	// Core NATS publishes from each LaunchLayer cluster should be captured by a
	// CSC-owned stream after stream interest propagates across the leaf path.
	clusters := getNATSClusters()
	csc := findNATSCluster(clusters, "CSC")
	if csc == nil {
		t.Fatal("CSC NATS cluster not found")
	}

	tests := []struct {
		name   string
		source launchLayerEndpoint
	}{
		{
			name:   "CSC local",
			source: launchLayerCSC(t, *csc),
		},
		{
			name:   "CPC-1 to CSC",
			source: launchLayerCPC(t, clusters, "1"),
		},
		{
			name:   "CPC-2 to CSC",
			source: launchLayerCPC(t, clusters, "2"),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			testLaunchLayerJetStream(t, tc.source, launchLayerCSC(t, *csc))
		})
	}
}

func TestLaunchLayerJetStreamAPIProxiesToCSC(t *testing.T) {
	clusters := getNATSClusters()
	csc := findNATSCluster(clusters, "CSC")
	if csc == nil {
		t.Fatal("CSC NATS cluster not found")
	}

	streamOwner := launchLayerCSC(t, *csc)
	for _, cluster := range clusters {
		cluster := cluster
		t.Run(cluster.name, func(t *testing.T) {
			testLaunchLayerJetStreamAPI(t, launchLayerCluster(t, cluster), streamOwner)
		})
	}
}

func testLaunchLayerJetStream(t *testing.T, source launchLayerEndpoint, streamOwner launchLayerEndpoint) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	sourceConn := connectLaunchLayer(t, source)
	defer sourceConn.Close()

	ownerConn := connectLaunchLayer(t, streamOwner)
	defer ownerConn.Close()

	ownerJS, err := ownerConn.JetStream(nats.Context(ctx))
	if err != nil {
		t.Fatalf("failed to create JetStream context for %s: %v", streamOwner.cluster.name, err)
	}

	token := strings.ReplaceAll(uuid.NewString(), "-", "")
	streamName := "LL_" + token[:12]
	subject := "launchlayer.js." + token
	payload := []byte(fmt.Sprintf("jetstream-%s-to-%s-%s", source.cluster.name, streamOwner.cluster.name, token))

	if _, err := ownerJS.AddStream(&nats.StreamConfig{
		Name:     streamName,
		Subjects: []string{subject},
		Storage:  nats.MemoryStorage,
		Replicas: 1,
	}, nats.Context(ctx)); err != nil {
		t.Fatalf("failed to create LaunchLayer stream %s on %s: %v", streamName, streamOwner.cluster.name, err)
	}
	defer func() {
		if err := ownerJS.DeleteStream(streamName); err != nil && !errors.Is(err, nats.ErrStreamNotFound) {
			t.Logf("failed to delete LaunchLayer stream %s: %v", streamName, err)
		}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := sourceConn.Publish(subject, payload); err != nil {
			t.Fatalf("failed to publish LaunchLayer payload from %s: %v", source.cluster.name, err)
		}
		if err := sourceConn.FlushWithContext(ctx); err != nil {
			t.Fatalf("failed to flush LaunchLayer publisher on %s: %v", source.cluster.name, err)
		}

		msg, err := ownerJS.GetLastMsg(streamName, subject, nats.Context(ctx))
		if err == nil {
			if string(msg.Data) != string(payload) {
				t.Fatalf("stored payload %q, want %q", msg.Data, payload)
			}
			t.Logf("JetStream stored LaunchLayer message in %s from %s at sequence %d",
				streamName, source.cluster.name, msg.Sequence)
			return
		}
		if !errors.Is(err, nats.ErrMsgNotFound) {
			t.Fatalf("failed to read LaunchLayer stream %s: %v", streamName, err)
		}

		// AddStream can return before leaf-node interest for the new subject has
		// reached the source cluster.
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("timeout waiting for LaunchLayer stream %s to store subject %s", streamName, subject)
		}
	}
}

func testLaunchLayerJetStreamAPI(t *testing.T, endpoint launchLayerEndpoint, streamOwner launchLayerEndpoint) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	ownerConn := connectLaunchLayer(t, streamOwner)
	defer ownerConn.Close()

	ownerJS, err := ownerConn.JetStream(nats.Context(ctx))
	if err != nil {
		t.Fatalf("failed to create JetStream context for %s: %v", streamOwner.cluster.name, err)
	}

	conn := connectLaunchLayer(t, endpoint)
	defer conn.Close()

	js, err := conn.JetStream(nats.Context(ctx))
	if err != nil {
		t.Fatalf("failed to create JetStream context for %s: %v", endpoint.cluster.name, err)
	}

	token := strings.ReplaceAll(uuid.NewString(), "-", "")
	streamName := "LL_API_" + token[:12]
	subject := "launchlayer.jsapi." + token
	payload := []byte(fmt.Sprintf("jetstream-api-%s-%s", endpoint.cluster.name, token))

	if _, err := ownerJS.AddStream(&nats.StreamConfig{
		Name:     streamName,
		Subjects: []string{subject},
		Storage:  nats.MemoryStorage,
		Replicas: 1,
	}, nats.Context(ctx)); err != nil {
		t.Fatalf("failed to create LaunchLayer stream %s on %s: %v", streamName, streamOwner.cluster.name, err)
	}
	defer func() {
		if err := ownerJS.DeleteStream(streamName); err != nil && !errors.Is(err, nats.ErrStreamNotFound) {
			t.Logf("failed to delete LaunchLayer stream %s: %v", streamName, err)
		}
	}()

	ack, err := js.Publish(subject, payload, nats.Context(ctx))
	if err != nil {
		t.Fatalf("failed to publish LaunchLayer JetStream payload through %s: %v", endpoint.cluster.name, err)
	}
	if ack.Stream != streamName {
		t.Fatalf("JetStream publish ack stream %q, want %q", ack.Stream, streamName)
	}

	got := waitForStoredLaunchLayerMessage(t, ctx, ownerJS, streamName, subject)
	if string(got.Data) != string(payload) {
		t.Fatalf("stored payload %q, want %q", got.Data, payload)
	}

	t.Logf("JetStream API from LaunchLayer on %s stored in %s at sequence %d",
		endpoint.cluster.name, streamOwner.cluster.name, got.Sequence)
}

func waitForStoredLaunchLayerMessage(
	t *testing.T,
	ctx context.Context,
	js nats.JetStreamContext,
	streamName string,
	subject string,
) *nats.RawStreamMsg {
	t.Helper()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		msg, err := js.GetLastMsg(streamName, subject, nats.Context(ctx))
		if err == nil {
			return msg
		}
		if !errors.Is(err, nats.ErrMsgNotFound) {
			t.Fatalf("failed to read LaunchLayer stream %s: %v", streamName, err)
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("timeout waiting for LaunchLayer stream %s to store subject %s", streamName, subject)
		}
	}
}

func testNATSMessageFlow(t *testing.T, source launchLayerEndpoint, target launchLayerEndpoint) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	subConn := connectLaunchLayer(t, target)
	defer subConn.Close()

	pubConn := connectLaunchLayer(t, source)
	defer pubConn.Close()

	subject := fmt.Sprintf("launchlayer.leaf.%s", uuid.NewString())
	payload := []byte(fmt.Sprintf("%s-to-%s-%s", source.cluster.name, target.cluster.name, uuid.NewString()))
	received := make(chan []byte, 1)

	sub, err := subConn.Subscribe(subject, func(msg *nats.Msg) {
		received <- msg.Data
	})
	if err != nil {
		t.Fatalf("failed to subscribe on %s: %v", target.cluster.name, err)
	}
	defer sub.Unsubscribe()

	if err := subConn.FlushWithContext(ctx); err != nil {
		t.Fatalf("failed to flush subscription on %s: %v", target.cluster.name, err)
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := pubConn.Publish(subject, payload); err != nil {
			t.Fatalf("failed to publish from %s: %v", source.cluster.name, err)
		}
		if err := pubConn.FlushWithContext(ctx); err != nil {
			t.Fatalf("failed to flush publisher on %s: %v", source.cluster.name, err)
		}

		select {
		case got := <-received:
			if string(got) != string(payload) {
				t.Fatalf("received payload %q, want %q", got, payload)
			}
			t.Logf("NATS message routed on %s account: %s -> %s",
				launchLayerAccount, source.cluster.name, target.cluster.name)
			return
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatalf("timeout waiting for %s message from %s to %s on subject %s",
				launchLayerAccount, source.cluster.name, target.cluster.name, subject)
		}
	}
}

func connectLaunchLayer(t *testing.T, endpoint launchLayerEndpoint) *nats.Conn {
	t.Helper()

	nc, err := nats.Connect(
		endpoint.cluster.broker,
		nats.Name(fmt.Sprintf("launchlayer-%s-%s", endpoint.cluster.name, uuid.NewString())),
		nats.UserInfo("oauthtoken", launchLayerOAuthToken(t)),
		nats.Timeout(5*time.Second),
		nats.RetryOnFailedConnect(false),
	)
	if err != nil {
		t.Fatalf("failed to connect to %s as %s: %v", endpoint.cluster.name, endpoint.account, err)
	}

	return nc
}

func getNATSClusters() []natsCluster {
	if broker := os.Getenv("NATS_BROKER"); broker != "" {
		return []natsCluster{
			{name: "Single", broker: broker},
		}
	}

	if brokerList := os.Getenv("NATS_BROKERS"); brokerList != "" {
		var clusters []natsCluster
		for _, entry := range strings.Split(brokerList, ",") {
			parts := strings.SplitN(entry, "=", 2)
			if len(parts) == 2 {
				clusters = append(clusters, natsCluster{name: parts[0], broker: parts[1]})
			}
		}
		if len(clusters) > 0 {
			return clusters
		}
	}

	return []natsCluster{
		{name: "CSC", broker: "nats://localhost:4222"},
		{name: "CPC-1", broker: "nats://localhost:4222"},
		{name: "CPC-2", broker: "nats://localhost:4222"},
	}
}

func findNATSCluster(clusters []natsCluster, name string) *natsCluster {
	for _, cluster := range clusters {
		if cluster.name == name {
			return &cluster
		}
	}
	return nil
}

func launchLayerCSC(t *testing.T, cluster natsCluster) launchLayerEndpoint {
	t.Helper()

	return launchLayerCluster(t, cluster)
}

func launchLayerCPC(t *testing.T, clusters []natsCluster, cpcID string) launchLayerEndpoint {
	t.Helper()

	clusterName := fmt.Sprintf("CPC-%s", cpcID)
	cluster := findNATSCluster(clusters, clusterName)
	if cluster == nil {
		t.Fatalf("%s NATS cluster not found", clusterName)
	}

	return launchLayerCluster(t, *cluster)
}

func launchLayerCluster(t *testing.T, cluster natsCluster) launchLayerEndpoint {
	t.Helper()

	return launchLayerEndpoint{
		cluster: cluster,
		account: launchLayerAccount,
	}
}

func launchLayerOAuthToken(t *testing.T) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	token, err := auth.GetKeycloakTokenContext(
		ctx,
		config.GetKeycloakURL(),
		"launchlayer-client",
		"launchlayer-client-secret",
	)
	if err != nil {
		t.Fatalf("failed to get LaunchLayer OAuth2 token: %v", err)
	}
	if strings.TrimSpace(token) == "" {
		t.Fatal("LaunchLayer OAuth2 token is empty")
	}
	return token
}
