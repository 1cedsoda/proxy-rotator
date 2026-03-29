package main

import (
	"context"
	"fmt"

	"proxy-gateway/core"
	"proxy-gateway/utils"
)

// Server holds the assembled pipeline and all introspection handles.
type Server struct {
	Pipeline core.Handler
	Sessions *utils.SessionManager
}

// BuildServer assembles the full handler pipeline from config.
func BuildServer(cfg *Config, configDir string, proxyPassword string) (*Server, error) {
	router, err := buildProxysetRouter(cfg, configDir)
	if err != nil {
		return nil, err
	}

	sessions := utils.NewSessionManager(router)
	pipeline := PasswordAuth(proxyPassword, ParseJSONCreds(sessions))

	return &Server{
		Pipeline: pipeline,
		Sessions: sessions,
	}, nil
}

// buildProxysetRouter creates a Handler that dispatches to the correct proxy
// source based on the set name in context.
func buildProxysetRouter(cfg *Config, configDir string) (core.Handler, error) {
	sources := make(map[string]core.Handler, len(cfg.ProxySets))
	for _, raw := range cfg.ProxySets {
		var src core.Handler
		var err error

		switch raw.SourceType {
		case "static_file":
			if raw.StaticFile == nil {
				return nil, fmt.Errorf("proxy set %q: static_file source requires a [static_file] section", raw.Name)
			}
			c := raw.StaticFile
			if c.Format == "" {
				c.Format = utils.DefaultProxyFormat
			}
			src, err = utils.NewStaticFileSource(c, configDir)

		case "bottingtools":
			if raw.Bottingtools == nil {
				return nil, fmt.Errorf("proxy set %q: bottingtools source requires a [bottingtools] section", raw.Name)
			}
			src, err = utils.NewBottingtoolsSource(raw.Bottingtools)

		case "geonode":
			if raw.Geonode == nil {
				return nil, fmt.Errorf("proxy set %q: geonode source requires a [geonode] section", raw.Name)
			}
			c := raw.Geonode
			if c.Protocol == "" {
				c.Protocol = utils.GeonodeProtocolHTTP
			}
			if c.Session.Type == "" {
				c.Session.Type = utils.GeonodeSessionRotating
			}
			src, err = utils.NewGeonodeSource(c)

		default:
			return nil, fmt.Errorf("proxy set %q: unknown source type %q (supported: static_file, bottingtools, geonode)", raw.Name, raw.SourceType)
		}

		if err != nil {
			return nil, fmt.Errorf("proxy set %q: %w", raw.Name, err)
		}
		sources[raw.Name] = src
	}

	return core.HandlerFunc(func(ctx context.Context, req *core.Request) (*core.Result, error) {
		set := getSet(ctx)
		h, ok := sources[set]
		if !ok {
			return nil, fmt.Errorf("unknown proxy set %q", set)
		}
		return h.Resolve(ctx, req)
	}), nil
}
