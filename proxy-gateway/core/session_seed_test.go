package core

import (
	"context"
	"testing"
)

func TestTopLevelSeedDeterministic(t *testing.T) {
	s1 := TopLevelSeed("alice\x00residential")
	s2 := TopLevelSeed("alice\x00residential")
	if s1 != s2 {
		t.Fatalf("same input should produce same seed: %d vs %d", s1, s2)
	}
}

func TestTopLevelSeedDifferentInputs(t *testing.T) {
	s1 := TopLevelSeed("alice\x00residential")
	s2 := TopLevelSeed("bob\x00residential")
	if s1 == s2 {
		t.Fatal("different inputs should produce different seeds")
	}
}

func TestNewSessionSeedDeterministic(t *testing.T) {
	tls := TopLevelSeed("test")
	s1 := NewSessionSeed(tls, 0)
	s2 := NewSessionSeed(tls, 0)
	if s1.Value() != s2.Value() {
		t.Fatalf("same inputs should produce same seed: %d vs %d", s1.Value(), s2.Value())
	}
}

func TestNewSessionSeedDifferentTopLevel(t *testing.T) {
	s1 := NewSessionSeed(TopLevelSeed("alice"), 0)
	s2 := NewSessionSeed(TopLevelSeed("bob"), 0)
	if s1.Value() == s2.Value() {
		t.Fatal("different top-level seeds should produce different session seeds")
	}
}

func TestNewSessionSeedDifferentRotations(t *testing.T) {
	tls := TopLevelSeed("test")
	s1 := NewSessionSeed(tls, 0)
	s2 := NewSessionSeed(tls, 1)
	if s1.Value() == s2.Value() {
		t.Fatal("different rotations should produce different seeds")
	}
}

func TestSessionSeedDeriveStringKey(t *testing.T) {
	s := NewSessionSeed(123456, 0)
	hex := "0123456789abcdef"

	id1 := s.DeriveStringKey(hex, 16)
	id2 := s.DeriveStringKey(hex, 16)
	if id1 != id2 {
		t.Fatalf("same seed should produce same key: %s vs %s", id1, id2)
	}
	if len(id1) != 16 {
		t.Fatalf("key should be 16 chars, got %d: %s", len(id1), id1)
	}

	// Different seed → different key
	s2 := NewSessionSeed(123456, 1)
	if s.DeriveStringKey(hex, 16) == s2.DeriveStringKey(hex, 16) {
		t.Fatal("different seeds should produce different keys")
	}
}

func TestSessionSeedDeriveStringKeyCharset(t *testing.T) {
	s := NewSessionSeed(99, 0)
	charset := "abc"
	key := s.DeriveStringKey(charset, 100)
	for i, c := range key {
		if c != 'a' && c != 'b' && c != 'c' {
			t.Fatalf("char %d is %q, not in charset %q", i, string(c), charset)
		}
	}
}

func TestSessionSeedDeriveStringKeyLength(t *testing.T) {
	s := NewSessionSeed(42, 0)
	for _, n := range []int{1, 5, 32, 64} {
		key := s.DeriveStringKey("0123456789abcdef", n)
		if len(key) != n {
			t.Fatalf("expected length %d, got %d", n, len(key))
		}
	}
}

func TestSessionSeedPick(t *testing.T) {
	s := NewSessionSeed(42, 0)
	idx := s.Pick(10)
	if idx < 0 || idx >= 10 {
		t.Fatalf("Pick(10) returned %d, expected [0,10)", idx)
	}
	if s.Pick(10) != idx {
		t.Fatal("Pick should be deterministic")
	}
}

func TestSessionSeedContext(t *testing.T) {
	ctx := context.Background()
	if GetSessionSeed(ctx) != nil {
		t.Fatal("expected nil seed from empty context")
	}

	seed := NewSessionSeed(42, 0)
	ctx = WithSessionSeed(ctx, seed)
	got := GetSessionSeed(ctx)
	if got == nil {
		t.Fatal("expected non-nil seed")
	}
	if got.Value() != seed.Value() {
		t.Fatalf("expected seed %d, got %d", seed.Value(), got.Value())
	}
}

func TestSessionSeedNilContext(t *testing.T) {
	ctx := context.Background()
	if GetSessionSeed(ctx) != nil {
		t.Fatal("empty context should return nil seed")
	}
}
