// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package watcher

// FileWatcher watches files and invokes a callback when they change.
type FileWatcher interface {
	WatchFile(filePath string, callback func(string) error) error
	Stop() error
}
