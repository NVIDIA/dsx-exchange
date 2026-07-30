// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Valkey client wrapper. Tests need to read/write the rate-limit
// counter store directly; the primary Service is ClusterIP-only,
// so we set up a client-go port-forward to the primary Pod and
// return a go-redis client connected through it. valkey is
// Redis-protocol compatible.
package runner

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/redis/go-redis/v9"
)

var gatewayValkeyStatefulSets = []struct {
	ns   string
	name string
}{
	{ns: "csc-dsx-agentgateway", name: "csc-dsx-agentgateway-valkey"},
	{ns: "cpc-1-dsx-agentgateway", name: "cpc-1-dsx-agentgateway-valkey"},
	{ns: "cpc-2-dsx-agentgateway", name: "cpc-2-dsx-agentgateway-valkey"},
}

// NewValkeyClient port-forwards to the chart's primary Valkey Pod
// and returns a connected go-redis client. Caller schedules
// t.Cleanup; this helper registers its own cleanup for the
// port-forward and the client.
//
// The chart's primary Service points at pod-0 of the StatefulSet
// `csc-dsx-agentgateway-valkey`, which is also where envoyproxy/
// ratelimit writes counters.
func NewValkeyClient(t *testing.T) *redis.Client {
	t.Helper()
	return NewValkeyClientForStatefulSet(t, "csc-dsx-agentgateway", "csc-dsx-agentgateway-valkey")
}

func NewValkeyClientForStatefulSet(t *testing.T, ns, name string) *redis.Client {
	t.Helper()
	client, cleanup := newValkeyClientForStatefulSet(t, ns, name)
	t.Cleanup(cleanup)
	return client
}

func newValkeyClientForStatefulSet(t *testing.T, ns, name string) (*redis.Client, func()) {
	t.Helper()
	const podPort = 6379
	pod := name + "-0"

	WaitForStatefulSetReady(t, ns, name, 45*time.Second)
	localPort, stopPortForward := startPodPortForward(t, ns, pod, podPort)

	client := redis.NewClient(&redis.Options{
		Addr:        fmt.Sprintf("127.0.0.1:%d", localPort),
		DialTimeout: 5 * time.Second,
		ReadTimeout: 5 * time.Second,
	})
	cleanup := func() {
		_ = client.Close()
		stopPortForward()
	}

	// Confirm the connection actually reaches Valkey.
	if err := client.Ping(context.Background()).Err(); err != nil {
		cleanup()
		t.Fatalf("valkey PING through port-forward: %v", err)
	}
	return client, cleanup
}

func startPodPortForward(t *testing.T, ns, pod string, podPort int) (int, func()) {
	t.Helper()
	k8sInit(t)

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	errCh := make(chan error, 1)

	pfURL := k8sClient.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(ns).
		Name(pod).
		SubResource("portforward").
		URL()
	transport, upgrader, err := spdy.RoundTripperFor(k8sCfg)
	if err != nil {
		t.Fatalf("spdy round tripper: %v", err)
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", pfURL)
	fw, err := portforward.New(dialer,
		[]string{fmt.Sprintf(":%d", podPort)},
		stopCh, readyCh, nil, nil)
	if err != nil {
		t.Fatalf("portforward.New: %v", err)
	}

	var pfOnce sync.Once
	pfStop := func() {
		pfOnce.Do(func() { close(stopCh) })
	}

	go func() {
		if err := fw.ForwardPorts(); err != nil {
			errCh <- err
		}
	}()

	readyCtx, readyCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer readyCancel()
	select {
	case <-readyCh:
	case err := <-errCh:
		pfStop()
		t.Fatalf("port-forward to %s/%s: %v", ns, pod, err)
	case <-readyCtx.Done():
		pfStop()
		t.Fatalf("port-forward to %s/%s never became ready: %v", ns, pod, readyCtx.Err())
	}

	ports, err := fw.GetPorts()
	if err != nil {
		pfStop()
		t.Fatalf("port-forward to %s/%s did not report bound ports: %v", ns, pod, err)
	}
	if len(ports) != 1 {
		pfStop()
		t.Fatalf("port-forward to %s/%s reported %d bound ports, want 1", ns, pod, len(ports))
	}
	localPort := int(ports[0].Local)
	urlForward, _ := url.Parse(pfURL.String())
	t.Logf("port-forwarded %s/%s:%d → 127.0.0.1:%d", urlForward.Host, pod, podPort, localPort)
	return localPort, pfStop
}

// FlushAndWaitForRateLimitWindow flushes every installed gateway
// Valkey primary so per-tenant counters are clean, then waits one
// full per-second window on each store so any in-flight bucket rolls
// forward. Returns silently if Valkey isn't installed.
func FlushAndWaitForRateLimitWindow(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for _, sts := range gatewayValkeyStatefulSets {
		if !StatefulSetExists(t, sts.ns, sts.name) {
			continue
		}
		func() {
			c, cleanup := newValkeyClientForStatefulSet(t, sts.ns, sts.name)
			defer cleanup()
			if err := c.FlushAll(ctx).Err(); err != nil {
				t.Fatalf("valkey %s/%s FLUSHALL: %v", sts.ns, sts.name, err)
			}
			flushAt, err := c.Time(ctx).Result()
			if err != nil {
				t.Fatalf("valkey %s/%s TIME after FLUSHALL: %v", sts.ns, sts.name, err)
			}
			if !WaitFor(1500*time.Millisecond, 50*time.Millisecond, func() bool {
				now, err := c.Time(ctx).Result()
				if err != nil {
					return false
				}
				return now.Unix() > flushAt.Unix()
			}) {
				t.Fatalf("valkey server clock for %s/%s did not advance to the next rate-limit window after FLUSHALL", sts.ns, sts.name)
			}
		}()
	}
}
