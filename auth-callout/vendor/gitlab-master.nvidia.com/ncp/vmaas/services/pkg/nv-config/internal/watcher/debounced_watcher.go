// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package watcher

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"
)

// DebouncedWatcher wraps a FileWatcher and debounces callback invocations per file.
type DebouncedWatcher struct {
	inner          FileWatcher
	debounce       time.Duration
	debounceTimers map[string]*time.Timer
	mutex          sync.Mutex
}

// NewDebouncedWatcher returns a watcher that debounces change events for the inner watcher.
func NewDebouncedWatcher(inner FileWatcher, debounce time.Duration) *DebouncedWatcher {
	return &DebouncedWatcher{
		inner:          inner,
		debounce:       debounce,
		debounceTimers: make(map[string]*time.Timer),
	}
}

// WatchFile registers the path with the inner watcher; the callback is invoked after no events for debounce.
func (d *DebouncedWatcher) WatchFile(filePath string, callback func(string) error) error {
	cleanPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for %s: %w", filePath, err)
	}

	wrapped := func(path string) error {
		d.mutex.Lock()
		if timer, exists := d.debounceTimers[path]; exists {
			timer.Stop()
		}
		d.debounceTimers[path] = time.AfterFunc(d.debounce, func() {
			if err := callback(path); err != nil {
				fmt.Printf("Debounced callback error for %s: %v\n", path, err)
			}
			d.mutex.Lock()
			delete(d.debounceTimers, path)
			d.mutex.Unlock()
		})
		d.mutex.Unlock()
		return nil
	}

	return d.inner.WatchFile(cleanPath, wrapped)
}

// Stop stops all debounce timers and the inner watcher.
func (d *DebouncedWatcher) Stop() error {
	d.mutex.Lock()
	for _, timer := range d.debounceTimers {
		timer.Stop()
	}
	d.debounceTimers = make(map[string]*time.Timer)
	d.mutex.Unlock()
	return d.inner.Stop()
}
