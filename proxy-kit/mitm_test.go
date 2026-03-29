package proxykit

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
)

type stubUpstream struct{}

func (stubUpstream) Dial(_ context.Context, _ *Proxy, _ string) (net.Conn, error) {
	return nil, fmt.Errorf("stub: not dialing")
}

func TestNewCA(t *testing.T) {
	ca, err := NewCA()
	if err != nil {
		t.Fatal(err)
	}
	if len(ca.Certificate) == 0 {
		t.Fatal("expected certificate")
	}
	if ca.PrivateKey == nil {
		t.Fatal("expected private key")
	}
}

func TestForgedCertProvider(t *testing.T) {
	ca, _ := NewCA()
	p, err := NewForgedCertProvider(ca)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := p.CertForHost("example.com")
	if err != nil || cert == nil {
		t.Fatal("expected cert")
	}
	cert2, _ := p.CertForHost("example.com")
	if cert != cert2 {
		t.Fatal("expected same cached cert")
	}
	cert3, _ := p.CertForHost("other.com")
	if cert3 == cert {
		t.Fatal("different host should get different cert")
	}
}

func TestStaticCertProvider(t *testing.T) {
	ca, _ := NewCA()
	p := &StaticCertProvider{Cert: ca}
	c1, _ := p.CertForHost("a.com")
	c2, _ := p.CertForHost("b.com")
	if c1 != c2 {
		t.Fatal("static provider should return same cert for all hosts")
	}
}

func TestQuickMITMPassesThroughWhenNoConn(t *testing.T) {
	ca, _ := NewCA()
	called := false
	inner := HandlerFunc(func(_ context.Context, _ *Request) (*Result, error) {
		called = true
		return Resolved(&Proxy{Host: "upstream", Port: 8080}), nil
	})

	h := QuickMITM(ca, stubUpstream{}, inner)
	result, err := h.Resolve(context.Background(), &Request{Target: "example.com:80"})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("inner should be called for non-CONNECT")
	}
	if result == nil || result.Proxy == nil || result.Proxy.Host != "upstream" {
		t.Fatal("should return inner's proxy")
	}
}

func TestMITMPassesThroughWhenTLSAlreadyBroken(t *testing.T) {
	ca, _ := NewCA()
	called := false
	inner := HandlerFunc(func(_ context.Context, _ *Request) (*Result, error) {
		called = true
		return Resolved(&Proxy{Host: "upstream", Port: 8080}), nil
	})

	h := QuickMITM(ca, stubUpstream{}, inner)
	ctx := WithTLSState(context.Background(), TLSState{Broken: true})
	h.Resolve(ctx, &Request{Target: "example.com:443"})
	if !called {
		t.Fatal("inner should be called when TLS already broken")
	}
}

func TestMITMWithCustomInterceptor(t *testing.T) {
	ca, _ := NewCA()
	certs, _ := NewForgedCertProvider(ca)
	inner := HandlerFunc(func(_ context.Context, _ *Request) (*Result, error) {
		return Resolved(&Proxy{Host: "upstream", Port: 8080}), nil
	})

	custom := InterceptorFunc(func(_ context.Context, _ *http.Request, _ string, _ *Proxy) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}}, nil
	})

	// Pass-through for non-CONNECT still works.
	h := MITM(certs, custom, inner)
	result, err := h.Resolve(context.Background(), &Request{Target: "example.com:80"})
	if err != nil || result == nil || result.Proxy == nil {
		t.Fatal("pass-through should work with custom interceptor")
	}
}

func TestMITMBlockingViaInnerPipeline(t *testing.T) {
	// Blocking is just a Handler in the inner pipeline — not a MITM feature.
	blocker := HandlerFunc(func(_ context.Context, req *Request) (*Result, error) {
		if req.HTTPRequest != nil && req.HTTPRequest.URL.Host == "blocked.com" {
			return nil, fmt.Errorf("blocked")
		}
		return Resolved(&Proxy{Host: "upstream", Port: 8080}), nil
	})

	httpReq, _ := http.NewRequest("GET", "https://blocked.com/page", nil)
	ctx := WithTLSState(context.Background(), TLSState{Broken: true})
	_, err := blocker.Resolve(ctx, &Request{HTTPRequest: httpReq})
	if err == nil {
		t.Fatal("expected block error")
	}

	httpReq2, _ := http.NewRequest("GET", "https://allowed.com/page", nil)
	result, err := blocker.Resolve(ctx, &Request{HTTPRequest: httpReq2})
	if err != nil || result == nil || result.Proxy == nil {
		t.Fatal("should pass for allowed domain")
	}
}

func TestResponseHookOnResult(t *testing.T) {
	result := &Result{
		Proxy: &Proxy{Host: "upstream", Port: 8080},
		ResponseHook: func(resp *http.Response) *http.Response {
			resp.Header.Set("X-Hooked", "yes")
			return resp
		},
	}
	resp := &http.Response{Header: http.Header{}}
	result.ResponseHook(resp)
	if resp.Header.Get("X-Hooked") != "yes" {
		t.Fatal("hook should fire")
	}
}
