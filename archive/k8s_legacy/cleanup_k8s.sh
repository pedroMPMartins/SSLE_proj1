#!/usr/bin/env bash
set -uo pipefail

echo "==> Deleting Kubernetes resources on k3s-master..."
incus exec k3s-master -- k3s kubectl delete -f /root/ssle-k8s.yaml || true
echo "==> K3s resources cleaned up."
