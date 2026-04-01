package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aliwert/aetheris/internal/balancer"
	"github.com/aliwert/aetheris/internal/config"
	"github.com/aliwert/aetheris/internal/event"
	"github.com/aliwert/aetheris/internal/middleware"
	"github.com/aliwert/aetheris/internal/observability"
	"github.com/aliwert/aetheris/internal/proxy"
	"github.com/aliwert/aetheris/internal/resilience"
	"github.com/aliwert/aetheris/internal/router"
	api "github.com/aliwert/aetheris/pkg/aetherisapi"
)

func main() {
	configPath := flag.String("config", "configs/aetheris.yaml", "path to config file")
	listenAddr := flag.String("listen", ":8080", "proxy listener address")
	adminAddr := flag.String("admin", ":9090", "admin and metrics listener address")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	logger.Info("Aetheris starting up...")

	// add configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	// launch observability (Metrics & Health)
	observability.RegisterMetrics()

	// set up balancer pool
	b1 := &api.Backend{ID: "b1", Address: "http://localhost:8081"}
	b2 := &api.Backend{ID: "b2", Address: "http://localhost:8082"}

	balancers := map[string]api.LoadBalancer{
		"backend-cluster-1": balancer.NewRoundRobin(b1, b2),
		"backend-cluster-2": balancer.NewLeastConnections(b1),
	}

	// start the async event spooler
	spooler := event.NewSpooler(10000, 5, logger)
	spooler.Start()

	// start the router
	dynRouter, err := router.New(cfg.Routes, balancers, logger)
	if err != nil {
		logger.Error("router initialization failed", "err", err)
		os.Exit(1)
	}

	// set up the proxy handler and middleware chain
	rateLimiter := resilience.NewRateLimiter(cfg.RateLimit)
	proxyHandler := proxy.NewHandler(dynRouter, spooler, logger)

	chain := middleware.NewChain(
		middleware.Recover(logger),
		middleware.RequestID(),
		middleware.StructuredLogger(logger),
		middleware.Metrics(),
		middleware.RateLimiter(middleware.RateLimiterConfig{Limiter: rateLimiter}, logger),
	).Then(proxyHandler)

	// configure servers
	srv := &http.Server{
		Addr:         *listenAddr,
		Handler:      chain,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	adminMux := http.NewServeMux()
	adminMux.Handle("/metrics", observability.MetricsHandler())
	adminMux.HandleFunc("/healthz", observability.HealthzHandler)
	adminMux.HandleFunc("/readyz", observability.ReadyzHandler)

	adminSrv := &http.Server{
		Addr:    *adminAddr,
		Handler: adminMux,
	}

	// notify kubernetes that we're ready
	observability.MarkReady()

	// start the servers asyncly
	go func() {
		logger.Info("proxy server listening", "addr", *listenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("proxy server failed", "err", err)
		}
	}()

	go func() {
		logger.Info("admin server listening", "addr", *adminAddr)
		if err := adminSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("admin server failed", "err", err)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down servers gracefully...")
	observability.MarkNotReady()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	srv.Shutdown(ctx)
	adminSrv.Shutdown(ctx)
	spooler.Stop()

	logger.Info("Aetheris stopped successfully")
}
