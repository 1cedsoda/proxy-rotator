package complex

import (
	"testing"

	"proxy-gateway/core"
)

func mustOpen(t *testing.T, p *Provider, sub string) core.ConnHandle {
	t.Helper()
	h, err := p.OpenConnection(sub)
	if err != nil {
		t.Fatalf("OpenConnection(%q): unexpected error: %v", sub, err)
	}
	return h
}

func mustReject(t *testing.T, p *Provider, sub string) {
	t.Helper()
	if _, err := p.OpenConnection(sub); err == nil {
		t.Fatalf("OpenConnection(%q): expected error, got nil", sub)
	}
}

func TestAuthCorrectPassword(t *testing.T) {
	p := New(Users{"alice": {Password: "s3cret"}})
	if err := p.Authenticate("alice", "s3cret"); err != nil {
		t.Fatal(err)
	}
}

func TestAuthWrongPassword(t *testing.T) {
	p := New(Users{"alice": {Password: "s3cret"}})
	if err := p.Authenticate("alice", "wrong"); err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestAuthUnknownUser(t *testing.T) {
	p := New(Users{"alice": {Password: "s3cret"}})
	if err := p.Authenticate("bob", "pw"); err == nil {
		t.Fatal("expected error for unknown user")
	}
}

func TestConcurrentConnectionLimit(t *testing.T) {
	p := New(Users{"alice": {Password: "pw", Limits: []RateLimit{
		{Type: LimitConcurrentConnections, Timeframe: Realtime, Max: 2},
	}}})
	h1 := mustOpen(t, p, "alice")
	h2 := mustOpen(t, p, "alice")
	mustReject(t, p, "alice")
	h1.Close(0, 0)
	h3 := mustOpen(t, p, "alice")
	h2.Close(0, 0)
	h3.Close(0, 0)
}

func TestConcurrentConnectionCounterReturnsToZero(t *testing.T) {
	p := New(Users{"alice": {Password: "pw", Limits: []RateLimit{
		{Type: LimitConcurrentConnections, Timeframe: Realtime, Max: 1},
	}}})
	h := mustOpen(t, p, "alice")
	h.Close(0, 0)
	mustOpen(t, p, "alice")
}

func TestTotalConnectionWindowedLimit(t *testing.T) {
	p := New(Users{"alice": {Password: "pw", Limits: []RateLimit{
		{Type: LimitTotalConnections, Timeframe: Minutely, Window: 1, Max: 3},
	}}})
	for i := 0; i < 3; i++ {
		h := mustOpen(t, p, "alice")
		h.Close(0, 0)
	}
	mustReject(t, p, "alice")
}

func TestUploadBandwidthLimit(t *testing.T) {
	p := New(Users{"alice": {Password: "pw", Limits: []RateLimit{
		{Type: LimitUploadBytes, Timeframe: Hourly, Window: 1, Max: 100},
	}}})
	h := mustOpen(t, p, "alice")
	cancelled := false
	h.RecordTraffic(true, 80, func() { cancelled = true })
	if cancelled {
		t.Fatal("should not cancel yet")
	}
	h.RecordTraffic(true, 30, func() { cancelled = true })
	if !cancelled {
		t.Fatal("expected cancel when upload limit exceeded")
	}
	h.Close(110, 0)
}

func TestUploadLimitDoesNotAffectDownload(t *testing.T) {
	p := New(Users{"alice": {Password: "pw", Limits: []RateLimit{
		{Type: LimitUploadBytes, Timeframe: Hourly, Window: 1, Max: 50},
	}}})
	h := mustOpen(t, p, "alice")
	cancelled := false
	h.RecordTraffic(false, 1000, func() { cancelled = true })
	if cancelled {
		t.Fatal("upload limit must not affect download traffic")
	}
	h.Close(0, 1000)
}

func TestDownloadBandwidthLimit(t *testing.T) {
	p := New(Users{"alice": {Password: "pw", Limits: []RateLimit{
		{Type: LimitDownloadBytes, Timeframe: Hourly, Window: 1, Max: 100},
	}}})
	h := mustOpen(t, p, "alice")
	cancelled := false
	h.RecordTraffic(false, 50, func() { cancelled = true })
	h.RecordTraffic(false, 60, func() { cancelled = true })
	if !cancelled {
		t.Fatal("expected cancel when download limit exceeded")
	}
	h.Close(0, 110)
}

func TestTotalBandwidthLimit(t *testing.T) {
	p := New(Users{"alice": {Password: "pw", Limits: []RateLimit{
		{Type: LimitTotalBytes, Timeframe: Daily, Window: 1, Max: 200},
	}}})
	h := mustOpen(t, p, "alice")
	cancelled := false
	cancel := func() { cancelled = true }
	h.RecordTraffic(true, 100, cancel)
	h.RecordTraffic(false, 100, cancel)
	if !cancelled {
		t.Fatal("expected cancel at combined 200 bytes")
	}
	h.Close(100, 100)
}

func TestMultipleLimitsAllEnforced(t *testing.T) {
	p := New(Users{"alice": {Password: "pw", Limits: []RateLimit{
		{Type: LimitConcurrentConnections, Timeframe: Realtime, Max: 2},
		{Type: LimitUploadBytes, Timeframe: Hourly, Window: 1, Max: 500},
		{Type: LimitDownloadBytes, Timeframe: Hourly, Window: 1, Max: 1000},
	}}})
	h1 := mustOpen(t, p, "alice")
	h2 := mustOpen(t, p, "alice")
	mustReject(t, p, "alice")
	h1.Close(0, 0)
	h2.Close(0, 0)

	h := mustOpen(t, p, "alice")
	uploadCancelled := false
	h.RecordTraffic(true, 600, func() { uploadCancelled = true })
	if !uploadCancelled {
		t.Fatal("expected upload limit to trigger")
	}
	h.Close(600, 0)
}

func TestPerSubIsolation(t *testing.T) {
	p := New(Users{
		"alice": {Password: "pw", Limits: []RateLimit{{Type: LimitConcurrentConnections, Timeframe: Realtime, Max: 1}}},
		"bob":   {Password: "pw", Limits: []RateLimit{{Type: LimitConcurrentConnections, Timeframe: Realtime, Max: 1}}},
	})
	mustOpen(t, p, "alice")
	mustOpen(t, p, "bob")
}

func TestResetUser(t *testing.T) {
	p := New(Users{"alice": {Password: "pw", Limits: []RateLimit{
		{Type: LimitTotalConnections, Timeframe: Minutely, Window: 1, Max: 2},
	}}})
	h1 := mustOpen(t, p, "alice")
	h1.Close(0, 0)
	h2 := mustOpen(t, p, "alice")
	h2.Close(0, 0)
	mustReject(t, p, "alice")
	p.ResetUser("alice")
	mustOpen(t, p, "alice")
}

func TestWindowMultiplierLabel(t *testing.T) {
	if got := windowLabel(RateLimit{Timeframe: Hourly, Window: 6}); got != "6 hours" {
		t.Fatalf("expected '6 hours', got %q", got)
	}
	if got := windowLabel(RateLimit{Timeframe: Daily, Window: 1}); got != "day" {
		t.Fatalf("expected 'day', got %q", got)
	}
}

func TestImplementsAuthProvider(t *testing.T) {
	var _ core.AuthProvider = New(Users{})
}

func TestImplementsConnectionTracker(t *testing.T) {
	var _ core.ConnectionTracker = New(Users{})
}
