package core

import (
	"sync"
	"time"
)

// CheapRandom returns a fast non-cryptographic random uint64.
// Uses a pool of xorshift64 states, one per goroutine slot.
// Seeded from time so different runs produce different sequences.
// Not suitable for cryptographic use — only for tie-breaking.

type xorshift64State struct {
	x uint64
}

func (s *xorshift64State) next() uint64 {
	s.x ^= s.x << 13
	s.x ^= s.x >> 7
	s.x ^= s.x << 17
	return s.x
}

var (
	rngPool = sync.Pool{
		New: func() interface{} {
			seed := uint64(time.Now().UnixNano()) ^ 0x517cc1b727220a95
			if seed == 0 {
				seed = 0x517cc1b727220a95
			}
			return &xorshift64State{x: seed}
		},
	}
)

// CheapRandom returns a random uint64. Fast and non-cryptographic.
func CheapRandom() uint64 {
	s := rngPool.Get().(*xorshift64State)
	v := s.next()
	rngPool.Put(s)
	return v
}
