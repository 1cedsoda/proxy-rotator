package bottingtools

import (
	"context"
	"fmt"
	"os"

	"proxy-gateway/core"
)

// Source dynamically generates upstream proxy credentials using the bottingtools username format.
type Source struct {
	accountUser string
	password    string
	host        string
	product     ProductConfig
}

// FromConfig creates a Source, reading the password from the configured env var.
func FromConfig(cfg *Config) (*Source, error) {
	password := os.Getenv(cfg.PasswordEnv)
	if password == "" {
		return nil, fmt.Errorf("environment variable %q not set or empty (required for bottingtools password)", cfg.PasswordEnv)
	}
	return &Source{
		accountUser: cfg.Username,
		password:    password,
		host:        cfg.Host,
		product:     cfg.Product,
	}, nil
}

func (s *Source) makeProxy(username string) *core.SourceProxy {
	port := uint16(1337)
	return &core.SourceProxy{
		Host:     s.host,
		Port:     port,
		Username: &username,
		Password: &s.password,
	}
}

func (s *Source) GetSourceProxy(_ context.Context, params core.AffinityParams) (*core.SourceProxy, error) {
	username := BuildUsername(s.accountUser, s.product, params)
	return s.makeProxy(username), nil
}

func (s *Source) GetSourceProxyForceRotate(_ context.Context, _ core.AffinityParams, current *core.SourceProxy) (*core.SourceProxy, error) {
	var newUsername string
	if current != nil && current.Username != nil {
		newUsername = RotateSessionID(*current.Username)
	} else {
		newUsername = BuildUsername(s.accountUser, s.product, core.NewAffinityParams())
	}
	return s.makeProxy(newUsername), nil
}

func (s *Source) Describe() string {
	var product string
	switch s.product.Type {
	case "residential":
		product = fmt.Sprintf("residential(%s)", s.product.Residential.Quality.AsTypeStr())
	case "isp":
		product = "isp"
	case "datacenter":
		product = "datacenter"
	}
	return fmt.Sprintf("bottingtools %s %s@%s", product, s.accountUser, s.host)
}

// BuildSource constructs a Source from a raw config for use via the dispatch factory.
func BuildSource(cfg *Config) (core.ProxySource, error) {
	return FromConfig(cfg)
}
