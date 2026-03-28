package gateway

import (
	"fmt"
	"io"
	"net"

	"proxy-gateway/core"
)

// ForwardHTTP sends a plain HTTP request through an upstream proxy and returns
// the raw response bytes. Exported for use by callers that need to make
// out-of-band requests through a proxy (e.g. the verify endpoint).
func ForwardHTTP(method, uri string, headers []string, body io.Reader, upstream *core.ResolvedProxy) ([]byte, error) {
	conn, err := net.Dial("tcp", hostPort(upstream.Host, upstream.Port))
	if err != nil {
		return nil, fmt.Errorf("connecting to upstream %s: %w", hostPort(upstream.Host, upstream.Port), err)
	}
	defer conn.Close()

	req := fmt.Sprintf("%s %s HTTP/1.1\r\n", method, uri)
	for _, h := range headers {
		req += h + "\r\n"
	}
	if auth := upstreamProxyAuth(upstream); auth != "" {
		req += "Proxy-Authorization: " + auth + "\r\n"
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
