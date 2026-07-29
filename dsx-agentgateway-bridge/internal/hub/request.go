// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package hub

import (
	"context"
	"errors"
	"net/http"

	"github.com/nats-io/nats.go"
)

func (h hub) requestLeafOnce(r *http.Request, subject string, body []byte) (*nats.Msg, error) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	return h.bus.RequestMsgWithContext(ctx, newLeafRequest(ctx, r.Header, subject, body))
}

// isNoReplyError classifies the absence of a reply, not transport health.
// ErrNoResponders is NATS' fast path, but wildcard subscribers can suppress
// it and leave timeout as the only observable no-answer result.
func isNoReplyError(err error) bool {
	return errors.Is(err, nats.ErrNoResponders) || errors.Is(err, nats.ErrTimeout) || errors.Is(err, context.DeadlineExceeded)
}
