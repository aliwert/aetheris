package observability

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

var readyFlag int32 // 0: Not Ready, 1: Ready

func MarkReady() {
	atomic.StoreInt32(&readyFlag, 1)
}

func MarkNotReady() {
	atomic.StoreInt32(&readyFlag, 0)
}

type healthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

// Liveness Probe: the app is alive (locked?)
func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(healthResponse{
		Status:    "ok",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// Readiness Probe: the app is ready to accept traffic
func ReadyzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if atomic.LoadInt32(&readyFlag) == 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(healthResponse{
			Status:    "not_ready",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(healthResponse{
		Status:    "ready",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}
