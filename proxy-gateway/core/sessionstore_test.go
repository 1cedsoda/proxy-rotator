package core

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
)

type vecSource struct {
	pool *CountingPool[SourceProxy]
}

func (v *vecSource) GetSourceProxy(_ context.Context, _ AffinityParams) (*SourceProxy, error) {
	p := v.pool.Next()
	if p == nil {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}

func (v *vecSource) GetSourceProxyForceRotate(_ context.Context, _ AffinityParams, current *SourceProxy) (*SourceProxy, error) {
	p := v.pool.NextExcluding(func(sp SourceProxy) bool {
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
	proxies := make([]SourceProxy, n)
	for i := range proxies {
		user := "testuser"
		pass := "testpass"
		proxies[i] = SourceProxy{
			Host:     fmt.Sprintf("proxy%d.example.com", i),
			Port:     8080,
			Username: &user,
			Password: &pass,
		}
	}
	return ProxySet{
		Name:   "test",
		Source: &vecSource{pool: NewCountingPool(proxies)},
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
	j := fmt.Sprintf(`{"sub":"testuser","meta":%s,"minutes":%d,"set":"%s"}`, meta, minutes, set)
	return base64.StdEncoding.EncodeToString([]byte(j))
}

func ctx() context.Context { return context.Background() }

func TestLeastUsedDistributesEvenly(t *testing.T) {
	store := NewSessionStore([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 0, [][2]string{{"k", "v"}})
	counts := map[string]int{}
	for i := 0; i < 400; i++ {
		p, err := store.NextProxy(ctx(), "test", 0, b64, NewAffinityParams())
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
	store := NewSessionStore([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 5, [][2]string{{"session", "mysession"}})
	first, _ := store.NextProxy(ctx(), "test", 5, b64, NewAffinityParams())
	for i := 0; i < 10; i++ {
		subsequent, _ := store.NextProxy(ctx(), "test", 5, b64, NewAffinityParams())
		if subsequent.Host != first.Host {
			t.Fatalf("affinity should pin to same proxy: got %s, want %s", subsequent.Host, first.Host)
		}
	}
}

func TestZeroMinutesNoAffinity(t *testing.T) {
	store := NewSessionStore([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 0, [][2]string{{"k", "v"}})
	hosts := map[string]bool{}
	for i := 0; i < 100; i++ {
		p, _ := store.NextProxy(ctx(), "test", 0, b64, NewAffinityParams())
		hosts[p.Host] = true
	}
	if len(hosts) <= 1 {
		t.Fatal("should distribute without affinity")
	}
}

func TestDifferentSessionKeysIndependent(t *testing.T) {
	store := NewSessionStore([]ProxySet{makeTestSet(4)})
	b64a := makeUsernameB64("test", 60, [][2]string{{"session", "sessA"}})
	b64b := makeUsernameB64("test", 60, [][2]string{{"session", "sessB"}})
	pa, _ := store.NextProxy(ctx(), "test", 60, b64a, NewAffinityParams())
	pb, _ := store.NextProxy(ctx(), "test", 60, b64b, NewAffinityParams())
	for i := 0; i < 5; i++ {
		pa2, _ := store.NextProxy(ctx(), "test", 60, b64a, NewAffinityParams())
		pb2, _ := store.NextProxy(ctx(), "test", 60, b64b, NewAffinityParams())
		if pa2.Host != pa.Host {
			t.Fatalf("session A should be pinned")
		}
		if pb2.Host != pb.Host {
			t.Fatalf("session B should be pinned")
		}
	}
}

func TestUnknownSetReturnsNil(t *testing.T) {
	store := NewSessionStore([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("unknown", 0, nil)
	p, err := store.NextProxy(ctx(), "unknown", 0, b64, NewAffinityParams())
	if err != nil || p != nil {
		t.Fatal("expected nil for unknown set")
	}
}

func TestGetSessionActive(t *testing.T) {
	store := NewSessionStore([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 5, [][2]string{{"session", "mysess"}})
	p, _ := store.NextProxy(ctx(), "test", 5, b64, NewAffinityParams())
	info := store.GetSession(b64)
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
	store := NewSessionStore([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 0, [][2]string{{"session", "nosess"}})
	store.NextProxy(ctx(), "test", 0, b64, NewAffinityParams())
	if store.GetSession(b64) != nil {
		t.Fatal("0-minute sessions should not be tracked")
	}
}

func TestListSessions(t *testing.T) {
	store := NewSessionStore([]ProxySet{makeTestSet(4)})
	b64a := makeUsernameB64("test", 5, [][2]string{{"session", "sessA"}})
	b64b := makeUsernameB64("test", 10, [][2]string{{"session", "sessB"}})
	b64z := makeUsernameB64("test", 0, [][2]string{{"session", "noaff"}})
	store.NextProxy(ctx(), "test", 5, b64a, NewAffinityParams())
	store.NextProxy(ctx(), "test", 10, b64b, NewAffinityParams())
	store.NextProxy(ctx(), "test", 0, b64z, NewAffinityParams())
	sessions := store.ListSessions()
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestSessionIDsAreUniqueAndIncrement(t *testing.T) {
	store := NewSessionStore([]ProxySet{makeTestSet(4)})
	b64a := makeUsernameB64("test", 5, [][2]string{{"session", "a"}})
	b64b := makeUsernameB64("test", 5, [][2]string{{"session", "b"}})
	b64c := makeUsernameB64("test", 5, [][2]string{{"session", "c"}})
	store.NextProxy(ctx(), "test", 5, b64a, NewAffinityParams())
	store.NextProxy(ctx(), "test", 5, b64b, NewAffinityParams())
	store.NextProxy(ctx(), "test", 5, b64c, NewAffinityParams())
	sessions := store.ListSessions()
	ids := map[uint64]bool{}
	for _, s := range sessions {
		ids[s.SessionID] = true
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 unique session IDs, got %d", len(ids))
	}
}

func TestForceRotateChangesUpstream(t *testing.T) {
	store := NewSessionStore([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 60, [][2]string{{"session", "rot"}})
	original, _ := store.NextProxy(ctx(), "test", 60, b64, NewAffinityParams())
	var rotatedHost string
	for i := 0; i < 20; i++ {
		info, _ := store.ForceRotate(ctx(), b64)
		if info == nil {
			t.Fatal("expected session info from force rotate")
		}
		for j := len(info.Upstream) - 1; j >= 0; j-- {
			if info.Upstream[j] == ':' {
				rotatedHost = info.Upstream[:j]
				break
			}
		}
		if rotatedHost != original.Host {
			break
		}
	}
	if rotatedHost == original.Host {
		t.Fatal("force_rotate should assign a different upstream")
	}
}

func TestForceRotatePreservesSessionID(t *testing.T) {
	store := NewSessionStore([]ProxySet{makeTestSet(4)})
	b64 := makeUsernameB64("test", 60, [][2]string{{"session", "preserve"}})
	store.NextProxy(ctx(), "test", 60, b64, NewAffinityParams())
	before := store.GetSession(b64)
	store.ForceRotate(ctx(), b64)
	after := store.GetSession(b64)
	if before.SessionID != after.SessionID {
		t.Fatal("session_id must be preserved on force_rotate")
	}
}

func TestForceRotateUnknownReturnsNil(t *testing.T) {
	store := NewSessionStore([]ProxySet{makeTestSet(4)})
	info, err := store.ForceRotate(ctx(), "nosuchkey")
	if err != nil || info != nil {
		t.Fatal("expected nil for unknown key")
	}
}

func TestCredentialsFromProxyEntry(t *testing.T) {
	store := NewSessionStore([]ProxySet{makeTestSet(1)})
	b64 := makeUsernameB64("test", 0, [][2]string{{"k", "v"}})
	p, _ := store.NextProxy(ctx(), "test", 0, b64, NewAffinityParams())
	if p.Username == nil || *p.Username != "testuser" {
		t.Fatalf("unexpected username: %v", p.Username)
	}
	if p.Password == nil || *p.Password != "testpass" {
		t.Fatalf("unexpected password: %v", p.Password)
	}
}
