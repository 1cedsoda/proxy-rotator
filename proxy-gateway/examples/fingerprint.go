// Package examples contains example middleware for the proxy gateway pipeline.
package examples

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sardanioss/httpcloak/client"

	"proxy-gateway/core"
)

// Fingerprint returns MITM middleware that uses httpcloak to spoof the
// upstream TLS fingerprint as a real browser.
//
// Instead of duplicating TLS termination logic, this uses core.MITM with
// a custom Interceptor that forwards via httpcloak instead of Go's crypto/tls.
//
// Usage:
//
//	ca, _ := core.NewCA()
//	pipeline := examples.Fingerprint(ca, "chrome-latest",
//	    core.Auth(auth,
//	        core.Session(source),
//	    ),
//	)
//	core.ListenHTTP(":8100", pipeline)
func Fingerprint(ca tls.Certificate, preset string, inner core.Handler) core.Handler {
	certs, err := core.NewForgedCertProvider(ca)
	if err != nil {
		panic(fmt.Sprintf("fingerprint: %v", err))
	}
	return core.MITM(certs, &FingerprintInterceptor{Preset: preset}, inner)
}

// FingerprintInterceptor forwards requests using httpcloak with a browser
// TLS fingerprint preset. Implements core.Interceptor.
type FingerprintInterceptor struct {
	Preset string
}

func (f *FingerprintInterceptor) RoundTrip(ctx context.Context, httpReq *http.Request, host string, proxy *core.Proxy) (*http.Response, error) {
	var proxyURL string
	switch proxy.Proto() {
	case core.ProtocolSOCKS5:
		if proxy.Username != "" {
			proxyURL = fmt.Sprintf("socks5://%s:%s@%s:%d", proxy.Username, proxy.Password, proxy.Host, proxy.Port)
		} else {
			proxyURL = fmt.Sprintf("socks5://%s:%d", proxy.Host, proxy.Port)
		}
	default:
		if proxy.Username != "" {
			proxyURL = fmt.Sprintf("http://%s:%s@%s:%d", proxy.Username, proxy.Password, proxy.Host, proxy.Port)
		} else {
			proxyURL = fmt.Sprintf("http://%s:%d", proxy.Host, proxy.Port)
		}
	}

	opts := []client.Option{
		client.WithTimeout(30 * time.Second),
	}
	if proxyURL != "" {
		opts = append(opts, client.WithProxy(proxyURL))
	}
	c := client.NewClient(f.Preset, opts...)
	defer c.Close()

	targetURL := fmt.Sprintf("https://%s%s", host, httpReq.URL.RequestURI())

	headers := make(map[string][]string)
	for k, vs := range httpReq.Header {
		lower := strings.ToLower(k)
		if lower == "connection" || lower == "proxy-authorization" || lower == "proxy-connection" {
			continue
		}
		headers[k] = vs
	}

	cloakReq := &client.Request{
		Method:  httpReq.Method,
		URL:     targetURL,
		Headers: headers,
	}
	if httpReq.Body != nil && httpReq.Method != http.MethodGet && httpReq.Method != http.MethodHead {
		cloakReq.Body = httpReq.Body
	}

	resp, err := c.Do(ctx, cloakReq)
	if err != nil {
		return nil, fmt.Errorf("httpcloak request to %s: %w", targetURL, err)
	}

	body, _ := resp.Bytes()
	httpResp := &http.Response{
		StatusCode: resp.StatusCode,
		Status:     fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(body))),
	}
	for k, vs := range resp.Headers {
		for _, v := range vs {
			httpResp.Header.Add(k, v)
		}
	}

	return httpResp, nil
}
