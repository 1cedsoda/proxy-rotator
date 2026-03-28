package main

import (
	"encoding/base64"
	"fmt"
	"strings"

	"proxy-gateway/core"
)

// ParseUsernameForVerify parses a raw base64 username string directly
// (no Basic auth wrapper). Used by the verify endpoint.
func ParseUsernameForVerify(usernameb64 string) (*core.ParsedUsername, error) {
	decoded, err := base64.StdEncoding.DecodeString(usernameb64)
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(usernameb64)
		if err != nil {
			return nil, fmt.Errorf("invalid base64 in username")
		}
	}
	usernameJSON := string(decoded)
	if i := strings.LastIndex(usernameJSON, ":"); i >= 0 {
		usernameJSON = usernameJSON[:i]
	}
	return core.ParseUsernameJSON(usernameJSON)
}

// PercentDecode decodes percent-encoded URL path segments.
func PercentDecode(s string) string {
	b := []byte(s)
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == '%' && i+2 < len(b) {
			hi := hexDigit(b[i+1])
			lo := hexDigit(b[i+2])
			if hi >= 0 && lo >= 0 {
				out = append(out, byte(hi*16+lo))
				i += 2
				continue
			}
		}
		out = append(out, b[i])
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
