package proxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/aliwert/aetheris/internal/event"
	api "github.com/aliwert/aetheris/pkg/aetherisapi"
)

type Handler struct {
	router  api.Router
	spooler *event.Spooler
	logger  *slog.Logger
}

func NewHandler(r api.Router, s *event.Spooler, l *slog.Logger) *Handler {
	return &Handler{router: r, spooler: s, logger: l}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	match, err := h.router.Match(r)
	if err != nil {
		if errors.Is(err, api.ErrNoRoute) {
			http.Error(w, "no matching route", http.StatusNotFound)
			return
		}
		h.logger.Error("router match error", "err", err)
		http.Error(w, "internal routing error", http.StatusBadGateway)
		return
	}

	// if the route is an 'event-spooler', add the req. to the queue and return 202
	if match.UpstreamID == "event-spooler" {
		h.handleEvent(w, r)
		return
	}

	h.handleProxy(w, r, match)
}

func (h *Handler) handleProxy(w http.ResponseWriter, r *http.Request, match api.RouteMatch) {
	timeout := match.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	r = r.WithContext(ctx)

	// pick backend from the pool
	backend, err := match.Balancer.Next(ctx)
	if err != nil {
		h.logger.Error("no healthy backend", "upstream", match.UpstreamID)
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		return
	}

	target, _ := url.Parse(backend.Address)

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			if match.StripPrefix != "" {
				req.URL.Path = strings.TrimPrefix(req.URL.Path, match.StripPrefix)
			}
			req.Host = target.Host
		},
		ErrorHandler: func(w http.ResponseWriter, req *http.Request, err error) {
			h.logger.Error("upstream error", "backend", backend.ID, "err", err)
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r)
}

func (h *Handler) handleEvent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // max 1MB
	if err != nil {
		http.Error(w, "payload too large", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	evt := event.Event{
		Payload:    body,
		ReceivedAt: time.Now(),
	}

	if err := h.spooler.Enqueue(r.Context(), evt); err != nil {
		h.logger.Warn("spooler full, dropping event")
		http.Error(w, "server busy", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}
