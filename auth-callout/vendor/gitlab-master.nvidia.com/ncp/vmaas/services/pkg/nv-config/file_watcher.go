// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"time"

	"gitlab-master.nvidia.com/ncp/vmaas/services/pkg/nv-config/internal/watcher"
)

const (
	defaultPollingInterval = 1 * time.Minute
	defaultDebounce        = 300 * time.Millisecond
)

type fileWatcherOptions struct {
	pollingInterval time.Duration
	debounce        time.Duration
}

// FileWatcherOption configures NewFileWatcher.
type FileWatcherOption func(*fileWatcherOptions)

// WithPollingInterval sets the polling watcher interval. Zero disables polling.
func WithPollingInterval(d time.Duration) FileWatcherOption {
	return func(o *fileWatcherOptions) { o.pollingInterval = d }
}

// WithDebounce sets the debounce duration for change events. Zero disables debouncing.
func WithDebounce(d time.Duration) FileWatcherOption {
	return func(o *fileWatcherOptions) { o.debounce = d }
}

// NewFileWatcher creates a FileWatcher that combines fsnotify and polling with debouncing.
// Options default to 1m polling and 300ms debounce. Zero disables that layer.
func NewFileWatcher(opts ...FileWatcherOption) (FileWatcher, error) {
	o := &fileWatcherOptions{
		pollingInterval: defaultPollingInterval,
		debounce:        defaultDebounce,
	}
	for _, fn := range opts {
		fn(o)
	}

	fsn, err := watcher.NewFSNotifyWatcher()
	if err != nil {
		return nil, err
	}

	var composite watcher.FileWatcher = fsn
	if o.pollingInterval > 0 {
		polling := watcher.NewPollingWatcher(o.pollingInterval)
		composite = watcher.NewCompositeWatcher(composite, polling)
	}
	if o.debounce > 0 {
		composite = watcher.NewDebouncedWatcher(composite, o.debounce)
	}
	return composite, nil
}
