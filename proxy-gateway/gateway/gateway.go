// Package gateway provides a minimal HTTP proxy server that tunnels traffic
// through upstream proxies managed by a core.SessionStore.
//
// There is no REST API and no configuration file loading. Callers construct
// a SessionStore and an AuthProvider programmatically and hand them to Run.
//
// Example:
//
//	store := core.NewSessionStore([]core.ProxySet{
//	    {Name: "residential", Source: mySource},
//	})
//	core.SpawnSessionCleanup(store)
//	auth := simple.New("alice", "s3cret")
//	log.Fatal(gateway.Run(":8100", store, auth))
package gateway

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"proxy-gateway/core"
)

// Run starts the proxy server on addr and blocks until it returns an error.
func Run(addr string, store *core.SessionStore, auth core.AuthProvider) error {
	slog.Info("proxy gateway listening", "addr", addr)
	slog.Info("available proxy sets", "sets", store.SetNames())
	return http.ListenAndServe(addr, Handler(store, auth))
}

// Handler returns the http.Handler for the proxy server so callers can embed
// it inside a larger http.Server (e.g. to set timeouts or TLS).
func Handler(store *core.SessionStore, auth core.AuthProvider) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)

		result, err := parseAndAuthenticate(r.Header.Get("Proxy-Authorization"), auth, store.SetNames())
		if err != nil {
			slog.Warn("auth error", "method", r.Method, "uri", r.RequestURI, "client", clientIP, "err", err)
			w.Header().Set("Proxy-Authenticate", `Basic realm="proxy-gateway"`)
			http.Error(w, err.Error(), http.StatusProxyAuthRequired)
			return
		}

		upstream, err := store.NextProxy(r.Context(), result.SetName, result.AffinityMinutes, result.UsernameB64, result.AffinityParams)
		if err != nil || upstream == nil {
			slog.Warn("unknown proxy set", "set", result.SetName)
			http.Error(w,
				fmt.Sprintf("Unknown proxy set '%s'. Available: %v", result.SetName, store.SetNames()),
				http.StatusBadRequest)
			return
		}

		slog.Info("routing request",
			"method", r.Method,
			"uri", r.RequestURI,
			"set", result.SetName,
			"minutes", result.AffinityMinutes,
			"upstream", fmt.Sprintf("%s:%d", upstream.Host, upstream.Port),
			"client", clientIP,
		)

		// Open a connection handle if the auth provider implements ConnectionTracker.
		var handle core.ConnHandle
		if tracker, ok := auth.(core.ConnectionTracker); ok {
			handle, err = tracker.OpenConnection(result.Sub)
			if err != nil {
				slog.Warn("connection rejected by tracker", "sub", result.Sub, "err", err)
				http.Error(w, err.Error(), http.StatusTooManyRequests)
				return
			}
		}

		if r.Method == http.MethodConnect {
			serveConnect(w, r, upstream, handle)
		} else {
			serveHTTP(w, r, upstream, handle)
		}
	})
}

// ---------------------------------------------------------------------------
// CONNECT tunnel
// ---------------------------------------------------------------------------

func serveConnect(w http.ResponseWriter, r *http.Request, upstream *core.ResolvedProxy, handle core.ConnHandle) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		if handle != nil {
			handle.Close(0, 0)
		}
		return
	}
	w.WriteHeader(http.StatusOK)
	conn, _, err := hj.Hijack()
	if err != nil {
		slog.Error("hijack failed", "err", err)
		if handle != nil {
			handle.Close(0, 0)
		}
		return
	}
	defer conn.Close()

	sent, received, err := connectTunnel(conn, r.Host, upstream, handle)
	if handle != nil {
		handle.Close(sent, received)
	}
	if err != nil {
		slog.Debug("tunnel closed", "err", err)
	}
}

func connectTunnel(clientConn net.Conn, target string, upstream *core.ResolvedProxy, handle core.ConnHandle) (sent, received int64, err error) {
	upstreamConn, err := net.Dial("tcp", hostPort(upstream.Host, upstream.Port))
	if err != nil {
		return 0, 0, fmt.Errorf("connecting to upstream %s: %w", hostPort(upstream.Host, upstream.Port), err)
	}
	defer upstreamConn.Close()

	// Send CONNECT to upstream.
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", target, target)
	if proxyAuth := upstreamProxyAuth(upstream); proxyAuth != "" {
		req += "Proxy-Authorization: " + proxyAuth + "\r\n"
	}
	req += "\r\n"
	if _, err = fmt.Fprint(upstreamConn, req); err != nil {
		return 0, 0, fmt.Errorf("sending CONNECT: %w", err)
	}

	// Read the full CONNECT response (loop until \r\n\r\n).
	// Some upstream proxies send the status line and headers in separate TCP
	// packets. A single Read() would only get the first packet and miss the
	// terminal \r\n\r\n, corrupting the subsequent TLS handshake.
	var respBuf []byte
	tmp := make([]byte, 1024)
	for {
		n, readErr := upstreamConn.Read(tmp)
		if n > 0 {
			respBuf = append(respBuf, tmp[:n]...)
		}
		if readErr != nil {
			return 0, 0, fmt.Errorf("reading CONNECT response: %w", readErr)
		}
		// Check for end of headers.
		for i := 0; i+3 < len(respBuf); i++ {
			if respBuf[i] == '\r' && respBuf[i+1] == '\n' && respBuf[i+2] == '\r' && respBuf[i+3] == '\n' {
				goto gotResponse
			}
		}
	}
gotResponse:
	resp := string(respBuf)
	if len(resp) < 12 || (resp[:12] != "HTTP/1.1 200" && resp[:12] != "HTTP/1.0 200") {
		return 0, 0, fmt.Errorf("upstream rejected CONNECT: %s", resp)
	}

	// Build a cancellable context so either goroutine can tear down both sides.
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	cancelConn := func() {
		cancel()
		_ = clientConn.SetDeadline(immediateDeadline())
		_ = upstreamConn.SetDeadline(immediateDeadline())
	}

	// Wrap connections with counting readers that feed RecordTraffic.
	var clientReader io.Reader = clientConn
	var upstreamReader io.Reader = upstreamConn
	if handle != nil {
		clientReader = &countingReader{r: clientConn, upstream: true, handle: handle, cancel: cancelConn}
		upstreamReader = &countingReader{r: upstreamConn, upstream: false, handle: handle, cancel: cancelConn}
	}

	type relayResult struct {
		n   int64
		err error
	}
	sentCh := make(chan relayResult, 1)
	recvCh := make(chan relayResult, 1)

	go func() {
		n, err := io.Copy(upstreamConn, clientReader)
		if tc, ok := upstreamConn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		sentCh <- relayResult{n, err}
	}()
	go func() {
		n, err := io.Copy(clientConn, upstreamReader)
		if tc, ok := clientConn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		recvCh <- relayResult{n, err}
	}()

	sr := <-sentCh
	rr := <-recvCh
	return sr.n, rr.n, nil
}

// ---------------------------------------------------------------------------
// Plain HTTP forwarding
// ---------------------------------------------------------------------------

func serveHTTP(w http.ResponseWriter, r *http.Request, upstream *core.ResolvedProxy, handle core.ConnHandle) {
	var headers []string
	for name, values := range r.Header {
		if isHopByHop(name) {
			continue
		}
		for _, v := range values {
			headers = append(headers, name+": "+v)
		}
	}

	uri := r.RequestURI
	if !strings.HasPrefix(uri, "http://") && !strings.HasPrefix(uri, "https://") {
		uri = "http://" + r.Host + uri
	}

	raw, err := ForwardHTTP(r.Method, uri, headers, r.Body, upstream)

	if handle != nil {
		var reqBytes int64
		if r.ContentLength > 0 {
			reqBytes = r.ContentLength
		}
		respBytes := int64(len(raw))
		handle.RecordTraffic(true, reqBytes, func() {})
		handle.RecordTraffic(false, respBytes, func() {})
		handle.Close(reqBytes, respBytes)
	}

	if err != nil {
		http.Error(w, "upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}

	writeRawResponse(w, raw)
}

// ---------------------------------------------------------------------------
// countingReader
// ---------------------------------------------------------------------------

type countingReader struct {
	r        io.Reader
	upstream bool
	handle   core.ConnHandle
	cancel   func()
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 {
		cr.handle.RecordTraffic(cr.upstream, int64(n), cr.cancel)
	}
	return n, err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeRawResponse(w http.ResponseWriter, raw []byte) {
	headerEnd := len(raw)
	for i := 0; i+4 <= len(raw); i++ {
		if raw[i] == '\r' && raw[i+1] == '\n' && raw[i+2] == '\r' && raw[i+3] == '\n' {
			headerEnd = i
			break
		}
	}
	bodyStart := headerEnd + 4
	if bodyStart > len(raw) {
		bodyStart = len(raw)
	}

	lines := strings.Split(string(raw[:headerEnd]), "\r\n")
	if len(lines) == 0 {
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	statusCode := http.StatusBadGateway
	if parts := strings.SplitN(lines[0], " ", 3); len(parts) >= 2 {
		if code, err := strconv.Atoi(parts[1]); err == nil {
			statusCode = code
		}
	}
	for _, line := range lines[1:] {
		if k, v, ok := strings.Cut(line, ":"); ok {
			w.Header().Set(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
	w.WriteHeader(statusCode)
	_, _ = w.Write(raw[bodyStart:])
}

func upstreamProxyAuth(upstream *core.ResolvedProxy) string {
	if upstream.Username == nil {
		return ""
	}
	pass := ""
	if upstream.Password != nil {
		pass = *upstream.Password
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(*upstream.Username+":"+pass))
}

func hostPort(host string, port uint16) string {
	if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func isHopByHop(header string) bool {
	switch http.CanonicalHeaderKey(header) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Te", "Trailers", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}

func immediateDeadline() time.Time {
	return time.Unix(0, 1)
}

// ---------------------------------------------------------------------------
// Auth parsing
// ---------------------------------------------------------------------------

func parseAndAuthenticate(headerVal string, auth core.AuthProvider, availableSets []string) (*core.AuthResult, error) {
	b64, ok := strings.CutPrefix(headerVal, "Basic ")
	if !ok {
		return nil, fmt.Errorf(
			"Proxy-Authorization must use Basic scheme. Expected: Basic base64({\"sub\":\"<user>\",\"set\":\"<proxyset>\",\"minutes\":<0-1440>,\"meta\":{...}}:<password>). Available sets: %v",
			availableSets,
		)
	}
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 in Proxy-Authorization")
	}

	raw := string(decoded)
	colonIdx := strings.LastIndex(raw, ":")
	if colonIdx < 0 {
		return nil, fmt.Errorf("invalid Basic credentials: missing colon separator")
	}
	usernameJSON := raw[:colonIdx]
	password := raw[colonIdx+1:]

	if usernameJSON == "" {
		return nil, fmt.Errorf("empty username in Proxy-Authorization")
	}

	parsed, err := core.ParseUsernameJSON(usernameJSON)
	if err != nil {
		return nil, err
	}

	if err := auth.Authenticate(parsed.Sub, password); err != nil {
		return nil, err
	}

	return &core.AuthResult{
		Sub:             parsed.Sub,
		SetName:         parsed.SetName,
		AffinityMinutes: parsed.AffinityMinutes,
		UsernameB64:     b64,
		AffinityParams:  parsed.AffinityParams,
	}, nil
}
