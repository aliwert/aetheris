package observability

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

// readyFlag is an atomic bool (0 = not ready, 1 = ready)
// It starts at 0 (not ready) and is flipped to 1 by MarkReady()
// once all subsystems have initialised successfully
var readyFlag int32

// signals that the proxy has completed initialisation and is
// ready to receive production traffic. call this from main() after the
// router, balancers, and spooler have been successfully started
func MarkReady() {
	atomic.StoreInt32(&readyFlag, 1)
}

// signals that the proxy is temporarily unavailable, for
// example during a configuration reload that detects a critical error.
// Kubernetes will stop routing traffic to the pod until MarkReady is
// called again
func MarkNotReady() {
	atomic.StoreInt32(&readyFlag, 0)
}

// JSON body returned by both health endpoints
type healthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// implements the Kubernetes liveness probe.
//
// Liveness asks: "Is this process fundamentally alive and not stuck?"
// It should only return 5xx if the process is in a state from which it
// cannot recover without a restart
//
// for Aetheris this is always 200 OK as long as the goroutine
// scheduler is running and this handler can be invoked, a hanging
// handler would itself cause Kubernetes to kill the pod
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)

	if r.Method == http.MethodHead {
		return
	}

	json.NewEncoder(w).Encode(healthResponse{ //nolint:errcheck
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// implements the Kubernetes readiness probe
//
// Readiness asks: "Is this instance ready to receive traffic right now?"
// It returns 503 until MarkReady() has been called, and returns 503
// again if MarkNotReady() is called
//
// this prevents Kubernetes from routing reqs. to a pod that is still
// warming up its connection pool, loading configuration, or is in a
// degraded state
func ReadyzHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	if atomic.LoadInt32(&readyFlag) == 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		if r.Method != http.MethodHead {
			json.NewEncoder(w).Encode(healthResponse{ //nolint:errcheck
				Status:    "not_ready",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			})
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		json.NewEncoder(w).Encode(healthResponse{ //nolint:errcheck
			Status:    "ready",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
	}
}
