package main

import (
	"encoding/base64"
	"fmt"
	"testing"
)

func authHeader(usernameJSON string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(usernameJSON + ":"))
	return "Basic " + encoded
}

func makeUsername(set string, minutes int, metaJSON string) string {
	return fmt.Sprintf(`{"meta":%s,"minutes":%d,"set":"%s"}`, metaJSON, minutes, set)
}

func TestValidBasic(t *testing.T) {
	u := makeUsername("residential", 5, `{"app":"myapp","user":"alice"}`)
	auth, err := ParseProxyAuthHeader(authHeader(u))
	if err != nil {
		t.Fatal(err)
	}
	if auth.SetName != "residential" {
		t.Fatalf("expected residential, got %s", auth.SetName)
	}
	if auth.AffinityMinutes != 5 {
		t.Fatalf("expected 5, got %d", auth.AffinityMinutes)
	}
	if auth.AffinityParams.GetString("app") != "myapp" {
		t.Fatal("expected app=myapp")
	}
}

func TestValidEmptyMeta(t *testing.T) {
	u := makeUsername("residential", 0, "{}")
	auth, err := ParseProxyAuthHeader(authHeader(u))
	if err != nil {
		t.Fatal(err)
	}
	if auth.AffinityMinutes != 0 || len(auth.AffinityParams) != 0 {
		t.Fatalf("unexpected: %+v", auth)
	}
}

func TestValidMaxMinutes(t *testing.T) {
	u := makeUsername("residential", 1440, `{"k":"v"}`)
	auth, err := ParseProxyAuthHeader(authHeader(u))
	if err != nil {
		t.Fatal(err)
	}
	if auth.AffinityMinutes != 1440 {
		t.Fatalf("expected 1440, got %d", auth.AffinityMinutes)
	}
}

func TestAnyKeyOrderAccepted(t *testing.T) {
	u := `{"set":"residential","meta":{},"minutes":5}`
	if _, err := ParseProxyAuthHeader(authHeader(u)); err != nil {
		t.Fatal(err)
	}
}

func TestNotBasicAuth(t *testing.T) {
	if _, err := ParseProxyAuthHeader("Bearer token123"); err == nil {
		t.Fatal("expected error")
	}
}

func TestEmptyUsername(t *testing.T) {
	if _, err := ParseProxyAuthHeader(authHeader("")); err == nil {
		t.Fatal("expected error")
	}
}

func TestMissingKeySet(t *testing.T) {
	if _, err := ParseProxyAuthHeader(authHeader(`{"meta":{},"minutes":5}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestMissingKeyMinutes(t *testing.T) {
	if _, err := ParseProxyAuthHeader(authHeader(`{"meta":{},"set":"residential"}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestMissingKeyMeta(t *testing.T) {
	if _, err := ParseProxyAuthHeader(authHeader(`{"minutes":5,"set":"residential"}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestExtraKeyRejected(t *testing.T) {
	if _, err := ParseProxyAuthHeader(authHeader(`{"extra":"x","meta":{},"minutes":5,"set":"residential"}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestSetEmptyRejected(t *testing.T) {
	if _, err := ParseProxyAuthHeader(authHeader(`{"meta":{},"minutes":5,"set":""}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestSetNonAlphanumericRejected(t *testing.T) {
	if _, err := ParseProxyAuthHeader(authHeader(`{"meta":{},"minutes":5,"set":"resi-dential"}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestSetNotStringRejected(t *testing.T) {
	if _, err := ParseProxyAuthHeader(authHeader(`{"meta":{},"minutes":5,"set":123}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestMinutesTooHigh(t *testing.T) {
	u := makeUsername("residential", 1441, `{"k":"v"}`)
	if _, err := ParseProxyAuthHeader(authHeader(u)); err == nil {
		t.Fatal("expected error")
	}
}

func TestMinutesFloatRejected(t *testing.T) {
	if _, err := ParseProxyAuthHeader(authHeader(`{"meta":{},"minutes":5.5,"set":"residential"}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestMinutesStringRejected(t *testing.T) {
	if _, err := ParseProxyAuthHeader(authHeader(`{"meta":{},"minutes":"5","set":"residential"}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestMetaNestedObjectRejected(t *testing.T) {
	u := makeUsername("residential", 5, `{"a":{"b":1}}`)
	_, err := ParseProxyAuthHeader(authHeader(u))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMetaArrayRejected(t *testing.T) {
	u := makeUsername("residential", 5, `{"a":[1,2,3]}`)
	_, err := ParseProxyAuthHeader(authHeader(u))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMetaBooleanRejected(t *testing.T) {
	u := makeUsername("residential", 5, `{"flag":true}`)
	_, err := ParseProxyAuthHeader(authHeader(u))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUsernameB64IsStable(t *testing.T) {
	u := makeUsername("residential", 5, `{"app":"myapp"}`)
	a, _ := ParseProxyAuthHeader(authHeader(u))
	b, _ := ParseProxyAuthHeader(authHeader(u))
	if a.UsernameB64 != b.UsernameB64 {
		t.Fatal("username_b64 should be stable for same input")
	}
}

func TestPercentDecode(t *testing.T) {
	cases := []struct{ in, out string }{
		{"abc%2Bdef", "abc+def"},
		{"abc%2Fdef", "abc/def"},
		{"abc%3Ddef", "abc=def"},
		{"plain", "plain"},
		{"", ""},
	}
	for _, c := range cases {
		got := PercentDecode(c.in)
		if got != c.out {
			t.Errorf("PercentDecode(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}
