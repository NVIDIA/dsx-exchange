// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package watcher

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
)

// CompositeWatcher fans out WatchFile/Stop to multiple FileWatchers.
type CompositeWatcher struct {
	watchers []FileWatcher
	mu       sync.Mutex
}

// NewCompositeWatcher returns a watcher that delegates to each of the given watchers.
func NewCompositeWatcher(watchers ...FileWatcher) *CompositeWatcher {
	return &CompositeWatcher{
		watchers: watchers,
	}
}

// WatchFile registers the path and callback with every sub-watcher.
func (c *CompositeWatcher) WatchFile(filePath string, callback func(string) error) error {
	cleanPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for %s: %w", filePath, err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, w := range c.watchers {
		if err := w.WatchFile(cleanPath, callback); err != nil {
			return fmt.Errorf("composite watch failed: %w", err)
		}
	}
	return nil
}

// Stop stops all sub-watchers and collects errors.
func (c *CompositeWatcher) Stop() error {
	c.mu.Lock()
	watchers := c.watchers
	c.watchers = nil
	c.mu.Unlock()

	var errs []error
	for _, w := range watchers {
		if err := w.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
