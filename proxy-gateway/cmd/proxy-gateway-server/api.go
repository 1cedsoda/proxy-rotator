package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"proxy-gateway/core"
	"proxy-gateway/gateway"
)

type APIError struct {
	Error string `json:"error"`
}

type VerifyResult struct {
	OK       bool                `json:"ok"`
	ProxySet string              `json:"proxy_set"`
	Minutes  uint16              `json:"minutes"`
	Metadata core.AffinityParams `json:"metadata"`
	Upstream string              `json:"upstream"`
	IP       string              `json:"ip"`
	Error    *string             `json:"error,omitempty"`
}

func bearerAuth(apiKey string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if apiKey == "" {
			writeJSON(w, http.StatusNotFound, APIError{Error: "API not enabled (API_KEY env var not set)"})
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token != apiKey {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, APIError{Error: "Invalid or missing API key"})
			return
		}
		next(w, r)
	}
}

func handleListSessions(store *core.SessionStore, apiKey string) http.HandlerFunc {
	return bearerAuth(apiKey, func(w http.ResponseWriter, r *http.Request) {
		sessions := store.ListSessions()
		if sessions == nil {
			sessions = []core.SessionInfo{}
		}
		writeJSON(w, http.StatusOK, sessions)
	})
}

func handleGetSession(store *core.SessionStore, apiKey string) http.HandlerFunc {
	return bearerAuth(apiKey, func(w http.ResponseWriter, r *http.Request) {
		username := PercentDecode(chi.URLParam(r, "username"))
		info := store.GetSession(username)
		if info == nil {
			writeJSON(w, http.StatusNotFound, APIError{Error: fmt.Sprintf("No active session for '%s'", username)})
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
}

func handleForceRotate(store *core.SessionStore, apiKey string) http.HandlerFunc {
	return bearerAuth(apiKey, func(w http.ResponseWriter, r *http.Request) {
		username := PercentDecode(chi.URLParam(r, "username"))
		info, err := store.ForceRotate(r.Context(), username)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, APIError{Error: err.Error()})
			return
		}
		if info == nil {
			writeJSON(w, http.StatusNotFound, APIError{Error: fmt.Sprintf("No active session for '%s'", username)})
			return
		}
		writeJSON(w, http.StatusOK, info)
	})
}

func handleVerify(store *core.SessionStore, apiKey string) http.HandlerFunc {
	return bearerAuth(apiKey, func(w http.ResponseWriter, r *http.Request) {
		usernameb64 := PercentDecode(chi.URLParam(r, "username"))

		auth, err := ParseUsernameForVerify(usernameb64)
		if err != nil {
			errStr := fmt.Sprintf("Invalid username: %s", err)
			writeJSON(w, http.StatusOK, VerifyResult{OK: false, Error: &errStr})
			return
		}

		upstream, err := store.PickAny(r.Context(), auth.SetName)
		if err != nil || upstream == nil {
			msg := fmt.Sprintf("Unknown proxy set '%s'", auth.SetName)
			if err != nil {
				msg = err.Error()
			}
			writeJSON(w, http.StatusOK, VerifyResult{
				OK: false, ProxySet: auth.SetName, Minutes: auth.AffinityMinutes,
				Metadata: auth.AffinityParams, Error: &msg,
			})
			return
		}

		upstreamAddr := fmt.Sprintf("%s:%d", upstream.Host, upstream.Port)
		ip, ipErr := fetchIPThroughProxy(upstream)
		if ipErr != nil {
			msg := fmt.Sprintf("Proxy connectivity check failed: %s", ipErr)
			writeJSON(w, http.StatusOK, VerifyResult{
				OK: false, ProxySet: auth.SetName, Minutes: auth.AffinityMinutes,
				Metadata: auth.AffinityParams, Upstream: upstreamAddr, Error: &msg,
			})
			return
		}

		writeJSON(w, http.StatusOK, VerifyResult{
			OK: true, ProxySet: auth.SetName, Minutes: auth.AffinityMinutes,
			Metadata: auth.AffinityParams, Upstream: upstreamAddr, IP: ip,
		})
	})
}

func fetchIPThroughProxy(upstream *core.ResolvedProxy) (string, error) {
	raw, err := gateway.ForwardHTTP("GET", "http://api.ipify.org/?format=text",
		[]string{"Host: api.ipify.org"}, nil, upstream)
	if err != nil {
		return "", err
	}
	idx := len(raw)
	for i := 0; i+4 <= len(raw); i++ {
		if raw[i] == '\r' && raw[i+1] == '\n' && raw[i+2] == '\r' && raw[i+3] == '\n' {
			idx = i + 4
			break
		}
	}
	ip := strings.TrimSpace(string(raw[idx:]))
	if ip == "" {
		return "", fmt.Errorf("empty response from ip check")
	}
	return ip, nil
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
