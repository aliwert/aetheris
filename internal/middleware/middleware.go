package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/aliwert/aetheris/internal/observability"
	"github.com/google/uuid"
)

type Chain struct {
	middlewares []func(http.Handler) http.Handler
}

func NewChain(mws ...func(http.Handler) http.Handler) Chain {
	return Chain{middlewares: mws}
}

func (c Chain) Then(h http.Handler) http.Handler {
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		h = c.middlewares[i](h)
	}
	return h
}

type traceIDKey struct{}
type routeIDKey struct{}

// a custom ResponseWriter to capture the status code for metrics
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					logger.Error("panic recovered",
						"panic", fmt.Sprintf("%v", rec),
						"stack", string(debug.Stack()),
					)
					http.Error(w, http.StatusText(500), 500)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// assigns a uuid to each request (for distributed tracing)
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = uuid.New().String()
			}
			w.Header().Set("X-Request-ID", id)
			ctx := context.WithValue(r.Context(), traceIDKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// save the matched route ID in the context for metrics labeling
func Metrics() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			observability.ActiveConnections.Inc()
			defer observability.ActiveConnections.Dec()

			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, statusCode: 200}

			next.ServeHTTP(rw, r)

			duration := time.Since(start).Seconds()
			routeID := "unknown"
			if id, ok := r.Context().Value(routeIDKey{}).(string); ok {
				routeID = id
			}

			observability.RequestsTotal.WithLabelValues(routeID, r.Method, strconv.Itoa(rw.statusCode)).Inc()
			observability.RequestDuration.WithLabelValues(routeID, r.Method).Observe(duration)
		})
	}
}
