package resilience

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

type RLConfig struct {
	DefaultRate  float64
	DefaultBurst float64
}

type tokenBucket struct {
	tokensU64      uint64 // stored as uint64 for atomic operations; use math.Float64frombits to read as float64
	lastRefillNano int64
	rate           float64
	burst          float64
}

func newTokenBucket(rate, burst float64) *tokenBucket {
	return &tokenBucket{
		tokensU64:      math.Float64bits(burst),
		lastRefillNano: time.Now().UnixNano(),
		rate:           rate,
		burst:          burst,
	}
}

func (tb *tokenBucket) tryConsume(n float64) (bool, float64) {
	now := time.Now().UnixNano()
	for {
		oldTokensU64 := atomic.LoadUint64(&tb.tokensU64)
		oldNano := atomic.LoadInt64(&tb.lastRefillNano)

		elapsed := float64(now-oldNano) / float64(time.Second)
		accrued := elapsed * tb.rate
		current := math.Float64frombits(oldTokensU64)

		refilled := math.Min(current+accrued, tb.burst)
		if refilled < n {
			return false, 0
		}

		newTokens := refilled - n
		newU64 := math.Float64bits(newTokens)

		// Lock-free CAS (Compare-And-Swap) operation
		if atomic.CompareAndSwapUint64(&tb.tokensU64, oldTokensU64, newU64) {
			atomic.StoreInt64(&tb.lastRefillNano, now)
			return true, newTokens
		}
	}
}

type RateLimiter struct {
	cfg     RLConfig
	buckets sync.Map // map[string]*tokenBucket
}

func NewRateLimiter(cfg RLConfig) *RateLimiter {
	return &RateLimiter{cfg: cfg}
}

func (rl *RateLimiter) Allow(ctx context.Context, key string) (bool, int) {
	bucket := rl.getOrCreate(key)
	allowed, remaining := bucket.tryConsume(1.0)
	return allowed, int(math.Floor(remaining))
}

func (rl *RateLimiter) getOrCreate(key string) *tokenBucket {
	if v, ok := rl.buckets.Load(key); ok {
		return v.(*tokenBucket)
	}
	newBucket := newTokenBucket(rl.cfg.DefaultRate, rl.cfg.DefaultBurst)
	actual, _ := rl.buckets.LoadOrStore(key, newBucket)
	return actual.(*tokenBucket)
}
