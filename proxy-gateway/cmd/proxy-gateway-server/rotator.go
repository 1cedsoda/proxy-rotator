package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"proxy-gateway/core"
)

// Rotator manages multiple proxy sets and sticky-session affinity.
type Rotator struct {
	sets          []*rotatorSet
	nextSessionID atomic.Uint64
}

type rotatorSet struct {
	name   string
	source core.ProxySource
	mu     sync.RWMutex
	// keyed by the raw base64 username string
	affinityMap map[string]*affinityEntry
}

type affinityEntry struct {
	sessionID      uint64
	proxy          core.SourceProxy
	startedAt      time.Time
	createdAt      string // ISO 8601 UTC, pre-formatted
	nextRotationAt time.Time
	lastRotationAt time.Time
	duration       time.Duration
	affinityParams core.AffinityParams
}

// ResolvedProxy is the upstream proxy for a single request.
type ResolvedProxy struct {
	Host     string
	Port     uint16
	Username *string
	Password *string
}

// SessionInfo describes an active session, returned by the API.
type SessionInfo struct {
	SessionID      uint64              `json:"session_id"`
	Username       string              `json:"username"`
	ProxySet       string              `json:"proxy_set"`
	Upstream       string              `json:"upstream"`
	CreatedAt      string              `json:"created_at"`
	NextRotationAt string              `json:"next_rotation_at"`
	LastRotationAt string              `json:"last_rotation_at"`
	Metadata       core.AffinityParams `json:"metadata"`
}

// NewRotator builds a Rotator from fully-initialised proxy sets.
func NewRotator(sets []ProxySet) *Rotator {
	rs := make([]*rotatorSet, len(sets))
	for i, ps := range sets {
		rs[i] = &rotatorSet{
			name:        ps.Name,
			source:      ps.Source,
			affinityMap: make(map[string]*affinityEntry),
		}
	}
	return &Rotator{sets: rs}
}

// SetNames returns all proxy set names.
func (r *Rotator) SetNames() []string {
	names := make([]string, len(r.sets))
	for i, s := range r.sets {
		names[i] = s.name
	}
	return names
}

// NextProxy picks the next proxy for a request, honouring sticky affinity.
func (r *Rotator) NextProxy(ctx context.Context, setName string, affinityMinutes uint16, usernameb64 string, params core.AffinityParams) (*ResolvedProxy, error) {
	set := r.findSet(setName)
	if set == nil {
		return nil, nil
	}
	proxy, err := set.pick(ctx, affinityMinutes, usernameb64, params, &r.nextSessionID)
	if err != nil || proxy == nil {
		return nil, err
	}
	return resolvedFrom(proxy), nil
}

// PickAny picks a proxy from a named set without creating an affinity entry.
func (r *Rotator) PickAny(ctx context.Context, setName string) (*ResolvedProxy, error) {
	set := r.findSet(setName)
	if set == nil {
		return nil, nil
	}
	proxy, err := set.source.GetSourceProxy(ctx, core.NewAffinityParams())
	if err != nil || proxy == nil {
		return nil, err
	}
	return resolvedFrom(proxy), nil
}

// ForceRotate force-rotates the upstream proxy for an existing session.
func (r *Rotator) ForceRotate(ctx context.Context, username string) (*SessionInfo, error) {
	for _, set := range r.sets {
		info, err := set.forceRotate(ctx, username)
		if err != nil {
			return nil, err
		}
		if info != nil {
			return info, nil
		}
	}
	return nil, nil
}

// GetSession returns info for an active session by username key.
func (r *Rotator) GetSession(username string) *SessionInfo {
	for _, set := range r.sets {
		if info := set.getSession(username); info != nil {
			return info
		}
	}
	return nil
}

// ListSessions returns all active (non-expired) sessions.
func (r *Rotator) ListSessions() []SessionInfo {
	var all []SessionInfo
	for _, set := range r.sets {
		all = append(all, set.listSessions()...)
	}
	return all
}

// SpawnAffinityCleanup starts a background goroutine that evicts expired sessions every minute.
func SpawnAffinityCleanup(r *Rotator) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			for _, set := range r.sets {
				set.cleanExpired()
			}
		}
	}()
}

// ---------------------------------------------------------------------------
// rotatorSet helpers
// ---------------------------------------------------------------------------

func (s *rotatorSet) pick(ctx context.Context, affinityMinutes uint16, usernameb64 string, params core.AffinityParams, idCounter *atomic.Uint64) (*core.SourceProxy, error) {
	if affinityMinutes == 0 {
		return s.source.GetSourceProxy(ctx, params)
	}

	duration := time.Duration(affinityMinutes) * time.Minute

	// Fast path: valid existing entry.
	s.mu.RLock()
	entry, ok := s.affinityMap[usernameb64]
	if ok && time.Since(entry.startedAt) < entry.duration {
		cp := entry.proxy
		s.mu.RUnlock()
		return &cp, nil
	}
	s.mu.RUnlock()

	// No valid entry — ask source for a new proxy.
	proxy, err := s.source.GetSourceProxy(ctx, params)
	if err != nil || proxy == nil {
		return proxy, err
	}

	sessionID := idCounter.Add(1) - 1
	nowWall := time.Now().UTC()
	newEntry := &affinityEntry{
		sessionID:      sessionID,
		proxy:          *proxy,
		startedAt:      nowWall,
		createdAt:      formatTime(nowWall),
		nextRotationAt: nowWall.Add(duration),
		lastRotationAt: nowWall,
		duration:       duration,
		affinityParams: params,
	}

	s.mu.Lock()
	// Check again under write lock to avoid a race.
	if existing, ok := s.affinityMap[usernameb64]; ok && time.Since(existing.startedAt) < existing.duration {
		cp := existing.proxy
		s.mu.Unlock()
		return &cp, nil
	}
	s.affinityMap[usernameb64] = newEntry
	s.mu.Unlock()

	return proxy, nil
}

func (s *rotatorSet) forceRotate(ctx context.Context, username string) (*SessionInfo, error) {
	s.mu.RLock()
	entry, ok := s.affinityMap[username]
	if !ok || time.Since(entry.startedAt) >= entry.duration {
		s.mu.RUnlock()
		return nil, nil
	}
	params := entry.affinityParams
	currentProxy := entry.proxy
	duration := entry.duration
	sessionID := entry.sessionID
	createdAt := entry.createdAt
	s.mu.RUnlock()

	newProxy, err := s.source.GetSourceProxyForceRotate(ctx, params, &currentProxy)
	if err != nil {
		return nil, err
	}
	if newProxy == nil {
		return nil, nil
	}

	now := time.Now().UTC()
	s.mu.Lock()
	entry, ok = s.affinityMap[username]
	if !ok {
		s.mu.Unlock()
		return nil, nil
	}
	entry.proxy = *newProxy
	entry.lastRotationAt = now
	entry.nextRotationAt = now.Add(duration)
	info := sessionInfoFrom(sessionID, username, s.name, entry)
	s.mu.Unlock()

	_ = createdAt // already in entry.createdAt
	return info, nil
}

func (s *rotatorSet) getSession(username string) *SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.affinityMap[username]
	if !ok || time.Since(entry.startedAt) >= entry.duration {
		return nil
	}
	return sessionInfoFrom(entry.sessionID, username, s.name, entry)
}

func (s *rotatorSet) listSessions() []SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []SessionInfo
	for key, entry := range s.affinityMap {
		if time.Since(entry.startedAt) >= entry.duration {
			continue
		}
		out = append(out, *sessionInfoFrom(entry.sessionID, key, s.name, entry))
	}
	return out
}

func (s *rotatorSet) cleanExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := len(s.affinityMap)
	for key, entry := range s.affinityMap {
		if time.Since(entry.startedAt) >= entry.duration {
			delete(s.affinityMap, key)
		}
	}
	removed := before - len(s.affinityMap)
	if removed > 0 {
		// We don't have a logger here; callers can log if needed.
		_ = removed
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (r *Rotator) findSet(name string) *rotatorSet {
	for _, s := range r.sets {
		if s.name == name {
			return s
		}
	}
	return nil
}

func resolvedFrom(p *core.SourceProxy) *ResolvedProxy {
	return &ResolvedProxy{
		Host:     p.Host,
		Port:     p.Port,
		Username: p.Username,
		Password: p.Password,
	}
}

func sessionInfoFrom(sessionID uint64, username, setName string, e *affinityEntry) *SessionInfo {
	return &SessionInfo{
		SessionID:      sessionID,
		Username:       username,
		ProxySet:       setName,
		Upstream:       fmt.Sprintf("%s:%d", e.proxy.Host, e.proxy.Port),
		CreatedAt:      e.createdAt,
		NextRotationAt: formatTime(e.nextRotationAt),
		LastRotationAt: formatTime(e.lastRotationAt),
		Metadata:       e.affinityParams,
	}
}

// formatTime formats a time.Time as ISO 8601 UTC without external deps.
func formatTime(t time.Time) string {
	t = t.UTC()
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02dZ",
		t.Year(), int(t.Month()), t.Day(),
		t.Hour(), t.Minute(), t.Second())
}
