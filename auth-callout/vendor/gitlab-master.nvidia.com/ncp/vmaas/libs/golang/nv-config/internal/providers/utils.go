package providers

import "strings"

// SetNestedValue sets a value in a nested map using dot notation key
// This is a shared utility function used by multiple providers
func SetNestedValue(data map[string]interface{}, key string, value interface{}) {
	keys := strings.Split(key, ".")
	current := data

	// Navigate to the parent of the final key
	for _, k := range keys[:len(keys)-1] {
		if _, exists := current[k]; !exists {
			current[k] = make(map[string]interface{})
		}

		// Type assertion to map[string]interface{}
		if nextMap, ok := current[k].(map[string]interface{}); ok {
			current = nextMap
		} else {
			// If the intermediate key exists but is not a map, we need to convert it
			// This can happen when we have both "db" and "db.host" keys
			newMap := make(map[string]interface{})
			newMap["value"] = current[k] // Preserve the existing value
			current[k] = newMap
			current = newMap
		}
	}

	// Set the final value
	finalKey := keys[len(keys)-1]
	current[finalKey] = value
}
