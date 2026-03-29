package utils

import (
	"sync/atomic"

	"proxy-kit"
)

// CountingPool is a fixed-size pool with least-used selection.
type CountingPool[T any] struct {
	entries []poolEntry[T]
}

type poolEntry[T any] struct {
	value    T
	useCount atomic.Uint64
}

func NewCountingPool[T any](items []T) *CountingPool[T] {
	entries := make([]poolEntry[T], len(items))
	for i, v := range items {
		entries[i].value = v
	}
	return &CountingPool[T]{entries: entries}
}

func (p *CountingPool[T]) Len() int      { return len(p.entries) }
func (p *CountingPool[T]) IsEmpty() bool { return len(p.entries) == 0 }

// Next returns the least-used entry, breaking ties randomly.
func (p *CountingPool[T]) Next() *T {
	if len(p.entries) == 0 {
		return nil
	}
	idx := pickLeastUsed(p.entries, CheapRandom())
	p.entries[idx].useCount.Add(1)
	return &p.entries[idx].value
}

// NextWithSeed returns the least-used entry, breaking ties deterministically
// using the given SessionSeed. Same seed → same tie-break → same entry.
// If seed is nil, falls back to random tie-breaking (same as Next).
func (p *CountingPool[T]) NextWithSeed(seed *proxykit.SessionSeed) *T {
	if len(p.entries) == 0 {
		return nil
	}
	tieBreaker := CheapRandom()
	if seed != nil {
		tieBreaker = seed.Value()
	}
	idx := pickLeastUsed(p.entries, tieBreaker)
	p.entries[idx].useCount.Add(1)
	return &p.entries[idx].value
}

func (p *CountingPool[T]) NextExcluding(equal func(T) bool) *T {
	if len(p.entries) == 0 {
		return nil
	}
	var nonExcluded []int
	for i := range p.entries {
		if !equal(p.entries[i].value) {
			nonExcluded = append(nonExcluded, i)
		}
	}
	var idx int
	if len(nonExcluded) == 0 {
		idx = pickLeastUsed(p.entries, CheapRandom())
	} else {
		idx = pickLeastUsedIndices(p.entries, nonExcluded, CheapRandom())
	}
	p.entries[idx].useCount.Add(1)
	return &p.entries[idx].value
}

func pickLeastUsed[T any](entries []poolEntry[T], tieBreaker uint64) int {
	minCount := entries[0].useCount.Load()
	for i := 1; i < len(entries); i++ {
		if c := entries[i].useCount.Load(); c < minCount {
			minCount = c
		}
	}
	var candidates []int
	for i := range entries {
		if entries[i].useCount.Load() == minCount {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return candidates[tieBreaker%uint64(len(candidates))]
}

func pickLeastUsedIndices[T any](entries []poolEntry[T], indices []int, tieBreaker uint64) int {
	minCount := entries[indices[0]].useCount.Load()
	for _, i := range indices[1:] {
		if c := entries[i].useCount.Load(); c < minCount {
			minCount = c
		}
	}
	var candidates []int
	for _, i := range indices {
		if entries[i].useCount.Load() == minCount {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return candidates[tieBreaker%uint64(len(candidates))]
}
