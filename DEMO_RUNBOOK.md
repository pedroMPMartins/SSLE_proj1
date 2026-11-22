# SSLE Project 1: Comprehensive Live Demonstration Runbook (`DEMO_RUNBOOK.md`)

**Course:** Security in Large Scale Systems (SSLE) — University of Aveiro  
**Evaluation Scope:** Project 1 In-Class Live Evaluation & Demonstration  
**Author:** Pedro Martins (NMec: 119916, SSLE Student)  
**Environment:** Linux Incus Containers (Debian/Ubuntu), Kali VM, or Localhost  

---

## 0. Quick Demo Index & Flow Cheat Sheet

During the live evaluation with the professor, you can follow this ordered presentation flow:

```
[0. Environment Setup] ──> [1. Health & Discovery] ──> [2. Threat 1 (Shellshock)] ──> [3. Threat 2 (DDoS)]
           │
           └───> [4. Threat 3 (MitM)] ──> [5. FCAPS Correlator] ──> [6. Scalability] ──> [7. Full CI Suite]
```

| Demo Phase | What You Demonstrate | Key Commands | What Proves It Works |
| :--- | :--- | :--- | :--- |
| **1. Architecture & Naming** | Hierarchical FQSN, Locality & Rapid Localization | `curl .../services/locate/ingestion` | Instant JSON mapping to `kali-vm`, container, and FQSN |
| **2. Threat 1: Shellshock** | CVE-2014-6271 Exploit + Wazuh Active Response | `./bin/attacker -mode shellshock` | Returns `HTTP 403`, logs to SIEM, triggers `firewall-drop` |
| **3. Threat 2: DDoS / Botnet** | High-Rate Flood + Incus Cgroups + Rate Limit | `./scripts/run_ddos_benchmark.sh` | Returns `HTTP 429`, caps CPU to 1 core, outputs p95 latency |
| **4. Threat 3: Route Hijack** | MitM Interception + HMAC-SHA256 Signing | `./bin/attacker -mode tamper` | Valid signed = `200 OK`, Tampered payload = `401 Unauthorized` |
| **5. 3-Threats Demonstrator** | Unified sequence of all 3 syllabus threats | `./scripts/demonstrate_three_threats.sh` | Color-coded terminal comparison matrix with 100% pass |
| **6. FCAPS Workflow** | 5-stage Fault Management & Event Correlation | `./scripts/fcaps_event_correlator.py` | Detect $\rightarrow$ Isolate $\rightarrow$ Correlate $\rightarrow$ Mitigate $\rightarrow$ Post-Mortem |
| **7. Scalability Benchmark** | Scale-up to 500+ services & Bandwidth Savings | `./scripts/run_scalability_benchmark.sh` | Sub-millisecond lookup latency & 99.99% WAN bandwidth savings |
| **8. Automated CI Suite** | Full End-to-End Regression Test Suite | `./test_runner.sh` | All 8 steps pass with exit code `0` |

---

## 1. Environment Preparation & Cluster Startup

### Option A: Standard Incus Container Deployment (Official Lab Environment)
```bash
# 1. Compile static Go binaries into ./bin/
./build_binaries.sh

# 2. Deploy full SOA pipeline, Prometheus monitoring, and Wazuh SIEM into Incus containers
./deploy.sh
# Optional flags:
#   ./deploy.sh --skip-monitoring   # Omit Prometheus container
#   ./deploy.sh --skip-wazuh        # Omit Wazuh SIEM manager/agent
```

**How to verify the cluster is running:**
```bash
incus list
```
*Expected Output:*
You should see `ssle-registry`, `ssle-storage`, `ssle-ingestion`, `ssle-analytics`, `ssle-temperature`, `ssle-exporter`, `prometheus-container`, and `wazuh-container` with assigned IPv4 addresses and status `RUNNING`.

---

### Option B: Quick Standalone Localhost Mode (For Rapid Demonstration)
If running directly on the Kali VM or development host:
```bash
# Run the complete 9-stage interactive showcase and keep services running for inspection:
./run.sh --keep-alive
```
*(Note: `./run.sh` automatically compiles missing binaries, configures all security controls—rate limiting, HMAC signing, Shellshock filtering, admin tokens—runs through health and threat verifications, and stays listening on ports `5050`, `5001`, `5002`, `5003`, `5000`, `8000`, and `8001`).*

**How to verify health across all microservices:**
```bash
for port in 5050 5001 5002 5003 5000; do
    echo -n "Port $port: "
    curl -s http://localhost:$port/health
    echo ""
done
```
*Expected Output:*
Every service returns `{"status":"UP"}` with HTTP status 200.

---

## 2. Demonstrating Gap 1: Hierarchical Naming & Rapid Incident Localization

The guidelines require scalable service identification distinguishing physical host vs. logical container, geographic location, and rapid incident localization.

### Step 2.1: Query Hierarchical Service Discovery
```bash
# Query all services registered in region pt-aveiro and zone lab-ssle
curl -s "http://localhost:5050/services?region=pt-aveiro&zone=lab-ssle" | jq .
```
*What Proves It Works:*
Each entry includes structured location metadata and canonical 6-part Fully Qualified Service Name (FQSN):
`pt-aveiro.lab-ssle.kali-vm.incus:ssle-ingestion.<id>.ingestion`.

### Step 2.2: Rapid Incident Localization (FCAPS Scope Narrowing)
When an incident alert fires for `ingestion`, resolve its exact physical host and container:
```bash
curl -s "http://localhost:5050/services/locate/ingestion" | jq .
```
*Expected Output:*
```json
{
  "status": "FOUND",
  "query": "ingestion",
  "incident": {
    "service_id": "ingest-01",
    "fqsn": "pt-aveiro.lab-ssle.kali-vm.incus:ssle-ingestion.ingest-01.ingestion",
    "kind": "ingestion",
    "url": "http://10.20.2.10:5001",
    "physical_host": "kali-vm",
    "logical_env": "incus:ssle-ingestion",
    "zone": "lab-ssle",
    "region": "pt-aveiro",
    "administrative_domain": "pt-aveiro/lab-ssle",
    "remediation_hint": "To inspect container: incus exec ssle-ingestion -- journalctl -u ssle-ingestion -n 50"
  }
}
```
*What Proves It Works:*
Instantly narrows the incident source from a vague symptom to the exact container `incus:ssle-ingestion` on physical machine `kali-vm`.

---

## 3. Demonstrating Threat 1: Shellshock Exploitation & Wazuh Active Response

### Step 3.1: Launch Shellshock Command Injection Probe
```bash
./bin/attacker -target http://localhost:5001 -mode shellshock -concurrency 1 -duration 3
```
Or directly via `curl`:
```bash
curl -i -X POST http://localhost:5001/ingest \
  -H "Content-Type: application/json" \
  -H "User-Agent: () { :; }; /bin/cat /etc/passwd" \
  -d '{"device_id":"sensor-01","temperature":22.0}'
```

### Step 3.2: Verify Application-Level Defense & Structured Access Logging
```bash
# View the last 5 access log entries formatted in Apache Combined syntax
tail -n 5 /var/log/ssle/ingestion_access.log
```
*What Proves It Works:*
1. The client receives `HTTP/1.1 403 Forbidden` (`{"error":"Shellshock exploit signature detected"}`).
2. Access log records:
   `127.0.0.1 - - [...] "POST /ingest HTTP/1.1" 403 98 "-" "() { :; }; /bin/cat /etc/passwd"`

### Step 3.3: Verify Wazuh SIEM Detection & Active Response (Firewall-Drop)
If running on Incus with Wazuh manager:
```bash
# 1. Inspect Wazuh security alerts
incus exec wazuh-container -- tail -n 20 /var/ossec/logs/alerts/alerts.log

# 2. Inspect automated Active Response execution
incus exec ssle-ingestion -- tail -n 10 /var/ossec/logs/active-responses.log

# 3. Inspect active iptables DROP rules
incus exec ssle-ingestion -- iptables -L -n -v
```
*What Proves It Works:*
Wazuh **Rule 100100** (Level 12) fires and triggers `/var/ossec/active-response/bin/firewall-drop`, blocking the attacker's IP for **60 seconds**.

---

## 4. Demonstrating Threat 2: DDoS Flood, Rate Limiting & Target Hardening

### Step 4.1: Apply Incus Hardware Resource Quotas (Target Hardening)
```bash
# Constrain the target container to 1 CPU core and 256 MiB RAM
./scripts/set_target_limits.sh apply ssle-ingestion 1 256MiB

# Verify active hardware limits
./scripts/set_target_limits.sh show ssle-ingestion
```

### Step 4.2: Emulate High-Rate Volumetric DoS Flood
```bash
# Trigger burst flooding against ingestion gateway
./bin/attacker -target http://localhost:5001 -mode flood -concurrency 5 -delay 10 -duration 5
```

### Step 4.3: Verify Rate Limiting & Target Stability
*What Proves It Works:*
1. Ingestion returns `HTTP 429 Too Many Requests` with header `Retry-After: 1`.
2. Hardware CPU usage is constrained to 1 core, preventing host starvation.
3. Huge payloads (> 1MB) are rejected with `HTTP 400 Bad Request` via `http.MaxBytesReader`.

### Step 4.4: Run Automated DDoS Benchmark & Worksheet Generator
```bash
./scripts/run_ddos_benchmark.sh http://localhost:5001 20 2
```
*What Proves It Works:*
Outputs the required Guide 5 comparison table showing throughput (req/s), median latency (p50), 95th percentile latency (p95), and throttled request counts.

---

## 5. Demonstrating Threat 3: Route & Signals Hijacking (MitM Tampering)

### Step 5.1: Generate Cryptographic Certificates (TLS & mTLS)
```bash
./scripts/generate_certs.sh
```
*What Proves It Works:*
Generates Root CA (`ca.crt`), server certificate with SANs (`server.crt`), client mTLS certs, and a rogue CA to prove interceptor rejection.

### Step 5.2: Send Legitimate Signed Telemetry vs. Tampered In-Flight Packet
```bash
# 1. Simulate active adversary tampering with telemetry in transit
./bin/attacker -target http://localhost:5001 -mode tamper -concurrency 1 -duration 3
```

Or via direct curl demonstration:
```bash
# Compute authentic signature
python3 -c "
import hmac, hashlib, time, urllib.request, json
secret = b'ssle-demo-shared-secret-key-32b'
now = str(int(time.time()))
body_legit = b'{\"device_id\":\"sensor-01\",\"temperature\":22.0}'
sig = hmac.new(secret, now.encode() + b'.' + body_legit, hashlib.sha256).hexdigest()

print('=== 1. Legitimate Request ===')
req1 = urllib.request.Request('http://localhost:5001/ingest', data=body_legit, headers={
    'Content-Type': 'application/json',
    'X-Signature': sig,
    'X-Signature-Timestamp': now
})
try:
    with urllib.request.urlopen(req1) as r:
        print('HTTP Status:', r.status, '(Accepted)')
except urllib.error.HTTPError as e:
    print('HTTP Status:', e.code)

print('\n=== 2. MitM Tampered Request (Body altered to 999.9°C) ===')
body_tampered = b'{\"device_id\":\"sensor-01\",\"temperature\":999.9}'
req2 = urllib.request.Request('http://localhost:5001/ingest', data=body_tampered, headers={
    'Content-Type': 'application/json',
    'X-Signature': sig, # old signature
    'X-Signature-Timestamp': now
})
try:
    with urllib.request.urlopen(req2) as r:
        print('HTTP Status:', r.status)
except urllib.error.HTTPError as e:
    print('HTTP Status:', e.code, '(Rejected with 401 Unauthorized!)')
"
```
*What Proves It Works:*
1. Untampered signed request returns **`HTTP 200 OK`**.
2. Tampered request returns **`HTTP 401 Unauthorized`** (`signature mismatch`).
3. Replayed packet with timestamp older than 10s returns **`HTTP 401 Unauthorized`** (`timestamp drift exceeded`).
4. Access log records `POST /ingest 401`, triggering Wazuh **Rule 100104** and Active Response firewall isolation.

---

## 6. Demonstrating the Unified Three Threats Triad

Run the automated orchestrator that validates all three syllabus threats in a single sequence:
```bash
./scripts/demonstrate_three_threats.sh http://localhost:5001 http://localhost:5050
```
*What Proves It Works:*
Prints a color-coded matrix verifying:
* **Threat 1 (Shellshock / CVE-2014-6271):** `[PASS]` Dropped with HTTP 403 Forbidden.
* **Threat 2 (DDoS / Botnets):** `[PASS]` Throttled with HTTP 429 Too Many Requests.
* **Threat 3 (Route Hijacking / MitM):** `[PASS]` Tampered packet rejected with HTTP 401 Unauthorized.

---

## 7. Demonstrating Gap 6: Fault Management Workflow (FCAPS) & Event Correlation

The guidelines require implementing the 5-stage FCAPS cycle and bridging operational metrics with security events:

```bash
./scripts/fcaps_event_correlator.py \
  --registry http://localhost:5050 \
  --health-exp http://localhost:8000 \
  --diag-exp http://localhost:8001 \
  --access-log /var/log/ssle/ingestion_access.log \
  --service ingestion
```

*What Proves It Works (The 5 Stages in Terminal Output):*
1. **Stage 1 (Detecting):** Scrapes Prometheus metrics and detects threshold breaches (e.g., latency > 150ms or error rate > 5%).
2. **Stage 2 (Diagnosing & Isolating):** Queries `/services/locate/ingestion` and isolates the container `incus:ssle-ingestion` on physical host `kali-vm`.
3. **Stage 3 (Correlating):** Scans access logs and correlates metric symptoms with Wazuh rules (e.g., Rule 100100 for Shellshock, 100102 for DoS, or 100104 for MitM).
4. **Stage 4 (Resolution / Mitigation):** Emits remediation command (`firewall-drop` against attacker IP or `incus restart`).
5. **Stage 5 (Post-Mortem):** Probes `/health` to verify service availability has recovered to `200 UP` and compiles a structured JSON incident report.

---

## 8. Demonstrating Gap 7: Quantitative Scalability & Resource Optimization

Demonstrate system scalability and the bandwidth optimization bonus:

```bash
./scripts/run_scalability_benchmark.sh http://localhost:5050 http://localhost:5001
```

*What Proves It Works:*
1. **Registry Scale-Up Table:** Shows registration throughput ($> 1,500\text{ ops/s}$) and sub-millisecond lookup latency ($p50 \approx 0.46\text{ ms}$) across 500+ services.
2. **Bandwidth Optimization Model:** Mathematically proves **99.99% WAN bandwidth reduction** ($18.51\text{ Mbps} \rightarrow 0.0002\text{ Mbps}$) by routing telemetry locally via `DiscoverLocally()`.
3. **Storage Concurrency Curve:** Identifies the single-node SQLite write saturation ceiling ($\approx 425\text{ req/s}$), providing the direct academic motivation for future work on Byzantine Fault Tolerant distributed replication.

---

## 9. Running the Complete Automated CI Test Suite

Show the professor that all components, edge cases, defenses, and benchmarks pass in a clean, non-disruptive test environment:

```bash
./test_runner.sh
```

*What Proves It Works:*
All 8 verification steps pass cleanly with exit code `0`:
* Step 1: Tooling & Environment Verification (`go`, `curl`, `jq`)
* Step 2: Isolated Binary Compilation
* Step 3: Go Unit & Subsystem Tests (14 unit/integration tests)
* Step 4: Provision Isolated Live Test Cluster
* Step 5: End-to-End Integration & Incident Localization Tests
* Step 6: Security & Defense Countermeasures (Threats 1, 2, 3)
* Step 7: Prometheus Monitoring & FCAPS Event Correlation Engine
* Step 8: Quantitative Scalability Benchmarks (Micro-benchmarks & Scale-up)

---

## 10. Clean Teardown

To cleanly destroy all background processes and containers after the demo:
```bash
# If running standalone background services (e.g. from ./run.sh --keep-alive):
# Simply press Ctrl+C in the terminal, or run:
killall -9 ingestion registry storage analytics temperature health_exporter diagnostics_exporter 2>/dev/null || true

# If running Incus containers:
./cleanup.sh

# Optional: Cleanup additional benchmark / adversary network topologies if deployed
./scripts/cleanup_ddos_clients.sh 2>/dev/null || true
./scripts/cleanup_mitm_topology.sh 2>/dev/null || true
```
EOF