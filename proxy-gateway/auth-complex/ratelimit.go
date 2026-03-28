package complex

import (
	"sync"
	"sync/atomic"
	"time"
)

// LimitType defines what resource the limit applies to.
type LimitType int

const (
	// LimitConcurrentConnections caps simultaneous open connections.
	LimitConcurrentConnections LimitType = iota
	// LimitTotalConnections caps the number of connections opened within the window.
	LimitTotalConnections
	// LimitUploadBytes caps bytes sent client→upstream within the window.
	LimitUploadBytes
	// LimitDownloadBytes caps bytes sent upstream→client within the window.
	LimitDownloadBytes
	// LimitTotalBytes caps combined bytes (upload+download) within the window.
	LimitTotalBytes
)

// Timeframe defines the rolling window size for a rate limit.
type Timeframe int

const (
	// Realtime means the limit is enforced instantly with no time window
	// (only meaningful for LimitConcurrentConnections).
	Realtime Timeframe = iota
	Secondly
	Minutely
	Hourly
	Daily
	Weekly
	Monthly
)

func (tf Timeframe) duration() time.Duration {
	switch tf {
	case Secondly:
		return time.Second
	case Minutely:
		return time.Minute
	case Hourly:
		return time.Hour
	case Daily:
		return 24 * time.Hour
	case Weekly:
		return 7 * 24 * time.Hour
	case Monthly:
		return 30 * 24 * time.Hour
	default:
		return 0
	}
}

// RateLimit describes a single constraint on a user.
type RateLimit struct {
	Type      LimitType
	Timeframe Timeframe
	// Window multiplies the base timeframe duration (e.g. Hourly+Window=6 → 6h rolling).
	// Ignored for Realtime; must be >= 1.
	Window int
	// Max is the maximum value allowed within the window.
	Max int64
}

func (r RateLimit) windowDuration() time.Duration {
	w := r.Window
	if w < 1 {
		w = 1
	}
	return r.Timeframe.duration() * time.Duration(w)
}

// ---------------------------------------------------------------------------
// counter — rolling window backed by atomic buckets
// ---------------------------------------------------------------------------

const numBuckets = 60

type counter struct {
	mu       sync.Mutex
	buckets  []atomic.Int64
	times    []time.Time
	slotSize time.Duration
}

func newCounter(window time.Duration) *counter {
	n := numBuckets
	slotSize := window / time.Duration(n)
	if slotSize < time.Millisecond {
		slotSize = time.Millisecond
	}
	c := &counter{
		buckets:  make([]atomic.Int64, n),
		times:    make([]time.Time, n),
		slotSize: slotSize,
	}
	now := time.Now()
	for i := range c.times {
		c.times[i] = now
	}
	return c
}

// Add adds delta and returns the rolling window total.
func (c *counter) Add(delta int64) int64 {
	c.evict()
	idx := c.currentBucket()
	c.buckets[idx].Add(delta)
	return c.sum()
}

// Total returns the current rolling window total without adding.
func (c *counter) Total() int64 {
	c.evict()
	return c.sum()
}

func (c *counter) currentBucket() int {
	return int(time.Now().UnixNano()/int64(c.slotSize)) % len(c.buckets)
}

func (c *counter) evict() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.buckets {
		if now.Sub(c.times[i]) >= c.slotSize*time.Duration(len(c.buckets)) {
			c.buckets[i].Store(0)
			c.times[i] = now
		}
	}
}

func (c *counter) sum() int64 {
	var total int64
	for i := range c.buckets {
		total += c.buckets[i].Load()
	}
	return total
}
