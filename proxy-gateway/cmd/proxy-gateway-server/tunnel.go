package main

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
)

// HandleConnect establishes a CONNECT tunnel through an upstream proxy,
// then relays data bidirectionally between client and upstream.
func HandleConnect(clientConn net.Conn, targetAuthority string, upstream *ResolvedProxy) error {
	proxyAddr := hostPort(upstream.Host, upstream.Port)
	upstreamConn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to upstream proxy %s: %w", proxyAddr, err)
	}
	defer upstreamConn.Close()

	if err := sendConnectRequest(upstreamConn, targetAuthority, upstream); err != nil {
		return err
	}

	// Bidirectional relay.
	done := make(chan error, 2)
	go func() {
		_, err := io.Copy(upstreamConn, clientConn)
		if tc, ok := upstreamConn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- err
	}()
	go func() {
		_, err := io.Copy(clientConn, upstreamConn)
		if tc, ok := clientConn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- err
	}()
	<-done
	<-done
	return nil
}

// ForwardHTTP sends a plain HTTP request through the upstream proxy and returns the raw response bytes.
func ForwardHTTP(method, uri string, headers []string, body io.Reader, upstream *ResolvedProxy) ([]byte, error) {
	proxyAddr := hostPort(upstream.Host, upstream.Port)
	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to upstream proxy %s: %w", proxyAddr, err)
	}
	defer conn.Close()

	req := fmt.Sprintf("%s %s HTTP/1.1\r\n", method, uri)
	for _, h := range headers {
		req += h + "\r\n"
	}
	if auth := proxyAuthHeader(upstream); auth != "" {
		req += fmt.Sprintf("Proxy-Authorization: %s\r\n", auth)
	}
	req += "\r\n"

	if _, err := fmt.Fprint(conn, req); err != nil {
		return nil, err
	}
	if body != nil {
		if _, err := io.Copy(conn, body); err != nil {
			return nil, err
		}
	}

	return io.ReadAll(conn)
}

// writeRawResponse parses a raw HTTP response byte slice and writes it to w.
func writeRawResponse(w http.ResponseWriter, raw []byte) {
	headerEnd := len(raw)
	for i := 0; i+4 <= len(raw); i++ {
		if raw[i] == '\r' && raw[i+1] == '\n' && raw[i+2] == '\r' && raw[i+3] == '\n' {
			headerEnd = i
			break
		}
	}

	headerSection := string(raw[:headerEnd])
	bodyStart := headerEnd + 4
	if bodyStart > len(raw) {
		bodyStart = len(raw)
	}
	body := raw[bodyStart:]

	lines := strings.Split(headerSection, "\r\n")
	if len(lines) == 0 {
		w.WriteHeader(http.StatusBadGateway)
		return
	}

	// Parse status line.
	statusParts := strings.SplitN(lines[0], " ", 3)
	statusCode := http.StatusBadGateway
	if len(statusParts) >= 2 {
		if code, err := strconv.Atoi(statusParts[1]); err == nil {
			statusCode = code
		}
	}

	// Copy response headers.
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		if k, v, ok := strings.Cut(line, ":"); ok {
			w.Header().Set(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}

	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func sendConnectRequest(conn net.Conn, targetAuthority string, upstream *ResolvedProxy) error {
	req := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", targetAuthority, targetAuthority)
	if auth := proxyAuthHeader(upstream); auth != "" {
		req += fmt.Sprintf("Proxy-Authorization: %s\r\n", auth)
	}
	req += "\r\n"

	if _, err := fmt.Fprint(conn, req); err != nil {
		return fmt.Errorf("sending CONNECT request: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("reading CONNECT response: %w", err)
	}
	resp := string(buf[:n])
	if len(resp) < 12 || (resp[:12] != "HTTP/1.1 200" && resp[:12] != "HTTP/1.0 200") {
		return fmt.Errorf("upstream proxy rejected CONNECT: %s", resp)
	}
	return nil
}

func proxyAuthHeader(upstream *ResolvedProxy) string {
	if upstream.Username == nil {
		return ""
	}
	password := ""
	if upstream.Password != nil {
		password = *upstream.Password
	}
	creds := base64.StdEncoding.EncodeToString([]byte(*upstream.Username + ":" + password))
	return "Basic " + creds
}

// hostPort builds a host:port string safe for both IPv4/hostname and IPv6.
func hostPort(host string, port uint16) string {
	if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}
