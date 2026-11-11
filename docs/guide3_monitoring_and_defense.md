# Practical Guide 3: Attack-Defense Trees and Monitoring
**Course:** Large Scale Systems Security (SSLE)  

---

## 1. Executive Summary & Objective

This document provides the complete theoretical foundation, architectural specifications, practical implementation, and experimental analysis required for **Practical Guide 3**.

The core objectives of Guide 3 are:
1. **Attack-Defense Modeling:** Formulate and maintain an Attack-Defense Tree (ADTree) for our Service-Oriented Architecture (SOA).
2. **Machine & Attacker Setup:** Implement an adversary service (`cmd/attacker`) capable of mounting DoS floods, telemetry data poisoning, and malicious registry deregistration.
3. **Problem Detection:** Deploy Prometheus and develop a custom Go Service Health Exporter exposing `service_health` (1=UP, 0=DOWN) and latency metrics.
4. **Pushgateway Challenge:** Implement a Pushgateway metric publisher and provide a comprehensive architectural analysis of why pull-based exporters are superior for microservice monitoring.
5. **Problem Diagnosis:** Develop a Diagnostics Exporter tracking container/process CPU utilization, memory footprint, request throughput, and error rates.
6. **Problem Identification:** Isolate attacker traces using Prometheus observability and system audit logs.
7. **Problem Remediation & Prevention:** Design, implement, and evaluate defensive controls (Rate Limiting, Plausibility Bounds Checking, Blocklisting, and Admin Tokens), backed by a rigorous Pros & Cons trade-off analysis.

---

## 2. Attack-Defense Tree (ADTree) Alignment

The complete threat model and Mermaid diagram are maintained in [`docs/attack_defense_tree.md`](./attack_defense_tree.md) and [`docs/attack_defense_tree.png`](./attack_defense_tree.png).

### Target System Components
* **Service Registry (`:5050`)**: Dynamic service directory (`/register`, `/services`).
* **Temperature Sensor (`:5000`)**: Edge producer emitting readings and exposing `/health`.
* **Ingestion Service (`:5001`)**: Gateway receiving sensor readings and routing to storage.
* **Storage Service (`:5002`)**: Persistent SQLite storage abstraction.
* **Analytics Service (`:5003`)**: Aggregates readings to calculate temperature statistics (`/average`).

### Primary Attack Vectors Modeled
* **A1: Denial of Service (DoS / Flood):** High-frequency HTTP floods against `/ingest` or `/register` to exhaust CPU, memory, and database connections.
* **A2: Data Poisoning & Sensor Spoofing:** Injecting forged, extreme temperature readings (e.g., `999.0°C` or `-273.15°C`) to distort analytics.
* **A3: Service Registry Poisoning & Hijacking:** Deleting active service registrations via `DELETE /register/{id}` to blind the topology.
* **A4: Reconnaissance & Topology Disclosure:** Harvesting active service IP addresses and ports via `GET /services`.

---

## 3. Machine Setup & Attacker Implementation

### 3.1 Attacker Service (`cmd/attacker/main.go`)
An adversary microservice has been implemented in Go to execute controlled attacks against the SOA cluster:

```bash
# Build static attacker binary
cd incremental_system
go build -o bin/attacker ./cmd/attacker
```

#### Attack Modes Supported:
1. **`flood` (Denial of Service):** Launches concurrent worker goroutines firing high-frequency `POST /ingest` requests with 1KB padding bodies to induce CPU starvation, socket exhaustion, and request queue delays.
2. **`poison` (Data Poisoning):** Transmits physically impossible temperature readings (`999.99°C`, `-273.15°C`, `1250.0°C`) from arbitrary forged device IDs to corrupt `/average` calculations.
3. **`deregister` (Registry Hijacking):** Periodically polls `GET /services` and issues unauthenticated `DELETE /register/{id}` requests against every active service.
4. **`all` (Compound Attack):** Dynamically alternates across flood, poison, and deregister vectors to simulate an advanced persistent threat.

#### Running the Attacker:
```bash
# Run DoS flood against ingestion:
./bin/attacker -target http://localhost:5001 -mode flood -concurrency 10 -delay 0

# Run Data Poisoning against ingestion:
./bin/attacker -target http://localhost:5001 -mode poison -concurrency 2 -delay 500

# Run Malicious Deregistration against registry:
./bin/attacker -registry http://localhost:5050 -mode deregister -delay 1000
```

---

## 4. Problem Detection (Prometheus & Health Exporter)

### 4.1 Deploying Prometheus in Incus
Following Guide 3 instructions, Prometheus is deployed in a dedicated Debian Trixie container:
```bash
# Automated deployment script:
./monitoring/deploy_monitoring.sh
```

Manual commands executed inside the VM:
```bash
incus launch images:debian/trixie prometheus-container
incus exec prometheus-container -- apt-get update
incus exec prometheus-container -- apt-get install -y prometheus
```

### 4.2 Prometheus Scrape Configuration (`monitoring/prometheus.yml`)
Configured to scrape Prometheus self-metrics, the Service Health Exporter (`:8000`), and the Diagnostics Exporter (`:8001`):
```yaml
global:
  scrape_interval: 5s
  evaluation_interval: 5s

scrape_configs:
  - job_name: 'service_health_exporter'
    metrics_path: '/metrics'
    scrape_interval: 3s
    static_configs:
      - targets: ['10.0.2.2:8000', '127.0.0.1:8000']
        labels:
          tier: 'soa-liveness'

  - job_name: 'service_diagnostics_exporter'
    metrics_path: '/metrics'
    scrape_interval: 5s
    static_configs:
      - targets: ['10.0.2.2:8001', '127.0.0.1:8001']
        labels:
          tier: 'soa-diagnostics'
```

### 4.3 Service Health Exporter (`cmd/health_exporter/main.go`)
Implements the exporter logic specified in Guide 3 Section 4 as a standalone, high-performance Go microservice:
* Dynamically queries the registry (`GET /services`) to discover active service URLs.
* Probes each service's `/health` endpoint every 3 seconds.
* Exposes the following Prometheus metrics on port `8000`:
  * `service_health{service="...", endpoint="..."}`: `1.0` if healthy (`200 OK`), `0.0` if unresponsive or failing.
  * `service_response_time_seconds{service="...", endpoint="..."}`: Measured round-trip latency in seconds.

---

## 5. Architectural Challenge: Pushgateway vs. Pull Exporters

> **Guide 3 Challenge:** *"Can you think of why we should use an exporter instead of the pushgateway? In theory, it makes sense. However, to see for yourself, create a reliable method that publishes metrics via the pushgateway instead of using an exporter."*

### 5.1 Pushgateway Implementation (`cmd/pushgateway_publisher/main.go`)
We implemented a dedicated Go publisher that runs as a background loop, querying microservice endpoints and publishing `pushed_service_health` and `pushed_service_latency_seconds` directly to `http://localhost:9091/metrics/job/ssle_push_monitor`.

### 5.2 Why the Pull Model (Exporters) is Superior for SOA Monitoring

| Architectural Dimension | Pull Model (Exporters) | Push Model (Pushgateway) |
| :--- | :--- | :--- |
| **Instance Liveness (`up` metric)** | **Automatic & Immediate:** If a service crashes or hangs, Prometheus marks `up == 0` on the very next scrape interval. | **Masked Failures (Stale Metrics):** Pushgateway retains the *last pushed metric indefinitely* in memory. If an instance dies, Pushgateway continues reporting `pushed_service_health = 1` until manually wiped! |
| **Centralization & Bottlenecks** | **Decentralized:** Each service exposes its own `/metrics` port. Prometheus scrapes them directly or via reverse proxies. | **Single Point of Failure:** Pushgateway becomes a central bottleneck and single point of failure. If Pushgateway drops, all telemetry is lost. |
| **Backpressure Management** | **Load is Controlled by Scraper:** Prometheus controls its scrape rate (`scrape_interval`). Targets do not get overwhelmed by outgoing connections. | **Thundering Herd:** Multiple instances pushing concurrently during flash crowds can cause network congestion and socket exhaustion. |
| **Topology Discovery** | **Native Integration:** Prometheus integrates natively with DNS, Incus, Kubernetes, and Consul service discovery. | **Complex State Management:** Pushgateway requires complex instance label management and manual tombstoning of retired nodes. |

### Conclusion on Pushgateway
Pushgateway is **not** an aggregator or event collector for long-running services. It is designed exclusively for **ephemeral, short-lived batch jobs** (e.g., a 5-second backup script or cron task) that terminate before Prometheus has an opportunity to scrape them. For persistent microservices (Registry, Ingestion, Storage, Analytics, Temperature), the **Pull Exporter pattern is strictly recommended**.

---

## 6. Problem Diagnosis & Identification (Sections 5 & 6)

### 6.1 Diagnostic Exporter (`cmd/diagnostics_exporter/main.go`)
To diagnose quality of service degradation under attack, the diagnostics exporter exposes:
* `service_container_cpu_usage_percent`: Real-time container/process CPU load.
* `service_container_memory_usage_bytes`: Resident set size (RSS) memory consumption.
* `service_requests_per_second`: Observed request throughput.
* `service_error_rate_percent`: Percentage of client requests returning `4xx` or `5xx`.

### 6.2 Diagnosing Active Attacks

#### 1. Diagnosing DoS Floods (Attack Vector A1):
* **Symptom in Prometheus:** `service_response_time_seconds` spikes from `~5ms` to `>1.5s` for `ingestion`.
* **Correlated Diagnostics:** `service_container_cpu_usage_percent` jumps to 90-100%, and `service_requests_per_second` surges.
* **Identification:** Ingestion server logs show thousands of requests originating from IP `10.0.2.x` or header payload `attacker-bot-01`.

#### 2. Diagnosing Data Poisoning (Attack Vector A2):
* **Symptom in Analytics:** `GET /average` suddenly leaps from `~25.0°C` to `>300.0°C`.
* **Correlated Diagnostics:** Ingestion throughput shows anomalous bursts of readings from randomized IDs (`rogue-sensor-XX`).
* **Identification:** Inspecting `temp_data.db` directly:
  ```bash
  sqlite3 temp_data.db "SELECT * FROM temperatures WHERE temperature > 100 OR temperature < -50;"
  ```
  Reveals entries with `temperature = 999.99` and `1250.0`.

#### 3. Diagnosing Malicious Deregistration (Attack Vector A3):
* **Symptom in Prometheus:** `service_health` for storage or temperature drops from `1.0` to `0.0`.
* **Identification:** Registry logs reveal `DELETE /register/<id>` requests originating from unverified client sockets without proper authorization tokens.

---

## 7. Problem Remediation & Prevention (Section 7)

### 7.1 Defensive Controls Implemented in the Codebase

All defensive controls have been implemented natively in the Go microservices and can be toggled via environment variables:

1. **Token-Bucket Rate Limiting (`cmd/ingestion/main.go`):**
   * Configured via `ENABLE_RATE_LIMIT=true` and `RATE_LIMIT_RPS=20.0`.
   * Enforces a per-client token bucket. When the rate is exceeded, responds with `HTTP 429 Too Many Requests` and `Retry-After: 1`.
2. **Plausibility Bounds Validation (`cmd/ingestion/main.go`):**
   * Configured via `ENABLE_INPUT_VALIDATION=true`, `MIN_TEMP=-50.0`, `MAX_TEMP=100.0`.
   * Checks ingested readings. Non-physical readings (e.g. `999.99°C`) are immediately rejected with `HTTP 422 Unprocessable Entity` before reaching storage or analytics.
3. **Blocklisting / Deny List Filtering (`cmd/ingestion/main.go`):**
   * Configured via `BLOCKED_DEVICES="attacker-bot-01,rogue-sensor-1"`.
   * Identified malicious device IDs are blocked at ingress with `HTTP 403 Forbidden`.
4. **Max Payload Capping (`cmd/ingestion/main.go`):**
   * Restricts request bodies to 1MB (`http.MaxBytesReader`) to mitigate memory-filling payload attacks.
5. **Protected Service Deregistration (`cmd/registry/main.go`):**
   * Configured via `ADMIN_TOKEN="your-secure-token"`.
   * Requires `X-Admin-Token` or `Authorization: Bearer <ADMIN_TOKEN>` to delete services. Unauthenticated unregistration attempts are rejected with `HTTP 403 Forbidden`.

---

### 7.2 Trade-Off Analysis: Pros & Cons of Countermeasures

As mandated by Section 7 of Guide 3, responding to attacks requires balancing security posture against operational complexity and potential side effects:

| Countermeasure | Pros | Cons & Risks | Recommended Context |
| :--- | :--- | :--- | :--- |
| **Blocklisting (IP / Device ID)** | • Instant mitigation for known rogue actors.<br>• Extremely low compute overhead. | • Easily bypassed via IP spoofing or rotating device IDs.<br>• High risk of collateral damage if dynamic DHCP/NAT IPs are blocked. | Use as an emergency reactive measure, but never as the sole defense. |
| **Rate Limiting (Token Bucket)** | • Protects downstream database (SQLite) from concurrency starvation.<br>• Guarantees fair resource allocation among clients.<br>• Maintains service availability under moderate volumetric floods. | • Legitimate burst traffic (e.g., reconnecting sensors after an outage) may be throttled.<br>• Requires state storage (memory) per client IP. | **Mandatory** at the API Gateway / Ingestion boundary. |
| **Input Validation (Bounds Checking)** | • Completely neutralizes telemetry data poisoning.<br>• Zero external dependencies.<br>• Protects analytical accuracy and business integrity. | • Needs careful domain definition (thresholds too tight can drop genuine extreme weather events). | **Mandatory** for all data-ingesting endpoints. |
| **Admin Tokens / API Keys** | • Prevents unauthorized service deletion and registry tampering.<br>• Enforces clear separation of privilege. | • Requires secure key distribution and rotation mechanisms.<br>• Increases configuration burden for edge clients. | **Mandatory** for administrative endpoints (`DELETE`, `PUT`). |

---

## 8. Verification & Demonstration Commands

To demonstrate the entire monitoring and remediation lifecycle:

### Step 1: Run Baseline System with Defenses Enabled
```bash
cd incremental_system

# Launch services with defensive remediation enabled:
ENABLE_RATE_LIMIT=true \
RATE_LIMIT_RPS=10 \
ENABLE_INPUT_VALIDATION=true \
MIN_TEMP=-50 \
MAX_TEMP=100 \
BLOCKED_DEVICES=attacker-bot-01 \
ADMIN_TOKEN=ssle-secret-token \
bash run.sh
```

### Step 2: Start Exporters (Go Binaries)
```bash
# In separate terminal or background:
./bin/health_exporter &
./bin/diagnostics_exporter &
```

### Step 3: Launch Attacker to Validate Defense Enforcement
```bash
# Test 1: High-rate flood -> Observe HTTP 429 rate limiting
./bin/attacker -target http://localhost:5001 -mode flood -concurrency 5 -delay 5

# Test 2: Data poisoning -> Observe HTTP 422 rejected readings (average stays clean at ~25°C)
./bin/attacker -target http://localhost:5001 -mode poison -concurrency 2 -delay 100

# Test 3: Unauthorized deregistration -> Observe HTTP 403 Forbidden (services remain registered)
./bin/attacker -registry http://localhost:5050 -mode deregister -delay 500
```

### Step 4: Verify Metrics in Prometheus
Navigate to `http://<PROMETHEUS_IP>:9090`:
* Query `service_health`: All legitimate services report `1.0`.
* Query `service_response_time_seconds`: Latencies remain healthy (< 20ms).
* Query `service_error_rate_percent`: Shows attacker requests being blocked without impacting genuine sensor traffic.
