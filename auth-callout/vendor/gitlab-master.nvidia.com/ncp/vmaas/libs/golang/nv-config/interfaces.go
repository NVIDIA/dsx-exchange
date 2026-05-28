package config

import (
	"strings"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

// LibraryConfigProvider defines the interface that external libraries must implement
// to integrate with the configuration system and provide their default configurations.
//
// Libraries implementing this interface can be automatically registered with ConfigManager
// and will have their defaults merged into the global configuration. The config system
// will also generate CLI flags and handle environment variable mapping automatically.
//
// Example implementation:
//
//	type HTTPClientProvider struct{}
//
//	func (p *HTTPClientProvider) Name() string {
//		return "httpclient"
//	}
//
//	func (p *HTTPClientProvider) DefaultConfig() (interface{}, error) {
//		// Return struct with default values and koanf tags
//		return &HTTPClientConfig{
//			Timeout: "30s",
//			Retries: 3,
//		}, nil
//	}
//
//	func (p *HTTPClientProvider) ConfigKeys() []string {
//		return []string{"httpclient.timeout", "httpclient.retries"}
//	}
//
// See examples/library/ for complete working examples.
type LibraryConfigProvider interface {
	// Name returns the unique name of this configuration provider
	Name() string

	// DefaultConfig returns the default configuration structure for this provider
	// This should typically unmarshal embedded YAML into a struct
	DefaultConfig() (interface{}, error)

	// ConfigKeys returns all configuration keys that this provider uses
	// This is used for conflict detection between providers
	ConfigKeys() []string
}

// PluginManager manages registered configuration providers
type PluginManager struct {
	providers    map[string]LibraryConfigProvider
	keyMap       map[string]string // maps config key to provider name
	descriptions map[string]string // maps config key to description
}

// NewPluginManager creates a new plugin manager
func NewPluginManager() *PluginManager {
	return &PluginManager{
		providers:    make(map[string]LibraryConfigProvider),
		keyMap:       make(map[string]string),
		descriptions: make(map[string]string),
	}
}

// RegisterProvider registers a configuration provider
func (pm *PluginManager) RegisterProvider(provider LibraryConfigProvider) error {
	name := provider.Name()

	// Check if provider name is already registered
	if _, exists := pm.providers[name]; exists {
		return &ConfigError{
			Type:    ErrorTypeProviderConflict,
			Message: "provider name already registered: " + name,
			Source:  "plugin_manager",
		}
	}

	// Check for configuration key conflicts
	keys := provider.ConfigKeys()
	for _, key := range keys {
		if existingProvider, exists := pm.keyMap[key]; exists {
			return &ConfigError{
				Type:    ErrorTypeKeyConflict,
				Message: "configuration key conflict between providers: " + key + " (existing: " + existingProvider + ", new: " + name + ")",
				Source:  "plugin_manager",
			}
		}
	}

	// Register the provider
	pm.providers[name] = provider

	// Register all its keys
	for _, key := range keys {
		pm.keyMap[key] = name
	}

	// Extract descriptions from the provider
	var descriptions map[string]string
	if aliasedProvider, ok := provider.(*AliasedProvider); ok {
		// For aliased providers, use the pre-transformed descriptions
		descriptions = aliasedProvider.aliasedDescriptions
	} else {
		// For regular providers, extract descriptions from DefaultConfig
		if defaultConfig, err := provider.DefaultConfig(); err == nil && defaultConfig != nil {
			descriptions = ExtractDescriptionsWithPrefix(defaultConfig, "")
		}
	}

	// Store the descriptions
	for key, desc := range descriptions {
		pm.descriptions[key] = desc
	}

	return nil
}

// GetProvider returns a registered provider by name
func (pm *PluginManager) GetProvider(name string) (LibraryConfigProvider, bool) {
	provider, exists := pm.providers[name]
	return provider, exists
}

// GetAllProviders returns all registered providers
func (pm *PluginManager) GetAllProviders() map[string]LibraryConfigProvider {
	// Return a copy to prevent external modification
	result := make(map[string]LibraryConfigProvider)
	for name, provider := range pm.providers {
		result[name] = provider
	}
	return result
}

// GetDescriptions returns all config key descriptions extracted from providers
func (pm *PluginManager) GetDescriptions() map[string]string {
	// Return a copy to prevent external modification
	result := make(map[string]string)
	for key, desc := range pm.descriptions {
		result[key] = desc
	}
	return result
}

// LoadDefaults loads default configuration from all registered providers
func (pm *PluginManager) LoadDefaults() (map[string]interface{}, error) {
	merged := make(map[string]interface{})

	for name, provider := range pm.providers {
		defaults, err := provider.DefaultConfig()
		if err != nil {
			return nil, &ConfigError{
				Type:    ErrorTypeProvider,
				Message: "failed to load provider defaults: " + name,
				Source:  "plugin_manager",
				Cause:   err,
			}
		}

		// Merge the defaults into the result
		if err := mergeConfig(merged, defaults); err != nil {
			return nil, &ConfigError{
				Type:    ErrorTypeMerge,
				Message: "failed to merge provider defaults: " + name,
				Source:  "plugin_manager",
				Cause:   err,
			}
		}
	}

	return merged, nil
}

// mergeConfig merges source configuration into target using koanf's built-in merging
func mergeConfig(target map[string]interface{}, source interface{}) error {
	// Convert source to map[string]interface{} if it's a struct
	sourceMap, err := structToMap(source)
	if err != nil {
		return err
	}

	// Use koanf's built-in merging by creating temporary koanf instances
	// This leverages koanf's proven merge logic which handles nested structures properly
	targetKoanf := koanf.New(".")
	sourceKoanf := koanf.New(".")

	// Load existing target data
	if err := targetKoanf.Load(confmap.Provider(target, "."), nil); err != nil {
		return err
	}

	// Load source data and merge it
	if err := sourceKoanf.Load(confmap.Provider(sourceMap, "."), nil); err != nil {
		return err
	}

	// Merge source into target using koanf's merge capability
	if err := targetKoanf.Merge(sourceKoanf); err != nil {
		return err
	}

	// Copy merged result back to target
	merged := targetKoanf.All()
	for key, value := range merged {
		target[key] = value
	}

	return nil
}

// ProviderWithAlias represents a provider with an optional alias
type ProviderWithAlias struct {
	Provider LibraryConfigProvider
	Alias    string // If empty, uses provider's default prefix
}

// AliasedProvider wraps a LibraryConfigProvider with a custom prefix
type AliasedProvider struct {
	baseProvider        LibraryConfigProvider
	alias               string
	originalPrefix      string
	aliasedKeys         []string
	aliasedDescriptions map[string]string
}

// NewAliasedProvider creates a provider wrapper with a custom alias
func NewAliasedProvider(baseProvider LibraryConfigProvider, alias string) *AliasedProvider {
	originalKeys := baseProvider.ConfigKeys()

	// Extract original prefix from first key (e.g., "httpclient.timeout" -> "httpclient")
	var originalPrefix string
	if len(originalKeys) > 0 {
		parts := strings.Split(originalKeys[0], ".")
		if len(parts) > 1 {
			originalPrefix = parts[0]
		}
	}

	// Transform all keys: "httpclient.timeout" -> "api-client.timeout"
	aliasedKeys := make([]string, len(originalKeys))
	for i, key := range originalKeys {
		aliasedKeys[i] = strings.Replace(key, originalPrefix, alias, 1)
	}

	// Extract and transform descriptions from the base provider
	aliasedDescriptions := make(map[string]string)
	if defaultConfig, err := baseProvider.DefaultConfig(); err == nil && defaultConfig != nil {
		baseDescriptions := ExtractDescriptionsWithPrefix(defaultConfig, "")
		for key, desc := range baseDescriptions {
			// Transform description keys to use alias prefix
			aliasedKey := strings.Replace(key, originalPrefix, alias, 1)
			aliasedDescriptions[aliasedKey] = desc
		}
	}

	return &AliasedProvider{
		baseProvider:        baseProvider,
		alias:               alias,
		originalPrefix:      originalPrefix,
		aliasedKeys:         aliasedKeys,
		aliasedDescriptions: aliasedDescriptions,
	}
}

func (ap *AliasedProvider) Name() string {
	return ap.alias
}

func (ap *AliasedProvider) ConfigKeys() []string {
	return ap.aliasedKeys
}

func (ap *AliasedProvider) DefaultConfig() (interface{}, error) {
	baseConfig, err := ap.baseProvider.DefaultConfig()
	if err != nil {
		return nil, err
	}

	// Transform config struct to use alias prefix
	return transformConfigPrefix(baseConfig, ap.originalPrefix, ap.alias)
}

// GetBaseProvider returns the underlying base provider (for struct type extraction)
func (ap *AliasedProvider) GetBaseProvider() LibraryConfigProvider {
	return ap.baseProvider
}
