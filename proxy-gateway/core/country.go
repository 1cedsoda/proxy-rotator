package core

import (
	"fmt"
	"strings"
)

// Country is an ISO 3166-1 alpha-2 country code.
type Country string

// AsParamStr returns the lowercase string used in upstream proxy usernames.
func (c Country) AsParamStr() string {
	return strings.ToLower(string(c))
}

// UnmarshalTOML implements toml.Unmarshaler, accepting uppercase country codes.
func (c *Country) UnmarshalTOML(data interface{}) error {
	s, ok := data.(string)
	if !ok {
		return fmt.Errorf("country must be a string")
	}
	*c = Country(strings.ToUpper(s))
	return nil
}

// UnmarshalJSON accepts uppercase country codes.
func (c *Country) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	*c = Country(strings.ToUpper(s))
	return nil
}
