package providers

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// CLIAutoFlagsProvider automatically generates CLI flags from configuration structures
// and provides their values as configuration data
type CLIAutoFlagsProvider struct {
	cobraCmd     *cobra.Command
	flagPrefix   string
	priority     int
	configStruct interface{}
	flagMappings map[string]string // flag name -> config key
	flagValues   map[string]interface{}
	flagDefaults map[string]interface{}

	// Options
	generateShortFlags bool
	kebabCase          bool
	helpPrefix         string
	flagSeparator      string
}

// CLIAutoFlagsProviderOption defines functional options for CLIAutoFlagsProvider
type CLIAutoFlagsProviderOption func(*CLIAutoFlagsProvider)

// NewCLIAutoFlagsProvider creates a new CLI auto-flags provider
func NewCLIAutoFlagsProvider(cobraCmd *cobra.Command, configStruct interface{}, opts ...CLIAutoFlagsProviderOption) *CLIAutoFlagsProvider {
	p := &CLIAutoFlagsProvider{
		cobraCmd:           cobraCmd,
		flagPrefix:         "",
		priority:           70, // High priority by default (CLI usually overrides config files)
		configStruct:       configStruct,
		flagMappings:       make(map[string]string),
		flagValues:         make(map[string]interface{}),
		flagDefaults:       make(map[string]interface{}),
		generateShortFlags: false,
		kebabCase:          true,
		helpPrefix:         "",
		flagSeparator:      "-",
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// WithCLIFlagPrefix sets the prefix for generated flags
func WithCLIFlagPrefix(prefix string) CLIAutoFlagsProviderOption {
	return func(p *CLIAutoFlagsProvider) {
		p.flagPrefix = prefix
	}
}

// WithCLIPriority sets the provider priority
func WithCLIPriority(priority int) CLIAutoFlagsProviderOption {
	return func(p *CLIAutoFlagsProvider) {
		p.priority = priority
	}
}

// WithCLIShortFlags enables generation of short flags (single letter)
func WithCLIShortFlags(enabled bool) CLIAutoFlagsProviderOption {
	return func(p *CLIAutoFlagsProvider) {
		p.generateShortFlags = enabled
	}
}

// WithCLIKebabCase enables kebab-case flag names (default: true)
func WithCLIKebabCase(enabled bool) CLIAutoFlagsProviderOption {
	return func(p *CLIAutoFlagsProvider) {
		p.kebabCase = enabled
	}
}

// WithCLIHelpPrefix sets a prefix for help text
func WithCLIHelpPrefix(prefix string) CLIAutoFlagsProviderOption {
	return func(p *CLIAutoFlagsProvider) {
		p.helpPrefix = prefix
	}
}

// WithCLIFlagSeparator sets the separator for nested flags (default: "-")
func WithCLIFlagSeparator(separator string) CLIAutoFlagsProviderOption {
	return func(p *CLIAutoFlagsProvider) {
		p.flagSeparator = separator
	}
}

// WithCLICustomMappings sets custom flag name to config key mappings
func WithCLICustomMappings(mappings map[string]string) CLIAutoFlagsProviderOption {
	return func(p *CLIAutoFlagsProvider) {
		for flagName, configKey := range mappings {
			p.flagMappings[flagName] = configKey
		}
	}
}

// Name returns the provider name
func (p *CLIAutoFlagsProvider) Name() string {
	return "cli-auto-flags"
}

// Priority returns the provider priority
func (p *CLIAutoFlagsProvider) Priority() int {
	return p.priority
}

// Load loads configuration from CLI flags
func (p *CLIAutoFlagsProvider) Load() (map[string]interface{}, error) {
	data := make(map[string]interface{})

	// Get all flags that were set
	p.cobraCmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if !flag.Changed {
			return // Skip flags that weren't explicitly set
		}

		// Get config key for this flag
		configKey := p.getConfigKey(flag.Name)
		if configKey == "" {
			return // Skip unmapped flags
		}

		// Parse flag value to appropriate type
		value, err := p.parseFlagValue(flag)
		if err != nil {
			// Skip invalid flags silently - parsing errors are expected for some flag types
			// Libraries should not print to stdout/stderr
			return
		}

		// Set in configuration data
		SetNestedValue(data, configKey, value)

		// Store for debugging
		p.flagValues[flag.Name] = value
	})

	return data, nil
}

// GenerateFlags automatically generates CLI flags from the configuration structure
func (p *CLIAutoFlagsProvider) GenerateFlags() error {
	return p.generateFlagsFromStruct(reflect.TypeOf(p.configStruct), reflect.ValueOf(p.configStruct), "")
}

// generateFlagsFromStruct recursively generates flags from a struct
func (p *CLIAutoFlagsProvider) generateFlagsFromStruct(t reflect.Type, v reflect.Value, prefix string) error {
	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		if v.IsNil() {
			// Create a new instance to examine the type
			v = reflect.New(t.Elem())
		}
		return p.generateFlagsFromStruct(t.Elem(), v.Elem(), prefix)
	}

	if t.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct, got %s", t.Kind())
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get koanf tag for config key mapping
		koanfTag := field.Tag.Get("koanf")
		if koanfTag == "" || koanfTag == "-" {
			continue
		}

		// Build config key path
		configKey := koanfTag
		if prefix != "" {
			configKey = prefix + "." + koanfTag
		}

		// Handle nested structs
		if field.Type.Kind() == reflect.Struct {
			err := p.generateFlagsFromStruct(field.Type, fieldValue, configKey)
			if err != nil {
				return fmt.Errorf("failed to generate flags for nested struct %s: %w", field.Name, err)
			}
			continue
		}

		// Generate flag for this field
		err := p.generateFlag(field, fieldValue, configKey)
		if err != nil {
			return fmt.Errorf("failed to generate flag for field %s: %w", field.Name, err)
		}
	}

	return nil
}

// generateFlag creates a CLI flag for a specific field
func (p *CLIAutoFlagsProvider) generateFlag(field reflect.StructField, value reflect.Value, configKey string) error {
	// Generate flag name
	flagName := p.generateFlagName(configKey)

	// Check if flag already exists
	if p.cobraCmd.Flags().Lookup(flagName) != nil {
		return nil // Flag already exists, skip
	}

	// Get default value
	defaultValue := p.getDefaultValue(field, value)

	// Get help text
	helpText := p.generateHelpText(field, configKey)

	// Generate short flag if enabled
	var shortFlag string
	if p.generateShortFlags {
		shortFlag = p.generateShortFlag(flagName)
	}

	// Store mapping
	p.flagMappings[flagName] = configKey
	p.flagDefaults[flagName] = defaultValue

	// Handle special types first
	if field.Type == reflect.TypeOf(time.Duration(0)) {
		duration := defaultValue.(time.Duration)
		if shortFlag != "" {
			p.cobraCmd.Flags().DurationP(flagName, shortFlag, duration, helpText)
		} else {
			p.cobraCmd.Flags().Duration(flagName, duration, helpText)
		}
		return nil
	}

	// Add flag based on type
	switch field.Type.Kind() {
	case reflect.String:
		if shortFlag != "" {
			p.cobraCmd.Flags().StringP(flagName, shortFlag, defaultValue.(string), helpText)
		} else {
			p.cobraCmd.Flags().String(flagName, defaultValue.(string), helpText)
		}

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if shortFlag != "" {
			p.cobraCmd.Flags().IntP(flagName, shortFlag, int(defaultValue.(int64)), helpText)
		} else {
			p.cobraCmd.Flags().Int(flagName, int(defaultValue.(int64)), helpText)
		}

	case reflect.Bool:
		if shortFlag != "" {
			p.cobraCmd.Flags().BoolP(flagName, shortFlag, defaultValue.(bool), helpText)
		} else {
			p.cobraCmd.Flags().Bool(flagName, defaultValue.(bool), helpText)
		}

	case reflect.Float32, reflect.Float64:
		if shortFlag != "" {
			p.cobraCmd.Flags().Float64P(flagName, shortFlag, defaultValue.(float64), helpText)
		} else {
			p.cobraCmd.Flags().Float64(flagName, defaultValue.(float64), helpText)
		}

	default:
		// Fallback to string for unknown types
		if shortFlag != "" {
			p.cobraCmd.Flags().StringP(flagName, shortFlag, fmt.Sprintf("%v", defaultValue), helpText)
		} else {
			p.cobraCmd.Flags().String(flagName, fmt.Sprintf("%v", defaultValue), helpText)
		}
	}

	return nil
}

// generateFlagName creates a CLI flag name from a config key
func (p *CLIAutoFlagsProvider) generateFlagName(configKey string) string {
	name := configKey

	if p.kebabCase {
		// Convert dots to separators and make kebab-case
		name = strings.ReplaceAll(name, ".", p.flagSeparator)
		name = strings.ReplaceAll(name, "_", p.flagSeparator)
	}

	if p.flagPrefix != "" {
		name = p.flagPrefix + p.flagSeparator + name
	}

	return name
}

// generateShortFlag creates a short flag (single letter) from a flag name
func (p *CLIAutoFlagsProvider) generateShortFlag(flagName string) string {
	// Simple strategy: use first letter, skip if already taken
	if len(flagName) == 0 {
		return ""
	}

	candidate := strings.ToLower(string(flagName[0]))

	// Check if already used (basic check)
	if p.cobraCmd.Flags().ShorthandLookup(candidate) != nil {
		return "" // Already used, skip short flag
	}

	return candidate
}

// generateHelpText creates help text for a flag
func (p *CLIAutoFlagsProvider) generateHelpText(field reflect.StructField, configKey string) string {
	// Check for desc tag first (preferred)
	if desc := field.Tag.Get("desc"); desc != "" {
		return desc
	}

	// Check for explicit help in tag (fallback)
	if help := field.Tag.Get("help"); help != "" {
		return help
	}

	// Generate help from field name and type
	help := fmt.Sprintf("Set %s", configKey)

	if p.helpPrefix != "" {
		help = p.helpPrefix + " " + help
	}

	// Add type information
	switch field.Type.Kind() {
	case reflect.String:
		help += " (string)"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		help += " (integer)"
	case reflect.Bool:
		help += " (boolean)"
	case reflect.Float32, reflect.Float64:
		help += " (float)"
	default:
		if field.Type == reflect.TypeOf(time.Duration(0)) {
			help += " (duration, e.g., 30s, 5m)"
		}
	}

	return help
}

// getDefaultValue extracts the default value from a field
func (p *CLIAutoFlagsProvider) getDefaultValue(field reflect.StructField, value reflect.Value) interface{} {
	// Check for default in tag
	if defaultTag := field.Tag.Get("default"); defaultTag != "" {
		return p.parseDefaultValue(defaultTag, field.Type)
	}

	// Use zero value or current value
	if value.IsValid() && !value.IsZero() {
		return value.Interface()
	}

	// Handle special types first
	if field.Type == reflect.TypeOf(time.Duration(0)) {
		return time.Duration(0)
	}

	// Return appropriate zero value based on kind
	switch field.Type.Kind() {
	case reflect.String:
		return ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int64(0)
	case reflect.Bool:
		return false
	case reflect.Float32, reflect.Float64:
		return float64(0)
	default:
		return ""
	}
}

// parseDefaultValue parses a default value from a string tag
func (p *CLIAutoFlagsProvider) parseDefaultValue(defaultStr string, fieldType reflect.Type) interface{} {
	// Handle special types first
	if fieldType == reflect.TypeOf(time.Duration(0)) {
		if val, err := time.ParseDuration(defaultStr); err == nil {
			return val
		}
		return time.Duration(0)
	}

	switch fieldType.Kind() {
	case reflect.String:
		return defaultStr
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if val, err := strconv.ParseInt(defaultStr, 10, 64); err == nil {
			return val
		}
		return int64(0)
	case reflect.Bool:
		if val, err := strconv.ParseBool(defaultStr); err == nil {
			return val
		}
		return false
	case reflect.Float32, reflect.Float64:
		if val, err := strconv.ParseFloat(defaultStr, 64); err == nil {
			return val
		}
		return float64(0)
	default:
		return defaultStr
	}
}

// getConfigKey returns the config key for a flag name
func (p *CLIAutoFlagsProvider) getConfigKey(flagName string) string {
	// Check explicit mappings first
	if configKey, exists := p.flagMappings[flagName]; exists {
		return configKey
	}

	// Return empty string for unmapped flags
	return ""
}

// parseFlagValue parses a flag value to the appropriate Go type
func (p *CLIAutoFlagsProvider) parseFlagValue(flag *pflag.Flag) (interface{}, error) {
	switch flag.Value.Type() {
	case "string":
		return flag.Value.String(), nil
	case "int":
		return strconv.Atoi(flag.Value.String())
	case "bool":
		return strconv.ParseBool(flag.Value.String())
	case "float64":
		return strconv.ParseFloat(flag.Value.String(), 64)
	case "duration":
		return time.ParseDuration(flag.Value.String())
	default:
		// Fallback to string
		return flag.Value.String(), nil
	}
}

// GetFlagMappings returns the current flag to config key mappings
func (p *CLIAutoFlagsProvider) GetFlagMappings() map[string]string {
	mappings := make(map[string]string)
	for flag, config := range p.flagMappings {
		mappings[flag] = config
	}
	return mappings
}

// GetFlagValues returns the current flag values that were set
func (p *CLIAutoFlagsProvider) GetFlagValues() map[string]interface{} {
	values := make(map[string]interface{})
	for flag, value := range p.flagValues {
		values[flag] = value
	}
	return values
}
