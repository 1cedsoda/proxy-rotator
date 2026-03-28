package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"

	"proxy-gateway/core"
)

// vecSource is a test double backed by a CountingPool.
type vecSource struct {
	pool *core.CountingPool[core.SourceProxy]
}

func (v *vecSource) GetSourceProxy(_ context.Context, _ core.AffinityParams) (*core.SourceProxy, error) {
	p := v.pool.Next()
	if p == nil {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (v *vecSource) GetSourceProxyForceRotate(_ context.Context, _ core.AffinityParams, current *core.SourceProxy) (*core.SourceProxy, error) {
	p := v.pool.NextExcluding(func(sp core.SourceProxy) bool {
		if current == nil {
			return false
		}
		return sp.Equal(*current)
	})
	if p == nil {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (v *vecSource) Describe() string {
	return fmt.Sprintf("vecSource(%d entries)", v.pool.Len())
}

func makeTestSet(n int) ProxySet {
	proxies := make([]core.SourceProxy, n)
	for i := range proxies {
		user := "testuser"
		pass := "testpass"
		proxies[i] = core.SourceProxy{
			Host:     fmt.Sprintf("proxy%d.example.com", i),
			Port:     8080,
			Username: &user,
			Password: &pass,
		}
	}
	return ProxySet{
		Name:   "test",
		Source: &vecSource{pool: core.NewCountingPool(proxies)},
	}
}

func makeUsernameB64(set string, minutes uint16, pairs [][2]string) string {
	meta := "{"
	for i, p := range pairs {
		if i > 0 {
			meta += ","
		}
		meta += fmt.Sprintf("%q:%q", p[0], p[1])
	}
	meta += "}"
	j := fmt.Sprintf(`{"meta":%s,"minutes":%d,"set":"%s"}`, meta, minutes, set)
	return base64.StdEncoding.EncodeToString([]byte(j))
}

func ctx() context.Context { return context.Background() }

func TestLeastUsedDistributesEvenly(t *testing.T) {
	rotator := NewRotator([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 0, [][2]string{{"k", "v"}})
	counts := map[string]int{}
	for i := 0; i < 400; i++ {
		p, err := rotator.NextProxy(ctx(), "test", 0, b64, core.NewAffinityParams())
		if err != nil || p == nil {
			t.Fatal("expected proxy")
		}
		counts[p.Host]++
	}
	if len(counts) != 4 {
		t.Fatalf("expected 4 distinct hosts, got %d", len(counts))
	}
	for host, count := range counts {
		if count != 100 {
			t.Errorf("host %s: got %d, expected 100", host, count)
		}
	}
}

func TestSessionAffinityWithMinutes(t *testing.T) {
	rotator := NewRotator([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 5, [][2]string{{"session", "mysession"}})

	first, err := rotator.NextProxy(ctx(), "test", 5, b64, core.NewAffinityParams())
	if err != nil || first == nil {
		t.Fatal("expected proxy")
	}
	for i := 0; i < 10; i++ {
		subsequent, err := rotator.NextProxy(ctx(), "test", 5, b64, core.NewAffinityParams())
		if err != nil || subsequent == nil {
			t.Fatal("expected proxy")
		}
		if subsequent.Host != first.Host {
			t.Fatalf("affinity should pin to same proxy: got %s, want %s", subsequent.Host, first.Host)
		}
	}
}

func TestZeroMinutesNoAffinity(t *testing.T) {
	rotator := NewRotator([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 0, [][2]string{{"k", "v"}})
	hosts := map[string]bool{}
	for i := 0; i < 100; i++ {
		p, _ := rotator.NextProxy(ctx(), "test", 0, b64, core.NewAffinityParams())
		hosts[p.Host] = true
	}
	if len(hosts) <= 1 {
		t.Fatal("should distribute without affinity")
	}
}

func TestDifferentSessionKeysIndependent(t *testing.T) {
	rotator := NewRotator([]ProxySet{makeTestSet(4)})
	b64a := makeUsernameB64("test", 60, [][2]string{{"session", "sessA"}})
	b64b := makeUsernameB64("test", 60, [][2]string{{"session", "sessB"}})

	pa, _ := rotator.NextProxy(ctx(), "test", 60, b64a, core.NewAffinityParams())
	pb, _ := rotator.NextProxy(ctx(), "test", 60, b64b, core.NewAffinityParams())

	for i := 0; i < 5; i++ {
		pa2, _ := rotator.NextProxy(ctx(), "test", 60, b64a, core.NewAffinityParams())
		pb2, _ := rotator.NextProxy(ctx(), "test", 60, b64b, core.NewAffinityParams())
		if pa2.Host != pa.Host {
			t.Fatalf("session A should be pinned: got %s, want %s", pa2.Host, pa.Host)
		}
		if pb2.Host != pb.Host {
			t.Fatalf("session B should be pinned: got %s, want %s", pb2.Host, pb.Host)
		}
	}
}

func TestUnknownSetReturnsNil(t *testing.T) {
	rotator := NewRotator([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("unknown", 0, nil)
	p, err := rotator.NextProxy(ctx(), "unknown", 0, b64, core.NewAffinityParams())
	if err != nil || p != nil {
		t.Fatalf("expected nil for unknown set")
	}
}

func TestGetSessionActive(t *testing.T) {
	rotator := NewRotator([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 5, [][2]string{{"session", "mysess"}})

	p, _ := rotator.NextProxy(ctx(), "test", 5, b64, core.NewAffinityParams())
	info := rotator.GetSession(b64)
	if info == nil {
		t.Fatal("expected session info")
	}
	if info.ProxySet != "test" || info.Username != b64 {
		t.Fatalf("unexpected session info: %+v", info)
	}
	expected := fmt.Sprintf("%s:%d", p.Host, p.Port)
	if info.Upstream != expected {
		t.Fatalf("expected upstream %s, got %s", expected, info.Upstream)
	}
}

func TestGetSessionNoAffinityReturnsNil(t *testing.T) {
	rotator := NewRotator([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 0, [][2]string{{"session", "nosess"}})
	rotator.NextProxy(ctx(), "test", 0, b64, core.NewAffinityParams())
	if rotator.GetSession(b64) != nil {
		t.Fatal("0-minute sessions should not be tracked")
	}
}

func TestListSessions(t *testing.T) {
	rotator := NewRotator([]ProxySet{makeTestSet(4)})
	b64a := makeUsernameB64("test", 5, [][2]string{{"session", "sessA"}})
	b64b := makeUsernameB64("test", 10, [][2]string{{"session", "sessB"}})
	b64z := makeUsernameB64("test", 0, [][2]string{{"session", "noaff"}})

	rotator.NextProxy(ctx(), "test", 5, b64a, core.NewAffinityParams())
	rotator.NextProxy(ctx(), "test", 10, b64b, core.NewAffinityParams())
	rotator.NextProxy(ctx(), "test", 0, b64z, core.NewAffinityParams())

	sessions := rotator.ListSessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestSessionIDsAreUniqueAndIncrement(t *testing.T) {
	rotator := NewRotator([]ProxySet{makeTestSet(4)})
	b64a := makeUsernameB64("test", 5, [][2]string{{"session", "a"}})
	b64b := makeUsernameB64("test", 5, [][2]string{{"session", "b"}})
	b64c := makeUsernameB64("test", 5, [][2]string{{"session", "c"}})

	rotator.NextProxy(ctx(), "test", 5, b64a, core.NewAffinityParams())
	rotator.NextProxy(ctx(), "test", 5, b64b, core.NewAffinityParams())
	rotator.NextProxy(ctx(), "test", 5, b64c, core.NewAffinityParams())

	sessions := rotator.ListSessions()
	ids := map[uint64]bool{}
	for _, s := range sessions {
		ids[s.SessionID] = true
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 unique session IDs, got %d", len(ids))
	}
}

func TestForceRotateChangesUpstream(t *testing.T) {
	rotator := NewRotator([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 60, [][2]string{{"session", "rot"}})
	original, _ := rotator.NextProxy(ctx(), "test", 60, b64, core.NewAffinityParams())

	var rotatedHost string
	for i := 0; i < 20; i++ {
		info, _ := rotator.ForceRotate(ctx(), b64)
		if info == nil {
			t.Fatal("expected session info from force rotate")
		}
		parts := splitHost(info.Upstream)
		rotatedHost = parts
		if rotatedHost != original.Host {
			break
		}
	}
	if rotatedHost == original.Host {
		t.Fatal("force_rotate should assign a different upstream")
	}
}

func TestForceRotatePreservesSessionID(t *testing.T) {
	rotator := NewRotator([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 60, [][2]string{{"session", "preserve"}})
	rotator.NextProxy(ctx(), "test", 60, b64, core.NewAffinityParams())
	before := rotator.GetSession(b64)
	rotator.ForceRotate(ctx(), b64)
	after := rotator.GetSession(b64)
	if before.SessionID != after.SessionID {
		t.Fatal("session_id must be preserved on force_rotate")
	}
	if before.ProxySet != after.ProxySet || before.Username != after.Username {
		t.Fatal("proxy_set and username must be preserved")
	}
}

func TestForceRotateUnknownReturnsNil(t *testing.T) {
	rotator := NewRotator([]ProxySet{makeTestSet(4)})
	info, err := rotator.ForceRotate(ctx(), "nosuchkey")
	if err != nil || info != nil {
		t.Fatal("expected nil for unknown key")
	}
}

func TestCredentialsFromProxyEntry(t *testing.T) {
	rotator := NewRotator([]ProxySet{makeTestSet(1)})
	b64 := makeUsernameB64("test", 0, [][2]string{{"k", "v"}})
	p, _ := rotator.NextProxy(ctx(), "test", 0, b64, core.NewAffinityParams())
	if p.Username == nil || *p.Username != "testuser" {
		t.Fatalf("unexpected username: %v", p.Username)
	}
	if p.Password == nil || *p.Password != "testpass" {
		t.Fatalf("unexpected password: %v", p.Password)
	}
}

func splitHost(upstream string) string {
	for i := len(upstream) - 1; i >= 0; i-- {
		if upstream[i] == ':' {
			return upstream[:i]
		}
	}
	return upstream
}
