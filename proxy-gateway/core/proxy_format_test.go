package core

import "testing"

func TestParseUserPassAtHostPort(t *testing.T) {
	p, err := ParseProxyLine("user:pass@host.com:8080", ProxyFormatUserPassAtHostPort)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "host.com" || p.Port != 8080 {
		t.Fatalf("unexpected host/port: %s:%d", p.Host, p.Port)
	}
	if p.Username == nil || *p.Username != "user" {
		t.Fatal("expected username 'user'")
	}
	if p.Password == nil || *p.Password != "pass" {
		t.Fatal("expected password 'pass'")
	}
}

func TestParseUserPassAtHostPortNoCreds(t *testing.T) {
	p, err := ParseProxyLine("host.com:8080", ProxyFormatUserPassAtHostPort)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "host.com" || p.Port != 8080 {
		t.Fatalf("unexpected: %+v", p)
	}
	if p.Username != nil {
		t.Fatal("expected no username")
	}
}

func TestParseUserPassAtHostPortWithProtocol(t *testing.T) {
	p, err := ParseProxyLine("http://user:pass@host.com:3128", ProxyFormatUserPassAtHostPort)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "host.com" || p.Port != 3128 {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseUserPassAtHostPortIPv6(t *testing.T) {
	p, err := ParseProxyLine("user:pass@[::1]:3128", ProxyFormatUserPassAtHostPort)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "::1" || p.Port != 3128 {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseUserPassAtHostPortComplexUsername(t *testing.T) {
	p, err := ParseProxyLine("exampleuser_pool-custom_type-high:XpmTeTdYy8hT@proxy.bottingtools.com:1337", ProxyFormatUserPassAtHostPort)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "proxy.bottingtools.com" || p.Port != 1337 {
		t.Fatalf("unexpected: %+v", p)
	}
	if *p.Username != "exampleuser_pool-custom_type-high" {
		t.Fatalf("unexpected username: %s", *p.Username)
	}
}

func TestParseUserPassHostPort(t *testing.T) {
	p, err := ParseProxyLine("user:pass:host.com:8080", ProxyFormatUserPassHostPort)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "host.com" || p.Port != 8080 || *p.Username != "user" {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseUserPassHostPortNoCreds(t *testing.T) {
	p, err := ParseProxyLine("host.com:8080", ProxyFormatUserPassHostPort)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "host.com" || p.Port != 8080 || p.Username != nil {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseHostPortUserPass(t *testing.T) {
	p, err := ParseProxyLine("host.com:8080:user:pass", ProxyFormatHostPortUserPass)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "host.com" || p.Port != 8080 || *p.Username != "user" || *p.Password != "pass" {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseHostPortUserPassNoCreds(t *testing.T) {
	p, err := ParseProxyLine("host.com:8080", ProxyFormatHostPortUserPass)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "host.com" || p.Port != 8080 || p.Username != nil {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseHostPortUserPassIPv6(t *testing.T) {
	p, err := ParseProxyLine("[::1]:3128", ProxyFormatHostPortUserPass)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "::1" || p.Port != 3128 {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseHostPortUserPassIPv6WithCreds(t *testing.T) {
	p, err := ParseProxyLine("[2001:db8::1]:8080:user:pass", ProxyFormatHostPortUserPass)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "2001:db8::1" || p.Port != 8080 || *p.Username != "user" {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseHostPortUserPassWithProtocol(t *testing.T) {
	p, err := ParseProxyLine("http://host.com:8080:user:pass", ProxyFormatHostPortUserPass)
	if err != nil {
		t.Fatal(err)
	}
	if p.Host != "host.com" || p.Port != 8080 {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseHostPortUserPassBadFormat(t *testing.T) {
	if _, err := ParseProxyLine("justhost", ProxyFormatHostPortUserPass); err == nil {
		t.Fatal("expected error")
	}
}
