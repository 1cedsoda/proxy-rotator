package proxykit

import (
	"context"
	"encoding/binary"
	"hash/fnv"
)

// SessionSeed is a deterministic value that sources use to make reproducible
// choices (session IDs, country selection, pool tie-breaking).
//
// It is computed from a top-level seed (uint64, derived from affinity params)
// mixed with a rotation counter:
//
//	SessionSeed = hash(topLevelSeed + rotation)
//
// Same top-level seed + same rotation → same SessionSeed → same choices.
// Bumping the rotation counter produces a completely different SessionSeed.
//
// A nil *SessionSeed in context means no session affinity is active.
// Sources decide what nil means for their domain — randomize on every
// call, refuse to serve, or anything else.
type SessionSeed struct {
	value uint64
}

// Value returns the raw seed as a uint64. Sources use this to derive
// deterministic choices (e.g., seed % len(countries), hex-encoded session IDs).
func (s *SessionSeed) Value() uint64 { return s.value }

// DeriveStringKey returns a deterministic string of the given length using
// characters from charset. Useful for upstream session IDs, tokens, etc.
//
// Common charsets:
//
//	"0123456789abcdef"                           — hex
//	"abcdefghijklmnopqrstuvwxyz0123456789"       — alphanumeric lower
//	"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789" — alphanumeric
func (s *SessionSeed) DeriveStringKey(charset string, length int) string {
	n := uint64(len(charset))
	v := s.value ^ (s.value >> 17) ^ (s.value << 31)
	buf := make([]byte, length)
	for i := range buf {
		buf[i] = charset[v%n]
		v = v*6364136223846793005 + 1442695040888963407 // LCG step
	}
	return string(buf)
}

// Pick returns a deterministic index in [0, n) derived from the seed.
// Panics if n <= 0.
func (s *SessionSeed) Pick(n int) int {
	return int(s.value % uint64(n))
}

// NewSessionSeed computes a SessionSeed from a top-level seed and rotation counter.
func NewSessionSeed(topLevelSeed uint64, rotation uint64) *SessionSeed {
	h := fnv.New64a()
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], topLevelSeed)
	h.Write(buf[:])
	binary.LittleEndian.PutUint64(buf[:], rotation)
	h.Write(buf[:])
	return &SessionSeed{value: h.Sum64()}
}

// TopLevelSeed hashes an arbitrary string into a uint64 suitable for use
// as the topLevelSeed argument to NewSessionSeed.
func TopLevelSeed(key string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(key))
	return h.Sum64()
}

type ctxSeedKey struct{}

// WithSessionSeed stores a *SessionSeed in context.
func WithSessionSeed(ctx context.Context, seed *SessionSeed) context.Context {
	return context.WithValue(ctx, ctxSeedKey{}, seed)
}

// GetSessionSeed reads the *SessionSeed from context.
// Returns nil if no seed is set (no session affinity).
func GetSessionSeed(ctx context.Context) *SessionSeed {
	v, _ := ctx.Value(ctxSeedKey{}).(*SessionSeed)
	return v
}
