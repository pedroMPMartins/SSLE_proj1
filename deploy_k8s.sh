#!/usr/bin/env bash
set -euo pipefail

SERVICES=("registry" "storage" "ingestion" "analytics" "temperature")

echo "==> 1. Ensuring binaries are compiled..."
if [ ! -d "bin" ] || [ ! -f "bin/registry" ]; then
    ./build_binaries.sh
fi

echo "==> 2. Pushing project files to 'docker' Incus container..."
incus exec docker -- mkdir -p /root/ssle
incus file push -r ./bin ./docker ./go.mod ./go.sum ./cmd ./internal docker/root/ssle/

echo "==> 3. Building Docker images inside 'docker' container..."
for svc in "${SERVICES[@]}"; do
    echo "Building image ssle-${svc}:latest..."
    incus exec docker -- bash -c "cd /root/ssle && docker build -t ssle-${svc}:latest -f docker/Dockerfile.fast --build-arg SERVICE=${svc} ."
done

echo "==> 4. Importing images into K3s cluster nodes (k3s-master and k3s-node1)..."
for svc in "${SERVICES[@]}"; do
    echo "Transferring ssle-${svc}:latest to k3s-master..."
    incus exec docker -- docker save "ssle-${svc}:latest" | incus exec k3s-master -- k3s ctr images import -

    echo "Transferring ssle-${svc}:latest to k3s-node1..."
    incus exec docker -- docker save "ssle-${svc}:latest" | incus exec k3s-node1 -- k3s ctr images import -
done

echo "==> 5. Applying Kubernetes manifests on k3s-master..."
incus file push k8s/ssle-k8s.yaml k3s-master/root/ssle-k8s.yaml
incus exec k3s-master -- k3s kubectl apply -f /root/ssle-k8s.yaml

echo "==> 6. Waiting for Pods to be ready..."
sleep 15
incus exec k3s-master -- k3s kubectl get pods -o wide

echo "==> 7. Letting temperature sensor send readings (waiting 12s)..."
sleep 12

echo -e "\n==> 8. Verifying Cluster Endpoints (via NodePort on k3s-master)..."
echo -e "\n--- Registered Services (NodePort 30050) ---"
incus exec k3s-master -- curl --connect-timeout 3 -m 5 -s http://localhost:30050/services | jq .

echo -e "\n\n--- Analytics Average (NodePort 30003) ---"
incus exec k3s-master -- curl --connect-timeout 3 -m 5 -s http://localhost:30003/average | jq .
echo ""
