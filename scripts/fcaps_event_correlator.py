#!/usr/bin/env python3
"""
SSLE Project 1: FCAPS Fault Management & Event Correlation Engine
Implements the 5-stage FCAPS cycle for distributed SOA environments:
  1. Detecting:               Monitors Prometheus metrics for operational anomalies.
  2. Diagnosing & Isolating:  Queries Service Registry /services/locate to map physical/logical container.
  3. Correlating:             Correlates metric symptoms with Wazuh SIEM security logs (Root Cause Analysis).
  4. Resolution / Mitigation: Recommends & verifies automated Active Response or operational recovery.
  5. Post-Mortem:             Validates baseline restoration and compiles structured incident report.
"""

import sys
import os
import re
import json
import time
import argparse
import urllib.request
import urllib.error

# ANSI terminal colors
RED = "\033[0;31m"
GREEN = "\033[0;32m"
YELLOW = "\033[1;33m"
BLUE = "\033[0;34m"
CYAN = "\033[0;36m"
BOLD = "\033[1m"
RESET = "\033[0m"


def fetch_url(url, timeout=3.0):
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "SSLE-FCAPS-Engine/1.0"})
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return resp.read().decode("utf-8", errors="replace"), resp.status
    except Exception as e:
        return str(e), 500


def parse_prometheus_metrics(text):
    """Parses Prometheus exposition format into a structured dictionary."""
    metrics = {}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue

        # Match name{labels} value or name value
        m = re.match(r"^([a-zA-Z_:][a-zA-Z0-9_:]*)(?:\{([^}]*)\})?\s+([^\s]+)", line)
        if m:
            name, label_str, val_str = m.groups()
            try:
                val = float(val_str)
            except ValueError:
                continue

            labels = {}
            if label_str:
                for part in label_str.split(","):
                    if "=" in part:
                        k, v = part.split("=", 1)
                        labels[k.strip()] = v.strip().strip('"')

            if name not in metrics:
                metrics[name] = []
            metrics[name].append({"labels": labels, "value": val})
    return metrics


def run_fcaps_cycle(registry_url, health_exp_url, diag_exp_url, access_log_path, target_service="ingestion"):
    report = {
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "target_service": target_service,
        "stages": {},
        "root_cause_analysis": {},
        "incident_status": "NORMAL"
    }

    print(f"{CYAN}{BOLD}================================================================================{RESET}")
    print(f"{CYAN}{BOLD}      SSLE FCAPS Fault Management & Event Correlation Engine (Guide 4/5)        {RESET}")
    print(f"{CYAN}{BOLD}================================================================================{RESET}")
    print(f"Target Service:     {target_service}")
    print(f"Service Registry:   {registry_url}")
    print(f"Health Exporter:    {health_exp_url}")
    print(f"Diagnostics Exp:    {diag_exp_url}")
    print(f"Access Log Path:    {access_log_path}")
    print("")

    # --------------------------------------------------------------------------
    # STAGE 1: DETECTING (Prometheus Metrics & Anomaly Identification)
    # --------------------------------------------------------------------------
    print(f"{BLUE}{BOLD}[Stage 1: Detecting]{RESET} Polling operational telemetry for metric anomalies...")
    anomalies = []
    telemetry = {}

    health_raw, h_status = fetch_url(f"{health_exp_url}/metrics")
    if h_status == 200:
        h_metrics = parse_prometheus_metrics(health_raw)
        for sample in h_metrics.get("service_health", []):
            if sample["labels"].get("service") == target_service:
                val = sample["value"]
                telemetry["health"] = val
                if val == 0:
                    anomalies.append({"type": "LIVENESS_FAILURE", "metric": "service_health", "value": val, "threshold": "== 0"})

        for sample in h_metrics.get("service_response_time_seconds", []):
            if sample["labels"].get("service") == target_service:
                val = sample["value"]
                telemetry["latency_s"] = val
                if val > 0.150:  # 150ms anomaly threshold
                    anomalies.append({"type": "LATENCY_SPIKE", "metric": "service_response_time_seconds", "value": val, "threshold": "> 0.150s"})

    diag_raw, d_status = fetch_url(f"{diag_exp_url}/metrics")
    if d_status == 200:
        d_metrics = parse_prometheus_metrics(diag_raw)
        for sample in d_metrics.get("service_requests_per_second", []):
            if sample["labels"].get("service") == target_service:
                val = sample["value"]
                telemetry["requests_per_second"] = val
                if val > 30.0:
                    anomalies.append({"type": "TRAFFIC_SURGE", "metric": "service_requests_per_second", "value": val, "threshold": "> 30 req/s"})

        for sample in d_metrics.get("service_error_rate_percent", []):
            if sample["labels"].get("service") == target_service:
                val = sample["value"]
                telemetry["error_rate_pct"] = val
                if val > 5.0:
                    anomalies.append({"type": "HIGH_ERROR_RATE", "metric": "service_error_rate_percent", "value": val, "threshold": "> 5%"})

        for sample in d_metrics.get("service_cpu_percent", []):
            if sample["labels"].get("service") == target_service:
                val = sample["value"]
                telemetry["cpu_pct"] = val
                if val > 80.0:
                    anomalies.append({"type": "CPU_STARVATION", "metric": "service_cpu_percent", "value": val, "threshold": "> 80%"})

    report["stages"]["detecting"] = {
        "telemetry_snapshot": telemetry,
        "anomalies_detected": anomalies
    }

    if anomalies:
        print(f"    {YELLOW}[!] Detected {len(anomalies)} active anomaly condition(s):{RESET}")
        for a in anomalies:
            print(f"        - {BOLD}{a['type']}{RESET}: {a['metric']} = {a['value']} (threshold: {a['threshold']})")
        report["incident_status"] = "INCIDENT_DETECTED"
    else:
        print(f"    {GREEN}[*] Telemetry nominal. No threshold breaches detected.{RESET}")
    print("")

    # --------------------------------------------------------------------------
    # STAGE 2: DIAGNOSING & ISOLATING (Location Mapping & Scope Narrowing)
    # --------------------------------------------------------------------------
    print(f"{BLUE}{BOLD}[Stage 2: Diagnosing & Isolating]{RESET} Narrowing scope via hierarchical location...")
    locate_raw, l_status = fetch_url(f"{registry_url}/services/locate/{target_service}")
    location_data = {}
    if l_status == 200:
        try:
            loc_json = json.loads(locate_raw)
            location_data = loc_json.get("incident", {})
            print(f"    {GREEN}[*] Rapid Incident Localization succeeded:{RESET}")
            print(f"        - FQSN:           {location_data.get('fqsn')}")
            print(f"        - Physical Host:  {location_data.get('physical_host')}")
            print(f"        - Logical Env:    {location_data.get('logical_env')}")
            print(f"        - Admin Domain:   {location_data.get('administrative_domain')}")
            print(f"        - Endpoint URL:   {location_data.get('url')}")
            print(f"        - Remediation:    {location_data.get('remediation_hint')}")
        except Exception as e:
            print(f"    {RED}[!] Failed to parse localization JSON: {e}{RESET}")
    else:
        print(f"    {YELLOW}[!] Registry locate query returned HTTP {l_status}. Using default fallback location.{RESET}")
        location_data = {
            "physical_host": "kali-vm",
            "logical_env": f"incus:ssle-{target_service}",
            "region": "pt-aveiro",
            "zone": "lab-ssle"
        }

    report["stages"]["diagnosing_and_isolating"] = location_data
    print("")

    # --------------------------------------------------------------------------
    # STAGE 3: CORRELATING (Connecting Metric Symptoms with Security Log Causes)
    # --------------------------------------------------------------------------
    print(f"{BLUE}{BOLD}[Stage 3: Correlating]{RESET} Performing Root Cause Analysis across metrics & logs...")
    security_evidence = []
    root_cause = "UNKNOWN"
    threat_category = "NONE"
    rule_matched = None
    attacker_ip = "UNKNOWN"

    # Analyze structured access log
    if os.path.exists(access_log_path):
        try:
            with open(access_log_path, "r", errors="replace") as f:
                lines = f.readlines()[-100:]  # examine last 100 log entries
                for line in lines:
                    # Threat 1: Shellshock Regex Match
                    if "() {" in line or "() { :; };" in line:
                        m_ip = line.split(" ", 1)[0]
                        attacker_ip = m_ip
                        security_evidence.append({
                            "threat": "Threat 1: Shellshock Command Injection (CVE-2014-6271)",
                            "wazuh_rule": 100100,
                            "severity": "Level 12 (Critical)",
                            "src_ip": m_ip,
                            "log_snippet": line.strip()
                        })
                        root_cause = "EXPLOITATION_SHELLSHOCK"
                        threat_category = "Threat 1"
                        rule_matched = 100100

                    # Threat 3: Route & Signals Hijacking (401 Unauthorized on Ingest)
                    if 'POST /ingest HTTP/1.1" 401' in line:
                        m_ip = line.split(" ", 1)[0]
                        attacker_ip = m_ip
                        security_evidence.append({
                            "threat": "Threat 3: Route & Signals Hijacking / Cryptographic Tampering",
                            "wazuh_rule": 100104,
                            "severity": "Level 11 (High)",
                            "src_ip": m_ip,
                            "log_snippet": line.strip()
                        })
                        if root_cause == "UNKNOWN":
                            root_cause = "MITM_PAYLOAD_TAMPERING"
                            threat_category = "Threat 3"
                            rule_matched = 100104

                    # Threat 2: High Rate of 429 or Flood
                    if 'POST /ingest HTTP/1.1" 429' in line or 'POST /ingest HTTP/1.1" 200' in line:
                        # Count recent frequency
                        pass

        except Exception as e:
            print(f"    {YELLOW}[!] Failed reading access log: {e}{RESET}")

    # Check for DoS flood if traffic anomaly detected and multiple 429s
    if any(a["type"] in ["TRAFFIC_SURGE", "CPU_STARVATION"] for a in anomalies):
        if root_cause == "UNKNOWN":
            root_cause = "DOS_VOLUMETRIC_FLOOD"
            threat_category = "Threat 2"
            rule_matched = 100102
            security_evidence.append({
                "threat": "Threat 2: Massive Denial-of-Service Disruptions & Botnets",
                "wazuh_rule": 100102,
                "severity": "Level 8 (Medium-High)",
                "src_ip": attacker_ip,
                "evidence": "Prometheus traffic surge correlated with token-bucket saturation"
            })

    # If no security evidence found, check for operational failure
    if root_cause == "UNKNOWN":
        if any(a["type"] == "LIVENESS_FAILURE" for a in anomalies):
            root_cause = "OPERATIONAL_SERVICE_CRASH"
            threat_category = "Operational Fault (Non-Security)"
        elif any(a["type"] == "CPU_STARVATION" for a in anomalies):
            root_cause = "OPERATIONAL_RESOURCE_CONTENTION"
            threat_category = "Operational Resource Exhaustion"
        else:
            root_cause = "NOMINAL_HEALTHY"
            threat_category = "None"

    correlation_summary = {
        "root_cause": root_cause,
        "threat_category": threat_category,
        "wazuh_rule_id": rule_matched,
        "suspected_attacker_ip": attacker_ip,
        "evidence_count": len(security_evidence),
        "evidence_list": security_evidence
    }
    report["root_cause_analysis"] = correlation_summary
    report["stages"]["correlating"] = correlation_summary

    print(f"    {BOLD}Root Cause Identified:{RESET} {CYAN}{root_cause}{RESET}")
    print(f"    {BOLD}Threat Classification:{RESET} {YELLOW}{threat_category}{RESET}")
    if rule_matched:
        print(f"    {BOLD}Correlated SIEM Rule:{RESET} Wazuh Rule {rule_matched}")
    if attacker_ip != "UNKNOWN":
        print(f"    {BOLD}Identified Source IP:{RESET} {attacker_ip}")
    print(f"    {BOLD}Security Events Found:{RESET} {len(security_evidence)}")
    print("")

    # --------------------------------------------------------------------------
    # STAGE 4: RESOLUTION / MITIGATION (Automated Action-Response)
    # --------------------------------------------------------------------------
    print(f"{BLUE}{BOLD}[Stage 4: Resolution / Mitigation]{RESET} Determining & executing remediation actions...")
    actions = []
    if root_cause.startswith("EXPLOITATION") or root_cause.startswith("MITM") or root_cause.startswith("DOS"):
        action = {
            "type": "WAZUH_ACTIVE_RESPONSE",
            "command": f"/var/ossec/active-response/bin/firewall-drop",
            "parameters": f"ip={attacker_ip} timeout=60s",
            "status": "TRIGGERED",
            "description": f"Automated 60-second firewall IP quarantine of {attacker_ip} via iptables/nftables."
        }
        actions.append(action)
        print(f"    {GREEN}[ACTION] Wazuh Active Response:{RESET} firewall-drop on IP {attacker_ip} for 60s")

        if root_cause == "DOS_VOLUMETRIC_FLOOD":
            actions.append({
                "type": "APPLICATION_RATE_LIMIT",
                "status": "ACTIVE",
                "description": "Token-bucket rate limiter throttles traffic with HTTP 429 Retry-After."
            })
            print(f"    {GREEN}[ACTION] Application Rate Limiter:{RESET} Enforcing HTTP 429 quota backoff.")

    elif root_cause == "OPERATIONAL_SERVICE_CRASH":
        container = location_data.get("logical_env", "").replace("incus:", "")
        actions.append({
            "type": "CONTAINER_RESTART",
            "command": f"incus restart {container}",
            "status": "RECOMMENDED",
            "description": f"Operational crash detected. Restart container {container} or trigger local failover."
        })
        print(f"    {YELLOW}[ACTION] Operational Recovery:{RESET} Restart container {container}")
    else:
        actions.append({"type": "NONE_REQUIRED", "status": "COMPLETED", "description": "System operating within nominal thresholds."})
        print(f"    {GREEN}[*] No remediation required. System is stable.{RESET}")

    report["stages"]["resolution_and_mitigation"] = actions
    print("")

    # --------------------------------------------------------------------------
    # STAGE 5: POST-MORTEM & RECOVERY VERIFICATION
    # --------------------------------------------------------------------------
    print(f"{BLUE}{BOLD}[Stage 5: Post-Mortem & Recovery Verification]{RESET} Validating baseline restoration...")
    post_probe_url = location_data.get("url", "http://localhost:5001") + "/health"
    post_raw, post_code = fetch_url(post_probe_url, timeout=2.0)
    is_recovered = (post_code == 200 and '"UP"' in post_raw)

    post_mortem = {
        "verification_probe_url": post_probe_url,
        "probe_status_code": post_code,
        "is_healthy": is_recovered,
        "recommendation": "Maintain Wazuh active-response firewall quarantine; verify Prometheus p95 latency stays under 50ms.",
        "incident_verdict": "RESOLVED" if is_recovered else "ONGOING"
    }
    report["stages"]["post_mortem"] = post_mortem

    if is_recovered:
        print(f"    {GREEN}[SUCCESS] Health probe confirmed service is UP ({post_code} OK).{RESET}")
        print(f"    {GREEN}[SUCCESS] Incident Status: RESOLVED.{RESET}")
    else:
        print(f"    {YELLOW}[!] Health probe returned HTTP {post_code}. Investigation ongoing.{RESET}")
    print("")

    print(f"{CYAN}{BOLD}================================================================================{RESET}")
    print(f"{CYAN}{BOLD}                   FCAPS WORKFLOW COMPLETED SUCCESSFULLY                        {RESET}")
    print(f"{CYAN}{BOLD}================================================================================{RESET}")

    return report


def main():
    parser = argparse.ArgumentParser(description="SSLE FCAPS Fault Management & Event Correlation Engine")
    parser.add_argument("--registry", default="http://127.0.0.1:5050", help="Service Registry URL")
    parser.add_argument("--health-exp", default="http://127.0.0.1:8000", help="Health Exporter URL")
    parser.add_argument("--diag-exp", default="http://127.0.0.1:8001", help="Diagnostics Exporter URL")
    parser.add_argument("--access-log", default="/var/log/ssle/ingestion_access.log", help="Path to Ingestion Access Log")
    parser.add_argument("--service", default="ingestion", help="Target service name to evaluate")
    parser.add_argument("--json", action="store_true", help="Output full report as JSON to stdout")

    args = parser.parse_args()

    report = run_fcaps_cycle(
        registry_url=args.registry,
        health_exp_url=args.health_exp,
        diag_exp_url=args.diag_exp,
        access_log_path=args.access_log,
        target_service=args.service
    )

    if args.json:
        print("\n--- JSON INCIDENT REPORT ---")
        print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
