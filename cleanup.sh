#!/usr/bin/env bash
# ==============================================================================
# SSLE Project 1: Master Incus Infrastructure Cleanup & Teardown
# Stops and deletes all SSLE microservices, exporters, Prometheus, and Wazuh containers.
# ==============================================================================
set -uo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

echo -e "\n${YELLOW}${BOLD}================================================================================${NC}"
echo -e "${YELLOW}${BOLD}  SSLE Project 1: Complete Incus Infrastructure Teardown${NC}"
echo -e "${YELLOW}${BOLD}================================================================================${NC}\n"

ALL_CONTAINERS=(
    "ssle-registry"
    "ssle-storage"
    "ssle-ingestion"
    "ssle-analytics"
    "ssle-temperature"
    "ssle-exporter"
    "prometheus-container"
    "wazuh-container"
)

SHARED_HOST_DIR="/var/lib/ssle-data"

if command -v incus &>/dev/null; then
    for c in "${ALL_CONTAINERS[@]}"; do
        if incus info "$c" &>/dev/null; then
            echo -e " ${BLUE}[INFO]${NC} Deleting container '${c}'..."
            incus delete -f "$c" 2>/dev/null || true
        fi
    done
else
    echo -e " ${YELLOW}[WARNING]${NC} Command 'incus' not found; skipping container deletion."
fi

if [ -d "$SHARED_HOST_DIR" ]; then
    echo -e " ${BLUE}[INFO]${NC} Removing shared database directory (${SHARED_HOST_DIR})..."
    sudo rm -rf "$SHARED_HOST_DIR" 2>/dev/null || rm -rf "$SHARED_HOST_DIR" 2>/dev/null || true
fi

echo -e "\n${GREEN}${BOLD}[SUCCESS] Complete SSLE Incus infrastructure has been cleanly torn down.${NC}\n"
