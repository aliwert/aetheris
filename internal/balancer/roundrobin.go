package balancer

import (
	"context"
	"sync/atomic"

	api "github.com/aliwert/aetheris/pkg/aetherisapi"
)

// distributes requests across healthy backends in a circular,
// equal-weight sequence
//
// selection is performed using a single uint64 counter incremented with
// atomic.AddUint64, which is safe for concurrent use without any mutex.
// the modulo operation maps the ever-increasing counter onto the current
// pool size; because the counter never resets, there is no thundering
// herd when backends are added or removed mid-operation
//
// time complexity: O(n) for healthySnapshot (n = total backends),
// O(1) for the selection itself.
type RoundRobin struct {
	baseBalancer
	// counter is the only mutable state in the hot path.
	// declared as a struct field (not embedded) so its 64-bit alignment
	// is guaranteed on 32-bit architectures (Go spec: 64-bit atomics
	// must be 64-bit aligned; struct fields satisfy this automatically)
	counter uint64
}

// constructs a RoundRobin balancer pre-populated with the
// given backends. All backends are marked healthy on construction;
// call MarkHealthy to change their status after health-check results
// arrive
func NewRoundRobin(backends ...*api.Backend) *RoundRobin {
	rr := &RoundRobin{}
	for _, b := range backends {
		// mark as healthy by default; the health checker will
		// demote any that fail their first probe.
		atomic.StoreInt32(&b.HealthyFlag, 1)
		rr.backends = append(rr.backends, b)
	}
	return rr
}

// selects the next healthy backend in round-robin order
//
// The algorithm:
//  1. atomically increment the counter and capture the previous value
//  2. take a read-locked snapshot of healthy backends (avoids holding
//     a lock during the actual index calculation)
//  3. modulo the counter against the pool length to pick an index
//
// if the pool is empty (all backends unhealthy or none registered),
// returned immediately without blocking.
func (rr *RoundRobin) Next(ctx context.Context) (*api.Backend, error) {
	// context cancellation before doing any work
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	pool := rr.healthySnapshot()
	if len(pool) == 0 {
		return nil, api.ErrNoHealthyBackend
	}

	// returns the new value; subtract 1 to get the
	// pre-increment value so index 0 is selected on the very first call
	n := atomic.AddUint64(&rr.counter, 1) - 1
	selected := pool[n%uint64(len(pool))]
	return selected, nil
}
