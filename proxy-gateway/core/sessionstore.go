package core

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ResolvedProxy is the upstream proxy selected for a single request.
type ResolvedProxy struct {
	Host     string
	Port     uint16
	Username *string
	Password *string
}

// SessionInfo describes an active sticky session.
type SessionInfo struct {
	SessionID      uint64         `json:"session_id"`
	Username       string         `json:"username"`
	ProxySet       string         `json:"proxy_set"`
	Upstream       string         `json:"upstream"`
	CreatedAt      string         `json:"created_at"`
	NextRotationAt string         `json:"next_rotation_at"`
	LastRotationAt string         `json:"last_rotation_at"`
	Metadata       AffinityParams `json:"metadata"`
}

// ProxySet is a named proxy source.
type ProxySet struct {
	Name   string
	Source ProxySource
}

// SessionStore manages multiple proxy sets and sticky-session affinity.
type SessionStore struct {
	sets          []*sessionStoreSet
	nextSessionID atomic.Uint64
}

type sessionStoreSet struct {
	name        string
	source      ProxySource
	mu          sync.RWMutex
	affinityMap map[string]*affinityEntry
}

type affinityEntry struct {
	sessionID      uint64
	proxy          SourceProxy
	startedAt      time.Time
	createdAt      string
	nextRotationAt time.Time
	lastRotationAt time.Time
	duration       time.Duration
	affinityParams AffinityParams
}

// NewSessionStore builds a SessionStore from a slice of named proxy sets.
func NewSessionStore(sets []ProxySet) *SessionStore {
	rs := make([]*sessionStoreSet, len(sets))
	for i, ps := range sets {
		rs[i] = &sessionStoreSet{
			name:        ps.Name,
			source:      ps.Source,
			affinityMap: make(map[string]*affinityEntry),
		}
	}
	return &SessionStore{sets: rs}
}

// SetNames returns all proxy set names.
func (r *SessionStore) SetNames() []string {
	names := make([]string, len(r.sets))
	for i, s := range r.sets {
		names[i] = s.name
	}
	return names
}

// NextProxy picks the next proxy for a request, honouring sticky affinity.
func (r *SessionStore) NextProxy(ctx context.Context, setName string, affinityMinutes uint16, usernameb64 string, params AffinityParams) (*ResolvedProxy, error) {
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
func (r *SessionStore) PickAny(ctx context.Context, setName string) (*ResolvedProxy, error) {
	set := r.findSet(setName)
	if set == nil {
		return nil, nil
	}
	proxy, err := set.source.GetSourceProxy(ctx, NewAffinityParams())
	if err != nil || proxy == nil {
		return nil, err
	}
	return resolvedFrom(proxy), nil
}

// ForceRotate force-rotates the upstream proxy for an existing session.
func (r *SessionStore) ForceRotate(ctx context.Context, username string) (*SessionInfo, error) {
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
func (r *SessionStore) GetSession(username string) *SessionInfo {
	for _, set := range r.sets {
		if info := set.getSession(username); info != nil {
			return info
		}
	}
	return nil
}

// ListSessions returns all active (non-expired) sessions.
func (r *SessionStore) ListSessions() []SessionInfo {
	var all []SessionInfo
	for _, set := range r.sets {
		all = append(all, set.listSessions()...)
	}
	return all
}

// SpawnSessionCleanup starts a background goroutine that evicts expired sessions every minute.
func SpawnSessionCleanup(r *SessionStore) {
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
// sessionStoreSet internals
// ---------------------------------------------------------------------------

func (s *sessionStoreSet) pick(ctx context.Context, affinityMinutes uint16, usernameb64 string, params AffinityParams, idCounter *atomic.Uint64) (*SourceProxy, error) {
	if affinityMinutes == 0 {
		return s.source.GetSourceProxy(ctx, params)
	}

	duration := time.Duration(affinityMinutes) * time.Minute

	s.mu.RLock()
	entry, ok := s.affinityMap[usernameb64]
	if ok && time.Since(entry.startedAt) < entry.duration {
		cp := entry.proxy
		s.mu.RUnlock()
		return &cp, nil
	}
	s.mu.RUnlock()

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
		createdAt:      FormatTime(nowWall),
		nextRotationAt: nowWall.Add(duration),
		lastRotationAt: nowWall,
		duration:       duration,
		affinityParams: params,
	}

	s.mu.Lock()
	if existing, ok := s.affinityMap[usernameb64]; ok && time.Since(existing.startedAt) < existing.duration {
		cp := existing.proxy
		s.mu.Unlock()
		return &cp, nil
	}
	s.affinityMap[usernameb64] = newEntry
	s.mu.Unlock()

	return proxy, nil
}

func (s *sessionStoreSet) forceRotate(ctx context.Context, username string) (*SessionInfo, error) {
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

	return info, nil
}

func (s *sessionStoreSet) getSession(username string) *SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.affinityMap[username]
	if !ok || time.Since(entry.startedAt) >= entry.duration {
		return nil
	}
	return sessionInfoFrom(entry.sessionID, username, s.name, entry)
}

func (s *sessionStoreSet) listSessions() []SessionInfo {
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

func (s *sessionStoreSet) cleanExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, entry := range s.affinityMap {
		if time.Since(entry.startedAt) >= entry.duration {
			delete(s.affinityMap, key)
		}
	}
}

func (r *SessionStore) findSet(name string) *sessionStoreSet {
	for _, s := range r.sets {
		if s.name == name {
			return s
		}
	}
	return nil
}

func resolvedFrom(p *SourceProxy) *ResolvedProxy {
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
		NextRotationAt: FormatTime(e.nextRotationAt),
		LastRotationAt: FormatTime(e.lastRotationAt),
		Metadata:       e.affinityParams,
	}
}

// FormatTime formats a time.Time as ISO 8601 UTC.
func FormatTime(t time.Time) string {
	t = t.UTC()
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02dZ",
		t.Year(), int(t.Month()), t.Day(),
		t.Hour(), t.Minute(), t.Second())
}
