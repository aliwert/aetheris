package balancer

import (
	"sync"
	"sync/atomic"

	api "github.com/aliwert/aetheris/pkg/aetherisapi"
)

// holds the shared backend pool used by all strategies
// it is embedded by concrete implementations so they inherit the
type baseBalancer struct {
	mu       sync.RWMutex
	backends []*api.Backend
}

// registers a backend into the pool, if a backend with the same ID
// already exists, the call is a no-op
func (b *baseBalancer) Add(backend *api.Backend) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, existing := range b.backends {
		if existing.ID == backend.ID {
			return
		}
	}
	b.backends = append(b.backends, backend)
}

func (b *baseBalancer) Remove(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, backend := range b.backends {
		if backend.ID == id {
			// swap with last element and truncate; O(1), avoids copy.
			b.backends[i] = b.backends[len(b.backends)-1]
			b.backends[len(b.backends)-1] = nil // prevent GC leak
			b.backends = b.backends[:len(b.backends)-1]
			return
		}
	}
}

func (b *baseBalancer) MarkHealthy(id string, healthy bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, backend := range b.backends {
		if backend.ID == id {
			if healthy {
				atomic.StoreInt32(&backend.HealthyFlag, 1)
			} else {
				atomic.StoreInt32(&backend.HealthyFlag, 0)
			}
			return
		}
	}
}

func (b *baseBalancer) Backends() []*api.Backend {
	b.mu.RLock()
	defer b.mu.RUnlock()

	snapshot := make([]*api.Backend, len(b.backends))
	copy(snapshot, b.backends)
	return snapshot
}

func (b *baseBalancer) healthySnapshot() []*api.Backend {
	b.mu.RLock()
	defer b.mu.RUnlock()

	healthy := make([]*api.Backend, 0, len(b.backends))
	for _, backend := range b.backends {
		if atomic.LoadInt32(&backend.HealthyFlag) == 1 {
			healthy = append(healthy, backend)
		}
	}
	return healthy
}
