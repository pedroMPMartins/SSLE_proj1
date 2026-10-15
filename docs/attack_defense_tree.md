# Attack-Defense Tree (ADTree) - SSLE SOA System

## 1. Threat Model Context

* **Target System:** Service-Oriented Architecture (SOA) composed of:
  * Service Registry (`:5050`)
  * Ingestion Service (`:5001`)
  * Storage Service (`:5002`) & SQLite Database (`temp_data.db`)
  * Analytics Service (`:5003`)
  * Temperature Sensor (Client producer)
* **Attacker Profile:** Remote Network Attacker.
* **Attacker Capabilities:**
  * Can reach publicly exposed or cluster-accessible endpoints (`/register`, `/ingest`, `/average`, `/services`).
  * Can sniff unencrypted cleartext HTTP traffic on shared networks.
  * Can flood endpoints with requests or forge payloads.
  * Can register malicious/rogue services in the registry.
* **System Weaknesses in Baseline:**
  * Lack of authentication and authorization (no API keys, mTLS, or tokens).
  * Lack of rate limiting or traffic throttling.
  * Lack of strict input validation/bounds checking on temperature readings.
  * Unencrypted communications (plain HTTP).

---

## 2. Attack-Defense Tree Diagram

```mermaid
flowchart TD
    %% Styling Classes
    classDef goal fill:#8B0000,stroke:#333,stroke-width:2px,color:#fff;
    classDef attack fill:#D9534F,stroke:#333,stroke-width:1px,color:#fff;
    classDef subattack fill:#F0AD4E,stroke:#333,stroke-width:1px,color:#000;
    classDef defense fill:#5CB85C,stroke:#333,stroke-width:1px,color:#fff;
    classDef monitor fill:#0275D8,stroke:#333,stroke-width:1px,color:#fff;

    %% Root Goal
    ROOT["Root: Compromise SOA Infrastructure & Data Integrity"]:::goal

    %% Primary Attack Vectors
    A1["A1: Denial of Service - DoS / Availability Loss"]:::attack
    A2["A2: Data Poisoning & Integrity Tampering"]:::attack
    A3["A3: Service Hijacking & Man-in-the-Middle - MitM"]:::attack
    A4["A4: Unauthorized Reconnaissance & Data Leakage"]:::attack

    ROOT --> A1
    ROOT --> A2
    ROOT --> A3
    ROOT --> A4

    %% Sub-attacks & Defenses for A1 (DoS)
    A1 --> A11["A1.1: Flood Ingestion Service - High-rate HTTP POST"]:::subattack
    A11 -.-> D11["D1.1: Rate Limiting - Token Bucket / IP Throttling"]:::defense
    A11 -.-> M11["M1.1: Prometheus Monitoring - Request Rate / Latency Spikes"]:::monitor

    A1 --> A12["A1.2: Resource Exhaustion on Storage - Disk Flooding"]:::subattack
    A12 -.-> D12["D1.2: Storage Quotas, Retention Limits & Payload Size Capping"]:::defense
    A12 -.-> M12["M1.2: Prometheus Exporter - Disk Usage & Memory Gauge"]:::monitor

    A1 --> A13["A1.3: Service Registry Starvation / Registration Flood"]:::subattack
    A13 -.-> D13["D1.3: Connection Timeouts & Registry Ingress Filtering"]:::defense
    A13 -.-> M13["M1.3: Liveness Probing & Health Checks - service_health Gauge"]:::monitor

    %% Sub-attacks & Defenses for A2 (Data Poisoning)
    A2 --> A21["A2.1: Rogue Sensor Spoofing - Fake Extreme Temperatures"]:::subattack
    A21 -.-> D21["D2.1: Mutual TLS - mTLS / Device Authentication Tokens"]:::defense
    A21 -.-> D22["D2.2: Input Validation - Plausibility Bounds Checking"]:::defense
    A21 -.-> M21["M2.1: Statistical Anomaly Detection on Ingested Values"]:::monitor

    A2 --> A22["A2.2: Arbitrary Unregistration - DELETE /register/id"]:::subattack
    A22 -.-> D23["D2.3: Restrict DELETE Methods via RBAC / Admin Tokens"]:::defense
    A22 -.-> M22["M2.2: Prometheus Alert on Unexpected Service Deregistration"]:::monitor

    %% Sub-attacks & Defenses for A3 (Service Hijacking)
    A3 --> A31["A3.1: Registry Poisoning - Register Malicious Storage/Ingestion"]:::subattack
    A31 -.-> D31["D3.1: Cryptographic Service Identity Verification & Allowlisting"]:::defense
    A31 -.-> D32["D3.2: Kubernetes NetworkPolicies - Restrict Egress/Ingress"]:::defense
    A31 -.-> M31["M3.1: Service Discovery Audit & Unexpected Endpoint Alerting"]:::monitor

    %% Sub-attacks & Defenses for A4 (Reconnaissance)
    A4 --> A41["A4.1: Query Registry for System Topology - GET /services"]:::subattack
    A41 -.-> D41["D4.1: Network Segmentation - Keep Registry in Private Network"]:::defense
    A41 -.-> D42["D4.2: End-to-End Transport Layer Security - TLS 1.3"]:::defense
    A41 -.-> M41["M4.1: Access Logging & Audit Trails"]:::monitor
```

---

## 3. Detailed Component Breakdown

### Legend
* 🟥 **Root & Primary Attacks:** High-level attacker objectives.
* 🟧 **Sub-Attacks:** Concrete attack vectors exploiting current system vulnerabilities.
* 🟩 **Defenses (Prevention & Mitigation):** Cryptographic, architectural, and network countermeasures.
* 🟦 **Monitoring & Detection (Guide 3 Focus):** Observable traces in Prometheus (metrics, gauges, resource counters).

---

### Vector 1: Denial of Service (DoS)
* **Vulnerability:** Neither `cmd/ingestion` nor `cmd/registry` enforce rate limits or request quotas.
* **Attack Mechanism:** An attacker sends thousands of `POST /ingest` or `POST /register` requests per second.
* **Consequences:** SQLite write contention, CPU starvation, disk exhaustion, or dropped genuine sensor readings.
* **Defensive Controls:**
  * Rate-limiting at the reverse proxy / API gateway level.
  * Request payload size constraints (`http.MaxBytesReader`).
  * Disk pruning policies for `temp_data.db`.
* **Detection Traces in Monitoring (Prometheus):**
  * Sharp increase in `http_requests_total` rate.
  * Spike in container CPU / memory utilization.
  * Elevated request duration / latency percentiles (`http_request_duration_seconds`).

---

### Vector 2: Data Poisoning & Sensor Spoofing
* **Vulnerability:** `cmd/ingestion` accepts temperature readings from any HTTP client without validating caller identity.
* **Attack Mechanism:** An attacker sends forged readings (e.g., `1000.0°C` or negative values) under arbitrary or impersonated `device_id`s.
* **Consequences:** Analytics service computes corrupted averages (`GET /average`), leading to false alerts or distorted business logic.
* **Defensive Controls:**
  * Device authentication via signed tokens (HMAC/JWT) or client certificates (mTLS).
  * Domain validation on ingestion (e.g., discard readings outside `-50.0°C` to `100.0°C`).
* **Detection Traces in Monitoring (Prometheus):**
  * Gauge metric monitoring temperature mean and standard deviation.
  * Sudden variance or out-of-bounds metrics flagged by anomaly alerts.

---

### Vector 3: Service Registry Poisoning & Hijacking
* **Vulnerability:** `cmd/registry` allows any client to register or delete services via `POST /register` and `DELETE /register/{id}`.
* **Attack Mechanism:**
  1. **Malicious Deregistration:** An attacker continuously deletes active ingestion or storage services from the registry.
  2. **Rogue Service Injection:** Attacker registers a malicious `url` under kind `storage` or `ingestion`, intercepting sensor telemetry.
* **Consequences:** Telemetry diversion, eavesdropping, or complete service disconnection.
* **Defensive Controls:**
  * Require pre-shared tokens or mTLS for service registration.
  * Disable unauthenticated `DELETE` methods.
  * Restrict inter-service traffic via Kubernetes `NetworkPolicy`.
* **Detection Traces in Monitoring (Prometheus):**
  * `service_health` Gauge drops to `0` (liveness failure).
  * Number of registered instances per kind changes unexpectedly.

---

### Vector 4: Reconnaissance & Topology Disclosure
* **Vulnerability:** `GET /services` dumps all registered endpoints, URLs, and instance IDs to any requester.
* **Attack Mechanism:** Attacker scans port `5050` and reads the full network layout and internal IPs/ports of all services.
* **Defensive Controls:**
  * Isolate registry behind internal networks (ClusterIP only, no external NodePort).
  * Enforce authentication to list registry entries.
* **Detection Traces in Monitoring (Prometheus):**
  * High frequency of `GET /services` requests from unauthenticated origins.
