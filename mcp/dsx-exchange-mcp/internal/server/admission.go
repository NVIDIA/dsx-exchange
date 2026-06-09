// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package server

type admissionLimiter struct {
	ch chan struct{}
}

func newAdmissionLimiter(limit int) *admissionLimiter {
	if limit <= 0 {
		return nil
	}
	return &admissionLimiter{ch: make(chan struct{}, limit)}
}

func (l *admissionLimiter) tryAcquire() bool {
	if l == nil {
		return true
	}
	select {
	case l.ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func (l *admissionLimiter) release() {
	if l == nil {
		return
	}
	select {
	case <-l.ch:
	default:
	}
}
