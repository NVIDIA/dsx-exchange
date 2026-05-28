# Hot Config Reload Implementation Plan

## Overview

This document outlines the implementation plan for adding hot configuration reload functionality to the nv-config library. The system will support watching multiple configuration files for changes and automatically reloading configuration values with validation and component notification.

## Core Features

### 1. Multiple Hot Config Files
- `--hot-config` CLI flag supporting comma-separated file paths (similar to `--config`)
- Support for multiple specialized files:
  - `secrets.yaml` - Vault-managed secrets
  - `features.yaml` - Feature flags
  - `overrides.yaml` - Operational overrides

### 2. Dual Reload Strategy
- **Primary**: fsnotify-based file watcher for real-time updates
- **Fallback**: Periodic reload every 15s (configurable) to catch missed events
- Debounce rapid changes (300ms) to prevent reload storms
- Skip reload if file modification time unchanged (optimization)

### 3. Reload Process
1. Detect file change (via watcher or periodic check)
2. Load changed file into temporary structure
3. Validate against registered struct types
4. Call struct `Validate()` methods for business logic validation
5. If valid: merge hot config values into live configuration (hot config always wins)
6. Update metadata and notify registered watchers
7. If invalid: log error and keep existing configuration

### 4. Component Notification System
- Enhanced `ReloadCallback` interface with detailed change information
- `ConfigWatcher` interface for components to register interest in specific config sections
- Hybrid notification approach:
  - Immediate notification of config changes
  - Lazy reconnection/recreation of resources when needed

## Implementation Components

### ConfigManager Extensions
```go
type ConfigManager struct {
    // ... existing fields ...
    
    // Hot config support
    hotConfigFiles   []string
    hotConfigFlag    string
    reloadInterval   time.Duration
    fileWatchers     map[string]FileWatcher
    watchers         map[string][]ConfigWatcher
    lastModTimes     map[string]time.Time
    periodicTicker   *time.Ticker
}
```

### Aliased Provider Support
Hot config reload must handle aliased providers correctly:
- Aliased providers transform config keys (e.g., `httpclient.timeout` → `api-client.timeout`)
- Section detection must account for both original and aliased section names
- Component registration should use the aliased section name for watchers
- Validation should work with the aliased struct types

### New Interfaces
```go
// File watching capability
type FileWatcher interface {
    Watch(filePath string, callback func() error) error
    Stop() error
}

// Component notification interface
type ConfigWatcher interface {
    OnConfigChange(section string, oldValue, newValue interface{}) error
    WatchedSections() []string
}

// Enhanced reload callback with change details
type ReloadCallback func(changes *ConfigChanges) error

type ConfigChanges struct {
    OldData          map[string]interface{}
    NewData          map[string]interface{}
    ChangedKeys      []string
    AffectedSections []string
    SourceFile       string
}
```

### New Methods
- `StartWatching()` / `StopWatching()` - Manage file watching and periodic checks
- `RegisterWatcher(ConfigWatcher)` - Register component for notifications
- `reloadFromHotFile(filePath string)` - Handle file change reload
- `validateMergedConfig(tempKoanf)` - Validate configuration after merge
- `periodicReloadCheck()` - Check file mod times and reload if changed

### Configuration Options
- `WithHotConfigFiles(...string)` - Set hot config files
- `WithHotConfigFlag(string)` - Set CLI flag name (default: "hot-config")
- `WithHotConfigInterval(time.Duration)` - Set periodic check interval (default: 15s)

### CLI Integration
- Add `--hot-config` flag supporting comma-separated file paths
- Add `--hot-config-interval` flag for custom periodic check interval
- Follow same parsing logic as existing `--config` flag

## Usage Examples

### Basic Setup
```bash
# Single hot config file
myapp --hot-config /etc/myapp/secrets.yaml

# Multiple hot config files
myapp --hot-config /vault/secrets.yaml,/etc/features.yaml,/etc/overrides.yaml

# Combined with regular config
myapp --config prod.yaml --hot-config /vault/secrets.yaml

# Custom periodic interval
myapp --hot-config /vault/secrets.yaml --hot-config-interval 30s
```

### Code Integration
```go
// Service setup with hot config
cm, err := config.NewService(
    defaultYAML,
    serviceConfig,
    "MYAPP",
    providers,
    rootCmd,
    config.WithHotConfigFiles("/vault/secrets.yaml", "/etc/features.yaml"),
    config.WithHotConfigFlag("hot-config"),
    config.WithHotConfigInterval(15*time.Second),
    config.WithReloadCallback(handleConfigReload),
)

// Load initial configuration
err = cm.Load()

// Start watching for changes
err = cm.StartWatching()
defer cm.StopWatching()
```

## Error Handling Strategy

### Validation Failures
- Log detailed validation error with file path and section
- Keep existing configuration unchanged
- Continue monitoring for future changes
- Expose health check endpoint showing reload status

### File Watcher Failures
- Log file watcher errors
- Periodic polling continues as fallback
- Attempt to restart file watcher with exponential backoff

### Component Notification Failures
- Log component notification errors
- Continue notifying other registered components
- Don't block the reload process

### Periodic Check Failures
- Log periodic check errors
- Continue trying on next interval
- File watchers continue working independently

## Benefits

1. **Reliability**: Dual strategy ensures changes are never missed
2. **Validation**: Full struct validation maintains configuration integrity
3. **Flexibility**: Multiple files for different types of dynamic configuration
4. **Performance**: Efficient with debouncing and modification time checks
5. **Backward Compatibility**: No changes to existing configuration loading
6. **Production Ready**: Comprehensive error handling and fallback mechanisms

## Implementation Phases

### Phase 1: Core Infrastructure
- File watcher interface and implementation
- Basic hot config file loading and validation
- Simple reload callback system

### Phase 2: Enhanced Features
- Multiple file support
- Periodic reload fallback
- Component notification system

### Phase 3: Production Hardening
- Comprehensive error handling
- Performance optimizations
- Monitoring and observability features

## Aliased Provider Considerations

### Key Transformation
- Aliased providers transform config keys: `httpclient.timeout` → `api-client.timeout`
- Hot config files must use the aliased section names, not the original names
- Section detection in `findAffectedSections()` works with aliased names since registered structs use aliased section names

### Component Registration
```go
// Components register with aliased section names
type HTTPClient struct {
    section string // e.g., "api-client" not "httpclient"
}

func (c *HTTPClient) WatchedSections() []string {
    return []string{c.section} // Uses aliased name
}
```

### Hot Config File Structure
```yaml
# Hot config must use aliased section names
api-client:           # Aliased name, not "httpclient"
  timeout: "45s"
  api_key: "new_key"

external-scraper:     # Another alias for httpclient
  timeout: "60s"
  retries: 5
```

### Validation Support
- Aliased providers are already registered with their aliased section names in `registeredStructs`
- Validation automatically works with aliased sections since `AliasedProvider.Name()` returns the alias
- No special handling needed in validation logic

## Security Considerations

- Hot config files should have appropriate file permissions
- Validate file ownership and permissions before loading
- Log all configuration changes for audit purposes
- Consider rate limiting for reload frequency in production
