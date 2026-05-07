// Copyright 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/fsnotify/fsnotify"
	"github.com/nats-io/jwt/v2"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

// UserProfile contains the account and permissions for a user
type UserProfile struct {
	Name        string          `json:"-"` // Friendly name from config (not serialized)
	Account     string          `json:"account"`
	Permissions jwt.Permissions `json:"permissions,omitempty"`
}

// OAuth2Entry represents an OAuth2 user configuration
type OAuth2Entry struct {
	Subject       string          `json:"subject,omitempty"`
	Azp           string          `json:"azp,omitempty"` // Authorized party (client_id), alternative to subject
	Account       string          `json:"account"`
	Permissions   jwt.Permissions `json:"permissions,omitempty"`
	RequiredScope string          `json:"required_scope,omitempty"` // Required scope in JWT (defaults to "mqtt" if empty)
}

// MTLSEntry represents an mTLS user configuration
type MTLSEntry struct {
	Identity    string          `json:"identity"`
	Account     string          `json:"account"`
	Permissions jwt.Permissions `json:"permissions,omitempty"`
}

// NKeyEntry represents an NKey user configuration
type NKeyEntry struct {
	PublicKey   string          `json:"public_key"`
	Account     string          `json:"account"`
	Permissions jwt.Permissions `json:"permissions,omitempty"`
}

// NoAuthEntry represents a no-authentication user configuration
type NoAuthEntry struct {
	Account     string          `json:"account"`
	Permissions jwt.Permissions `json:"permissions,omitempty"`
}

// PermissionsConfig contains all authentication method mappings
type PermissionsConfig struct {
	OAuth2 map[string]*OAuth2Entry `json:"oauth2,omitempty"` // name -> entry
	MTLS   map[string]*MTLSEntry   `json:"mtls,omitempty"`   // name -> entry
	NKey   map[string]*NKeyEntry   `json:"nkey,omitempty"`   // name -> entry
	NoAuth *NoAuthEntry            `json:"noauth,omitempty"` // optional no-auth fallback
}

// PermissionsManager handles loading and hot-reloading of permissions
type PermissionsManager struct {
	oauth2Lookup  atomic.Pointer[map[oauth2Key]oauth2UserProfile] // key -> profile with scope
	mtlsLookup    atomic.Pointer[map[string]UserProfile]          // identity -> profile
	nkeyLookup    atomic.Pointer[map[string]UserProfile]          // public_key -> profile
	noauthProfile atomic.Pointer[UserProfile]                     // optional no-auth profile
	fileWatcher   *fsnotify.Watcher
	logger        *otelzap.Logger
}

// oauth2Key is the lookup key for OAuth2 profiles
type oauth2Key struct {
	Subject string
	Azp     string
}

// oauth2UserProfile embeds UserProfile with OAuth2-specific required scope
type oauth2UserProfile struct {
	UserProfile
	requiredScope string
}

// NewPermissionsManager creates a new permissions manager
func NewPermissionsManager(filePath string, logger *otelzap.Logger) (*PermissionsManager, error) {
	pm := &PermissionsManager{
		logger: logger,
	}

	if err := pm.load(filePath); err != nil {
		return nil, fmt.Errorf("failed to load initial config: %w", err)
	}

	if err := pm.startWatcher(filePath); err != nil {
		return nil, fmt.Errorf("failed to watch config file: %w", err)
	}

	logger.Info("Permissions manager initialized", zap.String("config_path", filePath))
	return pm, nil
}

// load reads and parses the permissions configuration file
func (pm *PermissionsManager) load(filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var config PermissionsConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config JSON: %w", err)
	}

	// Build lookup maps for O(1) access
	// Expand env vars on string values (not keys) using os.ExpandEnv
	oauth2Lookup := make(map[oauth2Key]oauth2UserProfile)
	for name, entry := range config.OAuth2 {
		subject := os.ExpandEnv(entry.Subject)
		azp := os.ExpandEnv(entry.Azp)
		account := os.ExpandEnv(entry.Account)
		requiredScope := os.ExpandEnv(entry.RequiredScope)

		if subject == "" && azp == "" {
			return fmt.Errorf("OAuth2 entry '%s' must have either 'subject' or 'azp' field", name)
		}
		if requiredScope == "" {
			requiredScope = "mqtt"
		}

		key := oauth2Key{
			Subject: subject,
			Azp:     azp,
		}
		oauth2Lookup[key] = oauth2UserProfile{
			UserProfile: UserProfile{
				Name:        name,
				Account:     account,
				Permissions: entry.Permissions,
			},
			requiredScope: requiredScope,
		}
	}

	mtlsLookup := make(map[string]UserProfile)
	for name, entry := range config.MTLS {
		identity := os.ExpandEnv(entry.Identity)
		account := os.ExpandEnv(entry.Account)
		mtlsLookup[identity] = UserProfile{
			Name:        name,
			Account:     account,
			Permissions: entry.Permissions,
		}
	}

	nkeyLookup := make(map[string]UserProfile)
	for name, entry := range config.NKey {
		publicKey := os.ExpandEnv(entry.PublicKey)
		account := os.ExpandEnv(entry.Account)
		nkeyLookup[publicKey] = UserProfile{
			Name:        name,
			Account:     account,
			Permissions: entry.Permissions,
		}
	}

	if config.NoAuth != nil {
		account := os.ExpandEnv(config.NoAuth.Account)
		noauthProfile := UserProfile{
			Name:        "anonymous",
			Account:     account,
			Permissions: config.NoAuth.Permissions,
		}
		pm.noauthProfile.Store(&noauthProfile)
	} else {
		pm.noauthProfile.Store(nil)
	}

	// Atomic swaps
	pm.oauth2Lookup.Store(&oauth2Lookup)
	pm.mtlsLookup.Store(&mtlsLookup)
	pm.nkeyLookup.Store(&nkeyLookup)

	noauthStr := "disabled"
	if config.NoAuth != nil {
		noauthStr = fmt.Sprintf("enabled (account: %s)", config.NoAuth.Account)
	}

	if pm.logger != nil {
		pm.logger.Info("Loaded permissions config",
			zap.Int("oauth2_count", len(oauth2Lookup)),
			zap.Int("mtls_count", len(mtlsLookup)),
			zap.Int("nkey_count", len(nkeyLookup)),
			zap.String("noauth", noauthStr),
		)
	}

	return nil
}

func (pm *PermissionsManager) startWatcher(filePath string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	pm.fileWatcher = watcher

	if err := watcher.Add(filepath.Dir(filePath)); err != nil {
		_ = watcher.Close()
		return fmt.Errorf("failed to watch config directory: %w", err)
	}

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !shouldReload(event, filePath) {
					continue
				}
				pm.logger.Info("Config file changed, reloading...")
				if err := pm.load(filePath); err != nil {
					pm.logger.Error("Error reloading config", zap.Error(err))
					continue
				}
				pm.logger.Info("Config reloaded successfully")
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				pm.logger.Error("Config watcher error", zap.Error(err))
			}
		}
	}()

	return nil
}

func shouldReload(event fsnotify.Event, filePath string) bool {
	if filepath.Clean(event.Name) != filepath.Clean(filePath) {
		return false
	}
	return event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename)
}

// GetOAuth2Profile returns the user profile and required scope for OAuth2 claims (subject and azp)
func (pm *PermissionsManager) GetOAuth2Profile(subject, azp string) (UserProfile, string, bool) {
	lookup := pm.oauth2Lookup.Load()

	// Try exact match first (both subject and azp)
	oauth2Lookup := (*lookup)
	if entry, ok := oauth2Lookup[oauth2Key{Subject: subject, Azp: azp}]; ok {
		return entry.UserProfile, entry.requiredScope, true
	}

	// Try subject-only match
	if entry, ok := oauth2Lookup[oauth2Key{Subject: subject}]; ok {
		return entry.UserProfile, entry.requiredScope, true
	}

	// Try azp-only match
	if entry, ok := oauth2Lookup[oauth2Key{Azp: azp}]; ok {
		return entry.UserProfile, entry.requiredScope, true
	}

	return UserProfile{}, "", false
}

// GetMTLSProfile returns the user profile for an mTLS identity
func (pm *PermissionsManager) GetMTLSProfile(identity string) (UserProfile, bool) {
	lookup := pm.mtlsLookup.Load()
	profile, ok := (*lookup)[identity]
	return profile, ok
}

// GetNKeyProfile returns the user profile for an NKey public key
func (pm *PermissionsManager) GetNKeyProfile(publicKey string) (UserProfile, bool) {
	lookup := pm.nkeyLookup.Load()
	profile, ok := (*lookup)[publicKey]
	return profile, ok
}

// GetNoAuthProfile returns the no-auth user profile if configured
func (pm *PermissionsManager) GetNoAuthProfile() (UserProfile, bool) {
	profile := pm.noauthProfile.Load()
	if profile == nil {
		return UserProfile{}, false
	}
	return *profile, true
}

// Close stops the file watcher
func (pm *PermissionsManager) Close() error {
	if pm.fileWatcher == nil {
		return nil
	}
	return pm.fileWatcher.Close()
}
