package main

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// RunProxy starts the proxy+API server on bindAddr.
func RunProxy(bindAddr string, rotator *Rotator, apiKey string) error {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	// API routes (Bearer-auth gated).
	r.Route("/api", func(r chi.Router) {
		r.Get("/sessions", handleListSessions(rotator, apiKey))
		r.Get("/sessions/{username}", handleGetSession(rotator, apiKey))
		r.Post("/sessions/{username}/rotate", handleForceRotate(rotator, apiKey))
		r.Get("/verify/{username}", handleVerify(rotator, apiKey))
	})

	// Everything else is proxy traffic.
	r.HandleFunc("/*", handleProxy(rotator))
	r.HandleFunc("/", handleProxy(rotator))

	srv := &http.Server{
		Addr:    bindAddr,
		Handler: r,
	}

	slog.Info("Proxy gateway listening", "addr", bindAddr)
	slog.Info("Available proxy sets", "sets", rotator.SetNames())
	slog.Info("Usage: Proxy-Authorization: Basic base64(<json>:)")
	slog.Info(`  Example: base64({"meta":{"app":"myapp"},"minutes":5,"set":"residential"}:)`)
	if apiKey != "" {
		slog.Info("API endpoints enabled", "routes", "GET /api/sessions, GET /api/sessions/{username}, POST /api/sessions/{username}/rotate, GET /api/verify/{username}")
	} else {
		slog.Info("API endpoints disabled (no API_KEY configured)")
	}

	return srv.ListenAndServe()
}

// handleProxy handles both CONNECT tunnel requests and plain HTTP forwarding.
func handleProxy(rotator *Rotator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)

		// Parse Proxy-Authorization.
		auth, err := ParseProxyAuthHeader(r.Header.Get("Proxy-Authorization"))
		if err != nil {
			slog.Warn("auth error",
				"method", r.Method,
				"uri", r.RequestURI,
				"client", clientIP,
				"err", err,
			)
			w.Header().Set("Proxy-Authenticate", `Basic realm="proxy-gateway"`)
			http.Error(w, fmt.Sprintf(
				"%s. Expected: Basic base64({\"meta\":{...},\"minutes\":<0-1440>,\"set\":\"<proxyset>\"}:). Available sets: %v",
				err.Error(), rotator.SetNames(),
			), http.StatusProxyAuthRequired)
			return
		}

		upstream, err := rotator.NextProxy(r.Context(), auth.SetName, auth.AffinityMinutes, auth.UsernameB64, auth.AffinityParams)
		if err != nil || upstream == nil {
			msg := fmt.Sprintf("Unknown proxy set '%s'. Available: %v", auth.SetName, rotator.SetNames())
			slog.Warn("unknown proxy set", "set", auth.SetName)
			http.Error(w, msg, http.StatusBadRequest)
			return
		}

		slog.Info("routing request",
			"method", r.Method,
			"uri", r.RequestURI,
			"set", auth.SetName,
			"minutes", auth.AffinityMinutes,
			"upstream", fmt.Sprintf("%s:%d", upstream.Host, upstream.Port),
			"client", clientIP,
		)

		if r.Method == http.MethodConnect {
			handleConnect(w, r, upstream)
		} else {
			handleHTTP(w, r, upstream)
		}
	}
}

// handleConnect hijacks the connection and establishes a CONNECT tunnel.
func handleConnect(w http.ResponseWriter, r *http.Request, upstream *ResolvedProxy) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	// Acknowledge the CONNECT before hijacking.
	w.WriteHeader(http.StatusOK)

	conn, _, err := hj.Hijack()
	if err != nil {
		slog.Error("hijack failed", "err", err)
		return
	}
	defer conn.Close()

	if err := HandleConnect(conn, r.Host, upstream); err != nil {
		slog.Error("tunnel error", "err", err)
	}
}

// handleHTTP forwards a plain HTTP request through the upstream proxy.
func handleHTTP(w http.ResponseWriter, r *http.Request, upstream *ResolvedProxy) {
	// Build header list, stripping hop-by-hop and proxy headers.
	var headers []string
	for name, values := range r.Header {
		if isHopByHop(name) {
			continue
		}
		for _, v := range values {
			headers = append(headers, fmt.Sprintf("%s: %s", name, v))
		}
	}

	// Ensure absolute URI for proxy forwarding.
	uri := r.RequestURI
	if !isAbsoluteURI(uri) {
		scheme := "http"
		uri = scheme + "://" + r.Host + uri
	}

	raw, err := ForwardHTTP(r.Method, uri, headers, r.Body, upstream)
	if err != nil {
		http.Error(w, fmt.Sprintf("Proxy error: %s", err), http.StatusBadGateway)
		return
	}

	writeRawResponse(w, raw)
}

func isHopByHop(header string) bool {
	switch http.CanonicalHeaderKey(header) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Te", "Trailers", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}

func isAbsoluteURI(uri string) bool {
	return len(uri) > 7 && (uri[:7] == "http://" || uri[:8] == "https://")
}
