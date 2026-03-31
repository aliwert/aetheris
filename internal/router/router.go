package router

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"

	api "github.com/aliwert/aetheris/pkg/aetherisapi"
)

type Router struct {
	mu        sync.RWMutex
	table     []compiledRoute
	index     map[string]int
	logger    *slog.Logger
	balancers map[string]api.LoadBalancer
}

func New(
	routes []api.RouteConfig,
	balancers map[string]api.LoadBalancer,
	logger *slog.Logger,
) (*Router, error) {
	r := &Router{
		balancers: balancers,
		logger:    logger,
		index:     make(map[string]int),
	}
	if err := r.Reload(routes); err != nil {
		return nil, fmt.Errorf("router: initial load failed: %w", err)
	}
	return r, nil
}

func (rt *Router) Match(r *http.Request) (api.RouteMatch, error) {
	rt.mu.RLock()
	table := rt.table
	rt.mu.RUnlock()

	for i := range table {
		cr := &table[i]
		if !cr.matches(r) {
			continue
		}

		lb, ok := rt.balancers[cr.original.UpstreamID]
		if !ok {
			rt.logger.Error("route references unknown upstream",
				"route_id", cr.id,
				"upstream_id", cr.original.UpstreamID,
			)
			continue
		}

		return api.RouteMatch{
			UpstreamID:  cr.original.UpstreamID,
			Balancer:    lb,
			StripPrefix: cr.original.StripPrefix,
			Timeout:     cr.original.Timeout,
		}, nil
	}

	return api.RouteMatch{}, api.ErrNoRoute
}

func (rt *Router) Reload(routes []api.RouteConfig) error {
	compiled := make([]compiledRoute, 0, len(routes))
	for _, rc := range routes {
		if rc.ID == "" {
			return fmt.Errorf("router: route with empty ID is invalid")
		}
		if rc.UpstreamID == "" {
			return fmt.Errorf("router: route %q has no upstream_id", rc.ID)
		}
		compiled = append(compiled, compile(rc))
	}

	// sort descending by priority so the most specific routes are
	// checked first in match
	sort.SliceStable(compiled, func(i, j int) bool {
		return compiled[i].priority > compiled[j].priority
	})

	rt.mu.Lock()
	rt.table = compiled
	rt.mu.Unlock()

	rt.logger.Info("routing table reloaded", "route_count", len(compiled))
	return nil
}

// returns a copy of the current routing table as RouteConfigs
func (rt *Router) Snapshot() []api.RouteConfig {
	rt.mu.RLock()
	table := rt.table
	rt.mu.RUnlock()

	out := make([]api.RouteConfig, len(table))
	for i, cr := range table {
		out[i] = cr.original
	}
	return out
}
