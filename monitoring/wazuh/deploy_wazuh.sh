#!/usr/bin/env bash
# ==============================================================================
# SSLE Practical Guide 4: Automated Wazuh SIEM Manager Deployment (Incus)
# ==============================================================================
set -euo pipefail

CONTAINER_NAME="wazuh-container"
BASE_IMAGE="images:ubuntu/22.04"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "=========================================================="
echo "  Deploying Wazuh SIEM Manager (SSLE Guide 4)"
echo "=========================================================="

# 1. Launch Wazuh container if not already running
if incus info "$CONTAINER_NAME" &>/dev/null; then
    echo "==> Container $CONTAINER_NAME already exists."
else
    echo "==> Launching $CONTAINER_NAME using $BASE_IMAGE..."
    incus launch "$BASE_IMAGE" "$CONTAINER_NAME" \
        -c limits.cpu=2 \
        -c limits.memory=2GiB
    echo "==> Waiting for container initialization (8s)..."
    sleep 8
fi

# 2. Check if Wazuh manager is already installed
echo "==> Checking Wazuh installation inside $CONTAINER_NAME..."
if ! incus exec "$CONTAINER_NAME" -- which wazuh-control &>/dev/null; then
    echo "==> Adding Wazuh repository and installing wazuh-manager..."
    incus exec "$CONTAINER_NAME" -- bash -c '
        apt-get update && apt-get install -y curl apt-transport-https lsb-release gnupg2
        curl -s https://packages.wazuh.com/key/GPG-KEY-WAZUH | gpg --no-default-keyring --keyring gnupg-ring:/usr/share/keyrings/wazuh.gpg --import && chmod 644 /usr/share/keyrings/wazuh.gpg
        echo "deb [signed-by=/usr/share/keyrings/wazuh.gpg] https://packages.wazuh.com/4.x/apt/ stable main" > /etc/apt/sources.list.d/wazuh.list
        apt-get update
        apt-get install -y wazuh-manager python3
    '
fi

# 3. Deploy custom detection rules and decoders
echo "==> Deploying custom rules and decoders to Wazuh Manager..."
incus file push "${SCRIPT_DIR}/rules/local_rules.xml" "$CONTAINER_NAME/var/ossec/etc/rules/local_rules.xml"
incus file push "${SCRIPT_DIR}/decoders/local_decoder.xml" "$CONTAINER_NAME/var/ossec/etc/decoders/local_decoder.xml"

# 4. Deploy AlienVault IP reputation list
echo "==> Deploying AlienVault IP reputation list..."
incus exec "$CONTAINER_NAME" -- mkdir -p /var/ossec/etc/lists
incus file push "${SCRIPT_DIR}/lists/alienvault_reputation" "$CONTAINER_NAME/var/ossec/etc/lists/alienvault_reputation"

# 5. Configure ruleset list and Active Response inside ossec.conf
echo "==> Applying ruleset list and Active Response inside ossec.conf..."
incus exec "$CONTAINER_NAME" -- python3 -c '
conf_path = "/var/ossec/etc/ossec.conf"
with open(conf_path, "r") as f:
    content = f.read()

# 1. Declare AlienVault reputation list inside <ruleset>
if "alienvault_reputation" not in content:
    if "</ruleset>" in content:
        content = content.replace("</ruleset>", "    <list>etc/lists/alienvault_reputation</list>\n  </ruleset>")

# 2. Insert Active Response inside <ossec_config> before closing tag
if "100100" not in content:
    ar_block = """
  <!-- SSLE Active Response for All Three Syllabus Threats -->
  <active-response>
    <command>firewall-drop</command>
    <location>local</location>
    <rules_id>100100, 100101, 100102, 100104</rules_id>
    <timeout>60</timeout>
  </active-response>
"""
    if "</ossec_config>" in content:
        content = content.replace("</ossec_config>", ar_block + "\n</ossec_config>")

with open(conf_path, "w") as f:
    f.write(content)
'

# 6. Compile CDB list inside container
echo "==> Compiling CDB list with wazuh-makelists..."
incus exec "$CONTAINER_NAME" -- bash -c '
    if [ -x /var/ossec/bin/wazuh-makelists ]; then
        /var/ossec/bin/wazuh-makelists
    elif [ -x /var/ossec/bin/ossec-makelists ]; then
        /var/ossec/bin/ossec-makelists
    fi
'

echo "==> Setting Wazuh ownership and file permissions..."
incus exec "$CONTAINER_NAME" -- bash -c '
    chown -R wazuh:wazuh /var/ossec/etc/ 2>/dev/null || chown -R ossec:ossec /var/ossec/etc/ 2>/dev/null || true
    chmod 640 /var/ossec/etc/rules/local_rules.xml /var/ossec/etc/decoders/local_decoder.xml 2>/dev/null || true
    chmod 640 /var/ossec/etc/lists/alienvault_reputation* 2>/dev/null || true
'

echo "==> Testing Wazuh Manager configuration (/var/ossec/bin/wazuh-analysisd -t)..."
incus exec "$CONTAINER_NAME" -- /var/ossec/bin/wazuh-analysisd -t

# 7. Restart Wazuh manager
echo "==> Restarting wazuh-manager..."
incus exec "$CONTAINER_NAME" -- systemctl restart wazuh-manager

# 7. Print manager details
CONTAINER_IP=$(incus list "$CONTAINER_NAME" --format csv -c 4 | awk '{print $1}')
echo ""
echo "=========================================================="
echo "  Wazuh SIEM Manager deployed successfully!"
echo "  Container:    $CONTAINER_NAME"
echo "  Manager IP:   $CONTAINER_IP"
echo "  Rules:        Threat 1 (100100, 100101), Threat 2 (100102), Threat 3 (100104)"
echo "  Active Resp:  firewall-drop (60s ban for all 3 threats)"
echo "=========================================================="
echo "Next step: Install the agent on the target container using:"
echo "  ./monitoring/wazuh/install_agent.sh $CONTAINER_IP ssle-ingestion"
echo "=========================================================="
