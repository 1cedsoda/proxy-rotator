package main

import (
	"context"
	"testing"
	"time"

	"proxy-gateway/core"
	"proxy-gateway/utils"
)

var testProxy = &core.Proxy{Host: "upstream", Port: 8080}
var testSource = core.HandlerFunc(func(_ context.Context, _ *core.Request) (*core.Result, error) {
	return core.Resolved(testProxy), nil
})

// ---------------------------------------------------------------------------
// ParseJSONCreds
// ---------------------------------------------------------------------------

func TestParseJSONCredsPopulatesContext(t *testing.T) {
	h := ParseJSONCreds(core.HandlerFunc(func(ctx context.Context, _ *core.Request) (*core.Result, error) {
		if getSet(ctx) != "res" {
			t.Fatalf("expected set=res, got %q", getSet(ctx))
		}
		if utils.GetSeedTTL(ctx) != 5*time.Minute {
			t.Fatalf("expected ttl=5m, got %v", utils.GetSeedTTL(ctx))
		}
		if utils.GetTopLevelSeed(ctx) == 0 {
			t.Fatal("expected non-zero top-level seed")
		}
		return core.Resolved(testProxy), nil
	}))
	req := &core.Request{
		RawUsername: `{"set":"res","minutes":5}`,
	}
	r, err := h.Resolve(context.Background(), req)
	if err != nil || r.Proxy.Host != "upstream" {
		t.Fatalf("unexpected: err=%v result=%+v", err, r)
	}
}

func TestParseJSONCredsTopLevelSeedStableForSameUsername(t *testing.T) {
	var gotSeed1, gotSeed2 uint64
	h := ParseJSONCreds(core.HandlerFunc(func(ctx context.Context, _ *core.Request) (*core.Result, error) {
		gotSeed1 = utils.GetTopLevelSeed(ctx)
		return core.Resolved(testProxy), nil
	}))
	h.Resolve(context.Background(), &core.Request{
		RawUsername: `{"set":"res","minutes":5,"meta":{"app":"x"}}`,
	})

	h2 := ParseJSONCreds(core.HandlerFunc(func(ctx context.Context, _ *core.Request) (*core.Result, error) {
		gotSeed2 = utils.GetTopLevelSeed(ctx)
		return core.Resolved(testProxy), nil
	}))
	h2.Resolve(context.Background(), &core.Request{
		RawUsername: `{"set":"res","minutes":5,"meta":{"app":"x"}}`,
	})
	if gotSeed1 != gotSeed2 {
		t.Fatalf("same username should produce same seed: %d vs %d", gotSeed1, gotSeed2)
	}
}

func TestParseJSONCredsDifferentMetaDifferentSeed(t *testing.T) {
	var gotSeed1, gotSeed2 uint64
	h := ParseJSONCreds(core.HandlerFunc(func(ctx context.Context, _ *core.Request) (*core.Result, error) {
		gotSeed1 = utils.GetTopLevelSeed(ctx)
		return core.Resolved(testProxy), nil
	}))
	h.Resolve(context.Background(), &core.Request{
		RawUsername: `{"set":"res","minutes":5,"meta":{"user":"alice"}}`,
	})

	h2 := ParseJSONCreds(core.HandlerFunc(func(ctx context.Context, _ *core.Request) (*core.Result, error) {
		gotSeed2 = utils.GetTopLevelSeed(ctx)
		return core.Resolved(testProxy), nil
	}))
	h2.Resolve(context.Background(), &core.Request{
		RawUsername: `{"set":"res","minutes":5,"meta":{"user":"bob"}}`,
	})
	if gotSeed1 == gotSeed2 {
		t.Fatal("different meta should produce different seeds")
	}
}

func TestParseJSONCredsRejectsEmptyUsername(t *testing.T) {
	h := ParseJSONCreds(testSource)
	if _, err := h.Resolve(context.Background(), &core.Request{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseJSONCredsRejectsInvalidJSON(t *testing.T) {
	h := ParseJSONCreds(testSource)
	if _, err := h.Resolve(context.Background(), &core.Request{RawUsername: "notjson"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseJSONCredsRejectsMissingSet(t *testing.T) {
	h := ParseJSONCreds(testSource)
	if _, err := h.Resolve(context.Background(), &core.Request{
		RawUsername: `{"minutes":0,"meta":{}}`,
	}); err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// PasswordAuth
// ---------------------------------------------------------------------------

func TestPasswordAuthRejectsWrong(t *testing.T) {
	h := PasswordAuth("s3cret", testSource)
	_, err := h.Resolve(context.Background(), &core.Request{
		RawUsername: `{"set":"res","minutes":0,"meta":{}}`,
		RawPassword: "wrong",
	})
	if err == nil {
		t.Fatal("expected auth error")
	}
}

func TestPasswordAuthAcceptsCorrect(t *testing.T) {
	h := PasswordAuth("s3cret", testSource)
	_, err := h.Resolve(context.Background(), &core.Request{
		RawUsername: `{"set":"res","minutes":0,"meta":{}}`,
		RawPassword: "s3cret",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
}

func TestPasswordAuthDisabledWhenEmpty(t *testing.T) {
	h := PasswordAuth("", testSource)
	_, err := h.Resolve(context.Background(), &core.Request{
		RawUsername: `{"set":"res","minutes":0,"meta":{}}`,
		RawPassword: "anything",
	})
	if err != nil {
		t.Fatalf("expected pass-through when no password, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Full pipeline with SessionManager
// ---------------------------------------------------------------------------

func TestFullPipeline(t *testing.T) {
	sm := utils.NewSessionManager(testSource)
	pipeline := PasswordAuth("pw", ParseJSONCreds(sm))

	req := &core.Request{
		RawUsername: `{"set":"test","minutes":5,"meta":{}}`,
		RawPassword: "pw",
	}
	r, err := pipeline.Resolve(context.Background(), req)
	if err != nil || r == nil || r.Proxy == nil {
		t.Fatalf("expected proxy, got err=%v", err)
	}

	// Same username → same sticky session
	r2, _ := pipeline.Resolve(context.Background(), &core.Request{
		RawUsername: `{"set":"test","minutes":5,"meta":{}}`,
		RawPassword: "pw",
	})
	if r2.Proxy.Port != r.Proxy.Port {
		t.Fatal("sticky should return same proxy for same username")
	}

	// Wrong password
	if _, err := pipeline.Resolve(context.Background(), &core.Request{
		RawUsername: `{"set":"test","minutes":5,"meta":{}}`,
		RawPassword: "wrong",
	}); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestFullPipelineZeroTTLNoAffinity(t *testing.T) {
	counter := 0
	source := core.HandlerFunc(func(_ context.Context, _ *core.Request) (*core.Result, error) {
		counter++
		return core.Resolved(&core.Proxy{Host: "host", Port: uint16(counter)}), nil
	})

	pipeline := ParseJSONCreds(utils.NewSessionManager(source))

	r1, _ := pipeline.Resolve(context.Background(), &core.Request{
		RawUsername: `{"set":"test","minutes":0,"meta":{}}`,
	})
	r2, _ := pipeline.Resolve(context.Background(), &core.Request{
		RawUsername: `{"set":"test","minutes":0,"meta":{}}`,
	})
	if r1.Proxy.Port == r2.Proxy.Port {
		t.Fatal("0 minutes should not pin")
	}
}

func TestFullPipelineSeedFlowsToSource(t *testing.T) {
	var gotSeed *core.SessionSeed
	source := core.HandlerFunc(func(ctx context.Context, _ *core.Request) (*core.Result, error) {
		gotSeed = core.GetSessionSeed(ctx)
		return core.Resolved(testProxy), nil
	})

	pipeline := ParseJSONCreds(utils.NewSessionManager(source))
	pipeline.Resolve(context.Background(), &core.Request{
		RawUsername: `{"set":"test","minutes":5,"meta":{}}`,
	})

	if gotSeed == nil {
		t.Fatal("source should receive a non-nil SessionSeed when minutes > 0")
	}
}

func TestFullPipelineNilSeedWithoutAffinity(t *testing.T) {
	var gotSeed *core.SessionSeed
	called := false
	source := core.HandlerFunc(func(ctx context.Context, _ *core.Request) (*core.Result, error) {
		gotSeed = core.GetSessionSeed(ctx)
		called = true
		return core.Resolved(testProxy), nil
	})

	pipeline := ParseJSONCreds(utils.NewSessionManager(source))
	pipeline.Resolve(context.Background(), &core.Request{
		RawUsername: `{"set":"test","minutes":0,"meta":{}}`,
	})

	if !called {
		t.Fatal("source should have been called")
	}
	if gotSeed != nil {
		t.Fatal("minutes=0 should result in nil seed")
	}
}

func TestForceRotateChangesSeed(t *testing.T) {
	var seeds []*core.SessionSeed
	source := core.HandlerFunc(func(ctx context.Context, _ *core.Request) (*core.Result, error) {
		seeds = append(seeds, core.GetSessionSeed(ctx))
		return core.Resolved(&core.Proxy{Host: "host", Port: uint16(len(seeds))}), nil
	})

	sm := utils.NewSessionManager(source)
	pipeline := ParseJSONCreds(sm)

	username := `{"set":"test","minutes":60,"meta":{}}`
	pipeline.Resolve(context.Background(), &core.Request{RawUsername: username})

	u, _ := ParseUsername(username)
	seed := u.Affinity.Seed()
	info := sm.GetSession(seed)
	if info == nil {
		t.Fatal("expected session")
	}
	seedBefore := info.Seed

	info2, err := sm.ForceRotate(seed)
	if err != nil {
		t.Fatal(err)
	}
	if info2 == nil {
		t.Fatal("expected rotated session")
	}
	if info2.Seed == seedBefore {
		t.Fatal("force rotate should produce a different seed")
	}
	if info2.Rotation != 1 {
		t.Fatalf("expected rotation=1, got %d", info2.Rotation)
	}
}
