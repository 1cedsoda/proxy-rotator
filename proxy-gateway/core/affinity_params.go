package core

import (
	"encoding/json"
	"fmt"
)

// AffinityParams holds the validated "meta" object from the proxy-authorization
// username. Values are restricted to strings and numbers only.
type AffinityParams map[string]interface{}

// NewAffinityParams returns an empty AffinityParams.
func NewAffinityParams() AffinityParams {
	return AffinityParams{}
}

// ParseAffinityParams validates a raw map: every value must be a string or number.
func ParseAffinityParams(raw map[string]interface{}) (AffinityParams, error) {
	for key, val := range raw {
		switch val.(type) {
		case string, float64, json.Number:
			// ok
		case bool:
			return nil, fmt.Errorf("'meta.%s' has a boolean value. Only string and number values are allowed.", key)
		case nil:
			return nil, fmt.Errorf("'meta.%s' has a null value. Only string and number values are allowed.", key)
		case []interface{}:
			return nil, fmt.Errorf("'meta.%s' has an array value. Only string and number values are allowed.", key)
		case map[string]interface{}:
			return nil, fmt.Errorf("'meta.%s' has a nested object value. Only string and number values are allowed.", key)
		default:
			return nil, fmt.Errorf("'meta.%s' has an unsupported value type. Only string and number values are allowed.", key)
		}
	}
	return AffinityParams(raw), nil
}

// Get returns the value for key, or nil if not present.
func (a AffinityParams) Get(key string) interface{} {
	return a[key]
}

// GetString returns the string value for key, or "" if not present or not a string.
func (a AffinityParams) GetString(key string) string {
	v, _ := a[key].(string)
	return v
}

// MarshalJSON marshals as a plain JSON object.
func (a AffinityParams) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}(a))
}
