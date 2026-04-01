package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/aliwert/aetheris/internal/resilience"
	api "github.com/aliwert/aetheris/pkg/aetherisapi"
)

// the root configuration object. It is loaded once at startup
// and re-loaded by the config watcher on every valid file change.
//
// Example aetheris.yaml:
//
//	server:
//	  listen: ":8080"
//	  admin:  ":9090"
//	  read_header_timeout: 5s
//	  read_timeout:        30s
//	  write_timeout:       60s
//	  idle_timeout:        120s
//
//	rate_limit:
//	  default_rate:  1000
//	  default_burst: 2000
//
//	upstreams:
//	  - id: api-service
//	    backends:
//	      - id: api-1
//	        address: "http://api-service-1:8080"
//	        weight: 1
//	      - id: api-2
//	        address: "http://api-service-2:8080"
//	        weight: 1
//	    load_balancer: round_robin
//	    circuit_breaker:
//	      max_failures:       5
//	      open_timeout:       30s
//	      half_open_max_calls: 1
//	      success_threshold:  2
//
//	routes:
//	  - id: api-route
//	    path_prefix:  /api/v1
//	    methods:      [GET, POST, PUT, DELETE]
//	    upstream_id:  api-service
//	    strip_prefix: /api/v1
//	    timeout:      30s
//
//	  - id: event-route
//	    path_prefix:  /events
//	    methods:      [POST]
//	    upstream_id:  event-spooler
//	    timeout:      5s
type Config struct {
	Server    ServerConfig        `yaml:"server"     json:"server"`
	RateLimit resilience.RLConfig `yaml:"rate_limit" json:"rate_limit"`
	Upstreams []UpstreamConfig    `yaml:"upstreams" json:"upstreams"`
	Routes    []api.RouteConfig   `yaml:"routes"   json:"routes"`
	Spooler   SpoolerConfig       `yaml:"spooler"   json:"spooler"`
}

type ServerConfig struct {
	Listen            string   `yaml:"listen" json:"listen"`
	Admin             string   `yaml:"admin" json:"admin"`
	ReadHeaderTimeout Duration `yaml:"read_header_timeout" json:"read_header_timeout"`
	ReadTimeout       Duration `yaml:"read_timeout" json:"read_timeout"`
	WriteTimeout      Duration `yaml:"write_timeout" json:"write_timeout"`
	IdleTimeout       Duration `yaml:"idle_timeout" json:"idle_timeout"`
	MaxHeaderBytes    int      `yaml:"max_header_bytes" json:"max_header_bytes"`
}

// fills zero-value fields with production-safe defaults.
func (s ServerConfig) withDefaults() ServerConfig {
	if s.Listen == "" {
		s.Listen = ":8080"
	}
	if s.Admin == "" {
		s.Admin = ":9090"
	}
	if s.ReadHeaderTimeout.Duration == 0 {
		s.ReadHeaderTimeout = Duration{5 * time.Second}
	}
	if s.ReadTimeout.Duration == 0 {
		s.ReadTimeout = Duration{30 * time.Second}
	}
	if s.WriteTimeout.Duration == 0 {
		s.WriteTimeout = Duration{60 * time.Second}
	}
	if s.IdleTimeout.Duration == 0 {
		s.IdleTimeout = Duration{120 * time.Second}
	}
	if s.MaxHeaderBytes == 0 {
		s.MaxHeaderBytes = 1 << 20 // 1 MiB
	}
	return s
}

// describes a named pool of backends with a shared load-
// balancing strategy and circuit-breaker configuration. Multiple routes
// can point at the same upstream
type UpstreamConfig struct {
	ID       string          `yaml:"id" json:"id"`
	Backends []BackendConfig `yaml:"backends" json:"backends"`
	// selects the balancing algorithm.
	// Valid values: "round_robin" (default), "least_connections"
	LoadBalancer   string               `yaml:"load_balancer" json:"load_balancer"`
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker" json:"circuit_breaker"`
}
type BackendConfig struct {
	ID string `yaml:"id" json:"id"`
	// the full base URL of the backend
	Address string `yaml:"address" json:"address"`
	Weight  int    `yaml:"weight" json:"weight"`
}

// maps directly to resilience.CBConfig.
type CircuitBreakerConfig struct {
	MaxFailures      int      `yaml:"max_failures" json:"max_failures"`
	OpenTimeout      Duration `yaml:"open_timeout" json:"open_timeout"`
	HalfOpenMaxCalls int      `yaml:"half_open_max_calls" json:"half_open_max_calls"`
	SuccessThreshold int      `yaml:"success_threshold" json:"success_threshold"`
}

func (c CircuitBreakerConfig) withDefaults() CircuitBreakerConfig {
	if c.MaxFailures <= 0 {
		c.MaxFailures = 5
	}
	if c.OpenTimeout.Duration == 0 {
		c.OpenTimeout = Duration{30 * time.Second}
	}
	if c.HalfOpenMaxCalls <= 0 {
		c.HalfOpenMaxCalls = 1
	}
	if c.SuccessThreshold <= 0 {
		c.SuccessThreshold = 1
	}
	return c
}

// controls the async event ingestion pipeline
type SpoolerConfig struct {
	// the capacity of the internal event channel
	// events received when the channel is full are rejected with 503
	BufferSize int `yaml:"buffer_size" json:"buffer_size"`

	// the number of goroutines that drain the channel
	// and dispatch events to downstream queues.
	WorkerCount int `yaml:"worker_count" json:"worker_count"`

	// caps the size of an accepted event body
	// requests exceeding this are rejected with 413
	MaxPayloadBytes int64 `yaml:"max_payload_bytes" json:"max_payload_bytes"`

	// the number of dispatch retries before an
	// event is dead-lettered
	RetryMaxAttempts int `yaml:"retry_max_attempts" json:"retry_max_attempts"`

	// the wait before the first retry.
	RetryInitialInterval Duration `yaml:"retry_initial_interval" json:"retry_initial_interval"`
}

func (s SpoolerConfig) withDefaults() SpoolerConfig {
	if s.BufferSize <= 0 {
		s.BufferSize = 65_536
	}
	if s.WorkerCount <= 0 {
		s.WorkerCount = 16
	}
	if s.MaxPayloadBytes <= 0 {
		s.MaxPayloadBytes = 1 << 20 // 1 MiB
	}
	if s.RetryMaxAttempts <= 0 {
		s.RetryMaxAttempts = 3
	}
	if s.RetryInitialInterval.Duration == 0 {
		s.RetryInitialInterval = Duration{100 * time.Millisecond}
	}
	return s
}

// a time.Duration that marshals/unmarshals as a human-readable
// string ("5s", "500ms", "1m30s") in both YAML and JSON, matching the
// format used by the Go standard library's time.ParseDuration
//
// this avoids the common mistake of writing durations as raw integers in
// config files (which format? nanoseconds? milliseconds?) and makes config
// files self-documenting
type Duration struct {
	time.Duration
}

// parses a YAML scalar string into a Duration
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return fmt.Errorf("config: duration must be a string (e.g. \"30s\"): %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("config: invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	return nil
}

// serialises the Duration back to a string for round-trip correctness
func (d Duration) MarshalYAML() (interface{}, error) {
	return d.Duration.String(), nil
}

// parses a JSON string value into a Duration
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("config: duration must be a JSON string: %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("config: invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	return nil
}

// serialises the Duration back to a JSON string
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

// reads and parses the config file at path, applies defaults,
// and validates the result, both YAML and JSON are supported; the format
// is inferred from the file extension
//
// safe to call from multiple goroutines concurrently because it
// only reads from the filesystem and performs no writes to shared state
// The caller is responsible for propagating the returned Config to
// the subsystems that need it
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: cannot read file %q: %w", path, err)
	}

	var cfg Config
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("config: YAML parse error in %q: %w", path, err)
		}
	case ".json":
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("config: JSON parse error in %q: %w", path, err)
		}
	default:
		return nil, fmt.Errorf("config: unsupported file extension %q (use .yaml or .json)", ext)
	}

	// apply subsystem defaults before validation so validators can
	// rely on zero-value checks being meaningful
	cfg.Server = cfg.Server.withDefaults()
	cfg.Spooler = cfg.Spooler.withDefaults()
	for i := range cfg.Upstreams {
		cfg.Upstreams[i].CircuitBreaker = cfg.Upstreams[i].CircuitBreaker.withDefaults()
	}

	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("config: validation failed: %w", err)
	}

	return &cfg, nil
}

// checks the Config for logical consistency, it returns a
// combined error listing all problems so operators can fix everything
// in a single edit rather than discovering errors one at a time
func validate(cfg *Config) error {
	var errs []string

	// build a set of declared upstream IDs for route cross-reference
	upstreamIDs := make(map[string]struct{}, len(cfg.Upstreams))
	for i, u := range cfg.Upstreams {
		if u.ID == "" {
			errs = append(errs, fmt.Sprintf("upstreams[%d]: id is required", i))
			continue
		}
		if _, dup := upstreamIDs[u.ID]; dup {
			errs = append(errs, fmt.Sprintf("upstreams[%d]: duplicate id %q", i, u.ID))
		}
		upstreamIDs[u.ID] = struct{}{}

		for j, b := range u.Backends {
			if b.ID == "" {
				errs = append(errs, fmt.Sprintf("upstreams[%d].backends[%d]: id is required", i, j))
			}
			if b.Address == "" {
				errs = append(errs, fmt.Sprintf("upstreams[%d].backends[%d]: address is required", i, j))
			}
		}

		lb := strings.ToLower(u.LoadBalancer)
		if lb != "" && lb != "round_robin" && lb != "least_connections" {
			errs = append(errs, fmt.Sprintf(
				"upstreams[%d]: invalid load_balancer %q (valid: round_robin, least_connections)", i, u.LoadBalancer,
			))
		}
	}

	// "event-spooler" is a virtual upstream ID handled internally by
	// the proxy handler; it does not require an entry in Upstreams
	upstreamIDs["event-spooler"] = struct{}{}

	routeIDs := make(map[string]struct{}, len(cfg.Routes))
	for i, r := range cfg.Routes {
		if r.ID == "" {
			errs = append(errs, fmt.Sprintf("routes[%d]: id is required", i))
		}
		if _, dup := routeIDs[r.ID]; dup {
			errs = append(errs, fmt.Sprintf("routes[%d]: duplicate id %q", i, r.ID))
		}
		routeIDs[r.ID] = struct{}{}

		if r.PathPrefix == "" {
			errs = append(errs, fmt.Sprintf("routes[%d] (%q): path_prefix is required", i, r.ID))
		}
		if r.UpstreamID == "" {
			errs = append(errs, fmt.Sprintf("routes[%d] (%q): upstream_id is required", i, r.ID))
		} else if _, ok := upstreamIDs[r.UpstreamID]; !ok {
			errs = append(errs, fmt.Sprintf(
				"routes[%d] (%q): upstream_id %q references an undeclared upstream",
				i, r.ID, r.UpstreamID,
			))
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%d error(s):\n  - %s", len(errs), strings.Join(errs, "\n  - "))
}
