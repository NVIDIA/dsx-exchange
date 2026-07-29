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

package functional

import (
	"bytes"
	"net/http"
	"testing"
)

func readAll(t *testing.T, r *http.Response) (*http.Response, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r.Body); err != nil {
		t.Fatalf("readAll: %v", err)
	}
	return r, buf.Bytes()
}

// lastSSEData picks the last `data: ` line from an SSE-framed body
// and returns the raw payload. Returns the input unchanged when the
// body is plain JSON.
func lastSSEData(body []byte) []byte {
	if !bytes.Contains(body, []byte("\ndata: ")) && !bytes.HasPrefix(body, []byte("data: ")) {
		return body
	}
	out := body
	for _, line := range bytes.Split(body, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("data: ")) {
			out = bytes.TrimPrefix(line, []byte("data: "))
		}
	}
	return out
}
