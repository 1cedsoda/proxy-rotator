package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"proxy-gateway/core"
	"proxy-gateway/utils"
)

// Username is the parsed proxy-gateway username JSON.
//
//	{"set":"residential", "minutes":5, "meta":{"platform":"myapp","user":"alice"}}
type Username struct {
	Affinity AffinityParams
	Minutes  int
	Raw      string // original JSON string, stored as session label
}

// ParseUsername parses a raw JSON username string.
func ParseUsername(raw string) (*Username, error) {
	var j struct {
		Set     string                 `json:"set"`
		Minutes int                    `json:"minutes"`
		Meta    map[string]interface{} `json:"meta"`
	}
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		return nil, fmt.Errorf("username is not valid JSON: %w", err)
	}
	if j.Set == "" {
		return nil, fmt.Errorf("'set' must not be empty")
	}
	return &Username{
		Affinity: AffinityParams{Set: j.Set, Meta: j.Meta},
		Minutes:  j.Minutes,
		Raw:      raw,
	}, nil
}

// ---------------------------------------------------------------------------
// Context keys
// ---------------------------------------------------------------------------

type ctxKey int

const (
	ctxSet ctxKey = iota
)

func withSet(ctx context.Context, set string) context.Context {
	return context.WithValue(ctx, ctxSet, set)
}

func getSet(ctx context.Context) string {
	v, _ := ctx.Value(ctxSet).(string)
	return v
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// ParseJSONCreds is middleware that parses RawUsername as a JSON object and
// populates context with set, seed TTL, top-level seed, and session label.
func ParseJSONCreds(next core.Handler) core.Handler {
	return core.HandlerFunc(func(ctx context.Context, req *core.Request) (*core.Result, error) {
		if req.RawUsername == "" {
			return nil, fmt.Errorf("empty username")
		}
		u, err := ParseUsername(req.RawUsername)
		if err != nil {
			return nil, err
		}

		ctx = withSet(ctx, u.Affinity.Set)
		ctx = utils.WithSeedTTL(ctx, time.Duration(u.Minutes)*time.Minute)
		ctx = utils.WithTopLevelSeed(ctx, u.Affinity.Seed())
		ctx = utils.WithSessionLabel(ctx, u.Raw)

		return next.Resolve(ctx, req)
	})
}

// PasswordAuth is middleware that checks req.RawPassword against a fixed
// password. If password is empty, all requests pass through.
func PasswordAuth(password string, next core.Handler) core.Handler {
	if password == "" {
		return next
	}
	return core.HandlerFunc(func(ctx context.Context, req *core.Request) (*core.Result, error) {
		if req.RawPassword != password {
			return nil, fmt.Errorf("invalid credentials")
		}
		return next.Resolve(ctx, req)
	})
}
