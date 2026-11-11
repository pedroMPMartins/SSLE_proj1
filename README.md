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


## Wazuh

Wazuh credentials inside the wazuh-container:
```
User: admin
Password: AFH*ngfeY91F2EO1vlA2Exf0*iVk.V6?
```
