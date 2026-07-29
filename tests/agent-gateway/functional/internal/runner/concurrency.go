// Copyright 2026 NVIDIA Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
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
// tests/agent-gateway/run.sh runs these tests in a separate serial phase for full gates.
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
