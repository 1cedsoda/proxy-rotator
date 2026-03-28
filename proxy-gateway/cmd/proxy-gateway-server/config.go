package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"

	"proxy-gateway/core"
	simple "proxy-gateway/auth-simple"
	bottingtools "proxy-gateway/source-bottingtools"
	geonode "proxy-gateway/source-geonode"
	staticfile "proxy-gateway/source-static-file"
)

type Config struct {
	BindAddr     string        `toml:"bind_addr"      yaml:"bind_addr"      json:"bind_addr"`
	LogLevel     string        `toml:"log_level"      yaml:"log_level"      json:"log_level"`
	AuthType     string        `toml:"auth_type"      yaml:"auth_type"      json:"auth_type"`
	AuthSub      string        `toml:"auth_sub"       yaml:"auth_sub"       json:"auth_sub"`
	AuthPassword string        `toml:"auth_password"  yaml:"auth_password"  json:"auth_password"`
	ProxySets    []ProxySetRaw `toml:"proxy_set"      yaml:"proxy_set"      json:"proxy_set"`
}

type ProxySetRaw struct {
	Name       string                 `toml:"name"        yaml:"name"        json:"name"`
	SourceType string                 `toml:"source_type" yaml:"source_type" json:"source_type"`
	Source     map[string]interface{} `toml:"source"      yaml:"source"      json:"source"`
}

// LoadConfig reads and parses a config file.
// Format is detected from file extension: .toml (default), .yaml/.yml, .json.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	cfg := &Config{
		BindAddr: "127.0.0.1:8100",
		LogLevel: "info",
	}

	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config %s: %w", path, err)
		}
	case ".json":
		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config %s: %w", path, err)
		}
	default:
		if err := toml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config %s: %w", path, err)
		}
	}

	return cfg, nil
}

// BuildAuthProvider constructs the configured AuthProvider.
// auth_type "simple" (default) requires auth_sub and auth_password.
func BuildAuthProvider(cfg *Config) (core.AuthProvider, error) {
	switch cfg.AuthType {
	case "", "simple":
		if cfg.AuthSub == "" {
			return nil, fmt.Errorf("auth_type \"simple\" requires auth_sub to be set")
		}
		if cfg.AuthPassword == "" {
			return nil, fmt.Errorf("auth_type \"simple\" requires auth_password to be set")
		}
		return simple.New(cfg.AuthSub, cfg.AuthPassword), nil
	default:
		return nil, fmt.Errorf("unknown auth_type %q (supported: simple)", cfg.AuthType)
	}
}

func BuildProxySets(cfg *Config, configDir string) ([]core.ProxySet, error) {
	var sets []core.ProxySet
	for _, raw := range cfg.ProxySets {
		src, err := buildSource(raw.SourceType, raw.Source, configDir)
		if err != nil {
			return nil, fmt.Errorf("proxy set %q (type %q): %w", raw.Name, raw.SourceType, err)
		}
		sets = append(sets, core.ProxySet{Name: raw.Name, Source: src})
	}
	return sets, nil
}

func buildSource(sourceType string, rawSource map[string]interface{}, configDir string) (core.ProxySource, error) {
	jsonBytes, err := json.Marshal(normalizeMap(rawSource))
	if err != nil {
		return nil, fmt.Errorf("re-encoding source config: %w", err)
	}

	switch sourceType {
	case "static_file":
		var cfg staticfile.Config
		cfg.Format = core.DefaultProxyFormat
		if err := json.Unmarshal(jsonBytes, &cfg); err != nil {
			return nil, fmt.Errorf("invalid static_file source config: %w", err)
		}
		return staticfile.BuildSource(&cfg, configDir)

	case "bottingtools":
		var rawCfg struct {
			Username    string                        `json:"username"`
			PasswordEnv string                        `json:"password_env"`
			Host        string                        `json:"host"`
			Product     bottingtools.RawProductConfig `json:"product"`
		}
		if err := json.Unmarshal(jsonBytes, &rawCfg); err != nil {
			return nil, fmt.Errorf("invalid bottingtools source config: %w", err)
		}
		product, err := bottingtools.ParseProductConfig(rawCfg.Product)
		if err != nil {
			return nil, err
		}
		return bottingtools.BuildSource(&bottingtools.Config{
			Username:    rawCfg.Username,
			PasswordEnv: rawCfg.PasswordEnv,
			Host:        rawCfg.Host,
			Product:     product,
		})

	case "geonode":
		var cfg geonode.Config
		cfg.Protocol = geonode.GeonodeProtocolHTTP
		if err := json.Unmarshal(jsonBytes, &cfg); err != nil {
			return nil, fmt.Errorf("invalid geonode source config: %w", err)
		}
		if cfg.Session.Type == "" {
			cfg.Session.Type = geonode.SessionTypeRotating
		}
		return geonode.BuildSource(&cfg)

	default:
		return nil, fmt.Errorf("unknown source type %q (supported: static_file, bottingtools, geonode)", sourceType)
	}
}

func normalizeMap(v interface{}) interface{} {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, v := range val {
			out[fmt.Sprintf("%v", k)] = normalizeMap(v)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, v := range val {
			out[k] = normalizeMap(v)
		}
		return out
	case []interface{}:
		for i, item := range val {
			val[i] = normalizeMap(item)
		}
		return val
	default:
		return val
	}
}

func apiKeyFromEnv() string {
	return os.Getenv("API_KEY")
}
