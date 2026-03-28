package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"proxy-gateway/core"
)

func main() {
	configPath := "config.toml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	configDir := filepath.Dir(configPath)
	if configDir == "" {
		configDir = "."
	}

	proxySets, err := BuildProxySets(cfg, configDir)
	if err != nil {
		slog.Error("failed to build proxy sets", "err", err)
		os.Exit(1)
	}

	for _, ps := range proxySets {
		slog.Info("loaded proxy set", "name", ps.Name, "source", ps.Source.Describe())
	}

	auth, err := BuildAuthProvider(cfg)
	if err != nil {
		slog.Error("failed to build auth provider", "err", err)
		os.Exit(1)
	}

	store := core.NewSessionStore(proxySets)
	core.SpawnSessionCleanup(store)

	if err := RunServer(cfg.BindAddr, store, auth, APIKey()); err != nil {
		slog.Error("proxy server error", "err", err)
		os.Exit(1)
	}
}
