# Kubernetes & Docker Legacy Deployment Assets

This directory contains experimental Kubernetes (K3s) manifests and Docker build scripts previously used for container orchestration experiments.

## Reason for Archiving
1. **Course Mandate:** SSLE Project 1 guidelines and evaluation exclusively focus on **Incus system containers** running on the Kali Linux environment.
2. **Resource & Stability Issues:** Running nested K3s clusters inside Incus containers caused severe VM kernel panics and memory overcommit contention.
3. **Primary Deployment:** The official system deployment is fully automated using Incus via [`deploy_incus.sh`](../../deploy_incus.sh) and [`cleanup_incus.sh`](../../cleanup_incus.sh).
