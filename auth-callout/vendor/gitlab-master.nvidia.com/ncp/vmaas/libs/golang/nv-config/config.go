// Package config provides comprehensive configuration management with support for multiple
// sources (YAML files, environment variables, CLI flags) and hierarchical precedence handling.
//
// # Overview
//
// This library enables applications to load configuration from multiple sources with a clear
// precedence order: CLI flags > Environment Variables > YAML files > Embedded defaults.
// It supports both simple applications and complex services with multiple library dependencies.
//
// # Key Features
//
//   - Multi-source configuration loading (YAML, env vars, CLI flags)
//   - Type-safe configuration with automatic validation
//   - Library provider pattern for reusable components
//   - Configuration aliasing for multiple instances
//   - Fail-fast validation during startup
//   - Built-in Cobra CLI integration
//   - Context-aware configuration management
//
// # Quick Start
//
// For simple applications, use the New constructor:
//
//	cm, err := config.New(embeddedYAML, "MYAPP", nil)
//	if err != nil {
//		log.Fatal(err)
//	}
//	if err := cm.Load(); err != nil {
//		log.Fatal(err)
//	}
//
// # Examples
//
// See the examples directory for complete working examples:
//   - examples/basic/     - Simple HTTP server configuration
//   - examples/service/   - Complex service with multiple library providers
//   - examples/library/   - Example library configuration providers
//
// # Library Integration
//
// Libraries should implement LibraryConfigProvider to provide default configurations:
//
//	type MyLibraryProvider struct{}
//
//	func (p *MyLibraryProvider) Name() string { return "mylibrary" }
//	func (p *MyLibraryProvider) DefaultConfig() (interface{}, error) { ... }
//	func (p *MyLibraryProvider) ConfigKeys() []string { ... }
package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gitlab-master.nvidia.com/ncp/vmaas/libs/golang/nv-config/internal/providers"
	yamlv3 "gopkg.in/yaml.v3"
)

// Version information
const (
	Version = "1.0.0"
)

// Provider defines the interface that all configuration providers must implement.
// Providers are responsible for loading configuration data from different sources
// such as files, environment variables, CLI arguments, etc.
type Provider interface {
	// Name returns a human-readable name for the provider
	Name() string

	// Priority returns the provider's priority level.
	// Higher priority providers override lower priority ones.
	Priority() int

	// Load loads configuration data from the provider's source
	// and returns it as a map[string]interface{}
	Load() (map[string]interface{}, error)
}

// ConfigProvider defines an interface for providers that can be configured
// by the main ConfigManager (e.g., for callbacks, validation, etc.)
type ConfigProvider interface {
	Provider

	// Configure allows the ConfigManager to configure the provider
	Configure(options map[string]interface{}) error
}

// ErrorType represents different types of configuration errors
type ErrorType int

const (
	ErrorTypeUnknown ErrorType = iota
	ErrorTypeValidation
	ErrorTypeProvider
	ErrorTypeProviderConflict
	ErrorTypeKeyConflict
	ErrorTypeMerge
	ErrorTypeLoad
	ErrorTypeParse
)

// String returns the string representation of ErrorType
func (e ErrorType) String() string {
	switch e {
	case ErrorTypeValidation:
		return "VALIDATION"
	case ErrorTypeProvider:
		return "PROVIDER_FAILED"
	case ErrorTypeProviderConflict:
		return "PROVIDER_CONFLICT"
	case ErrorTypeKeyConflict:
		return "KEY_CONFLICT"
	case ErrorTypeMerge:
		return "MERGE_CONFLICT"
	case ErrorTypeLoad:
		return "LOAD_FAILED"
	case ErrorTypeParse:
		return "PARSE_ERROR"
	default:
		return "UNKNOWN"
	}
}

// ConfigError represents a configuration-related error with additional context
type ConfigError struct {
	Type        ErrorType
	Code        string
	Message     string
	Source      string
	Suggestions []string
	Cause       error
}

// Error implements the error interface
func (e *ConfigError) Error() string {
	var parts []string

	if e.Code != "" {
		parts = append(parts, fmt.Sprintf("[%s]", e.Code))
	}

	parts = append(parts, fmt.Sprintf("%s: %s", e.Type.String(), e.Message))

	if e.Source != "" {
		parts = append(parts, fmt.Sprintf("(source: %s)", e.Source))
	}

	result := strings.Join(parts, " ")

	if len(e.Suggestions) > 0 {
		result += "\nSuggestions:\n"
		for _, suggestion := range e.Suggestions {
			result += fmt.Sprintf("  - %s\n", suggestion)
		}
	}

	if e.Cause != nil {
		result += "\nCause:\n" + e.Cause.Error()
	}

	return result
}

// Unwrap returns the underlying error
func (e *ConfigError) Unwrap() error {
	return e.Cause
}

// ReloadCallback is called when configuration is reloaded
type ReloadCallback func(oldData, newData map[string]interface{}) error

// FileWatcher interface for file watching capabilities
type FileWatcher interface {
	// Watch starts watching a file and calls the callback when it changes
	// The callback receives the file path that changed
	WatchFile(filePath string, callback func(string) error) error
	// Stop stops watching all files and cleans up resources
	Stop() error
}

// ConfigWatcher interface for components that want to be notified of config changes
type ConfigWatcher interface {
	// OnConfigChange is called when a watched configuration section changes
	OnConfigChange(section string, oldValue, newValue interface{}) error
	// WatchedSections returns the list of configuration sections this watcher is interested in
	WatchedSections() []string
}

// ErrorHandler handles configuration errors
type ErrorHandler interface {
	HandleError(error) error
}

// ConfigMetadata contains metadata about the loaded configuration
type ConfigMetadata struct {
	LoadTime time.Time              `json:"load_time"`
	Sources  map[string]string      `json:"sources"`
	Version  string                 `json:"version"`
	Computed map[string]interface{} `json:"computed,omitempty"`
}

// ConfigManager is the central configuration coordinator that manages multiple configuration
// sources and provides a unified interface for accessing configuration values.
//
// It automatically handles:
//   - Loading from multiple sources (YAML files, environment variables, CLI flags)
//   - Type conversion and validation
//   - Library provider registration and defaults
//   - Configuration precedence and merging
//   - Thread-safe concurrent access
//
// ConfigManager instances should be created using one of the constructor functions:
// New, NewService, NewForTesting, or NewWithOptions.
type ConfigManager struct {
	koanf     *koanf.Koanf
	providers []Provider

	validation     bool
	metadata       *ConfigMetadata
	computedVals   map[string]interface{}
	reloadCallback ReloadCallback
	errorHandler   ErrorHandler

	// Default ENV provider settings
	envProvider Provider
	envPrefix   string

	// Plugin management
	pluginManager *PluginManager

	// Default YAML configuration
	defaultYAML string

	// Standard config file settings
	standardConfigFiles []string // Default config files to load automatically
	configFlag          string   // CLI flag name for config files

	// Hot config file settings
	hotConfigFiles  []string                   // Hot config files to watch
	hotConfigFlag   string                     // CLI flag name for hot config files
	reloadInterval  time.Duration              // Polling interval for file watcher (passed to NewFileWatcher)
	fileWatcher     FileWatcher                // Single watcher for all hot config files
	configWatchers  map[string][]ConfigWatcher // Map of section to list of watchers
	watchingStarted bool                       // Flag to track if watching has started

	// Hot config initial loading support
	hotConfigData    map[string]interface{} // Track what came from hot config (for ExportYAML exclusion)
	initialHotLoaded bool                   // Track if we've done initial hot config load

	// Cobra command for auto-flag generation
	cobraCmd *cobra.Command

	// Lookup map for correct flag to config key mapping
	flagToConfigKey map[string]string // Maps flag names to config keys

	// Lookup map for correct env var to config key mapping
	envVarToConfigKey map[string]string // Maps env var names to config keys

	// Templated env var patterns for dynamic map keys
	templatedEnvVarPatterns map[string]string // Maps env var patterns to config key templates

	// Field type mappings for type-aware env var parsing
	fieldTypeMappings map[string]string // Maps config keys to field types (e.g., "slice", "string")

	// Description map for CLI help text
	configDescriptions map[string]string // Maps config keys to descriptions

	// Struct registration for early validation
	registeredStructs map[string]reflect.Type // Maps section names to struct types from desc tags

	// Thread safety
	mu sync.RWMutex
}

// Option defines a functional option for configuring the ConfigManager
type Option func(*ConfigManager) error

// NewWithOptions creates a new ConfigManager with the given options.
// This is the advanced constructor for users who need fine-grained control
// over configuration providers and options.
//
// For most service applications, use New() instead which provides a simpler API.
//
// Example:
//
//	cm, err := config.NewWithOptions(
//	    config.WithDefaultYAML(defaultYAML),
//	    config.WithEnvPrefix("MYSERVICE"),
//	    config.WithProviders(providers...),
//	)
func NewWithOptions(options ...Option) (*ConfigManager, error) {
	cm := &ConfigManager{
		koanf:     koanf.New("."),
		providers: make([]Provider, 0),

		validation:          true, // Enable validation by default (safe defaults)
		computedVals:        make(map[string]interface{}),
		envPrefix:           "", // Default to no prefix (all env vars)
		pluginManager:       NewPluginManager(),
		standardConfigFiles: []string{"config.yaml"}, // Default config file
		configFlag:          "config",                // Default CLI flag name

		// Hot config defaults
		hotConfigFiles:  []string{},                       // No hot config files by default
		hotConfigFlag:   "hot-config",                     // Default hot config CLI flag name
		reloadInterval:  1 * time.Minute,                  // Default 1m for polling watcher
		configWatchers:  make(map[string][]ConfigWatcher), // Initialize config watcher map
		watchingStarted: false,                            // Not watching initially

		// Hot config initial loading defaults
		hotConfigData:    make(map[string]interface{}), // Initialize hot config tracking
		initialHotLoaded: false,                        // Not loaded initially

		flagToConfigKey:         make(map[string]string),       // Initialize flag lookup map
		envVarToConfigKey:       make(map[string]string),       // Initialize env var lookup map
		templatedEnvVarPatterns: make(map[string]string),       // Initialize templated pattern map
		fieldTypeMappings:       make(map[string]string),       // Initialize field type mapping
		configDescriptions:      make(map[string]string),       // Initialize description map
		registeredStructs:       make(map[string]reflect.Type), // Initialize struct registry
		metadata: &ConfigMetadata{
			Sources:  make(map[string]string),
			Version:  Version,
			Computed: make(map[string]interface{}),
		},
	}

	// Apply options first (they may modify envPrefix)
	for _, option := range options {
		if err := option(cm); err != nil {
			return nil, &ConfigError{
				Type:    ErrorTypeValidation,
				Code:    "CFG_NEW_001",
				Message: "Failed to apply configuration option",
				Cause:   err,
				Suggestions: []string{
					"Check option parameters for validity",
					"Ensure all required dependencies are available",
				},
			}
		}
	}

	// If Cobra command is provided, generate flags immediately after setup
	if cm.cobraCmd != nil {
		// Load defaults first to have configuration structure for flag generation
		if err := cm.loadInitialDefaults(); err != nil {
			return nil, err
		}

		// Generate flags based on the initial configuration
		if err := cm.generateCobraFlags(); err != nil {
			return nil, err
		}
	}

	// Create and register default ENV provider with naming converter
	cm.envProvider = providers.NewEnvProvider(
		providers.WithEnvPrefix(cm.envPrefix),
		providers.WithEnvPriority(50), // Medium priority by default
	)
	cm.providers = append(cm.providers, cm.envProvider)

	return cm, nil
}

// WithValidation enables or disables configuration validation
func WithValidation(enabled bool) Option {
	return func(cm *ConfigManager) error {
		cm.validation = enabled
		return nil
	}
}

// WithComputedValues sets computed values that are injected at runtime
func WithComputedValues(values map[string]interface{}) Option {
	return func(cm *ConfigManager) error {
		if values == nil {
			return &ConfigError{
				Type:    ErrorTypeValidation,
				Code:    "CFG_COMPUTED_001",
				Message: "Computed values map cannot be nil",
				Suggestions: []string{
					"Provide a valid map[string]interface{} or use empty map",
				},
			}
		}

		cm.computedVals = make(map[string]interface{})
		for k, v := range values {
			cm.computedVals[k] = v
			cm.metadata.Computed[k] = v
		}
		return nil
	}
}

// WithReloadCallback sets a callback to be called when configuration is reloaded
func WithReloadCallback(callback ReloadCallback) Option {
	return func(cm *ConfigManager) error {
		cm.reloadCallback = callback
		return nil
	}
}

// WithErrorHandler sets a custom error handler
func WithErrorHandler(handler ErrorHandler) Option {
	return func(cm *ConfigManager) error {
		cm.errorHandler = handler
		return nil
	}
}

// WithEnvPrefix sets the environment variable prefix for the default ENV provider
func WithEnvPrefix(prefix string) Option {
	return func(cm *ConfigManager) error {
		cm.envPrefix = prefix
		return nil
	}
}

// WithDefaultYAML sets embedded default YAML configuration
func WithDefaultYAML(yaml string) Option {
	return func(cm *ConfigManager) error {
		cm.defaultYAML = yaml
		return nil
	}
}

// WithProviders registers configuration providers for external libraries
func WithProviders(providers ...LibraryConfigProvider) Option {
	return func(cm *ConfigManager) error {
		for _, provider := range providers {
			if err := cm.pluginManager.RegisterProvider(provider); err != nil {
				return err
			}

			// Auto-register struct for early validation
			if err := cm.registerProviderStruct(provider); err != nil {
				// Don't fail on registration errors - log and continue
				continue
			}
		}
		return nil
	}
}

// WithCobraAutoFlags enables automatic CLI flag generation from configuration structure
func WithCobraAutoFlags(cmd *cobra.Command) Option {
	return func(cm *ConfigManager) error {
		cm.cobraCmd = cmd
		return nil
	}
}

// WithConfigStruct registers a config struct to extract descriptions from desc tags
// This enables rich CLI help text based on struct field descriptions
// It also registers the struct for validation and templated pattern generation
func WithConfigStruct(configStruct interface{}) Option {
	return func(cm *ConfigManager) error {
		descriptions := ExtractDescriptionsWithPrefix(configStruct, "")
		for key, desc := range descriptions {
			cm.configDescriptions[key] = desc
		}

		// Register the struct for validation and templated pattern generation
		// We need to determine the root section name from the struct
		structType := reflect.TypeOf(configStruct)
		if structType.Kind() == reflect.Ptr {
			structType = structType.Elem()
		}

		if structType.Kind() == reflect.Struct {
			// Register the entire struct as the root section (empty string)
			cm.registeredStructs[""] = structType
		}

		return nil
	}
}

// WithStandardConfigFiles sets the default config files to load automatically
func WithStandardConfigFiles(filenames ...string) Option {
	return func(cm *ConfigManager) error {
		cm.standardConfigFiles = filenames
		return nil
	}
}

// WithConfigFlag sets the CLI flag name for specifying config files
func WithConfigFlag(flagName string) Option {
	return func(cm *ConfigManager) error {
		cm.configFlag = flagName
		return nil
	}
}

// WithHotConfigFiles sets the default hot config files to watch automatically
func WithHotConfigFiles(filenames ...string) Option {
	return func(cm *ConfigManager) error {
		cm.hotConfigFiles = filenames
		return nil
	}
}

// WithHotConfigFlag sets the CLI flag name for specifying hot config files
func WithHotConfigFlag(flagName string) Option {
	return func(cm *ConfigManager) error {
		cm.hotConfigFlag = flagName
		return nil
	}
}

// WithHotConfigInterval sets the interval for periodic hot config reload checks
func WithHotConfigInterval(interval time.Duration) Option {
	return func(cm *ConfigManager) error {
		cm.reloadInterval = interval
		return nil
	}
}

// NewService creates a ConfigManager with common service application defaults.
// This is a convenience constructor that combines the most commonly used options
// for service applications in a single function call.
//
// Hot config support is automatically enabled with default files (secrets.yaml, overrides.yaml)
// and can be customized using --hot-config and --hot-config-interval CLI flags.
//
// Parameters:
//   - defaultYAML: Embedded YAML configuration string (typically from go:embed)
//   - serviceConfig: Service configuration struct with desc tags for CLI help extraction
//   - envPrefix: Environment variable prefix (e.g., "MYSERVICE") - underscore is added automatically
//   - providers: List of library configuration providers
//   - cobraCmd: Cobra command for auto-generating CLI flags (can be nil)
//
// Example:
//
//	providers := []config.LibraryConfigProvider{
//	    &httpclient.ConfigProvider{},
//	    &database.ConfigProvider{},
//	    &redis.ConfigProvider{},
//	}
//	cm, err := config.NewService(defaultConfigYAML, MyServiceConfig{}, "MYSERVICE", providers, rootCmd)
//	if err := cm.Load(); err != nil { return err }
//	if err := cm.StartWatching(); err != nil { return err } // Start hot config watching
func NewService(defaultYAML string, serviceConfig interface{}, envPrefix string, providers []LibraryConfigProvider, cobraCmd *cobra.Command) (*ConfigManager, error) {
	opts := []Option{
		WithDefaultYAML(defaultYAML),
		WithEnvPrefix(envPrefix),
		WithProviders(providers...),
		WithConfigStruct(serviceConfig), // Extract descriptions from service config
		// Hot config support (no files by default - user must specify)
		WithHotConfigFiles(),
		WithHotConfigFlag("hot-config"),
		WithHotConfigInterval(1 * time.Minute),
	}

	if cobraCmd != nil {
		opts = append(opts, WithCobraAutoFlags(cobraCmd))
	}

	return NewWithOptions(opts...)
}

// New creates a ConfigManager for service applications.
// This is the recommended constructor for most use cases, providing a simple API
// for common service configuration needs.
//
// Parameters:
//   - defaultYAML: Embedded YAML configuration string (typically from go:embed)
//   - envPrefix: Environment variable prefix (e.g., "MYSERVICE") - underscore is added automatically
//   - providers: List of library configuration providers
//
// Example:
//
//	providers := []config.LibraryConfigProvider{
//	    &httpclient.ConfigProvider{},
//	    &database.ConfigProvider{},
//	}
//	cm, err := config.New(defaultConfigYAML, "MYSERVICE", providers)
//
// For advanced use cases requiring fine-grained control, use NewWithOptions() instead.
func New(defaultYAML, envPrefix string, providers []LibraryConfigProvider) (*ConfigManager, error) {
	return NewWithOptions(
		WithDefaultYAML(defaultYAML),
		WithEnvPrefix(envPrefix),
		WithProviders(providers...),
	)
}

// NewForTesting creates a ConfigManager optimized for testing scenarios.
// This constructor disables external config file loading and focuses on
// embedded defaults and library providers only.
//
// Parameters:
//   - defaultYAML: Embedded YAML configuration string for test defaults
//   - providers: List of library configuration providers
//
// Example:
//
//	providers := []config.LibraryConfigProvider{
//	    &httpclient.ConfigProvider{},
//	    &database.ConfigProvider{},
//	}
//	cm, err := config.NewForTesting(testConfigYAML, providers)
func NewForTesting(defaultYAML string, providers []LibraryConfigProvider) (*ConfigManager, error) {
	return NewWithOptions(
		WithDefaultYAML(defaultYAML),
		WithProviders(providers...),
		WithStandardConfigFiles(), // Empty list - no external files for tests
	)
}

// RegisterProvider registers a configuration provider
func (cm *ConfigManager) RegisterProvider(provider Provider) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if provider == nil {
		return &ConfigError{
			Type:    ErrorTypeValidation,
			Code:    "CFG_PROVIDER_001",
			Message: "Provider cannot be nil",
			Suggestions: []string{
				"Ensure provider is properly initialized",
				"Check provider creation code for errors",
			},
		}
	}

	cm.providers = append(cm.providers, provider)
	return nil
}

// GetProviders returns a copy of the registered providers
func (cm *ConfigManager) GetProviders() []Provider {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	providers := make([]Provider, len(cm.providers))
	copy(providers, cm.providers)
	return providers
}

// Load loads and validates configuration from all registered providers.
//
// This method:
//  1. Loads defaults from library providers
//  2. Loads external YAML configuration files
//  3. Loads environment variables with prefix matching
//  4. Loads CLI flag values (if Cobra integration is enabled)
//  5. Validates all configuration for type correctness
//  6. Calls Validate() methods on registered structs for business logic validation
//  7. Automatically starts hot config watching if hot config files are configured
//
// Load must be called before using Get* methods or Unmarshal.
// It returns an error if any provider fails to load or validation fails.
func (cm *ConfigManager) Load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Record load time
	cm.metadata.LoadTime = time.Now()

	// 1. Load plugin defaults first (lowest priority)
	if err := cm.loadPluginDefaults(); err != nil {
		return err
	}

	// 2. Load default YAML if provided
	if cm.defaultYAML != "" {
		if err := cm.loadDefaultYAML(); err != nil {
			return err
		}
	}

	// 3. Load standard config files (config.yaml by default)
	if err := cm.loadStandardConfigFiles(); err != nil {
		return err
	}

	// 4. Load computed values (low priority)
	if len(cm.computedVals) > 0 {
		for key, value := range cm.computedVals {
			_ = cm.koanf.Set(key, value)
			cm.metadata.Sources[key] = "computed"
		}
	}

	// 4.5. Generate environment variable mappings (after config structure is known)
	if err := cm.generateEnvVarMappings(); err != nil {
		return &ConfigError{
			Type:    ErrorTypeLoad,
			Message: "failed to generate environment variable mappings",
			Source:  "env_mapping",
			Cause:   err,
		}
	}

	// 4.6. Generate templated environment variable patterns for map[string] fields
	if err := cm.generateTemplatedEnvVarPatterns(); err != nil {
		return &ConfigError{
			Type:    ErrorTypeLoad,
			Message: "failed to generate templated environment variable patterns",
			Source:  "templated_env_mapping",
			Cause:   err,
		}
	}

	// 4.7. Generate field type mappings for type-aware env var parsing
	if err := cm.generateFieldTypeMappings(); err != nil {
		return &ConfigError{
			Type:    ErrorTypeLoad,
			Message: "failed to generate field type mappings",
			Source:  "field_type_mapping",
			Cause:   err,
		}
	}

	// 4.8. Update env provider with generated mappings
	if err := cm.updateEnvProviderMappings(); err != nil {
		return &ConfigError{
			Type:    ErrorTypeLoad,
			Message: "failed to update environment provider mappings",
			Source:  "env_mapping",
			Cause:   err,
		}
	}

	// Sort providers by priority (ascending order, so higher priority overwrites)
	cm.sortProvidersByPriority()

	// Load from each provider
	for _, provider := range cm.providers {
		data, err := provider.Load()
		if err != nil {
			return &ConfigError{
				Type:    ErrorTypeProvider,
				Code:    "CFG_LOAD_001",
				Message: fmt.Sprintf("Provider '%s' failed to load configuration", provider.Name()),
				Source:  provider.Name(),
				Cause:   err,
				Suggestions: []string{
					"Check provider configuration and connectivity",
					"Verify data format and structure",
					"Review provider-specific error details",
				},
			}
		}

		// Merge the data
		if err := cm.mergeData(data, provider.Name()); err != nil {
			return err
		}
	}

	// 5. Load CLI flag values
	if cm.cobraCmd != nil {
		if err := cm.loadCobraFlags(); err != nil {
			return err
		}
	}

	// 6. Load hot config files (HIGHEST PRECEDENCE - overrides everything including CLI flags)
	if err := cm.loadInitialHotConfig(); err != nil {
		return err
	}

	// Validate if enabled
	if cm.validation {
		if err := cm.validateConfiguration(); err != nil {
			return err
		}

		// Validate registered structs with trial unmarshal
		// Structs are auto-registered during WithProviders()
		if err := cm.validateRegisteredStructs(); err != nil {
			return err
		}
	}

	// Auto-start hot config watching if hot config files are configured
	// We need to release the lock first since StartWatching acquires its own lock
	cm.mu.Unlock()
	if len(cm.hotConfigFiles) > 0 || (cm.cobraCmd != nil && cm.hotConfigFlag != "") {
		if err := cm.StartWatching(); err != nil {
			// StartWatching now fails on missing files - this is a configuration error
			cm.mu.Lock()
			return fmt.Errorf("failed to start hot config watching: %w", err)
		}
	}
	cm.mu.Lock()

	return nil
}

// Get retrieves a configuration value by key
func (cm *ConfigManager) Get(key string) interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.koanf.Get(key)
}

// GetString retrieves a string configuration value
func (cm *ConfigManager) GetString(key string) string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.koanf.String(key)
}

// GetInt retrieves an integer configuration value
func (cm *ConfigManager) GetInt(key string) int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.koanf.Int(key)
}

// GetBool retrieves a boolean configuration value
func (cm *ConfigManager) GetBool(key string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.koanf.Bool(key)
}

// Unmarshal populates a struct with values from the loaded configuration.
//
// The target struct should use `koanf` tags to map configuration keys to fields:
//
//	type Config struct {
//		Server struct {
//			Port int    `koanf:"port"`
//			Host string `koanf:"host"`
//		} `koanf:"server"`
//	}
//
// Type conversion is handled automatically. Load() must be called before Unmarshal.
// Returns an error if the target is not a pointer to a struct or if type conversion fails.
func (cm *ConfigManager) Unmarshal(target interface{}) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if err := cm.koanf.Unmarshal("", target); err != nil {
		return &ConfigError{
			Type:    ErrorTypeParse,
			Code:    "CFG_UNMARSHAL_001",
			Message: "Failed to unmarshal configuration",
			Cause:   err,
			Suggestions: []string{
				"Check target struct tags and types",
				"Ensure configuration structure matches target",
				"Verify no circular references in struct",
			},
		}
	}

	return nil
}

// UnmarshalSection unmarshals a configuration section into a struct
func (cm *ConfigManager) UnmarshalSection(section string, target interface{}) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Note: Auto-registration happens during plugin registration, not here

	if err := cm.koanf.Unmarshal(section, target); err != nil {
		return &ConfigError{
			Type:    ErrorTypeParse,
			Code:    "CFG_UNMARSHAL_002",
			Message: fmt.Sprintf("Failed to unmarshal configuration section '%s'", section),
			Source:  section,
			Cause:   err,
			Suggestions: []string{
				"Verify section exists in configuration",
				"Check target struct tags match section structure",
				"Ensure section path is correct",
			},
		}
	}

	return nil
}

// GetProviderConfig gets the merged configuration for a specific provider/section
// This is the preferred method for getting provider configuration
func (cm *ConfigManager) GetProviderConfig(section string, target interface{}) error {
	return cm.UnmarshalSection(section, target)
}

// RegisterConfigStruct registers a configuration struct for early validation
// This enables type validation during Load() instead of waiting for Unmarshal()
func (cm *ConfigManager) RegisterConfigStruct(section string, configStruct interface{}) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	structType := reflect.TypeOf(configStruct)

	// Handle pointer types
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}

	if structType.Kind() != reflect.Struct {
		return &ConfigError{
			Type:    ErrorTypeValidation,
			Code:    "CFG_REGISTER_001",
			Message: fmt.Sprintf("RegisterConfigStruct: expected struct, got %T", configStruct),
			Source:  section,
			Suggestions: []string{
				"Pass a struct or pointer to struct",
				"Ensure the config struct is properly defined",
			},
		}
	}

	cm.registeredStructs[section] = structType
	return nil
}

// GetMetadata returns configuration metadata
func (cm *ConfigManager) GetMetadata() *ConfigMetadata {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.metadata
}

// All returns all configuration data as a map
func (cm *ConfigManager) All() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.koanf.All()
}

// ExportYAML exports the complete configuration as YAML bytes, excluding hot config values
func (cm *ConfigManager) ExportYAML() ([]byte, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Get all data from koanf
	allData := cm.koanf.All()

	// Remove hot config keys (for generate-config use case)
	filteredData := make(map[string]interface{})
	for key, value := range allData {
		// Skip if this key came from hot config
		if _, isHotConfig := cm.hotConfigData[key]; !isHotConfig {
			filteredData[key] = value
		}
	}

	// Convert dot notation keys back to nested structure
	nested := make(map[string]interface{})
	for key, value := range filteredData {
		providers.SetNestedValue(nested, key, value)
	}

	// Use a custom encoder to set 2-space indentation
	var buf strings.Builder
	encoder := yamlv3.NewEncoder(&buf)
	encoder.SetIndent(2)

	if err := encoder.Encode(nested); err != nil {
		return nil, err
	}

	if err := encoder.Close(); err != nil {
		return nil, err
	}

	return []byte(buf.String()), nil
}

// GetEnvPrefix returns the current environment variable prefix
func (cm *ConfigManager) GetEnvPrefix() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return cm.envPrefix
}

// sortProvidersByPriority sorts providers by priority (ascending)
func (cm *ConfigManager) sortProvidersByPriority() {
	sort.Slice(cm.providers, func(i, j int) bool {
		return cm.providers[i].Priority() < cm.providers[j].Priority()
	})
}

// mergeData merges configuration data from a provider
func (cm *ConfigManager) mergeData(data map[string]interface{}, sourceName string) error {
	for key, value := range data {
		if cm.koanf.Exists(key) {
			existingSource := cm.metadata.Sources[key]
			if existingSource != "" && existingSource != "computed" {
				// Check if this is an allowed override or a library conflict
				if !cm.isAllowedOverride(existingSource, sourceName) {
					return &ConfigError{
						Type:    ErrorTypeMerge,
						Code:    "CFG_MERGE_001",
						Message: fmt.Sprintf("Library conflict for key '%s': %s conflicts with %s", key, sourceName, existingSource),
						Source:  sourceName,
						Suggestions: []string{
							"Check for conflicting keys between library plugins",
							"Use different configuration namespaces for libraries",
							"Remove one of the conflicting providers",
						},
					}
				}
			}
		}

		_ = cm.koanf.Set(key, value)
		cm.metadata.Sources[key] = sourceName
	}

	return nil
}

// isAllowedOverride determines if a source can override another source
// This implements smart merge policy: allow precedence-based overrides but prevent library conflicts
func (cm *ConfigManager) isAllowedOverride(existingSource, newSource string) bool {
	// Define precedence hierarchy (higher priority sources can override lower priority ones)
	precedenceOrder := map[string]int{
		"plugin_defaults": 10, // Lowest priority - library defaults
		"embedded_yaml":   20, // Service defaults
		"yaml-file":       30, // YAML files (can have multiple with different priorities)
		"env":             40, // Environment variables
		"cli":             50, // CLI flags
		"computed":        60, // Computed values (always allowed to override)
		"hot_config":      70, // Hot config files (highest priority - overrides everything)
	}

	// Extract base source type (remove provider-specific suffixes)
	existingType := cm.getSourceType(existingSource)
	newType := cm.getSourceType(newSource)

	// Always allow computed values to override anything
	if newType == "computed" {
		return true
	}

	// Get precedence levels
	existingPrecedence, existingExists := precedenceOrder[existingType]
	newPrecedence, newExists := precedenceOrder[newType]

	// If both sources are known, allow override if new source has higher or equal precedence
	if existingExists && newExists {
		return newPrecedence >= existingPrecedence
	}

	// Special case: multiple files of the same type can override each other
	if existingType == "yaml-file" && newType == "yaml-file" {
		return true
	}

	// Special case: different plugin sources should not conflict with each other
	if cm.isPluginSource(existingSource) && cm.isPluginSource(newSource) && existingSource != newSource {
		return false // This will trigger a library conflict error
	}

	// Default: allow override for unknown source types (be permissive)
	return true
}

// getSourceType extracts the base source type from a source name
func (cm *ConfigManager) getSourceType(sourceName string) string {
	// Handle common source patterns
	switch {
	case strings.Contains(sourceName, "plugin"):
		return "plugin_defaults"
	case strings.HasPrefix(sourceName, "hot_config:"):
		return "hot_config"
	case strings.Contains(sourceName, "yaml") || strings.HasSuffix(sourceName, ".yaml") || strings.HasSuffix(sourceName, ".yml") || sourceName == "standard-config-files":
		return "yaml-file"
	case sourceName == "env" || strings.Contains(sourceName, "environment") || strings.Contains(sourceName, "env-"):
		return "env"
	case sourceName == "cli" || strings.Contains(sourceName, "flags"):
		return "cli"
	case sourceName == "computed":
		return "computed"
	case sourceName == "embedded_yaml" || sourceName == "default_yaml":
		return "embedded_yaml"
	default:
		return sourceName
	}
}

// isPluginSource checks if a source comes from a plugin
func (cm *ConfigManager) isPluginSource(sourceName string) bool {
	return strings.Contains(sourceName, "plugin") ||
		strings.Contains(sourceName, "Provider") ||
		(sourceName != "env" && sourceName != "cli" && sourceName != "computed" &&
			sourceName != "embedded_yaml" && sourceName != "default_yaml" &&
			!strings.Contains(sourceName, "yaml-file"))
}

// validateConfiguration validates the loaded configuration
func (cm *ConfigManager) validateConfiguration() error {
	// Basic validation - check if configuration is empty
	if cm.koanf.All() == nil {
		return &ConfigError{
			Type:    ErrorTypeValidation,
			Code:    "CFG_VALIDATE_001",
			Message: "Configuration is empty after loading",
			Suggestions: []string{
				"Check that providers are loading data correctly",
				"Verify provider priorities and merge settings",
				"Ensure at least one provider has valid data",
			},
		}
	}

	return nil
}

// validateRegisteredStructs validates all registered configuration structs using trial unmarshal
// This catches type mismatches early during Load() instead of waiting for actual Unmarshal()
func (cm *ConfigManager) validateRegisteredStructs() error {
	for section, structType := range cm.registeredStructs {
		// Create new instance of the registered struct
		structInstance := reflect.New(structType).Interface()

		// Try unmarshaling the section - this catches type mismatches
		// NOTE: Don't call UnmarshalSection() here to avoid deadlock (we're already in a lock)
		if err := cm.koanf.Unmarshal(section, structInstance); err != nil {
			return &ConfigError{
				Type:    ErrorTypeValidation,
				Code:    "CFG_STRUCT_VALIDATION_001",
				Message: fmt.Sprintf("Configuration validation failed for section '%s'", section),
				Source:  section,
				Cause:   err,
				Suggestions: []string{
					"Check environment variables match expected types",
					"Verify configuration file values are correctly formatted",
					"Review struct field types and koanf tags",
					fmt.Sprintf("Check section '%s' configuration values", section),
				},
			}
		}

		// If the struct has a Validate() method, call it for business logic validation
		if err := cm.callValidateMethod(structInstance, section); err != nil {
			return err
		}
	}
	return nil
}

// callValidateMethod calls the Validate() method on a struct instance if it exists
func (cm *ConfigManager) callValidateMethod(structInstance interface{}, section string) error {
	// Use reflection to check if the struct has a Validate() method
	value := reflect.ValueOf(structInstance)

	// Handle pointer types
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return nil // Nothing to validate
		}
		value = value.Elem()
	}

	// Get the struct value (not pointer) for method lookup
	structValue := value
	if structValue.CanAddr() {
		structValue = structValue.Addr()
	}

	// Look for Validate() method
	validateMethod := structValue.MethodByName("Validate")
	if !validateMethod.IsValid() {
		return nil // No Validate method, skip validation
	}

	// Check if the method has the right signature: func() error
	methodType := validateMethod.Type()
	if methodType.NumIn() != 0 || methodType.NumOut() != 1 || methodType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
		return nil // Wrong signature, skip validation
	}

	// Call the Validate method
	results := validateMethod.Call(nil)
	if len(results) != 1 {
		return nil // Unexpected result count
	}

	// Check if validation returned an error
	if results[0].IsNil() {
		return nil // Validation passed
	}

	err := results[0].Interface().(error)
	return &ConfigError{
		Type:    ErrorTypeValidation,
		Code:    "CFG_BUSINESS_VALIDATION_001",
		Message: fmt.Sprintf("Business logic validation failed for section '%s'", section),
		Source:  section,
		Cause:   err,
		Suggestions: []string{
			"Review configuration values for business logic constraints",
			fmt.Sprintf("Check section '%s' configuration values", section),
			"Verify configuration values meet expected ranges and formats",
			"Consult library documentation for valid configuration options",
		},
	}
}

// registerProviderStruct registers a single provider's config struct for validation
func (cm *ConfigManager) registerProviderStruct(provider LibraryConfigProvider) error {
	// Handle aliased providers by using their base provider
	if aliasedProvider, ok := provider.(*AliasedProvider); ok {
		baseProvider := aliasedProvider.GetBaseProvider()
		baseConfig, err := baseProvider.DefaultConfig()
		if err != nil {
			return err
		}

		// For aliased providers, use the aliased section name for registration
		// but extract struct using the base provider's tag name
		sectionName := provider.Name()
		nestedStruct := cm.extractNestedStruct(baseConfig, baseProvider.Name())
		return cm.RegisterConfigStruct(sectionName, nestedStruct)
	} else {
		// Handle regular providers
		defaultConfig, err := provider.DefaultConfig()
		if err != nil {
			return err
		}

		// Determine the section path from ConfigKeys
		sectionPath := cm.deriveSectionPathFromConfigKeys(provider)

		// Extract the nested struct for the section path
		nestedStruct := cm.extractNestedStruct(defaultConfig, sectionPath)

		return cm.RegisterConfigStruct(sectionPath, nestedStruct)
	}
}

// extractNestedStruct extracts the nested struct that corresponds to the section name
// For example, from Config{HTTPClient: HTTPClientConfig{...}}, extract HTTPClientConfig for section "httpclient"
// Also handles dotted paths like "nv-http-client.shared-client"
func (cm *ConfigManager) extractNestedStruct(config interface{}, sectionName string) interface{} {
	// Split the section name by dots to handle nested paths
	parts := strings.Split(sectionName, ".")

	current := config

	// Walk through each part of the dotted path
	for _, part := range parts {
		current = cm.extractSingleLevel(current, part)
		if current == nil {
			return config // Return original if we can't find the path
		}
	}

	return current
}

// extractSingleLevel extracts one level of nesting from a struct
func (cm *ConfigManager) extractSingleLevel(config interface{}, tagName string) interface{} {
	configValue := reflect.ValueOf(config)

	// Handle pointer types
	for configValue.Kind() == reflect.Ptr {
		if configValue.IsNil() {
			return nil
		}
		configValue = configValue.Elem()
	}

	if configValue.Kind() != reflect.Struct {
		return nil
	}

	configType := configValue.Type()

	// Look for a field with koanf tag matching the tag name
	for i := 0; i < configValue.NumField(); i++ {
		field := configType.Field(i)
		fieldValue := configValue.Field(i)

		// Get koanf tag
		tag := field.Tag.Get("koanf")
		if tag == "" {
			continue
		}

		// Parse tag (remove options like "omitempty")
		koanfTag := strings.Split(tag, ",")[0]
		if koanfTag == tagName {
			return fieldValue.Interface()
		}
	}

	return nil // No matching field found
}

// deriveSectionPathFromConfigKeys determines the section path that services will use
// by analyzing the common prefix of ConfigKeys
func (cm *ConfigManager) deriveSectionPathFromConfigKeys(provider LibraryConfigProvider) string {
	keys := provider.ConfigKeys()
	if len(keys) == 0 {
		return provider.Name() // Fallback to provider name
	}

	// Find the common prefix of all keys (minus the last segment)
	// For example: ["nv-http-client.shared-client.max-idle-conns", "nv-http-client.shared-client.timeout-sec"]
	// Should return "nv-http-client.shared-client"

	firstKey := keys[0]
	parts := strings.Split(firstKey, ".")

	if len(parts) <= 1 {
		return provider.Name() // Single level, use provider name
	}

	// The section path is all parts except the last one (which is the field name)
	sectionParts := parts[:len(parts)-1]
	return strings.Join(sectionParts, ".")
}

// loadPluginDefaults loads default configuration from all registered plugins
func (cm *ConfigManager) loadPluginDefaults() error {
	defaults, err := cm.pluginManager.LoadDefaults()
	if err != nil {
		return err
	}

	if len(defaults) > 0 {
		if err := cm.koanf.Load(confmap.Provider(defaults, "."), nil); err != nil {
			return &ConfigError{
				Type:    ErrorTypeLoad,
				Message: "failed to load plugin defaults",
				Source:  "plugins",
				Cause:   err,
			}
		}
		cm.metadata.Sources["plugins"] = "plugin_defaults"
	}

	// Extract descriptions from plugin providers for CLI help text
	descriptions := cm.pluginManager.GetDescriptions()
	for key, desc := range descriptions {
		cm.configDescriptions[key] = desc
	}

	return nil
}

// loadDefaultYAML loads embedded default YAML configuration
func (cm *ConfigManager) loadDefaultYAML() error {
	if err := cm.koanf.Load(rawbytes.Provider([]byte(cm.defaultYAML)), yaml.Parser()); err != nil {
		return &ConfigError{
			Type:    ErrorTypeLoad,
			Message: "failed to load default YAML",
			Source:  "default_yaml",
			Cause:   err,
		}
	}
	cm.metadata.Sources["default_yaml"] = "embedded_yaml"
	return nil
}

// loadStandardConfigFiles loads the standard config files (config.yaml by default)
func (cm *ConfigManager) loadStandardConfigFiles() error {
	// Get config files from CLI flag if provided, otherwise use defaults
	configFiles := cm.getConfigFilesToLoad()

	if len(configFiles) == 0 {
		return nil // No config files to load
	}

	// Import the YAML provider creation function
	yamlProvider := providers.NewYAMLFileProvider(configFiles,
		providers.WithYAMLPriority(30),    // Between embedded YAML (20) and ENV (50)
		providers.WithYAMLRequired(false), // Don't error if files don't exist
	)

	// Load data from the YAML provider
	data, err := yamlProvider.Load()
	if err != nil {
		return &ConfigError{
			Type:    ErrorTypeLoad,
			Code:    "CFG_LOAD_002",
			Message: "Failed to load standard config files",
			Source:  "standard-config-files",
			Cause:   err,
			Suggestions: []string{
				"Check that config files exist and are readable",
				"Verify YAML syntax is correct",
				"Ensure file permissions allow reading",
			},
		}
	}

	// Merge the data
	if err := cm.mergeData(data, "standard-config-files"); err != nil {
		return err
	}

	return nil
}

// getConfigFilesToLoad determines which config files to load based on CLI flags or defaults
func (cm *ConfigManager) getConfigFilesToLoad() []string {
	// Check if CLI flag was provided
	if cm.cobraCmd != nil && cm.configFlag != "" {
		if flag := cm.cobraCmd.Flags().Lookup(cm.configFlag); flag != nil && flag.Changed {
			flagValue := flag.Value.String()
			if flagValue == "" {
				return []string{} // Empty string means disable config file loading
			}
			// Split comma-separated list and trim whitespace
			files := strings.Split(flagValue, ",")
			result := make([]string, 0, len(files))
			for _, file := range files {
				if trimmed := strings.TrimSpace(file); trimmed != "" {
					result = append(result, trimmed)
				}
			}
			return result
		}
	}

	// Use default config files
	return cm.standardConfigFiles
}

// generateCobraFlags automatically generates CLI flags from the current configuration structure
func (cm *ConfigManager) generateCobraFlags() error {
	// Add the --config flag for specifying config files
	if cm.configFlag != "" && cm.cobraCmd.Flags().Lookup(cm.configFlag) == nil {
		defaultValue := strings.Join(cm.standardConfigFiles, ",")
		cm.cobraCmd.Flags().String(cm.configFlag, defaultValue,
			"Comma-separated list of YAML configuration files to load")
	}

	// Add the --hot-config flag for specifying hot config files
	if cm.hotConfigFlag != "" && cm.cobraCmd.Flags().Lookup(cm.hotConfigFlag) == nil {
		defaultValue := strings.Join(cm.hotConfigFiles, ",")
		// Generate environment variable name using the same pattern as other flags
		envVarName := cm.configKeyToEnvVar(strings.ReplaceAll(cm.hotConfigFlag, "-", "."))
		helpText := fmt.Sprintf("Comma-separated list of YAML hot configuration files to watch for changes (e.g., secrets.yaml,overrides.yaml) (env: %s)", envVarName)
		cm.cobraCmd.Flags().String(cm.hotConfigFlag, defaultValue, helpText)
	}

	// Add the --hot-config-interval flag for setting reload interval
	intervalFlagName := cm.hotConfigFlag + "-interval"
	if cm.hotConfigFlag != "" && cm.cobraCmd.Flags().Lookup(intervalFlagName) == nil {
		// Generate environment variable name for the interval flag
		intervalConfigKey := strings.ReplaceAll(intervalFlagName, "-", ".")
		envVarName := cm.configKeyToEnvVar(intervalConfigKey)
		helpText := fmt.Sprintf("Polling interval for hot config file watcher (e.g., 15s, 1m) (env: %s)", envVarName)
		cm.cobraCmd.Flags().Duration(intervalFlagName, cm.reloadInterval, helpText)
	}

	// First, generate flags for loaded config values (with proper defaults)
	allKeys := cm.koanf.All()
	for key, value := range allKeys {
		flagName := cm.configKeyToFlag(key)
		envVarName := cm.configKeyToEnvVar(key)

		// Skip if flag already exists
		if cm.cobraCmd.Flags().Lookup(flagName) != nil {
			continue
		}

		// Store flag mapping for correct round-trip conversion
		cm.flagToConfigKey[flagName] = key

		// Generate help text with env var info
		helpText := ""
		if desc, exists := cm.configDescriptions[key]; exists {
			helpText = desc
		} else {
			helpText = fmt.Sprintf("Configuration for %s", key)
		}
		helpText += fmt.Sprintf(" (env: %s)", envVarName)

		// Add flag based on value type
		switch v := value.(type) {
		case string:
			cm.cobraCmd.Flags().String(flagName, v, helpText)
		case int:
			cm.cobraCmd.Flags().Int(flagName, v, helpText)
		case int64:
			cm.cobraCmd.Flags().Int64(flagName, v, helpText)
		case bool:
			cm.cobraCmd.Flags().Bool(flagName, v, helpText)
		case float64:
			cm.cobraCmd.Flags().Float64(flagName, v, helpText)
		default:
			// For complex types, convert to string
			cm.cobraCmd.Flags().String(flagName, fmt.Sprintf("%v", v), helpText)
		}
	}

	// Then, generate flags for any remaining config keys that weren't covered
	configKeys := cm.getAllConfigKeys()
	for _, configKey := range configKeys {
		// Generate flag name using inline conversion
		flagName := cm.configKeyToFlag(configKey)

		// Skip if flag already exists (already generated with defaults above)
		if cm.cobraCmd.Flags().Lookup(flagName) != nil {
			continue
		}

		// Store flag mapping for correct round-trip conversion
		cm.flagToConfigKey[flagName] = configKey

		// Generate help text with env var info
		envVarName := cm.configKeyToEnvVar(configKey)
		helpText := ""
		if desc, exists := cm.configDescriptions[configKey]; exists {
			helpText = desc
		} else {
			helpText = fmt.Sprintf("Configuration for %s", configKey)
		}
		helpText += fmt.Sprintf(" (env: %s)", envVarName)

		// Add string flag (simplified - all flags are strings for now)
		cm.cobraCmd.Flags().String(flagName, "", helpText)
	}

	return nil
}

// loadInitialDefaults loads plugin and service defaults for flag generation
func (cm *ConfigManager) loadInitialDefaults() error {
	// 1. Load plugin defaults first (lowest priority)
	if err := cm.loadPluginDefaults(); err != nil {
		return err
	}

	// 2. Load default YAML if provided
	if cm.defaultYAML != "" {
		if err := cm.loadDefaultYAML(); err != nil {
			return err
		}
	}

	return nil
}

// loadCobraFlags loads values from CLI flags into the configuration
func (cm *ConfigManager) loadCobraFlags() error {
	flagData := make(map[string]interface{})

	// Iterate through all flags and get their values
	cm.cobraCmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if flag.Changed {
			// Convert flag name back to config key using naming converter
			configKey := cm.flagNameToConfigKey(flag.Name)
			if configKey == "" {
				return // Skip unmapped flags
			}

			// Get the flag value based on its type
			switch flag.Value.Type() {
			case "string":
				if val, err := cm.cobraCmd.Flags().GetString(flag.Name); err == nil {
					flagData[configKey] = val
				}
			case "int":
				if val, err := cm.cobraCmd.Flags().GetInt(flag.Name); err == nil {
					flagData[configKey] = val
				}
			case "int64":
				if val, err := cm.cobraCmd.Flags().GetInt64(flag.Name); err == nil {
					flagData[configKey] = val
				}
			case "bool":
				if val, err := cm.cobraCmd.Flags().GetBool(flag.Name); err == nil {
					flagData[configKey] = val
				}
			case "float64":
				if val, err := cm.cobraCmd.Flags().GetFloat64(flag.Name); err == nil {
					flagData[configKey] = val
				}
			}
		}
	})

	// Load flag data into koanf if any flags were set
	if len(flagData) > 0 {
		if err := cm.koanf.Load(confmap.Provider(flagData, "."), nil); err != nil {
			return &ConfigError{
				Type:    ErrorTypeLoad,
				Message: "failed to load CLI flag values",
				Source:  "cli_flags",
				Cause:   err,
			}
		}
		cm.metadata.Sources["cli_flags"] = "cobra_flags"
	}

	return nil
}

// flagNameToConfigKey converts CLI flag name back to config key
func (cm *ConfigManager) flagNameToConfigKey(flagName string) string {
	// Skip the config flag itself
	if flagName == cm.configFlag {
		return ""
	}

	// Use lookup map for correct round-trip conversion
	if configKey, exists := cm.flagToConfigKey[flagName]; exists {
		return configKey
	}

	// If not found in map, return empty (skip unmapped flags)
	return ""
}

// generateEnvVarMappings generates mappings from environment variable names to config keys
// This ensures reliable round-trip conversion for complex nested config keys
func (cm *ConfigManager) generateEnvVarMappings() error {
	// Clear existing mappings
	cm.envVarToConfigKey = make(map[string]string)

	// Get all config keys from loaded configuration
	allKeys := cm.koanf.All()
	for configKey := range allKeys {
		envVarName := cm.configKeyToEnvVar(configKey)
		cm.envVarToConfigKey[envVarName] = configKey
	}

	// Also get keys from getAllConfigKeys() for comprehensive coverage
	configKeys := cm.getAllConfigKeys()
	for _, configKey := range configKeys {
		envVarName := cm.configKeyToEnvVar(configKey)
		cm.envVarToConfigKey[envVarName] = configKey
	}

	return nil
}

// generateFieldTypeMappings generates field type mappings for type-aware environment variable parsing
func (cm *ConfigManager) generateFieldTypeMappings() error {
	// Clear existing field type mappings
	cm.fieldTypeMappings = make(map[string]string)

	// Process each registered struct to extract field types
	for sectionName, structType := range cm.registeredStructs {
		cm.extractFieldTypesFromStruct(structType, sectionName, "")
	}

	return nil
}

// extractFieldTypesFromStruct recursively extracts field types from a struct
func (cm *ConfigManager) extractFieldTypesFromStruct(structType reflect.Type, basePath, currentPath string) {
	// Handle pointer types
	for structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}

	if structType.Kind() != reflect.Struct {
		return
	}

	// Iterate through struct fields
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		fieldType := field.Type

		// Get koanf tag
		koanfTag := field.Tag.Get("koanf")
		if koanfTag == "" || koanfTag == "-" {
			continue
		}

		// Parse tag (remove options like "omitempty")
		tagName := strings.Split(koanfTag, ",")[0]

		// Build the config path
		var configPath string
		if currentPath == "" {
			if basePath == "" {
				configPath = tagName
			} else {
				configPath = basePath + "." + tagName
			}
		} else {
			configPath = currentPath + "." + tagName
		}

		// Handle pointer types for field
		for fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		// Determine field type and store mapping
		switch fieldType.Kind() {
		case reflect.Slice:
			// Check if it's a slice of strings
			if fieldType.Elem().Kind() == reflect.String {
				cm.fieldTypeMappings[configPath] = "slice"
			}
		case reflect.String:
			cm.fieldTypeMappings[configPath] = "string"
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			cm.fieldTypeMappings[configPath] = "int"
		case reflect.Bool:
			cm.fieldTypeMappings[configPath] = "bool"
		case reflect.Map:
			// Handle map[string]SomeStruct - recurse into the struct type
			if fieldType.Key().Kind() == reflect.String {
				valueType := fieldType.Elem()
				// Handle pointer to struct in map value
				for valueType.Kind() == reflect.Ptr {
					valueType = valueType.Elem()
				}
				if valueType.Kind() == reflect.Struct {
					// Generate field types for the map struct fields with templated keys
					cm.generateFieldTypesForMapStruct(valueType, configPath)
				}
			}
		case reflect.Struct:
			// Recursively process nested structs
			cm.extractFieldTypesFromStruct(fieldType, basePath, configPath)
		}
	}
}

// generateFieldTypesForMapStruct generates field type mappings for fields within a map[string]struct
// This handles templated environment variables that create dynamic map entries
func (cm *ConfigManager) generateFieldTypesForMapStruct(structType reflect.Type, mapPath string) {
	// Handle pointer types
	for structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}

	if structType.Kind() != reflect.Struct {
		return
	}

	// Iterate through the struct fields to create field type mappings
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		fieldType := field.Type

		// Get koanf tag
		koanfTag := field.Tag.Get("koanf")
		if koanfTag == "" || koanfTag == "-" {
			continue
		}

		// Parse tag (remove options like "omitempty")
		tagName := strings.Split(koanfTag, ",")[0]

		// Create the templated config key pattern (with {key} placeholder)
		configKeyTemplate := mapPath + ".{key}." + tagName

		// Handle pointer types for field
		for fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		// Determine field type and store mapping for the template
		switch fieldType.Kind() {
		case reflect.Slice:
			// Check if it's a slice of strings
			if fieldType.Elem().Kind() == reflect.String {
				cm.fieldTypeMappings[configKeyTemplate] = "slice"
			}
		case reflect.String:
			cm.fieldTypeMappings[configKeyTemplate] = "string"
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			cm.fieldTypeMappings[configKeyTemplate] = "int"
		case reflect.Bool:
			cm.fieldTypeMappings[configKeyTemplate] = "bool"
		case reflect.Struct:
			// Handle nested structs within map values
			nestedPath := mapPath + ".{key}." + tagName
			cm.generateNestedFieldTypesForMapStruct(fieldType, nestedPath)
		}
	}
}

// generateNestedFieldTypesForMapStruct handles nested structs within map values
func (cm *ConfigManager) generateNestedFieldTypesForMapStruct(structType reflect.Type, basePath string) {
	// Handle pointer types
	for structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}

	if structType.Kind() != reflect.Struct {
		return
	}

	// Iterate through the struct fields to create field type mappings
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		fieldType := field.Type

		// Get koanf tag
		koanfTag := field.Tag.Get("koanf")
		if koanfTag == "" || koanfTag == "-" {
			continue
		}

		// Parse tag (remove options like "omitempty")
		tagName := strings.Split(koanfTag, ",")[0]

		// Create the templated config key pattern
		configKeyTemplate := basePath + "." + tagName

		// Handle pointer types for field
		for fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		// Determine field type and store mapping
		switch fieldType.Kind() {
		case reflect.Slice:
			// Check if it's a slice of strings
			if fieldType.Elem().Kind() == reflect.String {
				cm.fieldTypeMappings[configKeyTemplate] = "slice"
			}
		case reflect.String:
			cm.fieldTypeMappings[configKeyTemplate] = "string"
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			cm.fieldTypeMappings[configKeyTemplate] = "int"
		case reflect.Bool:
			cm.fieldTypeMappings[configKeyTemplate] = "bool"
		case reflect.Struct:
			// Recursively process nested structs
			nestedPath := basePath + "." + tagName
			cm.generateNestedFieldTypesForMapStruct(fieldType, nestedPath)
		}
	}
}

// updateEnvProviderMappings updates the environment provider with generated mappings
func (cm *ConfigManager) updateEnvProviderMappings() error {
	// Update the default env provider directly (we know it's an EnvProvider)
	if envProvider, ok := cm.envProvider.(*providers.EnvProvider); ok {
		envProvider.UpdateMappings(cm.envVarToConfigKey)
		envProvider.UpdateTemplatedPatterns(cm.templatedEnvVarPatterns)
		envProvider.UpdateFieldTypes(cm.fieldTypeMappings)
	}

	// Also check all providers in case there are additional env providers
	for _, provider := range cm.providers {
		if envProvider, ok := provider.(*providers.EnvProvider); ok {
			envProvider.UpdateMappings(cm.envVarToConfigKey)
			envProvider.UpdateTemplatedPatterns(cm.templatedEnvVarPatterns)
			envProvider.UpdateFieldTypes(cm.fieldTypeMappings)
		}
	}
	return nil
}

// envVarNameToConfigKey converts environment variable name back to config key
// Uses the mapping for reliable round-trip conversion
func (cm *ConfigManager) envVarNameToConfigKey(envVarName string) string {
	// Use lookup map for correct round-trip conversion
	if configKey, exists := cm.envVarToConfigKey[envVarName]; exists {
		return configKey
	}

	// No fallback needed - mappings should cover everything
	return ""
}

// GetEnvVarMappings returns the current environment variable to config key mappings
// This is useful for debugging and tooling
func (cm *ConfigManager) GetEnvVarMappings() map[string]string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Return copy to prevent external modification
	mappings := make(map[string]string)
	for envVar, configKey := range cm.envVarToConfigKey {
		mappings[envVar] = configKey
	}
	return mappings
}

// GetTemplatedEnvVarPatterns returns the current templated environment variable patterns
// This is useful for documentation and tooling to show users what dynamic env vars are available
func (cm *ConfigManager) GetTemplatedEnvVarPatterns() map[string]string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Return copy to prevent external modification
	patterns := make(map[string]string)
	for envVarPattern, configKeyTemplate := range cm.templatedEnvVarPatterns {
		patterns[envVarPattern] = configKeyTemplate
	}
	return patterns
}

// Context helper functions

type contextKey string

const configManagerKey contextKey = "config_manager"

// NewContextWithConfigManager creates a new context with the ConfigManager stored in it
func NewContextWithConfigManager(ctx context.Context, cm *ConfigManager) context.Context {
	return context.WithValue(ctx, configManagerKey, cm)
}

// ConfigManagerFromContext retrieves the ConfigManager from the context
func ConfigManagerFromContext(ctx context.Context) *ConfigManager {
	if ctx == nil {
		return nil
	}
	if cm, ok := ctx.Value(configManagerKey).(*ConfigManager); ok {
		return cm
	}
	return nil
}

// getAllConfigKeys returns all known config keys from library providers
func (cm *ConfigManager) getAllConfigKeys() []string {
	var keys []string

	// Get keys from library providers
	allProviders := cm.pluginManager.GetAllProviders()
	for _, provider := range allProviders {
		keys = append(keys, provider.ConfigKeys()...)
	}

	return keys
}

// configKeyToEnvVar converts config key to environment variable name
// Example: "http.max-retries" → "MYAPP_HTTP_MAX_RETRIES"
func (cm *ConfigManager) configKeyToEnvVar(configKey string) string {
	// Replace dots with underscores, hyphens with underscores, uppercase
	envVar := strings.ReplaceAll(configKey, ".", "_")
	envVar = strings.ReplaceAll(envVar, "-", "_")
	envVar = strings.ToUpper(envVar)

	if cm.envPrefix != "" {
		return strings.ToUpper(cm.envPrefix) + "_" + envVar
	}
	return envVar
}

// configKeyToFlag converts config key to CLI flag name
// Example: "http.max-retries" → "http-max-retries"
func (cm *ConfigManager) configKeyToFlag(configKey string) string {
	// Replace dots with hyphens, underscores with hyphens
	flag := strings.ReplaceAll(configKey, ".", "-")
	flag = strings.ReplaceAll(flag, "_", "-")
	return flag
}

// Hot Config Methods

// RegisterWatcher registers a ConfigWatcher to be notified of configuration changes
func (cm *ConfigManager) RegisterWatcher(watcher ConfigWatcher) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, section := range watcher.WatchedSections() {
		cm.configWatchers[section] = append(cm.configWatchers[section], watcher)
	}
}

// StartWatching starts watching hot config files for changes
func (cm *ConfigManager) StartWatching() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.watchingStarted {
		return nil // Already watching
	}

	// Apply hot config CLI flags if present
	cm.applyHotConfigFlags()

	// Get hot config files to watch
	hotConfigFiles := cm.getHotConfigFilesToWatch()
	if len(hotConfigFiles) == 0 {
		return nil // No hot config files to watch
	}

	// Check that all hot config files exist before starting watchers
	for _, filePath := range hotConfigFiles {
		if _, err := os.Stat(filePath); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("hot config file does not exist: %s (this is a misconfiguration - ensure the file exists or remove it from --hot-config)", filePath)
			}
			return fmt.Errorf("cannot access hot config file %s: %w", filePath, err)
		}
	}

	// Single file watcher for all hot config files
	watcher, err := NewFileWatcher(WithPollingInterval(cm.reloadInterval))
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	for _, filePath := range hotConfigFiles {
		if err := watcher.WatchFile(filePath, cm.reloadFromHotFile); err != nil {
			watcher.Stop()
			return fmt.Errorf("failed to watch hot config file %s: %w", filePath, err)
		}
	}

	cm.fileWatcher = watcher
	cm.watchingStarted = true
	return nil
}

// StopWatching stops watching all hot config files
func (cm *ConfigManager) StopWatching() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.watchingStarted {
		return nil // Not watching
	}

	var err error
	if cm.fileWatcher != nil {
		err = cm.fileWatcher.Stop()
		cm.fileWatcher = nil
	}
	cm.watchingStarted = false
	return err
}

// applyHotConfigFlags applies CLI flag values and environment variables for hot config settings
func (cm *ConfigManager) applyHotConfigFlags() {
	if cm.hotConfigFlag == "" {
		return
	}

	// Apply hot config files from environment variable if not set via CLI
	hotConfigEnvVar := cm.configKeyToEnvVar(strings.ReplaceAll(cm.hotConfigFlag, "-", "."))
	if envValue := os.Getenv(hotConfigEnvVar); envValue != "" {
		// Check if CLI flag was explicitly set
		flagSet := false
		if cm.cobraCmd != nil {
			if flag := cm.cobraCmd.Flags().Lookup(cm.hotConfigFlag); flag != nil && flag.Changed {
				flagSet = true
			}
		}

		// Only apply env var if CLI flag wasn't explicitly set
		if !flagSet {
			// Parse comma-separated files from environment variable
			files := strings.Split(envValue, ",")
			result := make([]string, 0, len(files))
			for _, file := range files {
				if trimmed := strings.TrimSpace(file); trimmed != "" {
					result = append(result, trimmed)
				}
			}
			cm.hotConfigFiles = result
		}
	}

	// Apply hot config interval from environment variable or CLI flag
	intervalFlagName := cm.hotConfigFlag + "-interval"
	intervalEnvVar := cm.configKeyToEnvVar(strings.ReplaceAll(intervalFlagName, "-", "."))

	// Check CLI flag first
	if cm.cobraCmd != nil {
		if flag := cm.cobraCmd.Flags().Lookup(intervalFlagName); flag != nil && flag.Changed {
			if interval, err := cm.cobraCmd.Flags().GetDuration(intervalFlagName); err == nil {
				cm.reloadInterval = interval
				return // CLI flag takes precedence
			}
		}
	}

	// Apply from environment variable if CLI flag wasn't set
	if envValue := os.Getenv(intervalEnvVar); envValue != "" {
		if interval, err := time.ParseDuration(envValue); err == nil {
			cm.reloadInterval = interval
		}
	}
}

// getHotConfigFilesToWatch determines which hot config files to watch
func (cm *ConfigManager) getHotConfigFilesToWatch() []string {
	// Check if CLI flag was provided
	if cm.cobraCmd != nil && cm.hotConfigFlag != "" {
		if flag := cm.cobraCmd.Flags().Lookup(cm.hotConfigFlag); flag != nil && flag.Changed {
			flagValue := flag.Value.String()
			if flagValue == "" {
				return []string{} // Empty string means disable hot config watching
			}
			// Split comma-separated list and trim whitespace
			files := strings.Split(flagValue, ",")
			result := make([]string, 0, len(files))
			for _, file := range files {
				if trimmed := strings.TrimSpace(file); trimmed != "" {
					result = append(result, trimmed)
				}
			}
			return result
		}
	}

	// Use default hot config files
	return cm.hotConfigFiles
}

// reloadFromHotFile handles reloading configuration from a changed hot config file
func (cm *ConfigManager) reloadFromHotFile(filePath string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Load the hot config file
	hotData, err := cm.loadYAMLFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to load hot config file %s: %w", filePath, err)
	}

	// Create a temporary koanf instance for validation
	tempKoanf := koanf.New(".")

	// Load current config into temp instance
	currentData := cm.koanf.All()
	if err := tempKoanf.Load(confmap.Provider(currentData, "."), nil); err != nil {
		return fmt.Errorf("failed to load current config for validation: %w", err)
	}

	// Merge hot config into temp instance
	for key, value := range hotData {
		tempKoanf.Set(key, value)
	}

	// Validate the merged configuration
	if err := cm.validateMergedConfig(tempKoanf, hotData); err != nil {
		return fmt.Errorf("hot config validation failed for %s: %w", filePath, err)
	}

	// Log hot config reload
	keys := make([]string, 0, len(hotData))
	for key := range hotData {
		keys = append(keys, key)
	}
	fmt.Printf("Hot config reload: %s updated keys: %v\n", filepath.Base(filePath), keys)

	// If validation passes, apply to live config
	oldData := cm.koanf.All()
	for key, value := range hotData {
		cm.koanf.Set(key, value)
	}
	newData := cm.koanf.All()

	// Update metadata
	cm.metadata.LoadTime = time.Now()
	for key := range hotData {
		cm.metadata.Sources[key] = fmt.Sprintf("hot_config:%s", filepath.Base(filePath))
	}

	// Notify watchers
	if err := cm.notifyConfigWatchers(hotData, oldData, newData); err != nil {
		// Log error but don't fail the reload
		fmt.Printf("Error notifying config watchers: %v\n", err)
	}

	// Call global reload callback if set
	if cm.reloadCallback != nil {
		if err := cm.reloadCallback(oldData, newData); err != nil {
			// Log error but don't fail the reload
			fmt.Printf("Error in reload callback: %v\n", err)
		}
	}

	return nil
}

// ProvidersWithAliases converts ProviderWithAlias slice to LibraryConfigProvider slice
// This is the main API for users with NewService
func ProvidersWithAliases(providers ...ProviderWithAlias) []LibraryConfigProvider {
	result := make([]LibraryConfigProvider, len(providers))
	for i, p := range providers {
		if p.Alias != "" {
			result[i] = NewAliasedProvider(p.Provider, p.Alias)
		} else {
			result[i] = p.Provider
		}
	}
	return result
}

// Hot Config Helper Methods

// loadInitialHotConfig loads hot config files during initial Load() for validation
func (cm *ConfigManager) loadInitialHotConfig() error {
	// Get hot config files (applies CLI flags and env vars)
	cm.applyHotConfigFlags()
	hotConfigFiles := cm.getHotConfigFilesToWatch()

	if len(hotConfigFiles) == 0 {
		return nil // No hot config files to load
	}

	// Initialize hot config tracking
	cm.hotConfigData = make(map[string]interface{})

	// Load each hot config file
	for _, filePath := range hotConfigFiles {
		// Check file exists (fail fast like StartWatching does)
		if _, err := os.Stat(filePath); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("hot config file does not exist: %s (ensure the file exists or remove it from --hot-config)", filePath)
			}
			return fmt.Errorf("cannot access hot config file %s: %w", filePath, err)
		}

		// Load the YAML file
		hotData, err := cm.loadYAMLFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to load hot config file %s: %w", filePath, err)
		}

		// Track what came from hot config (for ExportYAML exclusion)
		for key, value := range hotData {
			cm.hotConfigData[key] = value
		}

		// Merge into main config using standard merge process with highest precedence
		sourceName := fmt.Sprintf("hot_config:%s", filepath.Base(filePath))
		if err := cm.mergeData(hotData, sourceName); err != nil {
			return fmt.Errorf("failed to merge hot config file %s: %w", filePath, err)
		}
	}

	cm.initialHotLoaded = true
	return nil
}

// loadYAMLFile loads a YAML file and returns the parsed data
func (cm *ConfigManager) loadYAMLFile(filePath string) (map[string]interface{}, error) {
	// Use the existing YAML provider for consistent behavior
	yamlProvider := providers.NewYAMLFileProvider([]string{filePath},
		providers.WithYAMLRequired(true), // Hot config files should exist
	)

	return yamlProvider.Load()
}

// validateMergedConfig validates the merged configuration against registered structs
func (cm *ConfigManager) validateMergedConfig(tempKoanf *koanf.Koanf, hotData map[string]interface{}) error {
	// Find which sections are affected by the hot config changes
	affectedSections := cm.findAffectedSections(hotData)

	// Validate only affected sections (more efficient than full validation)
	for _, section := range affectedSections {
		if structType, exists := cm.registeredStructs[section]; exists {
			// Create new instance of the registered struct
			structInstance := reflect.New(structType).Interface()

			// Try unmarshaling the section with merged data
			if err := tempKoanf.Unmarshal(section, structInstance); err != nil {
				return &ConfigError{
					Type:    ErrorTypeValidation,
					Code:    "CFG_HOT_RELOAD_VALIDATION_001",
					Message: fmt.Sprintf("Hot config validation failed for section '%s'", section),
					Source:  "hot_config",
					Cause:   err,
					Suggestions: []string{
						"Check hot config file values match expected types",
						"Verify hot config file structure is correct",
						fmt.Sprintf("Review section '%s' in hot config file", section),
					},
				}
			}

			// Call Validate() method if it exists
			if err := cm.callValidateMethod(structInstance, section); err != nil {
				return &ConfigError{
					Type:    ErrorTypeValidation,
					Code:    "CFG_HOT_RELOAD_BUSINESS_VALIDATION_001",
					Message: fmt.Sprintf("Hot config business validation failed for section '%s'", section),
					Source:  "hot_config",
					Cause:   err,
				}
			}
		}
	}

	return nil
}

// findAffectedSections determines which config sections are impacted by hot config changes
// This handles both regular and aliased provider sections
func (cm *ConfigManager) findAffectedSections(hotData map[string]interface{}) []string {
	sectionSet := make(map[string]bool)

	for key := range hotData {
		// Extract section from dotted key (e.g., "database.password" -> "database")
		parts := strings.Split(key, ".")
		if len(parts) > 0 {
			section := parts[0]
			sectionSet[section] = true

			// Also check for nested sections (e.g., "nv-http-client.shared-client", "api-client.timeout")
			for i := 1; i < len(parts); i++ {
				nestedSection := strings.Join(parts[:i+1], ".")
				if _, exists := cm.registeredStructs[nestedSection]; exists {
					sectionSet[nestedSection] = true
				}
			}
		}
	}

	sections := make([]string, 0, len(sectionSet))
	for section := range sectionSet {
		sections = append(sections, section)
	}

	return sections
}

// notifyConfigWatchers notifies all registered config watchers of changes
func (cm *ConfigManager) notifyConfigWatchers(hotData, oldData, newData map[string]interface{}) error {
	affectedSections := cm.findAffectedSections(hotData)

	var errors []error
	for _, section := range affectedSections {
		if watchers, exists := cm.configWatchers[section]; exists {
			fmt.Printf("Hot config notifying %d watcher(s) for section '%s'\n", len(watchers), section)

			// Get old and new values for this section
			oldValue := cm.getSectionData(oldData, section)
			newValue := cm.getSectionData(newData, section)

			// Notify each watcher
			for _, watcher := range watchers {
				if err := watcher.OnConfigChange(section, oldValue, newValue); err != nil {
					errors = append(errors, fmt.Errorf("watcher notification failed for section %s: %w", section, err))
				}
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("config watcher notification errors: %v", errors)
	}

	return nil
}

// getSectionData extracts data for a specific section from the full config data
func (cm *ConfigManager) getSectionData(data map[string]interface{}, section string) interface{} {
	// Create a temporary koanf instance to extract section data
	tempKoanf := koanf.New(".")
	if err := tempKoanf.Load(confmap.Provider(data, "."), nil); err != nil {
		return nil
	}

	return tempKoanf.Get(section)
}

// generateTemplatedEnvVarPatterns generates templated environment variable patterns for map[string] fields
func (cm *ConfigManager) generateTemplatedEnvVarPatterns() error {
	// Clear existing templated patterns
	cm.templatedEnvVarPatterns = make(map[string]string)

	// Process each registered struct to find map[string] fields
	for sectionName, structType := range cm.registeredStructs {
		cm.extractTemplatedPatternsFromStruct(structType, sectionName, "")
	}

	return nil
}

// extractTemplatedPatternsFromStruct recursively extracts templated patterns from a struct type
func (cm *ConfigManager) extractTemplatedPatternsFromStruct(structType reflect.Type, basePath, currentPath string) {
	// Handle pointer types
	for structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}

	if structType.Kind() != reflect.Struct {
		return
	}

	// Iterate through struct fields
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		fieldType := field.Type

		// Get koanf tag
		koanfTag := field.Tag.Get("koanf")
		if koanfTag == "" || koanfTag == "-" {
			continue
		}

		// Parse tag (remove options like "omitempty")
		tagName := strings.Split(koanfTag, ",")[0]

		// Build the config path
		var configPath string
		if currentPath == "" {
			if basePath == "" {
				configPath = tagName
			} else {
				configPath = basePath + "." + tagName
			}
		} else {
			configPath = currentPath + "." + tagName
		}

		// Handle pointer types for field
		for fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		// Check if this is a map[string]SomeStruct
		if fieldType.Kind() == reflect.Map && fieldType.Key().Kind() == reflect.String {
			valueType := fieldType.Elem()

			// Handle pointer to struct in map value
			for valueType.Kind() == reflect.Ptr {
				valueType = valueType.Elem()
			}

			if valueType.Kind() == reflect.Struct {
				// This is a map[string]struct - generate templated patterns for its fields
				cm.generateTemplatedPatternsForMapStruct(valueType, configPath)
			}
		} else if fieldType.Kind() == reflect.Struct {
			// Recursively process nested structs
			cm.extractTemplatedPatternsFromStruct(fieldType, basePath, configPath)
		}
	}
}

// generateTemplatedPatternsForMapStruct generates templated patterns for fields within a map[string]struct
func (cm *ConfigManager) generateTemplatedPatternsForMapStruct(structType reflect.Type, mapConfigPath string) {
	// Iterate through the struct fields to create templated patterns
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		fieldType := field.Type

		// Get koanf tag
		koanfTag := field.Tag.Get("koanf")
		if koanfTag == "" || koanfTag == "-" {
			continue
		}

		// Parse tag (remove options like "omitempty")
		tagName := strings.Split(koanfTag, ",")[0]

		// Create the templated config key pattern
		// Example: "serviceauth.oauthdistributed.targets.{key}.issuerurl"
		configKeyTemplate := mapConfigPath + ".{key}." + tagName

		// Generate the templated env var pattern
		// Example: "MYAPP_SERVICEAUTH_OAUTHDISTRIBUTED_TARGETS_{KEY}_ISSUERURL"
		envVarPattern := cm.configKeyToEnvVarPattern(configKeyTemplate)

		// Store the pattern mapping
		cm.templatedEnvVarPatterns[envVarPattern] = configKeyTemplate

		// Handle nested structs within map values
		for fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		if fieldType.Kind() == reflect.Struct {
			// Recursively process nested structs within the map value
			// Use a different method to avoid generating {key} patterns for nested structs
			nestedConfigPath := mapConfigPath + ".{key}." + tagName
			cm.generateNestedStructPatterns(fieldType, nestedConfigPath)
		}
	}
}

// generateNestedStructPatterns generates templated patterns for nested structs within map values
// This handles cases like credentials struct within the OAuth target struct
func (cm *ConfigManager) generateNestedStructPatterns(structType reflect.Type, basePath string) {
	// Iterate through the struct fields to create templated patterns
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		fieldType := field.Type

		// Get koanf tag
		koanfTag := field.Tag.Get("koanf")
		if koanfTag == "" || koanfTag == "-" {
			continue
		}

		// Parse tag (remove options like "omitempty")
		tagName := strings.Split(koanfTag, ",")[0]

		// Create the templated config key pattern
		configKeyTemplate := basePath + "." + tagName

		// Generate the templated env var pattern
		envVarPattern := cm.configKeyToEnvVarPattern(configKeyTemplate)

		// Store the pattern mapping
		cm.templatedEnvVarPatterns[envVarPattern] = configKeyTemplate

		// Handle nested structs recursively
		for fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}

		if fieldType.Kind() == reflect.Struct {
			// Recursively process nested structs
			nestedPath := basePath + "." + tagName
			cm.generateNestedStructPatterns(fieldType, nestedPath)
		}
	}
}

// configKeyToEnvVarPattern converts a config key template to an env var pattern
// Example: "serviceauth.oauthdistributed.targets.{key}.issuerurl" → "MYAPP_SERVICEAUTH_OAUTHDISTRIBUTED_TARGETS_{KEY}_ISSUERURL"
func (cm *ConfigManager) configKeyToEnvVarPattern(configKeyTemplate string) string {
	// Replace dots with underscores, hyphens with underscores, uppercase
	envVar := strings.ReplaceAll(configKeyTemplate, ".", "_")
	envVar = strings.ReplaceAll(envVar, "-", "_")
	envVar = strings.ToUpper(envVar)

	// Replace {key} with {KEY} for consistency
	envVar = strings.ReplaceAll(envVar, "{KEY}", "{KEY}")
	envVar = strings.ReplaceAll(envVar, "{key}", "{KEY}")

	if cm.envPrefix != "" {
		return strings.ToUpper(cm.envPrefix) + "_" + envVar
	}
	return envVar
}
