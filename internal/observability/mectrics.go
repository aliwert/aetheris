package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// all metric variables are package-level so they survive across
// config hot-reloads and can be referenced from any package without
// creating a shared state struct
var (
	RequestsTotal         *prometheus.CounterVec
	RequestDuration       *prometheus.HistogramVec
	ActiveConnections     prometheus.Gauge
	UpstreamStatusCodes   *prometheus.CounterVec
	BackendErrors         *prometheus.CounterVec
	CircuitBreakerState   *prometheus.GaugeVec
	RateLimitedRequests   *prometheus.CounterVec
	EventsAccepted        prometheus.Counter
	EventSpoolDropped     prometheus.Counter
	EventDispatchDuration prometheus.Histogram
)

// initialises and registers all Prometheus metrics
// It must be called exactly once at startup, before any HTTP handler
// is registered. The function is intentionally not idempotent (it will
// panic on double-registration) to catch misconfiguration early
func RegisterMetrics() {
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "aetheris",
			Subsystem: "proxy",
			Name:      "requests_total",
			Help:      "Total number of HTTP requests processed by the proxy.",
		},
		[]string{"route_id", "method", "status_code"},
	)

	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "aetheris",
			Subsystem: "proxy",
			Name:      "request_duration_seconds",
			Help:      "End-to-end HTTP request latency in seconds.",
			Buckets:   []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"route_id", "method"},
	)

	ActiveConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "aetheris",
			Subsystem: "proxy",
			Name:      "active_connections",
			Help:      "Number of HTTP connections currently being served.",
		},
	)

	UpstreamStatusCodes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "aetheris",
			Subsystem: "upstream",
			Name:      "response_status_total",
			Help:      "HTTP response status codes received from upstream services.",
		},
		[]string{"upstream_id", "status_code"},
	)

	BackendErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "aetheris",
			Subsystem: "upstream",
			Name:      "errors_total",
			Help:      "Errors encountered when selecting or dialling an upstream backend.",
		},
		[]string{"upstream_id", "error_class"},
	)

	CircuitBreakerState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "aetheris",
			Subsystem: "resilience",
			Name:      "circuit_breaker_state",
			Help:      "Current circuit breaker state: 0=closed, 1=half_open, 2=open.",
		},
		[]string{"upstream_id"},
	)

	RateLimitedRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "aetheris",
			Subsystem: "resilience",
			Name:      "rate_limited_requests_total",
			Help:      "Requests rejected by the token-bucket rate limiter.",
		},
		[]string{"key"},
	)

	EventsAccepted = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "aetheris",
			Subsystem: "spooler",
			Name:      "events_accepted_total",
			Help:      "Total events accepted and enqueued by the async spooler.",
		},
	)

	EventSpoolDropped = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "aetheris",
			Subsystem: "spooler",
			Name:      "events_dropped_total",
			Help:      "Events dropped because the spool buffer was full.",
		},
	)

	EventDispatchDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "aetheris",
			Subsystem: "spooler",
			Name:      "dispatch_duration_seconds",
			Help:      "Latency from event enqueue to successful downstream dispatch.",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 12),
		},
	)

	// register all custom metrics.
	prometheus.MustRegister(
		RequestsTotal,
		RequestDuration,
		ActiveConnections,
		UpstreamStatusCodes,
		BackendErrors,
		CircuitBreakerState,
		RateLimitedRequests,
		EventsAccepted,
		EventSpoolDropped,
		EventDispatchDuration,
	)

	// register standard runtime and process metrics
	// these are invaluable for diagnosing Go-specific
	// performance issues in production
	prometheus.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// returns the Prometheus scrape endpoint handler
// mount this on the admin server, not the proxy server, to prevent
// external clients from accessing internal telemetry
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
