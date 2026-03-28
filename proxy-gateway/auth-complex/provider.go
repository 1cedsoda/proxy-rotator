// Package complex provides an AuthProvider that supports multiple users,
// each with their own password and a set of rate limits.
//
// Rate limits cover concurrent connections, total connections, upload bytes,
// download bytes, and combined bytes — all with configurable rolling windows.
//
// Example:
//
//	auth := complex.New(complex.Users{
//	    "alice": {
//	        Password: "s3cret",
//	        Limits: []complex.RateLimit{
//	            {Type: complex.LimitConcurrentConnections, Timeframe: complex.Realtime,  Max: 10},
//	            {Type: complex.LimitTotalConnections,      Timeframe: complex.Hourly,    Window: 1,  Max: 500},
//	            {Type: complex.LimitTotalBytes,            Timeframe: complex.Daily,     Window: 1,  Max: 10 << 30},
//	            {Type: complex.LimitUploadBytes,           Timeframe: complex.Minutely,  Window: 5,  Max: 50 << 20},
//	        },
//	    },
//	    "bob": {
//	        Password: "hunter2",
//	        Limits: []complex.RateLimit{
//	            {Type: complex.LimitConcurrentConnections, Timeframe: complex.Realtime, Max: 2},
//	        },
//	    },
//	})
package complex

import (
	"fmt"
	"sync"
	"sync/atomic"

	"proxy-gateway/core"
)

// UserConfig holds the credentials and rate limits for one user.
type UserConfig struct {
	Password string
	Limits   []RateLimit
}

// Users maps sub → UserConfig.
type Users map[string]UserConfig

// Provider implements core.AuthProvider and core.ConnectionTracker.
type Provider struct {
	users Users
	mu    sync.RWMutex
	state map[string]*userState
}

// New creates a Provider from a Users map.
func New(users Users) *Provider {
	return &Provider{
		users: users,
		state: make(map[string]*userState),
	}
}

// Authenticate checks the password and that no windowed limit is already exceeded.
func (p *Provider) Authenticate(sub, password string) error {
	cfg, ok := p.users[sub]
	if !ok {
		return fmt.Errorf("unknown user %q", sub)
	}
	if password != cfg.Password {
		return fmt.Errorf("invalid credentials")
	}
	return p.checkWindowedLimits(sub, cfg.Limits)
}

// OpenConnection implements core.ConnectionTracker.
func (p *Provider) OpenConnection(sub string) (core.ConnHandle, error) {
	cfg, ok := p.users[sub]
	if !ok {
		return nil, fmt.Errorf("unknown user %q", sub)
	}

	st := p.getState(sub, cfg.Limits)

	if err := p.checkWindowedLimits(sub, cfg.Limits); err != nil {
		return nil, err
	}

	// Enforce concurrent-connection limits.
	for _, rl := range cfg.Limits {
		if rl.Type != LimitConcurrentConnections {
			continue
		}
		current := st.concurrent.Add(1)
		if current > rl.Max {
			st.concurrent.Add(-1)
			return nil, fmt.Errorf("concurrent connection limit (%d) exceeded for %q", rl.Max, sub)
		}
	}

	// Count this connection for total-connection windowed limits.
	for i, rl := range cfg.Limits {
		if rl.Type != LimitTotalConnections {
			continue
		}
		total := st.counters[i].Add(1)
		if total > rl.Max {
			st.concurrent.Add(-p.concurrentLimitCount(cfg.Limits))
			return nil, fmt.Errorf("total connection limit (%d) per %s exceeded for %q", rl.Max, windowLabel(rl), sub)
		}
	}

	return &connHandle{provider: p, sub: sub, cfg: cfg, state: st}, nil
}

// ResetUser clears all counters for sub.
func (p *Provider) ResetUser(sub string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.state, sub)
}

// ---------------------------------------------------------------------------
// internals
// ---------------------------------------------------------------------------

func (p *Provider) checkWindowedLimits(sub string, limits []RateLimit) error {
	st := p.getState(sub, limits)
	for i, rl := range limits {
		switch rl.Type {
		case LimitConcurrentConnections, LimitTotalConnections:
			continue
		}
		if st.counters[i] != nil && st.counters[i].Total() >= rl.Max {
			return fmt.Errorf("%s limit (%d) per %s exceeded for %q",
				limitTypeLabel(rl.Type), rl.Max, windowLabel(rl), sub)
		}
	}
	return nil
}

func (p *Provider) getState(sub string, limits []RateLimit) *userState {
	p.mu.RLock()
	st, ok := p.state[sub]
	p.mu.RUnlock()
	if ok {
		return st
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if st, ok = p.state[sub]; ok {
		return st
	}
	st = newUserState(limits)
	p.state[sub] = st
	return st
}

func (p *Provider) concurrentLimitCount(limits []RateLimit) int64 {
	var n int64
	for _, rl := range limits {
		if rl.Type == LimitConcurrentConnections {
			n++
		}
	}
	return n
}

func limitTypeLabel(t LimitType) string {
	switch t {
	case LimitUploadBytes:
		return "upload"
	case LimitDownloadBytes:
		return "download"
	case LimitTotalBytes:
		return "total bandwidth"
	case LimitTotalConnections:
		return "total connections"
	case LimitConcurrentConnections:
		return "concurrent connections"
	}
	return "unknown"
}

func windowLabel(rl RateLimit) string {
	if rl.Timeframe == Realtime {
		return "realtime"
	}
	w := rl.Window
	if w < 1 {
		w = 1
	}
	unit := ""
	switch rl.Timeframe {
	case Secondly:
		unit = "second"
	case Minutely:
		unit = "minute"
	case Hourly:
		unit = "hour"
	case Daily:
		unit = "day"
	case Weekly:
		unit = "week"
	case Monthly:
		unit = "month"
	}
	if w == 1 {
		return unit
	}
	return fmt.Sprintf("%d %ss", w, unit)
}

// ---------------------------------------------------------------------------
// userState
// ---------------------------------------------------------------------------

type userState struct {
	concurrent atomic.Int64
	counters   []*counter
}

func newUserState(limits []RateLimit) *userState {
	st := &userState{
		counters: make([]*counter, len(limits)),
	}
	for i, rl := range limits {
		if rl.Timeframe == Realtime || rl.Type == LimitConcurrentConnections {
			continue
		}
		st.counters[i] = newCounter(rl.windowDuration())
	}
	return st
}

// ---------------------------------------------------------------------------
// connHandle
// ---------------------------------------------------------------------------

type connHandle struct {
	provider *Provider
	sub      string
	cfg      UserConfig
	state    *userState
}

func (h *connHandle) RecordTraffic(upstream bool, delta int64, cancel func()) {
	for i, rl := range h.cfg.Limits {
		var applies bool
		switch rl.Type {
		case LimitUploadBytes:
			applies = upstream
		case LimitDownloadBytes:
			applies = !upstream
		case LimitTotalBytes:
			applies = true
		default:
			continue
		}
		if !applies || h.state.counters[i] == nil {
			continue
		}
		if h.state.counters[i].Add(delta) >= rl.Max {
			cancel()
			return
		}
	}
}

func (h *connHandle) Close(_, _ int64) {
	if h.state.concurrent.Load() > 0 {
		h.state.concurrent.Add(-1)
	}
}
