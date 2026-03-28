package core

import (
	"fmt"
	"strconv"
	"strings"
)

// ProxyFormat describes how proxy lines are formatted in a file.
type ProxyFormat string

const (
	// ProxyFormatHostPortUserPass: host:port:user:pass (default)
	ProxyFormatHostPortUserPass ProxyFormat = "host_port_user_pass"
	// ProxyFormatUserPassAtHostPort: user:pass@host:port
	ProxyFormatUserPassAtHostPort ProxyFormat = "user_pass_at_host_port"
	// ProxyFormatUserPassHostPort: user:pass:host:port
	ProxyFormatUserPassHostPort ProxyFormat = "user_pass_host_port"
)

// DefaultProxyFormat is the default format.
const DefaultProxyFormat = ProxyFormatHostPortUserPass

// ParseProxyLine parses a single proxy line in the given format.
// An optional protocol:// prefix is stripped. Lines without credentials (host:port) are accepted.
func ParseProxyLine(s string, format ProxyFormat) (SourceProxy, error) {
	s = stripProtocol(s)
	switch format {
	case ProxyFormatUserPassAtHostPort:
		return parseUserPassAtHostPort(s)
	case ProxyFormatUserPassHostPort:
		return parseUserPassHostPort(s)
	default: // host_port_user_pass
		return parseHostPortUserPass(s)
	}
}

func stripProtocol(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		return s[i+3:]
	}
	return s
}

func parseUserPassAtHostPort(s string) (SourceProxy, error) {
	at := strings.LastIndex(s, "@")
	if at < 0 {
		host, port, err := parseHostPort(s)
		if err != nil {
			return SourceProxy{}, err
		}
		return SourceProxy{Host: host, Port: port}, nil
	}
	creds := s[:at]
	hostPort := s[at+1:]
	host, port, err := parseHostPort(hostPort)
	if err != nil {
		return SourceProxy{}, err
	}
	user, pass, err := splitUserPass(creds)
	if err != nil {
		return SourceProxy{}, err
	}
	return SourceProxy{Host: host, Port: port, Username: &user, Password: &pass}, nil
}

func parseUserPassHostPort(s string) (SourceProxy, error) {
	// Split from the right into at most 3 parts: [creds, host, port]
	// For "user:pass:host.com:8080" → ["user:pass", "host.com", "8080"]
	parts := rSplitN(s, ':', 3)
	if len(parts) == 3 {
		// parts[0]=creds, parts[1]=host, parts[2]=port
		if port, err := strconv.ParseUint(parts[2], 10, 16); err == nil {
			host := parts[1]
			creds := parts[0]
			if ci := strings.Index(creds, ":"); ci >= 0 {
				user := creds[:ci]
				pass := creds[ci+1:]
				if user != "" {
					p := uint16(port)
					return SourceProxy{Host: host, Port: p, Username: &user, Password: &pass}, nil
				}
			}
		}
	}
	// Fall back to host:port
	host, port, err := parseHostPort(s)
	if err != nil {
		return SourceProxy{}, fmt.Errorf("expected user:pass:host:port or host:port, got %q", s)
	}
	return SourceProxy{Host: host, Port: port}, nil
}

func parseHostPortUserPass(s string) (SourceProxy, error) {
	// Handle IPv6 in brackets
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return SourceProxy{}, fmt.Errorf("unclosed bracket in %q", s)
		}
		host := s[1:end]
		rest := s[end+1:]
		if !strings.HasPrefix(rest, ":") {
			return SourceProxy{}, fmt.Errorf("expected ':' after ']' in %q", s)
		}
		return parsePortAndOptionalCreds(host, rest[1:])
	}

	parts := strings.SplitN(s, ":", 4)
	switch len(parts) {
	case 2:
		port, err := strconv.ParseUint(parts[1], 10, 16)
		if err != nil {
			return SourceProxy{}, fmt.Errorf("invalid port in %q", s)
		}
		return SourceProxy{Host: parts[0], Port: uint16(port)}, nil
	case 4:
		port, err := strconv.ParseUint(parts[1], 10, 16)
		if err != nil {
			return SourceProxy{}, fmt.Errorf("invalid port in %q", s)
		}
		user, pass := parts[2], parts[3]
		return SourceProxy{Host: parts[0], Port: uint16(port), Username: &user, Password: &pass}, nil
	default:
		return SourceProxy{}, fmt.Errorf("expected host:port or host:port:user:pass, got %q", s)
	}
}

func parsePortAndOptionalCreds(host, rest string) (SourceProxy, error) {
	parts := strings.SplitN(rest, ":", 3)
	switch len(parts) {
	case 1:
		port, err := strconv.ParseUint(parts[0], 10, 16)
		if err != nil {
			return SourceProxy{}, fmt.Errorf("invalid port %q", parts[0])
		}
		return SourceProxy{Host: host, Port: uint16(port)}, nil
	case 3:
		port, err := strconv.ParseUint(parts[0], 10, 16)
		if err != nil {
			return SourceProxy{}, fmt.Errorf("invalid port %q", parts[0])
		}
		user, pass := parts[1], parts[2]
		return SourceProxy{Host: host, Port: uint16(port), Username: &user, Password: &pass}, nil
	default:
		return SourceProxy{}, fmt.Errorf("expected port or port:user:pass after host, got %q", rest)
	}
}

func parseHostPort(s string) (string, uint16, error) {
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]")
		if end < 0 {
			return "", 0, fmt.Errorf("unclosed bracket in %q", s)
		}
		host := s[1:end]
		rest := s[end+1:]
		if !strings.HasPrefix(rest, ":") {
			return "", 0, fmt.Errorf("expected ':' after ']' in %q", s)
		}
		port, err := strconv.ParseUint(rest[1:], 10, 16)
		if err != nil {
			return "", 0, fmt.Errorf("invalid port in %q", s)
		}
		return host, uint16(port), nil
	}
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", 0, fmt.Errorf("expected host:port, got %q", s)
	}
	port, err := strconv.ParseUint(s[i+1:], 10, 16)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port in %q", s)
	}
	return s[:i], uint16(port), nil
}

func splitUserPass(s string) (string, string, error) {
	i := strings.Index(s, ":")
	if i < 0 {
		return "", "", fmt.Errorf("expected user:pass, got %q", s)
	}
	return s[:i], s[i+1:], nil
}

// rSplitN splits s at c from the right, returning at most n parts.
func rSplitN(s string, c byte, n int) []string {
	parts := make([]string, 0, n)
	remaining := s
	for len(parts) < n-1 {
		i := strings.LastIndexByte(remaining, c)
		if i < 0 {
			break
		}
		parts = append(parts, remaining[i+1:])
		remaining = remaining[:i]
	}
	parts = append(parts, remaining)
	// reverse
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return parts
}
