# SSLE Fault Management Workflow (FCAPS) & Event Correlation Architecture

**Course:** Security in Large Scale Systems (SSLE) — University of Aveiro  
**Evaluation Scope:** Project 1 — Threat Detection, Monitoring, and Prevention  
**Author:** Pedro (SSLE Student)  
**Date:** November 2026  

---

## 1. Executive Summary & Problem Context

According to the ISO/ITU-T network management model, **FCAPS** categorizes system management into five domains:
* **F**ault Management
* **C**onfiguration Management
* **A**ccounting Management
* **P**erformance Management
* **S**ecurity Management

In traditional enterprise deployments, **Fault Management** is isolated from **Security Management**. As highlighted in the course lecture guidelines:
> *"Fault Management within FCAPS does not deal with security events (i.e., at least explicit security events)."*

When an operational degradation occurs (e.g., latency spikes, request queue exhaustion, or connection timeouts), traditional fault monitors observe symptoms but cannot determine whether the root cause is an operational hardware failure (bad disk, noisy neighbor) or an active cyber-attack (DDoS flood, exploit injection, MitM tampering).

### The Solution: Event Correlation Engine
Our architecture bridges this gap by unifying **Prometheus operational metrics** (symptom detection) and **Wazuh SIEM log analysis** (causal security detection) into an integrated **5-Stage FCAPS Event Correlation Workflow**:

```mermaid
flowchart TD
    subgraph S1["Stage 1: Detecting"]
        M1["Prometheus Telemetry<br>(service_health, latency_s, rps, error_rate_pct)"]
        A1{"Threshold<br>Breached?"}
        M1 --> A1
    end

    subgraph S2["Stage 2: Diagnosing & Isolating"]
        L1["Registry Rapid Localization<br>(GET /services/locate/{id})"]
        L2["Map to Physical Machine & Logical Container<br>(FQSN, Host, Container, Zone, Region)"]
        A1 -->|"Anomaly Detected"| L1 --> L2
    end

    subgraph S3["Stage 3: Correlating (Root Cause Analysis)"]
        W1["Wazuh SIEM & Access Log Correlation"]
        C1{"Match Security Event?"}
        L2 --> W1 --> C1
        C1 -->|"Rule 100100 / 100101"| RC1["Root Cause: Threat 1<br>(Shellshock / AlienVault)"]
        C1 -->|"Rule 100102"| RC2["Root Cause: Threat 2<br>(Volumetric DoS Flood)"]
        C1 -->|"Rule 100104"| RC3["Root Cause: Threat 3<br>(MitM Tampering / Replay)"]
        C1 -->|"No Security Log Match"| RC4["Root Cause: Operational Contention<br>(Hardware / Network Failure)"]
    end

    subgraph S4["Stage 4: Resolution / Mitigation"]
        R1["Security Incident:<br>Wazuh Active Response firewall-drop (60s)<br>+ Token-Bucket Throttling (429)"]
        R2["Operational Incident:<br>Locality Failover (DiscoverLocally)<br>+ Container Restart (incus restart)"]
        RC1 --> R1
        RC2 --> R1
        RC3 --> R1
        RC4 --> R2
    end

    subgraph S5["Stage 5: Post-Mortem & Recovery"]
        P1["Verify Baseline Recovery<br>(Health Probe UP, Latency < 20ms, Error = 0%)"]
        P2["Compile Structured FCAPS Incident Report"]
        R1 --> P1 --> P2
        R2 --> P1 --> P2
```

---

## 2. The 5-Stage FCAPS Workflow Walkthrough

### Stage 1: Detecting (Operational Symptom Observation)
* **Goal:** Detect deviations from baseline operational parameters before total service failure occurs.
* **Telemetry Sources:**
  * **Health Exporter (`:8000/metrics`):** Liveness (`service_health`), response duration (`service_response_time_seconds`).
  * **Diagnostics Exporter (`:8001/metrics`):** Throughput (`service_requests_per_second`), error percentage (`service_error_rate_percent`), container CPU (`service_cpu_percent`).
* **Prometheus Alerting Rules (`monitoring/prometheus_alerts.yml`):**
  | Alert Name | Metric Expression | Condition / Duration | Severity | Symptom Implication |
  | :--- | :--- | :--- | :--- | :--- |
  | `ServiceLivenessFailure` | `service_health == 0` | For 10s | **Critical** | Service down / uncommunicative |
  | `HighResponseLatency` | `service_response_time_seconds > 0.200` | For 10s | **Warning** | Degradation / resource starvation |
  | `ElevatedErrorRate` | `service_error_rate_percent > 10` | For 10s | **Warning** | Elevated 4xx/5xx responses |
  | `ContainerCPUSaturation`| `service_cpu_percent > 80` | For 15s | **Warning** | CPU contention or flood |
  | `VolumetricRequestSurge`| `service_requests_per_second > 50` | For 10s | **Warning** | Ingress traffic spike |

---

### Stage 2: Diagnosing & Isolating (Hierarchical Scope Narrowing)
* **Goal:** Map the observed anomaly to the precise physical hardware and logical container, isolating blast radius and avoiding cascading failures.
* **Mechanism:**
  The correlator invokes `GET /services/locate/{service_id_or_name}` on the Service Registry (`cmd/registry`):
  ```json
  {
    "status": "FOUND",
    "incident": {
      "service_id": "ingest-01",
      "fqsn": "pt-aveiro.lab-ssle.kali-vm.incus:ssle-ingestion.ingest-01.ingestion",
      "kind": "ingestion",
      "url": "http://10.0.2.15:5001",
      "physical_host": "kali-vm",
      "logical_env": "incus:ssle-ingestion",
      "zone": "lab-ssle",
      "region": "pt-aveiro",
      "administrative_domain": "pt-aveiro/lab-ssle",
      "remediation_hint": "incus exec ssle-ingestion -- journalctl -u ssle-ingestion -n 50"
    }
  }
  ```
* **Isolation Actions:**
  * If confined to a single container: Isolate container NIC or restrict ingress.
  * If zone-wide degradation: Leverage `DiscoverLocally()` to route client traffic to peer zone replicas, preserving inter-region bandwidth.

---

### Stage 3: Correlating (Root Cause Analysis across Telemetry & Security Logs)
* **Goal:** Distinguish between benign infrastructure faults and active security attacks.
* **Correlation Matrix:**

```
+----------------------------------------------------------------------------------------------------+
|                                      EVENT CORRELATION MATRIX                                      |
+------------------------------+---------------------------+-----------------------+-----------------+
| Prometheus Metric Symptom    | Wazuh SIEM Log Evidence   | Correlated Root Cause | Threat Category |
+------------------------------+---------------------------+-----------------------+-----------------+
| Latency Spike (>150ms)       | Rule 100102               | Massive HTTP Flood    | Threat 2 (DoS)  |
| Request Rate Surge (>30 rps) | Volumetric flood from IP  | Botnet Saturation     |                 |
| Error Rate Surge (429)       |                           |                       |                 |
+------------------------------+---------------------------+-----------------------+-----------------+
| Error Rate Surge (>5%)       | Rule 100100 (Shellshock)  | Remote Command        | Threat 1        |
| HTTP 403 Forbidden Spikes    | User-Agent: () { :; };    | Injection Attack      | (Exploitation)  |
+------------------------------+---------------------------+-----------------------+-----------------+
| Error Rate Surge (>5%)       | Rule 100101               | Malicious Scanner /   | Threat 1        |
| HTTP 403 Forbidden Spikes    | AlienVault CDB IP Match   | Threat Actor Traffic  | (Reputation)    |
+------------------------------+---------------------------+-----------------------+-----------------+
| Error Rate Surge (>5%)       | Rule 100104 (401 Status)  | In-flight Telemetry   | Threat 3        |
| HTTP 401 Unauthorized Spikes | HMAC signature mismatch   | Tampering / Replay    | (MitM Hijack)   |
+------------------------------+---------------------------+-----------------------+-----------------+
| Latency Spike (>150ms)       | NO security events in log | Internal Resource     | Operational     |
| High CPU/Memory Utilization  | Clean 200 OK access logs  | Contention (No Attack)| Hardware Fault  |
+------------------------------+---------------------------+-----------------------+-----------------+
| Liveness Failure             | NO security events in log | Application Panic or  | Operational     |
| service_health == 0          | TCP Connection Refused    | Process Crash         | Service Crash   |
+------------------------------+---------------------------+-----------------------+-----------------+
```

---

### Stage 4: Resolution / Mitigation (Automated Remediation)
Depending on the correlated root cause, the system executes an automated, targeted response:

1. **Security Incident (Threats 1, 2, 3):**
   * **Wazuh Active Response:** Executes `/var/ossec/active-response/bin/firewall-drop` on the target container, blocking the offending IP via `iptables`/`nftables` for **60 seconds**.
   * **Application Ingress Backoff:** Ingestion rate limiter returns `HTTP 429 Too Many Requests` with `Retry-After: 1` header, preserving backend SQLite stability.
2. **Operational Incident (Non-Security Fault):**
   * **Service Recovery:** Issues `incus restart <container>` or triggers systemd service restart.
   * **Traffic Failover:** Service consumers automatically invoke `DiscoverLocally()` to fail over to redundant instances in the local zone or region.

---

### Stage 5: Post-Mortem & Recovery Verification
* **Goal:** Ensure the system has returned to normal baseline operations and document the incident lifecycle.
* **Validation Steps:**
  1. Automated probe to `/health` confirms `HTTP 200 UP`.
  2. Prometheus telemetry verifies latency has normalized (`p95 < 20ms`) and error rate has returned to `0%`.
  3. Wazuh logs verify active response completion (`/var/ossec/logs/active-responses.log`).
  4. Generates a structured **FCAPS Incident Report** (JSON & Human-Readable).

---

## 3. Automated FCAPS Correlation Tooling

The entire 5-stage workflow is implemented and executed by [`scripts/fcaps_event_correlator.py`](file:///home/i/Desktop/Pedro/Universidade/4-1/SSLE/proj1/scripts/fcaps_event_correlator.py):

```bash
# Execute FCAPS correlation on Ingestion service
./scripts/fcaps_event_correlator.py \
  --registry http://127.0.0.1:5050 \
  --health-exp http://127.0.0.1:8000 \
  --diag-exp http://127.0.0.1:8001 \
  --access-log /var/log/ssle/ingestion_access.log \
  --service ingestion
```

### Sample Automated FCAPS Report Output:
```json
{
  "timestamp": "2026-11-12T10:35:00Z",
  "target_service": "ingestion",
  "incident_status": "RESOLVED",
  "stages": {
    "detecting": {
      "anomalies_detected": [
        {"type": "LATENCY_SPIKE", "metric": "service_response_time_seconds", "value": 0.185}
      ]
    },
    "diagnosing_and_isolating": {
      "fqsn": "pt-aveiro.lab-ssle.kali-vm.incus:ssle-ingestion.ingest-01.ingestion",
      "physical_host": "kali-vm",
      "logical_env": "incus:ssle-ingestion"
    },
    "correlating": {
      "root_cause": "DOS_VOLUMETRIC_FLOOD",
      "threat_category": "Threat 2: Massive Denial-of-Service Disruptions & Botnets",
      "wazuh_rule_id": 100102,
      "suspected_attacker_ip": "10.20.1.50"
    },
    "resolution_and_mitigation": [
      {"type": "WAZUH_ACTIVE_RESPONSE", "command": "firewall-drop", "status": "TRIGGERED"}
    ],
    "post_mortem": {
      "is_healthy": true,
      "incident_verdict": "RESOLVED"
    }
  }
}
```

---

## 4. Verification & Automated Test Integration

The FCAPS correlation workflow is validated via automated integration tests:
1. **Unit Test Coverage ([`tests/unit_and_integration_test.go`](file:///home/i/Desktop/Pedro/Universidade/4-1/SSLE/proj1/tests/unit_and_integration_test.go)):**
   - Tests `LocateIncidentDetails()` mapping physical host and container environment.
   - Tests correlation of Prometheus metric deviations with structured access logs.
2. **Automated CI Suite ([`test_runner.sh`](file:///home/i/Desktop/Pedro/Universidade/4-1/SSLE/proj1/test_runner.sh)):**
   - Step 5.1b: Rapid Incident Localization verification.
   - Step 6.10: Full FCAPS Event Correlator execution asserting 5-stage completion.
EOF