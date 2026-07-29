// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import "testing"

func TestDecodeRequestAcceptsJSONRequest(t *testing.T) {
	t.Parallel()

	req, err := DecodeRequest([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}
	if req.JSONRPC != "2.0" || req.Method != "tools/list" || req.ID.Value() != int64(1) {
		t.Fatalf("decoded request = %+v", req)
	}
}

func TestDecodeRequestRejectsInvalidEnvelope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
	}{
		{name: "malformed json", body: `not-json`},
		{name: "missing jsonrpc", body: `{"id":1,"method":"tools/list"}`},
		{name: "wrong jsonrpc", body: `{"jsonrpc":"1.0","id":1,"method":"tools/list"}`},
		{name: "missing method", body: `{"jsonrpc":"2.0","id":1}`},
		{name: "empty method", body: `{"jsonrpc":"2.0","id":1,"method":""}`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := DecodeRequest([]byte(tc.body)); err == nil {
				t.Fatalf("DecodeRequest accepted %s: %s", tc.name, tc.body)
			}
		})
	}
}
