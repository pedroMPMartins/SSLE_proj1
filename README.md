# System developed for SSLE Course


## Send code to the VM:
```bash
rsync -avz --delete ./ kali-vm:~/Desktop/ssle/
```


## Incus

Go into a container:
```bash
incus exec <container_name> -- sh
# OR
incus shell <container_name>
```

To exit the container just write `exit`.

Inspect a profile:
```bash
incus profile show <profile_name>
```

### Incus Deployment & Teardown

Deploy the full microservice pipeline into isolated Incus containers:
```bash
./deploy_incus.sh
```

Inspect running containers and IP addresses:
```bash
incus list
```

Teardown and purge containers and shared volumes:
```bash
./cleanup_incus.sh
```

*(Note: Legacy experimental K3s manifests and Docker build scripts have been archived in [`archive/k8s_legacy/`](archive/k8s_legacy/)).*




## Guide 3: Attack-Defense & Monitoring (Prometheus)

Comprehensive guide details, threat model mapping, and trade-off analysis can be found in [`docs/guide3_monitoring_and_defense.md`](docs/guide3_monitoring_and_defense.md).

### 1. Build Binaries
```bash
./build_binaries.sh
```

### 2. Deploy Prometheus Container (Incus)
```bash
./monitoring/deploy_monitoring.sh
```
Prometheus Web UI will be accessible at `http://<CONTAINER_IP>:9090` (check `incus list prometheus-container`).

### 3. Run Exporters (Go Binaries)
```bash
# Service Health Exporter (port 8000)
./bin/health_exporter &

# Diagnostics Exporter (port 8001)
./bin/diagnostics_exporter &

# Optional: Pushgateway Publisher (Challenge)
./bin/pushgateway_publisher &
```

### 4. Run Attacker
```bash
# Denial of Service (Flood)
./bin/attacker -target http://localhost:5001 -mode flood -concurrency 5 -delay 10

# Data Poisoning
./bin/attacker -target http://localhost:5001 -mode poison -concurrency 2 -delay 500

# Rogue Deregistration
./bin/attacker -registry http://localhost:5050 -mode deregister -delay 1000
```

### 5. Defensive Remediations
Run ingestion and registry with defensive controls enabled:
```bash
ENABLE_RATE_LIMIT=true \
RATE_LIMIT_RPS=10 \
ENABLE_INPUT_VALIDATION=true \
MIN_TEMP=-50 \
MAX_TEMP=100 \
BLOCKED_DEVICES=attacker-bot-01 \
ADMIN_TOKEN=ssle-secret-key \
bash run.sh
```


## Automated Test Suite

An extensive, fully automated test runner complying with non-disruptive isolation, reproducibility, fault tolerance, and human-readable reporting:

```bash
# Run the complete test suite
./test_runner.sh
```

### Testing Infrastructure Architecture:
1. **Non-Disruptive Isolation:**
   - Dedicated high-range ports (`15050`, `15001-15003`, `15000`, `18000-18001`) preventing interference with any active dev instances on default ports (`5050`, `5001`, `8000`, etc.).
   - Temporary SQLite database and isolated build output in `/tmp/ssle_test_XXXXXX`.
2. **Reproducibility & Idempotency:**
   - Pre-flight port cleanup.
   - Automatic `trap cleanup EXIT INT TERM` guarantees teardown of background test processes and removal of test workspaces.
3. **Fault Tolerance & CI Compliance:**
   - Runs all 7 validation phases regardless of individual failures.
   - Records errors in `FAILED_STEPS` array and exits with code `0` on success, or `1` on failure for CI/CD pipelines.
4. **Human-Readable Output:**
   - Color-coded prefixes: `[INFO]`, `[SUCCESS]`, `[WARNING]`, `[ERROR]`.


## Guide 4: SIEM Integration (Wazuh), Threat Detection & Active Response

Comprehensive documentation, rule analysis, and FCAPS mapping are available in [`docs/guide4_siem_wazuh.md`](docs/guide4_siem_wazuh.md).

### 1. Deploy Wazuh SIEM Manager (Incus)
```bash
./monitoring/wazuh/deploy_wazuh.sh
```

Wazuh credentials inside `wazuh-container`:
```
User: admin
Password: AFH*ngfeY91F2EO1vlA2Exf0*iVk.V6?
```

### 2. Deploy Wazuh Agent to Ingestion Service
```bash
# Retrieve Wazuh Manager IP
WAZUH_IP=$(incus list wazuh-container --format csv -c 4 | awk '{print $1}')

# Install and configure agent on ssle-ingestion
./monitoring/wazuh/install_agent.sh "$WAZUH_IP" ssle-ingestion
```

### 3. Simulate Attacks (Guide 4)
```bash
# Shellshock Command Injection (CVE-2014-6271 - Rule 100100)
./bin/attacker -target http://localhost:5001 -mode shellshock -concurrency 1 -duration 5

# AlienVault Blacklisted IP Traffic (Rule 100101)
./bin/attacker -target http://localhost:5001 -mode malicious_ip -concurrency 1 -duration 5
```

### 4. Verify Active Response (Firewall Drop)
```bash
# Inspect automated firewall-drop triggers inside ssle-ingestion
incus exec ssle-ingestion -- cat /var/ossec/logs/active-responses.log

# Inspect active iptables DROP rules
incus exec ssle-ingestion -- iptables -L -n -v
```


## Guide 5: Distributed Denial of Service (DDoS) & Target Hardening

Comprehensive analysis, hardening strategies, and experimental worksheets are documented in [`docs/guide5_ddos_and_hardening.md`](docs/guide5_ddos_and_hardening.md).

### 1. Apply Incus Resource Quotas (CPU & Memory Hardening)
```bash
# Constrain target ingestion container to 1 CPU core and 256MB RAM
./scripts/set_target_limits.sh apply ssle-ingestion 1 256MiB

# Query current resource usage and limits
./scripts/set_target_limits.sh show ssle-ingestion
```

### 2. Deploy Botnet Client Containers (`load-client-a` & `load-client-b`)
```bash
./scripts/deploy_ddos_clients.sh
```

### 3. Run Automated DDoS Benchmark & Worksheet Generator
```bash
# Runs Baseline (1 client, c=1) vs Distributed Attack (2 clients, c=2)
./scripts/run_ddos_benchmark.sh http://ssle-ingestion.incus:5001 40 2
```

### 4. Teardown Botnet Clients
```bash
./scripts/cleanup_ddos_clients.sh
```


## Guide 5: Route & Signals Hijacking / Man-in-the-Middle (MitM) Prevention

Comprehensive analysis, network topology, and cryptographic verification details are documented in [`docs/guide5_mitm_and_cryptography.md`](docs/guide5_mitm_and_cryptography.md).

### 1. Generate TLS & mTLS Certificates
```bash
# Generates Root CA, Ingestion Server certs (with SANs), mTLS client certs, and rogue MitM CA
./scripts/generate_certs.sh
```

### 2. Deploy Dual-Subnet Interception Topology (Incus)
```bash
# Creates ssle-net-client (10.20.1.0/24), ssle-net-server (10.20.2.0/24) and ssle-interceptor router
./scripts/deploy_mitm_topology.sh
```

### 3. Start Ingestion with HMAC Verification Enabled
```bash
ENABLE_HMAC_VERIFICATION=true \
HMAC_SECRET_KEY="your-shared-secret-key-32bytes-long" \
MAX_TIMESTAMP_DRIFT_SECS=10 \
./bin/ingestion
```

### 4. Simulate MitM In-Flight Tampering & Attacks
```bash
# Attacker modifies sensor telemetry payloads in transit (Ingestion responds HTTP 401)
./bin/attacker -target http://localhost:5001 -mode tamper -concurrency 1 -duration 5
```

### 5. Teardown Interception Topology
```bash
./scripts/cleanup_mitm_topology.sh
```


## Three Threats Selection & Evaluation (Course Syllabus)

Comprehensive analysis following Slide 12 criteria (threat description, relevance, exploit vector, monitoring actions, mitigations, active responses, and practical PoC post-mortem) is documented in [`docs/three_threats_evaluation.md`](docs/three_threats_evaluation.md), with formal attack modeling in [`docs/attack_defense_tree.md`](docs/attack_defense_tree.md).

### Run the Unified Three Threats Demonstrator
Demonstrates and validates all three threats (Shellshock, DDoS, and MitM) in sequence against live or containerized endpoints:
```bash
./scripts/demonstrate_three_threats.sh http://localhost:5001 http://localhost:5050
```

### Three Threats Summary Matrix:
| Threat | Syllabus Area | Primary Detection | Primary Defense | SIEM Rule & Active Response |
| :--- | :--- | :--- | :--- | :--- |
| **1. Shellshock / Reputation** | Reputation Systems & Exploitation | Access Log Pattern Regex | Header Drop (`403 Forbidden`) | Rule 100100 & 100101 $\rightarrow$ `firewall-drop` (60s) |
| **2. DDoS / Botnet** | Massive DoS Disruptions & Botnets | Prometheus `p95` Latency & Req/s | Token Bucket (`429`) & Incus cgroups | Rule 100102 $\rightarrow$ `firewall-drop` (60s) |
| **3. Route & Signals Hijacking** | Route and Signals Hijacking | Ingestion Signature / Replay Check | HMAC-SHA256 (`401`) & TLS/mTLS | Rule 100104 $\rightarrow$ `firewall-drop` (60s) |


## Fault Management Workflow (FCAPS) & Event Correlation

Comprehensive architecture, state diagrams, correlation matrices, and incident post-mortems are documented in [`docs/fcaps_fault_management.md`](docs/fcaps_fault_management.md).

### The 5-Stage FCAPS Cycle
1. **Detecting:** Prometheus scrapers monitor `service_health`, `service_response_time_seconds`, `service_requests_per_second`, and `service_cpu_percent` with alerting thresholds defined in [`monitoring/prometheus_alerts.yml`](monitoring/prometheus_alerts.yml).
2. **Diagnosing & Isolating:** Service Registry rapid incident localization (`GET /services/locate/{id}`) maps anomalies to canonical FQSN, physical machine (`kali-vm`), container (`incus:ssle-ingestion`), and administrative domain (`pt-aveiro/lab-ssle`).
3. **Correlating:** Bridges the gap between operational metrics and security logs, performing root cause analysis across Wazuh rules (100100/101, 100102, 100104) and Apache Combined access logs.
4. **Resolution / Mitigation:** Automated Wazuh Active Response (`firewall-drop` 60s IP ban), application-level rate limiting (`HTTP 429`), or container restart (`incus restart`).
5. **Post-Mortem:** Validates baseline recovery (`/health` UP, latency < 20ms) and outputs a structured incident report.

### Run the Autonomous FCAPS Correlator
```bash
./scripts/fcaps_event_correlator.py \
  --registry http://localhost:5050 \
  --health-exp http://localhost:8000 \
  --diag-exp http://localhost:8001 \
  --access-log /var/log/ssle/ingestion_access.log \
  --service ingestion
```





