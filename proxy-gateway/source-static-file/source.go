package staticfile

import (
	"context"
	"fmt"
	"path/filepath"

	"proxy-gateway/core"
)

// Source is a proxy source backed by a fixed list loaded from a text file at startup.
// Endpoint selection uses a least-used counter with random tie-breaking.
type Source struct {
	pool        *core.CountingPool[core.SourceProxy]
	pathDisplay string
}

// Load creates a StaticFileSource from the given path and format.
func Load(path string, format core.ProxyFormat) (*Source, error) {
	if format == "" {
		format = core.DefaultProxyFormat
	}
	proxies, err := LoadProxies(path, format)
	if err != nil {
		return nil, err
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no proxies found in %s", path)
	}
	return &Source{
		pool:        core.NewCountingPool(proxies),
		pathDisplay: path,
	}, nil
}

// BuildSource constructs a Source from config, resolving relative paths against configDir.
func BuildSource(cfg *Config, configDir string) (core.ProxySource, error) {
	path := cfg.ProxiesFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(configDir, path)
	}
	return Load(path, cfg.Format)
}

func (s *Source) GetSourceProxy(_ context.Context, _ core.AffinityParams) (*core.SourceProxy, error) {
	p := s.pool.Next()
	if p == nil {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (s *Source) GetSourceProxyForceRotate(_ context.Context, _ core.AffinityParams, current *core.SourceProxy) (*core.SourceProxy, error) {
	p := s.pool.NextExcluding(func(sp core.SourceProxy) bool {
		if current == nil {
			return false
		}
		return sp.Equal(*current)
	})
	if p == nil {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (s *Source) Describe() string {
	return fmt.Sprintf("static file %q with %d entries", s.pathDisplay, s.pool.Len())
}
