package geonode

import (
	"context"
	"fmt"
	"os"
	"strings"

	"proxy-gateway/core"
)

// Source generates geonode upstream proxy credentials per request.
type Source struct {
	config   Config
	password string
}

// FromConfig creates a GeonodeSource, reading the password from the env var.
func FromConfig(cfg *Config) (*Source, error) {
	password := os.Getenv(cfg.PasswordEnv)
	if password == "" {
		return nil, fmt.Errorf("geonode: env var %q (password_env) is not set or empty", cfg.PasswordEnv)
	}
	return &Source{config: *cfg, password: password}, nil
}

func (s *Source) makeProxy(username string) *core.SourceProxy {
	port := s.config.Port()
	host := s.config.Host()
	return &core.SourceProxy{
		Host:     host,
		Port:     port,
		Username: &username,
		Password: &s.password,
	}
}

func (s *Source) GetSourceProxy(_ context.Context, _ core.AffinityParams) (*core.SourceProxy, error) {
	return s.makeProxy(BuildUsername(&s.config)), nil
}

func (s *Source) GetSourceProxyForceRotate(_ context.Context, _ core.AffinityParams, _ *core.SourceProxy) (*core.SourceProxy, error) {
	return s.makeProxy(RotateUsername(&s.config)), nil
}

func (s *Source) Describe() string {
	parts := []string{"geonode"}
	if len(s.config.Countries) > 0 {
		codes := make([]string, len(s.config.Countries))
		for i, c := range s.config.Countries {
			codes[i] = strings.ToUpper(c.AsParamStr())
		}
		parts = append(parts, strings.Join(codes, ","))
	}
	parts = append(parts, fmt.Sprintf("%s@%s:%d", s.config.Username, s.config.Host(), s.config.Port()))
	return strings.Join(parts, " ")
}

// BuildSource constructs a Source from config.
func BuildSource(cfg *Config) (core.ProxySource, error) {
	return FromConfig(cfg)
}
