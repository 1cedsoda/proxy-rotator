package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"proxy-gateway/core"
)

// ProxyAuth holds the parsed fields from a Proxy-Authorization header.
type ProxyAuth struct {
	SetName         string
	AffinityMinutes uint16
	// UsernamB64 is the raw base64 string used as the affinity map key.
	UsernameB64    string
	AffinityParams core.AffinityParams
}

// ParsedUsername holds the public fields returned by ParseUsernameForVerify.
type ParsedUsername struct {
	SetName         string
	AffinityMinutes uint16
	AffinityParams  core.AffinityParams
}

// ParseProxyAuthHeader extracts and parses the Proxy-Authorization header value.
func ParseProxyAuthHeader(headerVal string) (*ProxyAuth, error) {
	return parseProxyAuthValue(headerVal)
}

// ParseUsernameForVerify parses a raw base64 username string directly (no Basic wrapper).
func ParseUsernameForVerify(usernameb64 string) (*ParsedUsername, error) {
	decoded, err := base64.StdEncoding.DecodeString(usernameb64)
	if err != nil {
		// Try URL-safe base64 as well
		decoded, err = base64.URLEncoding.DecodeString(usernameb64)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 in username")
		}
	}
	// Reconstruct a fake Basic auth header so we can reuse the full parser.
	faked := "Basic " + base64.StdEncoding.EncodeToString([]byte(string(decoded)+":"))
	auth, err := parseProxyAuthValue(faked)
	if err != nil {
		return nil, err
	}
	return &ParsedUsername{
		SetName:         auth.SetName,
		AffinityMinutes: auth.AffinityMinutes,
		AffinityParams:  auth.AffinityParams,
	}, nil
}

func parseProxyAuthValue(headerVal string) (*ProxyAuth, error) {
	b64, ok := strings.CutPrefix(headerVal, "Basic ")
	if !ok {
		return nil, fmt.Errorf("Proxy-Authorization must be Basic auth")
	}

	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 in Proxy-Authorization")
	}

	decodedStr := string(decoded)

	// Strip Basic-auth ":password" suffix using last colon.
	usernameJSON := decodedStr
	if i := strings.LastIndex(decodedStr, ":"); i >= 0 {
		usernameJSON = decodedStr[:i]
	}

	if usernameJSON == "" {
		return nil, fmt.Errorf("empty username in Proxy-Authorization")
	}

	// Parse as JSON.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(usernameJSON), &raw); err != nil {
		return nil, fmt.Errorf("username is not valid JSON: %w", err)
	}

	// Require exactly the three expected keys.
	expectedKeys := []string{"meta", "minutes", "set"}
	for _, k := range expectedKeys {
		if _, ok := raw[k]; !ok {
			return nil, fmt.Errorf("username JSON is missing required key %q. Required keys: 'meta', 'minutes', 'set'.", k)
		}
	}
	if len(raw) != 3 {
		var extra []string
		for k := range raw {
			found := false
			for _, ek := range expectedKeys {
				if k == ek {
					found = true
					break
				}
			}
			if !found {
				extra = append(extra, k)
			}
		}
		return nil, fmt.Errorf("username JSON has unexpected keys: %v. Only 'meta', 'minutes', 'set' are allowed.", extra)
	}

	// Validate `set`.
	var setName string
	if err := json.Unmarshal(raw["set"], &setName); err != nil {
		return nil, fmt.Errorf("'set' must be a string")
	}
	if setName == "" {
		return nil, fmt.Errorf("invalid proxy set name %q. Must be non-empty and alphanumeric only.", setName)
	}
	for _, c := range setName {
		if !isAlphanumeric(c) {
			return nil, fmt.Errorf("invalid proxy set name %q. Must be non-empty and alphanumeric only.", setName)
		}
	}

	// Validate `minutes` — must be a JSON integer (not a string, not a float).
	// Decode into interface{} first to detect the raw JSON type.
	var minutesIface interface{}
	if err := json.Unmarshal(raw["minutes"], &minutesIface); err != nil {
		return nil, fmt.Errorf("'minutes' must be an integer 0–1440")
	}
	var minutesI int64
	switch v := minutesIface.(type) {
	case float64:
		// JSON numbers decode as float64; reject non-integers like 5.5.
		if v != float64(int64(v)) {
			return nil, fmt.Errorf("'minutes' must be an integer 0–1440")
		}
		minutesI = int64(v)
	default:
		// string, bool, nil, object, array — all rejected.
		return nil, fmt.Errorf("'minutes' must be an integer 0–1440")
	}
	if minutesI < 0 || minutesI > 1440 {
		return nil, fmt.Errorf("'minutes' %d exceeds maximum of 1440 (24 hours).", minutesI)
	}
	minutes := uint16(minutesI)

	// Validate `meta`.
	var metaRaw map[string]interface{}
	if err := json.Unmarshal(raw["meta"], &metaRaw); err != nil {
		return nil, fmt.Errorf("'meta' must be a JSON object")
	}
	affinityParams, err := core.ParseAffinityParams(metaRaw)
	if err != nil {
		return nil, err
	}

	return &ProxyAuth{
		SetName:         setName,
		AffinityMinutes: minutes,
		UsernameB64:     b64,
		AffinityParams:  affinityParams,
	}, nil
}

func isAlphanumeric(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// PercentDecode decodes percent-encoded URL path segments.
func PercentDecode(s string) string {
	bytes := []byte(s)
	out := make([]byte, 0, len(bytes))
	for i := 0; i < len(bytes); i++ {
		if bytes[i] == '%' && i+2 < len(bytes) {
			hi := hexDigit(bytes[i+1])
			lo := hexDigit(bytes[i+2])
			if hi >= 0 && lo >= 0 {
				out = append(out, byte(hi*16+lo))
				i += 2
				continue
			}
		}
		out = append(out, bytes[i])
	}
	return string(out)
}

func hexDigit(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10
	}
	return -1
}
