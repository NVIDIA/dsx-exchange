// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

type admissionLimiter struct {
	ch chan struct{}
}

func newAdmissionLimiter(limit int) *admissionLimiter {
	if limit <= 0 {
		// no limit, allow all tool-calls through
		return nil
	}
	return &admissionLimiter{ch: make(chan struct{}, limit)}
}

func (l *admissionLimiter) tryAcquire() bool {
	if l == nil {
		// no limit, allow all tool-calls through
		return true
	}
	select {
	case l.ch <- struct{}{}:
		// admit request into channel buffer
		return true
	default:
		// no available slots in channel, reject request immediately
		return false
	}
}

func (l *admissionLimiter) release() {
	if l == nil {
		// no limit, allow all tool-calls through
		return
	}
	select {
	case <-l.ch:
		// release channel slot
	default:
	}
}
