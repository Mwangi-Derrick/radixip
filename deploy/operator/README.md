# RadixIP Kubernetes Operator

The **RadixIP Operator** manages distributed rate limiting and policy enforcement across Kubernetes workloads. Built with [controller-runtime](https://github.com/kubernetes-sigs/controller-runtime), it reconciles `RateLimiterPolicy` Custom Resources, dynamically registers Lua rate-limiting scripts into distributed Redis clusters, and monitors sidecar container health across targeted deployments.

---

## 🎯 Architecture

```text
  ┌─────────────────────────────────────────────────────────────┐
  │                    Kubernetes API Server                    │
  └──────────────────────────────┬──────────────────────────────┘
                                 │ Watches RateLimiterPolicy & Pods
                                 ▼
                 ┌───────────────────────────────┐
                 │       RadixIP Operator        │
                 │   (controller-runtime loop)   │
                 └───────┬───────────────┬───────┘
   1. SCRIPT LOAD        │               │ 2. Status & Health
   (token/leaky/sliding) │               │    (ActiveSidecars, Phase)
                         ▼               ▼
                 ┌───────────────┐  ┌─────────────────────────┐
                 │     Redis     │  │ Target Pods (Deployment)│
                 │  Distributed  │  │ ┌─────────────────────┐ │
                 │    Cluster    │  │ │ radixip-rate-limiter│ │
                 └───────────────┘  │ │       sidecar       │ │
                                    │ └─────────────────────┘ │
                                    └─────────────────────────┘
```

### Reconciliation Workflow
1. **Resource Detection**: Watches `RateLimiterPolicy` (`rlp`) resources in the `radixip.io/v1alpha1` group.
2. **Lua Script Loading**: Reads the appropriate rate-limiting Lua script (`token_bucket.lua`, `leaky_bucket.lua`, or `sliding_window.lua`) from disk/ConfigMap and executes `SCRIPT LOAD` on the Redis cluster defined in `spec.redis.url`.
3. **Persist EVALSHA**: Stores the returned SHA1 hash in `spec.redis.scriptSHA` for downstream sidecar consumption.
4. **Target Pod Inventory**: Discovers pods associated with `spec.targetRef.name` via `app=<targetRef.name>` label matching.
5. **Sidecar Health & Phase Transitions**:
   - `Pending`: Waiting for Lua script load into Redis, or no target pods exist.
   - `Degraded`: Target pods exist but none have the `radixip-rate-limiter` sidecar container.
   - `Active`: All target pods have active sidecar containers.
   - `Failed`: Critical failure during policy configuration.

---

## 📦 Custom Resource: `RateLimiterPolicy`

```yaml
apiVersion: radixip.io/v1alpha1
kind: RateLimiterPolicy
metadata:
  name: api-rate-limiter
  namespace: default
spec:
  targetRef:
    kind: Deployment
    name: api-service
  algorithm: token_bucket   # token_bucket | leaky_bucket | sliding_window
  globalPolicy:
    capacity: 1000          # Maximum token capacity (burst ceiling)
    refillRate: 100         # Tokens refilled per second
  methodPolicies:           # Optional per-method gRPC overrides
    - method: /radixip.v1.RadixService/Lookup
      rule:
        capacity: 5000
        refillRate: 500
  redis:
    url: redis://redis-master:6379
    poolSize: 10
  failureMode: local_fallback # local_fallback | open | closed
  ttlSecs: 300
```

---

## 🚀 Getting Started

### Local Development & Testing

Run unit and controller reconciliation tests:
```bash
go test -v ./...
```

Run the operator locally pointing to your active kubeconfig:
```bash
go run main.go --leader-elect=false --lua-dir=../../deploy/lua
```

### Container Build

Build the multi-arch operator image:
```bash
docker build -t ghcr.io/mwangi-derrick/radixip-operator:0.1.0 .
```

### Deploy to Kubernetes

#### Option A: Helm Chart (Recommended)
Deploy the operator, CRDs, Redis, and sidecar configuration via Helm:
```bash
helm install radixip deploy/helm/radixip \
  --set sidecar.flavor=rust \
  --set sampleWorkload.enabled=true
```

#### Option B: Manifests
Apply the CRD, RBAC, operator deployment, and a sample workload with sidecar:
```bash
kubectl apply -f config/crd/bases/radixip.io_ratelimiterpolicies.yaml
kubectl apply -f config/rbac/rbac.yaml
kubectl apply -f config/manager/manager.yaml
kubectl apply -f config/samples/deployment_with_grpc_sidecar.yaml
kubectl apply -f config/samples/radixip_v1alpha1_ratelimiterpolicy.yaml
```

---

## 🧩 gRPC Sidecars & Language Agnosticism

RadixIP provides two high-performance gRPC sidecars:
- **Rust Sidecar** (`cmd/grpc-server-rust`): Listens on gRPC `0.0.0.0:50052`, Prometheus metrics `http://0.0.0.0:9091/metrics`. Ultra-low latency and minimal memory footprint.
- **Go Sidecar** (`cmd/grpc-server-go`): Listens on gRPC `0.0.0.0:50051`, Prometheus metrics `http://0.0.0.0:9090/metrics`.

Any application running in the same Pod (Node.js, Python, Java, etc.) communicates with the sidecar over `127.0.0.1:<port>` via standard gRPC protocols defined in `proto/radixip/v1/radixip.proto`. The operator watches target pods, verifies the `radixip-rate-limiter` container, and updates the `RateLimiterPolicy` phase to `Active`.

