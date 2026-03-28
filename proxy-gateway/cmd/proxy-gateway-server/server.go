package main

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"proxy-gateway/core"
	"proxy-gateway/gateway"
)

// RunServer starts the proxy+API server on bindAddr.
// The API endpoints are enabled only when apiKey is non-empty.
func RunServer(bindAddr string, store *core.SessionStore, auth core.AuthProvider, apiKey string) error {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/sessions", handleListSessions(store, apiKey))
		r.Get("/sessions/{username}", handleGetSession(store, apiKey))
		r.Post("/sessions/{username}/rotate", handleForceRotate(store, apiKey))
		r.Get("/verify/{username}", handleVerify(store, apiKey))
	})

	proxyHandler := gateway.Handler(store, auth)
	r.HandleFunc("/*", proxyHandler.ServeHTTP)
	r.HandleFunc("/", proxyHandler.ServeHTTP)

	slog.Info("proxy gateway listening", "addr", bindAddr)
	slog.Info("available proxy sets", "sets", store.SetNames())
	slog.Info("usage: Proxy-Authorization: Basic base64(<json>:)")
	if apiKey != "" {
		slog.Info("API endpoints enabled",
			"routes", "GET /api/sessions, GET /api/sessions/{username}, POST /api/sessions/{username}/rotate, GET /api/verify/{username}")
	} else {
		slog.Info("API endpoints disabled (no API_KEY configured)")
	}

	return http.ListenAndServe(bindAddr, r)
}

// APIKey reads the API key from the API_KEY environment variable.
func APIKey() string {
	return apiKeyFromEnv()
}
