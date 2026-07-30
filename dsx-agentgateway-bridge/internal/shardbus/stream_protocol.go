// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
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
