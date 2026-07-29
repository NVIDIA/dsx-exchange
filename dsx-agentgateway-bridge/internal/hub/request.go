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
