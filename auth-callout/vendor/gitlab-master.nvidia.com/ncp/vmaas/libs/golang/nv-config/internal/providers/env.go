package providers

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// EnvProvider loads configuration from environment variables
type EnvProvider struct {
	prefix            string
	priority          int
	caseSensitive     bool
	mappings          map[string]string // env var name -> config key mapping
	templatedPatterns map[string]string // env var pattern -> config key template mapping
	fieldTypes        map[string]string // config key -> field type mapping (e.g., "slice", "string", "int")
}

// EnvProviderOption defines functional options for EnvProvider
type EnvProviderOption func(*EnvProvider)

// NewEnvProvider creates a new environment variable provider
func NewEnvProvider(opts ...EnvProviderOption) *EnvProvider {
	p := &EnvProvider{
		prefix:            "",
		priority:          50, // Medium priority by default
		caseSensitive:     false,
		mappings:          make(map[string]string),
		templatedPatterns: make(map[string]string),
		fieldTypes:        make(map[string]string),
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// WithEnvPrefix sets the environment variable prefix
// The library automatically adds an underscore separator, so users should provide
// just the logical prefix (e.g., "MYSERVICE" becomes "MYSERVICE_")
func WithEnvPrefix(prefix string) EnvProviderOption {
	return func(p *EnvProvider) {
		if prefix != "" {
			// Automatically add underscore separator for non-empty prefixes
			p.prefix = prefix + "_"
		} else {
			p.prefix = ""
		}
	}
}

// WithEnvPriority sets the provider priority
func WithEnvPriority(priority int) EnvProviderOption {
	return func(p *EnvProvider) {
		p.priority = priority
	}
}

// WithEnvCaseSensitive enables case-sensitive environment variable matching
func WithEnvCaseSensitive(enabled bool) EnvProviderOption {
	return func(p *EnvProvider) {
		p.caseSensitive = enabled
	}
}

// WithEnvMappings sets custom environment variable to config key mappings
func WithEnvMappings(mappings map[string]string) EnvProviderOption {
	return func(p *EnvProvider) {
		for envVar, configKey := range mappings {
			p.mappings[envVar] = configKey
		}
	}
}

// Name returns the provider name
func (p *EnvProvider) Name() string {
	if p.prefix != "" {
		return fmt.Sprintf("env-%s", strings.ToLower(p.prefix))
	}
	return "env"
}

// Priority returns the provider priority
func (p *EnvProvider) Priority() int {
	return p.priority
}

// Load loads configuration from environment variables
func (p *EnvProvider) Load() (map[string]interface{}, error) {
	data := make(map[string]interface{})

	// Get all environment variables
	envVars := os.Environ()

	for _, envVar := range envVars {
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) != 2 {
			continue
		}

		envName := parts[0]
		envValue := parts[1]

		// Check if this env var matches our criteria
		configKey := p.getConfigKey(envName)
		if configKey == "" {
			continue
		}

		// Parse the value to appropriate type (type-aware)
		parsedValue := p.parseValue(envValue, configKey)

		// Set nested keys using dot notation with special handling for templated keys
		p.setNestedValueWithMapSupport(data, configKey, parsedValue)
	}

	return data, nil
}

// getConfigKey determines if an environment variable should be included
// and returns the corresponding configuration key
func (p *EnvProvider) getConfigKey(envName string) string {
	// 1. Check mappings first (highest priority)
	if configKey, exists := p.mappings[envName]; exists {
		return configKey
	}

	// 2. Check templated patterns for dynamic map keys
	if configKey := p.matchTemplatedPattern(envName); configKey != "" {
		return configKey
	}

	// 3. Fallback to simple conversion for testing and backward compatibility
	return p.simpleEnvVarToConfigKey(envName)
}

// simpleEnvVarToConfigKey provides basic env var to config key conversion
// This is a simplified version of what the naming converter did
func (p *EnvProvider) simpleEnvVarToConfigKey(envName string) string {
	// Remove prefix if present
	if p.prefix != "" {
		var hasPrefix bool
		if p.caseSensitive {
			hasPrefix = strings.HasPrefix(envName, p.prefix)
		} else {
			hasPrefix = strings.HasPrefix(strings.ToUpper(envName), strings.ToUpper(p.prefix))
		}
		if !hasPrefix {
			return "" // Doesn't match prefix
		}
		envName = envName[len(p.prefix):]
	}

	// Convert to lowercase and create config key
	configKey := strings.ToLower(envName)

	// Simple conversion: HTTP_MAX_RETRIES → http.max_retries
	// Replace first underscore with dot to create section.key format
	parts := strings.Split(configKey, "_")
	if len(parts) >= 2 {
		section := parts[0]
		remaining := strings.Join(parts[1:], "_")
		configKey = section + "." + remaining
	}
	// Single part: DEBUG → debug (keep flat)

	return configKey
}

// parseValue attempts to parse environment variable values to appropriate Go types
// Now supports type-aware parsing based on target struct field types
func (p *EnvProvider) parseValue(value string, configKey string) interface{} {
	// Check if we have type information for this config key
	if fieldType, exists := p.fieldTypes[configKey]; exists {
		switch fieldType {
		case "slice":
			// Convert comma-separated string to slice for slice fields
			return p.parseSliceValue(value)
		case "string":
			// Keep as string (even if it contains commas)
			return value
		default:
			// For other types (int, bool, etc.), return as string and let koanf handle conversion
			return value
		}
	}

	// Check if this config key matches a templated pattern
	if fieldType := p.getFieldTypeFromTemplate(configKey); fieldType != "" {
		switch fieldType {
		case "slice":
			// Convert comma-separated string to slice for slice fields
			return p.parseSliceValue(value)
		case "string":
			// Keep as string (even if it contains commas)
			return value
		default:
			// For other types (int, bool, etc.), return as string and let koanf handle conversion
			return value
		}
	}

	// Fallback: return as string if no type information available
	// Type conversion should be handled by koanf during unmarshal based on target struct field types
	// This prevents conflicts where the provider guesses wrong about the expected type
	return value
}

// getFieldTypeFromTemplate checks if a config key matches any templated field type patterns
func (p *EnvProvider) getFieldTypeFromTemplate(configKey string) string {
	// Check all field type patterns for template matches
	for templatePattern, fieldType := range p.fieldTypes {
		if strings.Contains(templatePattern, "{key}") {
			// This is a template pattern, check if the config key matches
			if p.matchesTemplate(configKey, templatePattern) {
				return fieldType
			}
		}
	}
	return ""
}

// matchesTemplate checks if a config key matches a template pattern
// Example: "serviceauth.oauthdistributed.targets.newtarget.scopes" matches "serviceauth.oauthdistributed.targets.{key}.scopes"
func (p *EnvProvider) matchesTemplate(configKey, template string) bool {
	// Split both the config key and template by dots
	keyParts := strings.Split(configKey, ".")
	templateParts := strings.Split(template, ".")

	// They must have the same number of parts
	if len(keyParts) != len(templateParts) {
		return false
	}

	// Check each part
	for i, templatePart := range templateParts {
		if templatePart == "{key}" {
			// This part can be anything, skip
			continue
		}
		if keyParts[i] != templatePart {
			// Parts must match exactly
			return false
		}
	}

	return true
}

// parseSliceValue converts a comma-separated string to a slice of strings
func (p *EnvProvider) parseSliceValue(value string) []string {
	// Handle empty string
	if strings.TrimSpace(value) == "" {
		return []string{}
	}

	// Split by comma and trim whitespace from each element
	parts := strings.Split(value, ",")
	result := make([]string, len(parts))
	for i, part := range parts {
		result[i] = strings.TrimSpace(part)
	}

	return result
}

// GetEnvWithDefault gets an environment variable with a default value
func GetEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetEnvAsBool gets an environment variable as a boolean
func GetEnvAsBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	switch strings.ToLower(value) {
	case "true", "yes", "on", "1":
		return true
	case "false", "no", "off", "0":
		return false
	default:
		return defaultValue
	}
}

// GetEnvAsInt gets an environment variable as an integer
func GetEnvAsInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	if intVal, err := strconv.Atoi(value); err == nil {
		return intVal
	}

	return defaultValue
}

// GetEnvAsDuration gets an environment variable as a time.Duration
func GetEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	if durVal, err := time.ParseDuration(value); err == nil {
		return durVal
	}

	return defaultValue
}

// UpdateMappings updates the environment variable to config key mappings
// This is used by ConfigManager to provide generated mappings for reliable round-trip conversion
func (p *EnvProvider) UpdateMappings(mappings map[string]string) {
	// Replace existing mappings with new ones
	p.mappings = make(map[string]string)
	for envVar, configKey := range mappings {
		p.mappings[envVar] = configKey
	}
}

// UpdateTemplatedPatterns updates the templated environment variable patterns
// This is used by ConfigManager to provide generated templated patterns for dynamic map keys
func (p *EnvProvider) UpdateTemplatedPatterns(patterns map[string]string) {
	// Replace existing templated patterns with new ones
	p.templatedPatterns = make(map[string]string)
	for envVarPattern, configKeyTemplate := range patterns {
		p.templatedPatterns[envVarPattern] = configKeyTemplate
	}
}

// UpdateFieldTypes updates the field type information for config keys
// This is used by ConfigManager to provide type information for type-aware parsing
func (p *EnvProvider) UpdateFieldTypes(fieldTypes map[string]string) {
	// Replace existing field types with new ones
	p.fieldTypes = make(map[string]string)
	for configKey, fieldType := range fieldTypes {
		p.fieldTypes[configKey] = fieldType
	}
}

// matchTemplatedPattern attempts to match an environment variable name against templated patterns
// and returns the rendered config key if a match is found
func (p *EnvProvider) matchTemplatedPattern(envName string) string {
	for envVarPattern, configKeyTemplate := range p.templatedPatterns {
		if extractedKey := p.ExtractKeyFromPattern(envName, envVarPattern); extractedKey != "" {
			// Replace {key} in the config key template with the extracted key
			// Convert underscores to hyphens for the key (env vars use underscores, config keys use hyphens)
			configKey := strings.ReplaceAll(configKeyTemplate, "{key}", strings.ToLower(strings.ReplaceAll(extractedKey, "_", "-")))
			return configKey
		}
	}
	return ""
}

// ExtractKeyFromPattern extracts the dynamic key from an environment variable name using a pattern
// Example: envName="MYAPP_SERVICEAUTH_OAUTHDISTRIBUTED_TARGETS_SELF_ISSUERURL"
//
//	pattern="MYAPP_SERVICEAUTH_OAUTHDISTRIBUTED_TARGETS_{KEY}_ISSUERURL"
//	returns="SELF"
func (p *EnvProvider) ExtractKeyFromPattern(envName, pattern string) string {
	// Find the position of {KEY} in the pattern
	keyPlaceholder := "{KEY}"
	keyIndex := strings.Index(pattern, keyPlaceholder)
	if keyIndex == -1 {
		return ""
	}

	// Split the pattern into prefix and suffix around {KEY}
	prefix := pattern[:keyIndex]
	suffix := pattern[keyIndex+len(keyPlaceholder):]

	// Check if the env name matches the prefix and suffix
	if !strings.HasPrefix(envName, prefix) || !strings.HasSuffix(envName, suffix) {
		return ""
	}

	// Extract the key part between prefix and suffix
	keyStart := len(prefix)
	keyEnd := len(envName) - len(suffix)

	if keyStart >= keyEnd {
		return ""
	}

	extractedKey := envName[keyStart:keyEnd]

	// The extracted key can contain underscores (e.g., PAYMENT_SERVICE)
	// We'll convert them to hyphens when rendering the config key
	return extractedKey
}

// setNestedValueWithMapSupport sets a nested value with special handling for map structures
// This ensures that dynamic map keys (from templated env vars) create proper nested structures
func (p *EnvProvider) setNestedValueWithMapSupport(data map[string]interface{}, key string, value interface{}) {
	// Split the key into parts
	parts := strings.Split(key, ".")
	if len(parts) == 1 {
		// Simple key, set directly
		data[key] = value
		return
	}

	// Navigate/create the nested structure
	current := data
	for _, part := range parts[:len(parts)-1] {
		// Check if this part exists
		if existing, exists := current[part]; exists {
			// If it exists and is a map, continue navigating
			if existingMap, ok := existing.(map[string]interface{}); ok {
				current = existingMap
			} else {
				// If it exists but is not a map, we need to convert it or create a new map
				// This handles the case where we have both scalar and map values for the same key
				newMap := make(map[string]interface{})
				current[part] = newMap
				current = newMap
			}
		} else {
			// Create new map for this part
			newMap := make(map[string]interface{})
			current[part] = newMap
			current = newMap
		}
	}

	// Set the final value
	finalKey := parts[len(parts)-1]
	current[finalKey] = value
}
