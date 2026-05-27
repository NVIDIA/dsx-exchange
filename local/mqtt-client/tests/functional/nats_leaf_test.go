// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package functional

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

const launchLayerAccount = "LaunchLayer"

type natsCluster struct {
	name   string
	broker string
}

type launchLayerEndpoint struct {
	cluster natsCluster
	seed    string
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
	clusters := getNATSClusters()
	csc := findNATSCluster(clusters, "CSC")
	if csc == nil {
		t.Fatal("CSC NATS cluster not found")
	}

	for _, cpcID := range []string{"1", "2"} {
		cpcID := cpcID
		t.Run("CPC-"+cpcID, func(t *testing.T) {
			testLaunchLayerJetStream(t, launchLayerCPC(t, clusters, cpcID), launchLayerCSC(t, *csc))
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

	sourceJS, err := sourceConn.JetStream(nats.Context(ctx))
	if err != nil {
		t.Fatalf("failed to create JetStream context for %s: %v", source.cluster.name, err)
	}

	ownerJS, err := ownerConn.JetStream(nats.Context(ctx))
	if err != nil {
		t.Fatalf("failed to create JetStream context for %s: %v", streamOwner.cluster.name, err)
	}

	token := strings.ReplaceAll(uuid.NewString(), "-", "")
	streamName := "LL_" + token[:12]
	subject := "launchlayer.js." + token
	payload := []byte(fmt.Sprintf("jetstream-%s-to-%s-%s", source.cluster.name, streamOwner.cluster.name, token))

	if _, err := sourceJS.AddStream(&nats.StreamConfig{
		Name:     streamName,
		Subjects: []string{subject},
		Storage:  nats.MemoryStorage,
		Replicas: 1,
	}, nats.Context(ctx)); err != nil {
		t.Fatalf("failed to create LaunchLayer stream %s from %s: %v", streamName, source.cluster.name, err)
	}
	defer func() {
		if err := sourceJS.DeleteStream(streamName); err != nil && !errors.Is(err, nats.ErrStreamNotFound) {
			t.Logf("failed to delete LaunchLayer stream %s: %v", streamName, err)
		}
	}()

	ack, err := sourceJS.Publish(subject, payload, nats.Context(ctx))
	if err != nil {
		t.Fatalf("failed to publish LaunchLayer JetStream payload from %s: %v", source.cluster.name, err)
	}
	if ack.Stream != streamName {
		t.Fatalf("JetStream publish ack stream %q, want %q", ack.Stream, streamName)
	}

	got := waitForStoredLaunchLayerMessage(t, ctx, ownerJS, streamName, subject)
	if string(got.Data) != string(payload) {
		t.Fatalf("stored payload %q, want %q", got.Data, payload)
	}

	t.Logf("JetStream stored LaunchLayer message in %s from %s at sequence %d",
		streamName, source.cluster.name, got.Sequence)
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

	if err := subConn.Flush(); err != nil {
		t.Fatalf("failed to flush subscription on %s: %v", target.cluster.name, err)
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := pubConn.Publish(subject, payload); err != nil {
			t.Fatalf("failed to publish from %s: %v", source.cluster.name, err)
		}
		if err := pubConn.Flush(); err != nil {
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

	kp, err := nkeys.FromSeed([]byte(endpoint.seed))
	if err != nil {
		t.Fatalf("failed to parse %s seed for %s: %v", endpoint.account, endpoint.cluster.name, err)
	}
	t.Cleanup(func() {
		kp.Wipe()
	})

	publicKey, err := kp.PublicKey()
	if err != nil {
		t.Fatalf("failed to get %s public key for %s: %v", endpoint.account, endpoint.cluster.name, err)
	}

	nc, err := nats.Connect(
		endpoint.cluster.broker,
		nats.Name(fmt.Sprintf("launchlayer-%s-%s", endpoint.cluster.name, uuid.NewString())),
		nats.Nkey(publicKey, func(nonce []byte) ([]byte, error) {
			return kp.Sign(nonce)
		}),
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
		{name: "CSC", broker: "nats://172.18.200.1:4222"},
		{name: "CPC-1", broker: "nats://172.18.201.1:4222"},
		{name: "CPC-2", broker: "nats://172.18.202.1:4222"},
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

	return launchLayerEndpoint{
		cluster: cluster,
		seed:    readLaunchLayerSeed(t, "csc", "nats-launchlayer-client"),
		account: launchLayerAccount,
	}
}

func launchLayerCPC(t *testing.T, clusters []natsCluster, cpcID string) launchLayerEndpoint {
	t.Helper()

	clusterName := fmt.Sprintf("CPC-%s", cpcID)
	cluster := findNATSCluster(clusters, clusterName)
	if cluster == nil {
		t.Fatalf("%s NATS cluster not found", clusterName)
	}

	return launchLayerEndpoint{
		cluster: *cluster,
		seed:    readLaunchLayerSeed(t, "cpc-"+cpcID, "nats-launchlayer-client"),
		account: launchLayerAccount,
	}
}

func readLaunchLayerSeed(t *testing.T, cluster string, secret string) string {
	t.Helper()

	path := filepath.Join(launchLayerNKeyRoot(), cluster, "nkeys", secret, "seed")
	seed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read LaunchLayer seed %s: %v", path, err)
	}

	trimmed := strings.TrimSpace(string(seed))
	if trimmed == "" {
		t.Fatalf("LaunchLayer seed %s is empty", path)
	}
	return trimmed
}

func launchLayerNKeyRoot() string {
	if root := os.Getenv("LAUNCHLAYER_NKEY_ROOT"); root != "" {
		return root
	}
	return "../../../nats/secrets"
}
