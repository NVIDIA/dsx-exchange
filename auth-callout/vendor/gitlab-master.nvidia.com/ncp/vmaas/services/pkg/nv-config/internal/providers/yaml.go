// SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package providers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// YAMLFileProvider loads configuration from YAML files
type YAMLFileProvider struct {
	filePaths []string
	priority  int
	required  bool
	parser    *yaml.YAML

	// Error handling
	errorOnMissing bool
	filePerms      os.FileMode

	// Metadata
	lastModified map[string]time.Time
	fileExists   map[string]bool
}

// YAMLFileProviderOption defines functional options for YAMLFileProvider
type YAMLFileProviderOption func(*YAMLFileProvider)

// NewYAMLFileProvider creates a new YAML file provider
func NewYAMLFileProvider(filePaths []string, opts ...YAMLFileProviderOption) *YAMLFileProvider {
	p := &YAMLFileProvider{
		filePaths:      make([]string, len(filePaths)),
		priority:       40, // Medium-low priority by default
		required:       false,
		parser:         yaml.Parser(),
		errorOnMissing: false,
		filePerms:      0,
		lastModified:   make(map[string]time.Time),
		fileExists:     make(map[string]bool),
	}

	copy(p.filePaths, filePaths)

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// WithYAMLPriority sets the provider priority
func WithYAMLPriority(priority int) YAMLFileProviderOption {
	return func(p *YAMLFileProvider) {
		p.priority = priority
	}
}

// WithYAMLRequired marks files as required (error if missing)
func WithYAMLRequired(required bool) YAMLFileProviderOption {
	return func(p *YAMLFileProvider) {
		p.required = required
		p.errorOnMissing = required
	}
}

// WithYAMLFilePerms sets the expected file permissions for validation
func WithYAMLFilePerms(perms os.FileMode) YAMLFileProviderOption {
	return func(p *YAMLFileProvider) {
		p.filePerms = perms
	}
}

// Name returns the provider name
func (p *YAMLFileProvider) Name() string {
	if len(p.filePaths) == 1 {
		return fmt.Sprintf("yaml-file-%s", filepath.Base(p.filePaths[0]))
	}
	return fmt.Sprintf("yaml-files-%d", len(p.filePaths))
}

// Priority returns the provider priority
func (p *YAMLFileProvider) Priority() int {
	return p.priority
}

// Load loads configuration from YAML files
func (p *YAMLFileProvider) Load() (map[string]interface{}, error) {
	mergedData := make(map[string]interface{})

	for _, filePath := range p.filePaths {
		// Expand environment variables in file path
		expandedPath := os.ExpandEnv(filePath)

		// Check if file exists
		fileInfo, err := os.Stat(expandedPath)
		if err != nil {
			if os.IsNotExist(err) {
				p.fileExists[filePath] = false
				if p.errorOnMissing {
					return nil, fmt.Errorf("required YAML file not found: %s", filePath)
				}
				// Skip missing optional files
				continue
			}
			return nil, fmt.Errorf("failed to stat YAML file %s: %w", filePath, err)
		}

		p.fileExists[filePath] = true
		p.lastModified[filePath] = fileInfo.ModTime()

		// Validate file permissions if specified
		if p.filePerms != 0 && fileInfo.Mode().Perm() != p.filePerms {
			return nil, fmt.Errorf("YAML file %s has incorrect permissions: got %v, expected %v",
				filePath, fileInfo.Mode().Perm(), p.filePerms)
		}

		// Load file data
		fileData, err := p.loadSingleFile(expandedPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load YAML file %s: %w", filePath, err)
		}

		// Merge with existing data (later files override earlier ones)
		if err := p.mergeData(mergedData, fileData); err != nil {
			return nil, fmt.Errorf("failed to merge data from YAML file %s: %w", filePath, err)
		}
	}

	return mergedData, nil
}

// loadSingleFile loads and parses a single YAML file
func (p *YAMLFileProvider) loadSingleFile(filePath string) (map[string]interface{}, error) {
	// Use koanf's file provider for consistent behavior
	k := koanf.New(".")
	if err := k.Load(file.Provider(filePath), p.parser); err != nil {
		return nil, fmt.Errorf("failed to parse YAML file %s: %w", filePath, err)
	}

	return k.All(), nil
}

// mergeData merges source data into target data
func (p *YAMLFileProvider) mergeData(target, source map[string]interface{}) error {
	for key, value := range source {
		target[key] = value
	}
	return nil
}

// Close cleans up resources (no-op since file watching was removed)
func (p *YAMLFileProvider) Close() error {
	return nil
}

// GetFileInfo returns metadata about the loaded files
func (p *YAMLFileProvider) GetFileInfo() map[string]FileInfo {
	info := make(map[string]FileInfo)

	for _, filePath := range p.filePaths {
		info[filePath] = FileInfo{
			Path:         filePath,
			Exists:       p.fileExists[filePath],
			LastModified: p.lastModified[filePath],
			Watching:     false, // File watching removed
		}
	}

	return info
}

// FileInfo contains metadata about a configuration file
type FileInfo struct {
	Path         string
	Exists       bool
	LastModified time.Time
	Watching     bool
}

// Validate checks if all required files exist and are readable
func (p *YAMLFileProvider) Validate() error {
	var missingFiles []string
	var errors []string

	for _, filePath := range p.filePaths {
		if _, err := os.Stat(filePath); err != nil {
			if os.IsNotExist(err) {
				if p.required {
					missingFiles = append(missingFiles, filePath)
				}
			} else {
				errors = append(errors, fmt.Sprintf("%s: %v", filePath, err))
			}
		}
	}

	if len(missingFiles) > 0 {
		return fmt.Errorf("required YAML files not found: %s", strings.Join(missingFiles, ", "))
	}

	if len(errors) > 0 {
		return fmt.Errorf("YAML file errors: %s", strings.Join(errors, "; "))
	}

	return nil
}
