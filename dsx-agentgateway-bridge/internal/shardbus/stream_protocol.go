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

package shardbus

// Stream protocol contract: an initial SSE response carries its HTTP metadata
// and stream ID in headers, with the per-leaf stream subject as its NATS
// reply. Each READ is ordinary request/reply to that subject. Repeating cursor n
// replays its cached frame, while cursor n+1 acknowledges and releases it.
// Pending does not advance the cursor. EOF remains replayable until cleanup.
// CLOSE is idempotent and either releases a completed stream or cancels an
// active upstream request.
const (
	StreamIDHeader        = "DAG-Bridge-Stream-ID"
	StreamCursorHeader    = "DAG-Bridge-Stream-Cursor"
	StreamFrameHeader     = "DAG-Bridge-Stream-Frame"
	StreamOperationHeader = "DAG-Bridge-Stream-Operation"
	StreamOperationRead   = "read"
	StreamOperationClose  = "close"
	StreamFramePending    = "pending"
	StreamFrameEnd        = "end"

	HTTPStatusHeader      = "DAG-Bridge-HTTP-Status"
	HTTPContentTypeHeader = "DAG-Bridge-HTTP-Content-Type"
)
