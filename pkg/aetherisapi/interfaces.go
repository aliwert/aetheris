package aetherisapi

import (
	"context"
	"net/http"
	"time"
)

type Backend struct {
	ID          string
	Address     string // "scheme://host:port"
	Weight      int
	ActiveConns int64 // atomically incremented
	Healthy     bool
}

// the result of a successful route lookup.
// It carries both the upstream target and the load-balancer
// instance bound to that route's backend pool.
type RouteMatch struct {
	UpstreamID  string
	Balancer    LoadBalancer
	StripPrefix string // strip before forwarding (e.g. /api/v1 → /)
	Timeout     time.Duration
}

type Router interface {
	// the best RouteMatch for the request
	// returns ErrNoRoute when no rule applies
	Match(r *http.Request) (RouteMatch, error)

	// atomically replaces the routing table.
	// called by the config watcher on every valid config change.
	Reload(routes []RouteConfig) error

	// returns the current routing table for introspection
	// (admin API, health-check detail).
	Snapshot() []RouteConfig
}

// the data-transfer type parsed from YAML/JSON.
// intentionally kept separate from RouteMatch to avoid
// leaking serialization concerns into the hot path.
type RouteConfig struct {
	ID          string            `yaml:"id"           json:"id"`
	PathPrefix  string            `yaml:"path_prefix"  json:"path_prefix"`
	Methods     []string          `yaml:"methods"      json:"methods"`
	Headers     map[string]string `yaml:"headers"      json:"headers"`
	UpstreamID  string            `yaml:"upstream_id"  json:"upstream_id"`
	StripPrefix string            `yaml:"strip_prefix" json:"strip_prefix"`
	Timeout     time.Duration     `yaml:"timeout"      json:"timeout"`
}

type LoadBalancer interface {
	// picks the next backend according to the algorithm.
	// returns ErrNoHealthyBackend when the pool is empty or all
	// backends are marked unhealthy.
	Next(ctx context.Context) (*Backend, error)

	// registers a new backend into the pool
	// idempotent if the backend ID already exists
	Add(b *Backend)

	// deregisters a backend by ID
	// in-flight requests to that backend complete normally
	Remove(id string)

	// updates a backend's health flag
	// balancers must skip backends where Healthy == false
	MarkHealthy(id string, healthy bool)

	// returns a point-in-time snapshot of the pool.
	Backends() []*Backend
}

// the key is typically a client IP, an API key, or a route ID,
// allowing independent limits per dimension without shared state.
type RateLimiter interface {
	// consumes one token for the given key.
	// returns true and remaining token count if the request is permitted.
	// returns false and 0 if the bucket is exhausted (caller must reject).
	Allow(ctx context.Context, key string) (allowed bool, remaining int)

	// consumes n tokens atomically.
	// used for burst-aware requests (e.g. batch endpoints).
	AllowN(ctx context.Context, key string, n int) (allowed bool, remaining int)

	// returns the configured rate for a key (tokens/second).
	// returns the default rate if no per-key limit is configured.
	Limit(key string) float64
}

type CircuitBreaker interface {
	// returns nil if the call is permitted.
	// returns ErrCircuitOpen when the breaker is open.
	Allow() error

	// records a successful downstream call.
	RecordSuccess()

	// records a failed downstream call.
	// may transition Closed → Open.
	RecordFailure()

	// returns the current breaker state for observability.
	State() CircuitState
}

// an enum of the three FSM states.
type CircuitState uint8

const (
	StateClosed   CircuitState = iota // normal operation; all requests pass.
	StateOpen                         // all requests rejected; waiting for probe window.
	StateHalfOpen                     // single probe request allowed; evaluating recovery.
)

func (s CircuitState) String() string {
	return [...]string{"closed", "open", "half_open"}[s]
}

var (
	ErrNoRoute           = &proxyError{"no matching route"}
	ErrNoHealthyBackend  = &proxyError{"no healthy backend available"}
	ErrCircuitOpen       = &proxyError{"circuit breaker is open"}
	ErrRateLimitExceeded = &proxyError{"rate limit exceeded"}
)

type proxyError struct{ msg string }

func (e *proxyError) Error() string { return "aetheris: " + e.msg }
