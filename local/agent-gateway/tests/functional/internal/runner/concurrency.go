// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package runner

import (
	"os"
	"testing"
)

// ParallelReadOnly marks a test as parallel and lets it run beside
// other tests that only read the live dataplane/cluster state.
func ParallelReadOnly(t *testing.T) {
	t.Helper()
	t.Parallel()
}

// DestructiveFunctional marks a cluster-mutating functional test.
// The e2e runner runs these tests in a separate serial phase for full gates.
func DestructiveFunctional(t *testing.T) {
	t.Helper()
	if os.Getenv("RUN_DESTRUCTIVE_FUNCTIONAL") != "1" {
		t.Skip("set RUN_DESTRUCTIVE_FUNCTIONAL=1 to run destructive functional tests")
	}
}

// CleanupRateLimitState clears live limiter buckets after tests that
// intentionally mutate them.
func CleanupRateLimitState(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		FlushAndWaitForRateLimitWindow(t)
	})
}
