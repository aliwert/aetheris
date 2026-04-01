package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/aliwert/aetheris/internal/observability"
	api "github.com/aliwert/aetheris/pkg/aetherisapi"
	"github.com/google/uuid"
)

type traceIDKey struct{}
type routeIDKey struct{}

func TraceIDFromContext(r *http.Request) string {
	if id, ok := r.Context().Value(traceIDKey{}).(string); ok {
		return id
	}
	return ""
}

func RouteIDFromContext(r *http.Request) string {
	if id, ok := r.Context().Value(routeIDKey{}).(string); ok {
		return id
	}
	return "unknown"
}

// holds an ordered slice of middleware functions and composes
// them into a single http.Handler via Then()
//
// Middleware is applied right-to-left so that the first middleware
// in the slice is the outermost wrapper: first to execute on a request
// and last to execute on the response.
//
//	chain := NewChain(A, B, C).Then(finalHandler)
//	Request  order: A → B → C → finalHandler
//	Response order: finalHandler → C → B → A
type Chain struct {
	middlewares []func(http.Handler) http.Handler
}

func NewChain(mws ...func(http.Handler) http.Handler) Chain {
	return Chain{middlewares: append([]func(http.Handler) http.Handler{}, mws...)}
}

func (c Chain) Then(h http.Handler) http.Handler {
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		h = c.middlewares[i](h)
	}
	return h
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.written {
		return
	}
	rw.statusCode = code
	rw.written = true
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// catches any panic that escapes the handler stack, logs the
// stack trace at ERROR level, and returns a clean 500 to the client
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					// Re-panic on ErrAbortHandler per net/http convention.
					if rec == http.ErrAbortHandler {
						panic(rec)
					}

					stack := debug.Stack()
					logger.Error("panic recovered",
						"panic", fmt.Sprintf("%v", rec),
						"stack", string(stack),
						"path", r.URL.Path,
						"method", r.Method,
						"request_id", TraceIDFromContext(r),
					)
					http.Error(w,
						http.StatusText(http.StatusInternalServerError),
						http.StatusInternalServerError,
					)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// injects a unique trace ID into the request context and
// response headers on every request
func RequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = uuid.New().String()
			}

			// inject into response so clients can correlate their
			// logs with Aetheris logs using the same ID
			w.Header().Set("X-Request-ID", id)

			ctx := context.WithValue(r.Context(), traceIDKey{}, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func StructuredLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := newResponseWriter(w)

			next.ServeHTTP(rw, r)

			latency := time.Since(start)
			level := slog.LevelInfo
			if rw.statusCode >= 500 {
				level = slog.LevelError
			} else if rw.statusCode >= 400 {
				level = slog.LevelWarn
			}

			logger.LogAttrs(r.Context(), level, "request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("query", r.URL.RawQuery),
				slog.Int("status", rw.statusCode),
				slog.Duration("latency", latency),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user_agent", r.UserAgent()),
				slog.String("request_id", TraceIDFromContext(r)),
				slog.String("route_id", RouteIDFromContext(r)),
			)
		})
	}
}

// instruments every request with Prometheus counters and
// histograms. It must be placed after RequestID (so the trace ID is
// available) but before the proxy handler (so it measures proxy latency,
// not just middleware latency).
func Metrics() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			observability.ActiveConnections.Inc()
			defer observability.ActiveConnections.Dec()

			start := time.Now()
			rw := newResponseWriter(w)

			next.ServeHTTP(rw, r)

			duration := time.Since(start).Seconds()
			routeID := RouteIDFromContext(r)
			statusStr := strconv.Itoa(rw.statusCode)

			observability.RequestsTotal.
				WithLabelValues(routeID, r.Method, statusStr).Inc()
			observability.RequestDuration.
				WithLabelValues(routeID, r.Method).Observe(duration)
		})
	}
}

// controls the per-request rate-limiting strategy.
type RateLimiterConfig struct {
	KeyFunc func(r *http.Request) string
	Limiter api.RateLimiter
}

// rejects requests that exceed the configured rate limit
// with a 429 too Many reqs. response, including the Retry-After
// header set to 1 second as a client hint
func RateLimiter(cfg RateLimiterConfig, logger *slog.Logger) func(http.Handler) http.Handler {
	keyFn := cfg.KeyFunc
	if keyFn == nil {
		keyFn = remoteIP
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			allowed, remaining := cfg.Limiter.Allow(r.Context(), key)

			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			if !allowed {
				observability.RateLimitedRequests.WithLabelValues(key).Inc()
				logger.Warn("rate limit exceeded",
					"key", key,
					"request_id", TraceIDFromContext(r),
					"path", r.URL.Path,
				)
				w.Header().Set("Retry-After", "1")
				http.Error(w,
					http.StatusText(http.StatusTooManyRequests),
					http.StatusTooManyRequests,
				)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extracts the client IP from the request, preferring
// X-Forwarded-For (set by upstream load balancers) over RemoteAddr
// this is the default KeyFunc for RateLimiter
func remoteIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := len(xff); idx > 0 {
			if comma := len(xff); comma > 0 {
				for i, c := range xff {
					if c == ',' {
						return xff[:i]
					}
				}
				return xff
			}
		}
	}

	// "host:port"; strip the port
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
