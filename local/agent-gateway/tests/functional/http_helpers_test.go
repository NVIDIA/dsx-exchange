// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package functional

import (
	"bytes"
	"net/http"
	"testing"
)

func readAll(t *testing.T, r *http.Response) (*http.Response, []byte) {
	t.Helper()
	defer r.Body.Close()
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
