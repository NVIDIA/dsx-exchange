package config

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// ExtractKoanfKeysWithPrefix extracts all koanf tags from a struct and prefixes them
// This is used by ConfigProvider implementations to generate their ConfigKeys
func ExtractKoanfKeysWithPrefix(v interface{}, prefix string) []string {
	var keys []string
	extractKoanfKeysRecursive(reflect.TypeOf(v), prefix, &keys)
	return keys
}

// isLeafType determines if a type represents a configurable leaf value
func isLeafType(t reflect.Type) bool {
	// Handle pointers
	if t.Kind() == reflect.Ptr {
		return isLeafType(t.Elem())
	}

	switch t.Kind() {
	case reflect.String, reflect.Bool:
		return true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	case reflect.Float32, reflect.Float64:
		return true
	case reflect.Slice, reflect.Array:
		return isLeafType(t.Elem()) // Only if elements are leaf types
	case reflect.Map:
		return isLeafType(t.Key()) && isLeafType(t.Elem()) // Both key and value must be leaf
	case reflect.Struct:
		// Special cases for time types
		if t == reflect.TypeOf(time.Time{}) || t == reflect.TypeOf(time.Duration(0)) {
			return true
		}
		return false // All other structs are containers
	default:
		return false // Interfaces, channels, functions, etc.
	}
}

// extractKoanfKeysRecursive recursively extracts koanf keys from a struct type
func extractKoanfKeysRecursive(t reflect.Type, prefix string, keys *[]string) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		koanfTag := field.Tag.Get("koanf")
		if koanfTag != "" && koanfTag != "-" {
			// Parse the tag (might have options like "omitempty")
			tagName := strings.Split(koanfTag, ",")[0]

			// Build full configuration key path
			var fullKey string
			if prefix != "" {
				fullKey = prefix + "." + tagName
			} else {
				fullKey = tagName
			}

			// Check if this is a leaf type or a struct container
			if isLeafType(field.Type) {
				// This is a configurable leaf value - add to keys
				*keys = append(*keys, fullKey)
			} else {
				// This is a struct container - recurse but don't add as key
				fieldType := field.Type
				if fieldType.Kind() == reflect.Ptr {
					fieldType = fieldType.Elem()
				}
				if fieldType.Kind() == reflect.Struct {
					extractKoanfKeysRecursive(fieldType, fullKey, keys)
				}
			}
		}
	}
}

// ExtractDescriptionsWithPrefix extracts desc tags from a struct and maps them to config keys
// This is used to provide meaningful descriptions for CLI flags based on struct field descriptions
func ExtractDescriptionsWithPrefix(v interface{}, prefix string) map[string]string {
	descriptions := make(map[string]string)
	extractDescriptionsRecursive(reflect.TypeOf(v), prefix, &descriptions)
	return descriptions
}

// extractDescriptionsRecursive recursively extracts desc tags from a struct type
func extractDescriptionsRecursive(t reflect.Type, prefix string, descriptions *map[string]string) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		koanfTag := field.Tag.Get("koanf")
		descTag := field.Tag.Get("desc")

		if koanfTag != "" && koanfTag != "-" && descTag != "" {
			// Parse the koanf tag (might have options like "omitempty")
			tagName := strings.Split(koanfTag, ",")[0]

			// Build full configuration key path
			var fullKey string
			if prefix != "" {
				fullKey = prefix + "." + tagName
			} else {
				fullKey = tagName
			}

			// Store the description for this config key
			(*descriptions)[fullKey] = descTag

			// If this field is a struct, recursively extract its descriptions
			fieldType := field.Type
			if fieldType.Kind() == reflect.Ptr {
				fieldType = fieldType.Elem()
			}
			if fieldType.Kind() == reflect.Struct {
				extractDescriptionsRecursive(fieldType, fullKey, descriptions)
			}
		}
	}
}

// structToMap converts a struct to map[string]interface{} using koanf tag reflection
// This respects koanf tags to ensure proper field mapping for configuration
func structToMap(v interface{}) (map[string]interface{}, error) {
	// If it's already a map, return it directly
	if m, ok := v.(map[string]interface{}); ok {
		return m, nil
	}

	// Use reflection to properly handle koanf tags for structs
	return structToMapRecursive(reflect.ValueOf(v))
}

// structToMapRecursive recursively converts struct values to maps using koanf tags
func structToMapRecursive(v reflect.Value) (map[string]interface{}, error) {
	// Handle pointers and interfaces
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		// Handle primitive types and slices/maps that aren't structs
		return nil, fmt.Errorf("cannot marshal type: %T", v.Interface())
	}

	result := make(map[string]interface{})
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get koanf tag (fallback to yaml tag if no koanf tag)
		tag := field.Tag.Get("koanf")
		if tag == "" {
			tag = field.Tag.Get("yaml")
		}
		if tag == "" || tag == "-" {
			continue
		}

		// Parse tag (remove options like "omitempty")
		tagName := strings.Split(tag, ",")[0]
		if tagName == "" {
			continue
		}

		// Convert field value
		convertedValue, err := convertFieldValue(fieldValue)
		if err != nil {
			return nil, fmt.Errorf("failed to convert field %s: %w", field.Name, err)
		}

		result[tagName] = convertedValue
	}

	return result, nil
}

// convertFieldValue converts a reflect.Value to interface{} with proper type handling
func convertFieldValue(v reflect.Value) (interface{}, error) {
	// Handle pointers and interfaces
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil, nil
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		// Handle time.Duration and other special structs
		if v.Type().String() == "time.Duration" {
			// Convert duration to string representation
			duration := v.Interface().(time.Duration)
			return duration.String(), nil
		}
		// Recursively convert nested structs
		return structToMapRecursive(v)

	case reflect.Slice, reflect.Array:
		// Convert slices/arrays
		length := v.Len()
		result := make([]interface{}, length)
		for i := 0; i < length; i++ {
			converted, err := convertFieldValue(v.Index(i))
			if err != nil {
				return nil, err
			}
			result[i] = converted
		}
		return result, nil

	case reflect.Map:
		// Convert maps
		result := make(map[string]interface{})
		for _, key := range v.MapKeys() {
			keyStr := key.String()
			mapValue := v.MapIndex(key)
			converted, err := convertFieldValue(mapValue)
			if err != nil {
				return nil, err
			}
			result[keyStr] = converted
		}
		return result, nil

	default:
		// Return primitive types as-is
		return v.Interface(), nil
	}
}

// transformConfigPrefix transforms a config struct to use a new prefix
func transformConfigPrefix(config interface{}, originalPrefix, newPrefix string) (interface{}, error) {
	// Handle nil config
	if config == nil {
		return nil, nil
	}

	// If it's already a map, we can transform it directly
	if configMap, ok := config.(map[string]interface{}); ok {
		return transformMapPrefix(configMap, originalPrefix, newPrefix), nil
	}

	// Convert struct to map first, then transform
	configMap, err := structToMap(config)
	if err != nil {
		return nil, fmt.Errorf("failed to convert config struct to map: %w", err)
	}

	return transformMapPrefix(configMap, originalPrefix, newPrefix), nil
}

// transformMapPrefix transforms a config map to use a new prefix
func transformMapPrefix(configMap map[string]interface{}, originalPrefix, newPrefix string) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range configMap {
		if key == originalPrefix {
			// Replace the top-level key with the new prefix
			result[newPrefix] = value
		} else {
			// Keep other keys as-is
			result[key] = value
		}
	}

	return result
}
