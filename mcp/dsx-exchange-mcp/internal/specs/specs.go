// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package specs exposes the embedded DSX Exchange AsyncAPI documents.
package specs

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/NVIDIA/dsx-exchange/mcp/dsx-exchange-mcp/schemas"
)

var (
	once     sync.Once
	domains  []string
	contents map[string][]byte
)

func load() {
	contents = map[string][]byte{}
	_ = fs.WalkDir(schemas.FS, "asyncapi", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(path.Ext(p))
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil
		}
		body, rerr := schemas.FS.ReadFile(p)
		if rerr != nil || len(body) == 0 {
			return nil
		}
		if !bytes.Contains(body, []byte("asyncapi:")) {
			return nil
		}
		domain := path.Base(path.Dir(p))
		if domain == "" || domain == "asyncapi" {
			return nil
		}
		contents[domain] = body
		return nil
	})
	domains = make([]string, 0, len(contents))
	for k := range contents {
		domains = append(domains, k)
	}
	sort.Strings(domains)
}

// List returns the domain names with non-empty AsyncAPI specs.
func List() []string {
	once.Do(load)
	out := make([]string, len(domains))
	copy(out, domains)
	return out
}

// Read returns the raw spec bytes for a domain.
func Read(domain string) ([]byte, error) {
	once.Do(load)
	body, ok := contents[domain]
	if !ok {
		return nil, fmt.Errorf("unknown domain %q", domain)
	}
	return body, nil
}
