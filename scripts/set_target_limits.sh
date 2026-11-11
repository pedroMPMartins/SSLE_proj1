#!/usr/bin/env bash
# ==============================================================================
# SSLE Practical Guide 5: Incus Container Resource Constraints & Target Hardening
# ==============================================================================
set -euo pipefail

ACTION="${1:-show}"
TARGET="${2:-ssle-ingestion}"
CPU_LIMIT="${3:-1}"
MEM_LIMIT="${4:-256MiB}"

show_limits() {
    echo "=========================================================="
    echo "  Resource Limits for Target Container: $TARGET"
    echo "=========================================================="
    if ! incus info "$TARGET" &>/dev/null; then
        echo "Error: Container $TARGET does not exist!"
        exit 1
    fi
    echo "Configured CPU limit:    $(incus config get "$TARGET" limits.cpu || echo 'unlimited')"
    echo "Configured Memory limit: $(incus config get "$TARGET" limits.memory || echo 'unlimited')"
    echo "----------------------------------------------------------"
    echo "Current Resource Usage:"
    incus info "$TARGET" | grep -E "Memory \(current\)|CPU usage" || true
    echo "=========================================================="
}

apply_limits() {
    echo "=========================================================="
    echo "  Applying Incus Resource Constraints (Guide 5 Hardening)"
    echo "  Target: $TARGET | CPU: $CPU_LIMIT core(s) | Memory: $MEM_LIMIT"
    echo "=========================================================="
    incus config set "$TARGET" limits.cpu="$CPU_LIMIT"
    incus config set "$TARGET" limits.memory="$MEM_LIMIT"
    echo "==> Successfully constrained $TARGET to CPU=$CPU_LIMIT, Memory=$MEM_LIMIT."
    show_limits
}

unset_limits() {
    echo "=========================================================="
    echo "  Removing Resource Constraints on $TARGET (Unlimited)"
    echo "=========================================================="
    incus config unset "$TARGET" limits.cpu 2>/dev/null || true
    incus config unset "$TARGET" limits.memory 2>/dev/null || true
    echo "==> Hardware constraints removed."
    show_limits
}

case "$ACTION" in
    apply|set)
        apply_limits
        ;;
    unset|remove|reset)
        unset_limits
        ;;
    show|status)
        show_limits
        ;;
    *)
        echo "Usage: $0 {apply|unset|show} [target_container] [cpu_cores] [memory_limit]"
        echo "Example: $0 apply ssle-ingestion 1 256MiB"
        echo "Example: $0 unset ssle-ingestion"
        exit 1
        ;;
esac
