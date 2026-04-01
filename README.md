# 🌩️ Aetheris

![Go](https://img.shields.io/badge/Go-00ADD8?style=for-the-badge&logo=go&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=for-the-badge&logo=docker)
![Kubernetes](https://img.shields.io/badge/Kubernetes-Native-326CE5?style=for-the-badge&logo=kubernetes)
![License](https://img.shields.io/badge/License-MIT-green?style=for-the-badge)

**A high-performance, cloud-native edge proxy and asynchronous event router written in Go.**

Aetheris is designed to solve the "resource bloat" and cold-start latency issues prevalent in traditional JVM-based API gateways. Built purely on Go's standard library and advanced concurrency patterns, it routes traffic, limits rates, and spools high-throughput asynchronous events using a fraction of the memory (typically <20MB RAM) and CPU footprint.

---

## Key Features

- **Zero-Downtime Hot Reloads:** Update routing tables and backend configurations instantly via `sync.RWMutex` without dropping a single in-flight request.
- **Lock-Free Load Balancing:** Utilizes `sync/atomic` for high-throughput, mutex-free routing. Supports **Round Robin** and **Least Connections** strategies.
- **Resilience & Fault Tolerance:**
  - **Circuit Breaker:** FSM-based (Closed/Open/Half-Open) protection against cascading failures.
  - **Rate Limiter:** Lock-free Token Bucket algorithm for per-key (e.g., IP) traffic shaping.
  - **Retry Mechanism:** Exponential backoff with full jitter to prevent thundering herds.
- **Async Event Spooler:** Safely absorbs massive webhook/event spikes (HTTP 202 Accepted) and processes them asynchronously via a dedicated worker pool, decoupling client latency from backend processing.
- **Built-in Observability:** Native Prometheus metrics (`/metrics`), structured JSON logging (`log/slog`), and distributed tracing (`X-Request-ID` propagation).
- **Kubernetes Native:** Ships with out-of-the-box liveness/readiness probes (`/healthz`, `/readyz`) and a multi-stage, distroless Dockerfile.

---

## Architecture

Aetheris strictly adheres to the Standard Go Project Layout, ensuring clean separation of concerns and a secure, non-importable core.

```text
aetheris/
├── cmd/aetheris/        # Application entrypoint & wiring
├── internal/            # Private core business logic
│   ├── balancer/        # Routing algorithms (LeastConn, RoundRobin)
│   ├── router/          # Dynamic routing & rule matching
│   ├── resilience/      # Circuit Breaker, Rate Limiter, Retry
│   ├── event/           # Async spooling and worker pools
│   └── middleware/      # HTTP chain (Recover, Trace, Log, Metrics)
├── pkg/aetherisapi/     # Public, stable interfaces
├── configs/             # YAML configuration files
└── deployments/         # Dockerfiles and K8s manifests
```

## Getting Started

### Prerequisites

- [Go 1.23+](https://go.dev/doc/install)

### Installation

1. Clone the repository:

   ```bash
   git clone https://github.com/aliwert/aetheris.git
   cd aetheris
   ```

2. Download dependencies:

   ```bash
   go mod tidy
   ```

3. Run the server using the provided example configuration:
   ```bash
   go run cmd/aetheris/main.go -config configs/aetheris.yaml
   ```

By default, the proxy listens on `:8080` and the admin/metrics server listens on `:9090`.

---

## Configuration

Aetheris is entirely driven by a declarative configuration file (`aetheris.yaml` or `aetheris.json`).

```yaml
server:
listen: ":8080"
admin: ":9090"

rate_limit:
enabled: true
default_rate: 1000 # tokens per second
default_burst: 2000

upstreams:
  - id: "user-service"
    load_balancer: "least_connections"
    backends:
      - id: "user-1"
        address: "http://localhost:8081"
        circuit_breaker:
        max_failures: 5
        open_timeout: 30s

routes:
  - id: "users-api"
    path_prefix: "/api/v1/users"
    methods: ["GET", "POST"]
    upstream_id: "user-service"

  - id: "webhook-ingest"
    path_prefix: "/webhook"
    upstream_id: "event-spooler" # Routes directly to async workers
```

---

## Docker & Kubernetes Deployment

Aetheris includes a highly optimized, multi-stage `Dockerfile` utilizing Google's Distroless base image for maximum security and minimal size.

**Build the image:**

```bash
docker build -t aetheris:latest -f deployments/docker/Dockerfile .
```

**Deploy to Kubernetes:**
The repository includes standard manifests for a ConfigMap, Deployment, and Service.

```bash
kubectl apply -f deployments/kubernetes/configmap.yaml
kubectl apply -f deployments/kubernetes/deployment.yaml
```

---

## Observability

Aetheris exposes a dedicated admin port (default `:9090`) to keep telemetry traffic separate from public proxy traffic.

- **Prometheus Metrics:** `GET /metrics`
- **Liveness Probe:** `GET /healthz`
- **Readiness Probe:** `GET /readyz`

---

## Contributing

Contributions are welcome! If you find a bug or have a feature request, please open an issue. For major changes, please open an issue first to discuss what you would like to change.

1. Fork the Project
2. Create your Feature Branch (`git checkout -b feature/AmazingFeature`)
3. Commit your Changes (`git commit -m 'feat: add some AmazingFeature'`)
4. Push to the Branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request
