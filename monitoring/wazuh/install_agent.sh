#!/usr/bin/env bash
# ==============================================================================
# SSLE Practical Guide 4: Install Wazuh Agent on Target Container
# ==============================================================================
set -euo pipefail

WAZUH_MANAGER_IP="${1:-}"
TARGET_CONTAINER="${2:-ssle-ingestion}"

if [ -z "$WAZUH_MANAGER_IP" ]; then
    echo "Usage: $0 <WAZUH_MANAGER_IP> [TARGET_CONTAINER]"
    echo "Example: $0 10.0.4.50 ssle-ingestion"
    exit 1
fi

echo "=========================================================="
echo "  Deploying Wazuh Agent to $TARGET_CONTAINER"
echo "  Manager IP: $WAZUH_MANAGER_IP"
echo "=========================================================="

# 1. Verify target container exists
if ! incus info "$TARGET_CONTAINER" &>/dev/null; then
    echo "Error: Target container $TARGET_CONTAINER does not exist!"
    exit 1
fi

# 2. Install wazuh-agent inside target container
echo "==> Installing wazuh-agent inside $TARGET_CONTAINER..."
incus exec "$TARGET_CONTAINER" -- bash -c "
    apt-get update && apt-get install -y curl apt-transport-https lsb-release gnupg2 iptables
    curl -s https://packages.wazuh.com/key/GPG-KEY-WAZUH | gpg --no-default-keyring --keyring gnupg-ring:/usr/share/keyrings/wazuh.gpg --import && chmod 644 /usr/share/keyrings/wazuh.gpg
    echo 'deb [signed-by=/usr/share/keyrings/wazuh.gpg] https://packages.wazuh.com/4.x/apt/ stable main' > /etc/apt/sources.list.d/wazuh.list
    apt-get update
    WAZUH_MANAGER='$WAZUH_MANAGER_IP' apt-get install -y wazuh-agent
"

# 3. Configure log collection in ossec.conf
echo "==> Configuring <localfile> log collection for /var/log/ssle/ingestion_access.log..."
incus exec "$TARGET_CONTAINER" -- python3 -c '
conf_path = "/var/ossec/etc/ossec.conf"
with open(conf_path, "r") as f:
    content = f.read()

localfile_block = """
  <!-- SSLE Ingestion Gateway Apache-Compatible Access Log Ingestion -->
  <localfile>
    <log_format>apache</log_format>
    <location>/var/log/ssle/ingestion_access.log</location>
  </localfile>
"""

if "/var/log/ssle/ingestion_access.log" not in content:
    if "</ossec_config>" in content:
        content = content.replace("</ossec_config>", localfile_block + "\n</ossec_config>")
        with open(conf_path, "w") as f:
            f.write(content)
'

# 4. Start Wazuh agent service
echo "==> Starting wazuh-agent service..."
incus exec "$TARGET_CONTAINER" -- systemctl daemon-reload
incus exec "$TARGET_CONTAINER" -- systemctl enable --now wazuh-agent

echo "=========================================================="
echo "  Wazuh Agent successfully installed and active on $TARGET_CONTAINER!"
echo "  Logs monitored: /var/log/ssle/ingestion_access.log"
echo "=========================================================="
