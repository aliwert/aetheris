package balancer

import (
	"context"
	"sync/atomic"

	api "github.com/aliwert/aetheris/pkg/aetherisapi"
)

type LeastConnections struct {
	baseBalancer
}

// returned by Next and MUST be released via Done() when
// the upstream request completes. This two-phase acquire/release design
// makes accounting explicit and prevents callers from forgetting to
// decrement
type ConnToken struct {
	backend *api.Backend
}

func (t *ConnToken) Done() {
	atomic.AddInt64(&t.backend.ActiveConns, -1)
}

// returns the selected backend for use by the proxy handler
func (t *ConnToken) Backend() *api.Backend {
	return t.backend
}

// constructs a LeastConnections balancer.
func NewLeastConnections(backends ...*api.Backend) *LeastConnections {
	lc := &LeastConnections{}
	for _, b := range backends {
		atomic.StoreInt32(&b.HealthyFlag, 1)
		lc.backends = append(lc.backends, b)
	}
	return lc
}

// selects the healthy backend with the lowest active connection
// count and atomically increments its counter before returning
func (lc *LeastConnections) Next(ctx context.Context) (*api.Backend, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	pool := lc.healthySnapshot()
	if len(pool) == 0 {
		return nil, api.ErrNoHealthyBackend
	}

	// linear scan: find the backend with minimum ActiveConns
	// LoadInt64 is used (not a regular read) because ActiveConns may
	// be modified concurrently by other goroutines' Done() calls
	var (
		best     *api.Backend
		bestConn int64 = -1 // sentinel: -1 means "not set yet"
	)
	for _, b := range pool {
		conns := atomic.LoadInt64(&b.ActiveConns)
		if bestConn == -1 || conns < bestConn {
			bestConn = conns
			best = b
		}
	}
	atomic.AddInt64(&best.ActiveConns, 1)
	return best, nil
}

// the preferred call site for the proxy handler because
// it returns a ConnToken that enforces Done() to be called on completion
func (lc *LeastConnections) NextWithToken(ctx context.Context) (*ConnToken, error) {
	backend, err := lc.Next(ctx)
	if err != nil {
		return nil, err
	}
	return &ConnToken{backend: backend}, nil
}
