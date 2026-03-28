package core

import (
	"encoding/json"
	"fmt"
)

// ParsedUsername holds all fields decoded from the proxy username JSON.
type ParsedUsername struct {
	Sub             string
	SetName         string
	AffinityMinutes uint16
	AffinityParams  AffinityParams
}

// ParseUsernameJSON parses the JSON object that forms the proxy username.
//
// The JSON must have exactly four keys:
//   - "sub"     : non-empty string identifying the subscriber/user
//   - "set"     : non-empty alphanumeric string (proxy set name)
//   - "minutes" : integer 0–1440
//   - "meta"    : flat object (string/number values only)
func ParseUsernameJSON(usernameJSON string) (*ParsedUsername, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(usernameJSON), &raw); err != nil {
		return nil, fmt.Errorf("username is not valid JSON: %w", err)
	}

	expected := []string{"meta", "minutes", "set", "sub"}
	for _, k := range expected {
		if _, ok := raw[k]; !ok {
			return nil, fmt.Errorf("username JSON is missing required key %q. Required keys: 'sub', 'set', 'minutes', 'meta'", k)
		}
	}
	if len(raw) != 4 {
		var extra []string
		for k := range raw {
			found := false
			for _, ek := range expected {
				if k == ek {
					found = true
					break
				}
			}
			if !found {
				extra = append(extra, k)
			}
		}
		return nil, fmt.Errorf("username JSON has unexpected keys: %v. Only 'sub', 'set', 'minutes', 'meta' are allowed", extra)
	}

	// Validate "sub".
	var sub string
	if err := json.Unmarshal(raw["sub"], &sub); err != nil {
		return nil, fmt.Errorf("'sub' must be a string")
	}
	if sub == "" {
		return nil, fmt.Errorf("'sub' must not be empty")
	}

	// Validate "set".
	var setName string
	if err := json.Unmarshal(raw["set"], &setName); err != nil {
		return nil, fmt.Errorf("'set' must be a string")
	}
	if setName == "" {
		return nil, fmt.Errorf("invalid proxy set name %q: must be non-empty and alphanumeric only", setName)
	}
	for _, c := range setName {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return nil, fmt.Errorf("invalid proxy set name %q: must be non-empty and alphanumeric only", setName)
		}
	}

	// Validate "minutes".
	var minutesIface interface{}
	if err := json.Unmarshal(raw["minutes"], &minutesIface); err != nil {
		return nil, fmt.Errorf("'minutes' must be an integer 0–1440")
	}
	f, ok := minutesIface.(float64)
	if !ok {
		return nil, fmt.Errorf("'minutes' must be an integer 0–1440")
	}
	if f != float64(int64(f)) {
		return nil, fmt.Errorf("'minutes' must be an integer 0–1440")
	}
	if f < 0 || f > 1440 {
		return nil, fmt.Errorf("'minutes' %.0f exceeds maximum of 1440 (24 hours)", f)
	}

	// Validate "meta".
	var metaRaw map[string]interface{}
	if err := json.Unmarshal(raw["meta"], &metaRaw); err != nil {
		return nil, fmt.Errorf("'meta' must be a JSON object")
	}
	params, err := ParseAffinityParams(metaRaw)
	if err != nil {
		return nil, err
	}

	return &ParsedUsername{
		Sub:             sub,
		SetName:         setName,
		AffinityMinutes: uint16(f),
		AffinityParams:  params,
	}, nil
}
