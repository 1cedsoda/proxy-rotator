// Package simple provides an AuthProvider that validates against a single
// known (sub, password) pair. Suitable for single-user or service deployments.
package simple

import (
	"fmt"

	"proxy-gateway/core"
)

// Provider implements core.AuthProvider for a single (sub, password) credential.
type Provider struct {
	sub      string
	password string
}

// New returns an AuthProvider that accepts exactly one (sub, password) pair.
func New(sub, password string) core.AuthProvider {
	return &Provider{sub: sub, password: password}
}

// Authenticate returns nil if both sub and password match, otherwise an error.
func (p *Provider) Authenticate(sub, password string) error {
	if sub != p.sub || password != p.password {
		return fmt.Errorf("invalid credentials")
	}
	return nil
}
