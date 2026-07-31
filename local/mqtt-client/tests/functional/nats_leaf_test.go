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
	endpoints := launchLayerTestEndpoints(t)
	for i, creator := range endpoints {
		t.Run(creator.cluster.name, func(t *testing.T) {
			testLaunchLayerJetStreamAPI(
				t,
				creator,
				endpoints[(i+1)%len(endpoints)],
				endpoints[(i+2)%len(endpoints)],
				endpoints,
			)
		})
	}
}

func TestLaunchLayerJetStreamKVProxiesToCSC(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	endpoints := launchLayerTestEndpoints(t)
	cpc1Conn, cpc1JS := connectLaunchLayerJetStream(t, ctx, endpoints[1])
	defer cpc1Conn.Close()
	cpc2Conn, cpc2JS := connectLaunchLayerJetStream(t, ctx, endpoints[2])
	defer cpc2Conn.Close()
	cscConn, cscJS := connectLaunchLayerJetStream(t, ctx, endpoints[0])
	defer cscConn.Close()

	// Create through CPC-1.
	bucketName := testResourceName("LL_KV")
	if _, err := cpc1JS.CreateKeyValue(&nats.KeyValueConfig{
		Bucket: bucketName, Storage: nats.MemoryStorage, Replicas: 1,
	}); err != nil {
		t.Fatalf("create KV bucket through CPC-1: %v", err)
	}
	defer cpc1JS.DeleteKeyValue(bucketName)

	cpc2KV, err := cpc2JS.KeyValue(bucketName)
	if err != nil {
		t.Fatalf("bind KV bucket through CPC-2: %v", err)
	}
	cscKV, err := cscJS.KeyValue(bucketName)
	if err != nil {
		t.Fatalf("bind KV bucket through CSC: %v", err)
	}

	// Write through CPC-2 and read through CSC.
	key := "cross-cluster"
	first := []byte("from-cpc-2")
	revision, err := cpc2KV.Put(key, first)
	if err != nil {
		t.Fatalf("put KV value through CPC-2: %v", err)
	}
	requireKVValue(t, cscKV, key, first)

	// Update through CSC and read through CPC-2.
	second := []byte("from-csc")
	if _, err := cscKV.Update(key, second, revision); err != nil {
		t.Fatalf("update KV value through CSC: %v", err)
	}
	requireKVValue(t, cpc2KV, key, second)
	if err := cpc2KV.Delete(key); err != nil {
		t.Fatalf("delete KV value through CPC-2: %v", err)
	}
}

func TestLaunchLayerJetStreamObjectStoreProxiesToCSC(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	endpoints := launchLayerTestEndpoints(t)
	cpc1Conn, cpc1JS := connectLaunchLayerJetStream(t, ctx, endpoints[1])
	defer cpc1Conn.Close()
	cpc2Conn, cpc2JS := connectLaunchLayerJetStream(t, ctx, endpoints[2])
	defer cpc2Conn.Close()
	cscConn, cscJS := connectLaunchLayerJetStream(t, ctx, endpoints[0])
	defer cscConn.Close()

	// Create through CPC-1.
	bucketName := testResourceName("LL_OBJ")
	if _, err := cpc1JS.CreateObjectStore(&nats.ObjectStoreConfig{
		Bucket: bucketName, Storage: nats.MemoryStorage, Replicas: 1,
	}); err != nil {
		t.Fatalf("create Object Store through CPC-1: %v", err)
	}
	defer cpc1JS.DeleteObjectStore(bucketName)

	cpc2Store, err := cpc2JS.ObjectStore(bucketName)
	if err != nil {
		t.Fatalf("bind Object Store through CPC-2: %v", err)
	}
	cscStore, err := cscJS.ObjectStore(bucketName)
	if err != nil {
		t.Fatalf("bind Object Store through CSC: %v", err)
	}

	// Write through CPC-2 and read through CSC.
	objectName, payload := "cross-cluster", []byte("object-from-cpc-2")
	if _, err := cpc2Store.PutBytes(objectName, payload); err != nil {
		t.Fatalf("put object through CPC-2: %v", err)
	}
	got, err := cscStore.GetBytes(objectName)
	if err != nil {
		t.Fatalf("get object through CSC: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("object payload %q, want %q", got, payload)
	}
	if err := cpc2Store.Delete(objectName); err != nil {
		t.Fatalf("delete object through CPC-2: %v", err)
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

func testLaunchLayerJetStreamAPI(
	t *testing.T,
	creator launchLayerEndpoint,
	updater launchLayerEndpoint,
	deleter launchLayerEndpoint,
	endpoints []launchLayerEndpoint,
) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	creatorConn, creatorJS := connectLaunchLayerJetStream(t, ctx, creator)
	defer creatorConn.Close()

	// Create through CSC, CPC-1, and CPC-2.
	token := strings.ReplaceAll(uuid.NewString(), "-", "")
	streamName := "LL_API_" + token[:12]
	subjectPrefix := "launchlayer.jsapi." + token
	streamConfig := &nats.StreamConfig{
		Name:     streamName,
		Subjects: []string{subjectPrefix + ".>"},
		Storage:  nats.MemoryStorage,
		Replicas: 1,
	}
	if _, err := creatorJS.AddStream(streamConfig, nats.Context(ctx)); err != nil {
		t.Fatalf("create stream through %s: %v", creator.cluster.name, err)
	}

	// Update through the next cluster.
	updaterConn, updaterJS := connectLaunchLayerJetStream(t, ctx, updater)
	defer updaterConn.Close()
	streamConfig.Description = "updated through " + updater.cluster.name
	if _, err := updaterJS.UpdateStream(streamConfig, nats.Context(ctx)); err != nil {
		t.Fatalf("update stream through %s: %v", updater.cluster.name, err)
	}

	// Inspect through every cluster.
	for _, endpoint := range endpoints {
		conn, js := connectLaunchLayerJetStream(t, ctx, endpoint)
		info, err := js.StreamInfo(streamName, nats.Context(ctx))
		conn.Close()
		if err != nil {
			t.Fatalf("inspect stream through %s: %v", endpoint.cluster.name, err)
		}
		if info.Config.Description != streamConfig.Description {
			t.Fatalf("stream description through %s is %q", endpoint.cluster.name, info.Config.Description)
		}
	}

	// Publish, fetch, and ACK through every cluster.
	for _, endpoint := range endpoints {
		conn, js := connectLaunchLayerJetStream(t, ctx, endpoint)
		subject := subjectPrefix + "." + strings.ToLower(endpoint.cluster.name)
		consumer, err := js.PullSubscribe(
			subject,
			"C_"+strings.ReplaceAll(endpoint.cluster.name, "-", "_"),
			nats.BindStream(streamName),
		)
		if err != nil {
			conn.Close()
			t.Fatalf("create pull consumer through %s: %v", endpoint.cluster.name, err)
		}
		if _, err := js.Publish(subject, []byte("from-"+endpoint.cluster.name), nats.Context(ctx)); err != nil {
			conn.Close()
			t.Fatalf("publish through %s: %v", endpoint.cluster.name, err)
		}
		messages, err := consumer.Fetch(1, nats.Context(ctx))
		if err != nil {
			conn.Close()
			t.Fatalf("fetch through %s: %v", endpoint.cluster.name, err)
		}
		if !strings.HasPrefix(messages[0].Reply, "$JS.ACK."+streamName+".") {
			conn.Close()
			t.Fatalf("ACK subject %q does not identify stream %s", messages[0].Reply, streamName)
		}
		// NATS 2.12 ACK subjects do not include a domain.
		if err := messages[0].AckSync(nats.Context(ctx)); err != nil {
			conn.Close()
			t.Fatalf("ACK through %s: %v", endpoint.cluster.name, err)
		}
		conn.Close()
	}

	// Delete through the remaining cluster.
	deleterConn, deleterJS := connectLaunchLayerJetStream(t, ctx, deleter)
	defer deleterConn.Close()
	if err := deleterJS.DeleteStream(streamName, nats.Context(ctx)); err != nil {
		t.Fatalf("delete stream through %s: %v", deleter.cluster.name, err)
	}
}

func connectLaunchLayerJetStream(
	t *testing.T,
	ctx context.Context,
	endpoint launchLayerEndpoint,
) (*nats.Conn, nats.JetStreamContext) {
	t.Helper()

	conn := connectLaunchLayer(t, endpoint)
	js, err := conn.JetStream(nats.Context(ctx))
	if err != nil {
		conn.Close()
		t.Fatalf("create JetStream context for %s: %v", endpoint.cluster.name, err)
	}
	return conn, js
}

func launchLayerTestEndpoints(t *testing.T) []launchLayerEndpoint {
	t.Helper()

	clusters := getNATSClusters()
	csc := findNATSCluster(clusters, "CSC")
	if csc == nil {
		t.Fatal("CSC NATS cluster not found")
	}
	return []launchLayerEndpoint{
		launchLayerCSC(t, *csc),
		launchLayerCPC(t, clusters, "1"),
		launchLayerCPC(t, clusters, "2"),
	}
}

func testResourceName(prefix string) string {
	token := strings.ReplaceAll(uuid.NewString(), "-", "")
	return prefix + "_" + token[:12]
}

func requireKVValue(t *testing.T, bucket nats.KeyValue, key string, want []byte) {
	t.Helper()

	entry, err := bucket.Get(key)
	if err != nil {
		t.Fatalf("get KV key %q: %v", key, err)
	}
	if string(entry.Value()) != string(want) {
		t.Fatalf("KV value %q, want %q", entry.Value(), want)
	}
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

	token, err := auth.GetOIDCTokenContext(
		ctx,
		config.GetIDPURL(),
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
