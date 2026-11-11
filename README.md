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

Get node token from the actual container:
```bash
# Must be run inside the container
cat /var/lib/rancher/k3s/server/node-token
```

## Configure VM stuff

### Applying kernel settings
(These settings reset whenever the VM reboots)
Run:
```bash
# Kernel settings
sudo sysctl -w vm.overcommit_memory=1
sudo sysctl -w kernel.panic=10
sudo sysctl -w kernel.panic_on_oops=1

# Make /proc/sys read-write inside the containers:
incus exec k3s-master -- mount -o remount,rw /proc/sys
incus exec k3s-node1 -- mount -o remount,rw /proc/sys
##Restart K3s on both nodes:
incus exec k3s-master -- systemctl restart k3s
incus exec k3s-node1 -- systemctl restart k3s-agent
```

### Make /proc/sys read-write inside the containers:
  incus exec k3s-master -- mount -o remount,rw /proc/sys
  incus exec k3s-node1 -- mount -o remount,rw /proc/sys
#### 3. Restart K3s on both nodes:
  incus exec k3s-master -- systemctl restart k3s
  incus exec k3s-node1 -- systemctl restart k3s-agent



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


## Wazuh

Wazuh credentials inside the wazuh-container:
```
User: admin
Password: AFH*ngfeY91F2EO1vlA2Exf0*iVk.V6?
```
