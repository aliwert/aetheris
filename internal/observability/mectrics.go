package observability

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "aetheris",
			Subsystem: "proxy",
			Name:      "requests_total",
			Help:      "Total HTTP requests processed.",
		},
		[]string{"route_id", "method", "status_code"},
	)

	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "aetheris",
			Subsystem: "proxy",
			Name:      "request_duration_seconds",
			Help:      "End-to-end HTTP request latency.",
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
)

// should be called only once from main when the app starts
func RegisterMetrics() {
	prometheus.MustRegister(
		RequestsTotal,
		RequestDuration,
		ActiveConnections,
		collectors.NewGoCollector(), // runtime metrics
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// returns the prometheus handler for the /metrics endpoint
func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
