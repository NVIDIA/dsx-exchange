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
