#!/usr/bin/env bash
# XrayR Agent one-shot installer (served by Center). Metadata lines below are replaced when generated.
set -euo pipefail

# --- Injected by Center when this file is served (do not edit manually) ---
XRAYR_EMBED_AGENT_URL=__XRAYR_CENTER_INJECT_AGENT_URL__
XRAYR_EMBED_AGENT_SHA256=__XRAYR_CENTER_INJECT_AGENT_SHA256__
# --- End injected ---

usage() {
  echo "Usage: sudo bash -s -- -e <center_base_url> -t <register_token> [options]" >&2
  echo "  -e, --endpoint   Center base URL, e.g. https://center.example.com" >&2
  echo "  -t, --token      One-time node register token" >&2
  echo "  -c, --config     agent.yml path (default /etc/xrayr-agent/agent.yml)" >&2
  echo "  -v, --version    Reserved (Agent binary is chosen by Center metadata)" >&2
  echo "  -h, --help       Show this help" >&2
}

if [[ "$(id -u)" != "0" ]]; then
  echo "error: must run as root (example: curl ... | sudo bash -s -- ...)" >&2
  exit 1
fi

uname_s="$(uname -s)"
if [[ "${uname_s}" != "Linux" ]]; then
  echo "error: only Linux is supported (uname: ${uname_s})" >&2
  exit 1
fi

if [[ -z "${XRAYR_EMBED_AGENT_URL}" || -z "${XRAYR_EMBED_AGENT_SHA256}" ]]; then
  echo "error: Center did not inject Agent download URL/sha256." >&2
  echo "  Ask the operator to set CENTER_AGENT_DOWNLOAD_URL and CENTER_AGENT_SHA256," >&2
  echo "  or place the Agent binary under CENTER_ARTIFACT_DIR with CENTER_AGENT_VERSION (see Center docs)." >&2
  exit 1
fi

march="$(uname -m)"
case "${march}" in
  x86_64) ARCH=amd64 ;;
  aarch64 | arm64) ARCH=arm64 ;;
  *)
    echo "error: unsupported CPU architecture: ${march}" >&2
    exit 1
    ;;
esac
_="${ARCH}"

ENDPOINT=""
REGISTER_TOKEN=""
CONFIG_PATH="/etc/xrayr-agent/agent.yml"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -e | --endpoint)
      ENDPOINT="${2:-}"
      shift 2
      ;;
    -t | --token)
      REGISTER_TOKEN="${2:-}"
      shift 2
      ;;
    -c | --config)
      CONFIG_PATH="${2:-}"
      shift 2
      ;;
    -v | --version)
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown option: $1" >&2
      usage
      exit 1
      ;;
  esac
done

if [[ -z "${ENDPOINT}" || -z "${REGISTER_TOKEN}" ]]; then
  echo "error: -e/--endpoint and -t/--token are required" >&2
  usage
  exit 1
fi

while [[ "${ENDPOINT}" == */ ]]; do
  ENDPOINT="${ENDPOINT%/}"
done

download_to() {
  local url="$1" dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "${url}" -o "${dest}"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "${dest}" "${url}"
  else
    echo "error: curl or wget is required to download the Agent binary" >&2
    exit 1
  fi
}

getent group xrayr-agent >/dev/null 2>&1 || groupadd --system xrayr-agent
id xrayr-agent >/dev/null 2>&1 || useradd --system --gid xrayr-agent --shell /usr/sbin/nologin --home /var/lib/xrayr-agent --no-create-home xrayr-agent

install -d -m 0755 -o root -g root /etc/xrayr-agent /var/lib/xrayr-agent /var/log/xrayr-agent /var/lib/xrayr-agent/artifacts /var/lib/xrayr-agent/backups

TMP_BIN="$(mktemp)"
trap 'rm -f "${TMP_BIN}"' EXIT
download_to "${XRAYR_EMBED_AGENT_URL}" "${TMP_BIN}"
echo "${XRAYR_EMBED_AGENT_SHA256}  ${TMP_BIN}" | sha256sum -c -
install -m 0755 -o root -g root "${TMP_BIN}" /usr/local/bin/xrayr-agent
rm -f "${TMP_BIN}"
trap - EXIT

cat >"${CONFIG_PATH}" <<EOF
center:
  url: "${ENDPOINT}"
  node_id: ""
  node_secret: ""
  tls_verify: true
register_token: "${REGISTER_TOKEN}"
agent:
  heartbeat_interval: 30s
  report_interval: 60s
  command_poll_interval: 30s
  log_tail_limit: 300
  backup_dir: "/var/lib/xrayr-agent/backups"
  artifact_cache_dir: "/var/lib/xrayr-agent/artifacts"
  allow_commands:
    - PING
    - SYNC_STATUS
    - TAIL_LOG
    - CHECK_CONFIG
    - DISCOVER_XRAYR
    - STATUS_XRAYR
    - RESTART_XRAYR
    - APPLY_CONFIG
    - INSTALL_XRAYR
    - UPGRADE_XRAYR
xrayr:
  mode: "systemd"
  service_name: "xrayr"
  binary_path: "/usr/local/bin/XrayR"
  service_path: "/etc/systemd/system/xrayr.service"
  config_path: "/etc/XrayR/config.yml"
  config_backup_dir: "/etc/XrayR/backups"
  error_log_path: "/var/log/xrayr/error.log"
  access_log_path: "/var/log/xrayr/access.log"
  binary_search_paths:
    - "/usr/local/bin/XrayR"
    - "/usr/bin/XrayR"
    - "/opt/XrayR/XrayR"
  config_search_paths:
    - "/etc/XrayR/config.yml"
    - "/etc/XrayR/config.yaml"
  health_check:
    type: "systemd_process_port_log"
    timeout: "30s"
    ports: []
EOF
chown root:root "${CONFIG_PATH}"
chmod 0600 "${CONFIG_PATH}"

cat >/etc/systemd/system/xrayr-agent.service <<UNIT
[Unit]
Description=XrayR Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=xrayr-agent
Group=xrayr-agent
ExecStart=/usr/local/bin/xrayr-agent -config ${CONFIG_PATH}
Restart=always
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=/etc/xrayr-agent /etc/XrayR /var/log/xrayr /var/lib/xrayr-agent

[Install]
WantedBy=multi-user.target
UNIT

cat >/etc/sudoers.d/xrayr-agent <<'SUDO'
Defaults:xrayr-agent secure_path=/sbin:/bin:/usr/sbin:/usr/bin
xrayr-agent ALL=(root) NOPASSWD: /bin/systemctl restart xrayr
xrayr-agent ALL=(root) NOPASSWD: /bin/systemctl status xrayr
xrayr-agent ALL=(root) NOPASSWD: /bin/systemctl is-active xrayr
xrayr-agent ALL=(root) NOPASSWD: /bin/systemctl daemon-reload
SUDO
chmod 0440 /etc/sudoers.d/xrayr-agent
if command -v visudo >/dev/null 2>&1; then
  visudo -cf /etc/sudoers.d/xrayr-agent
fi

systemctl daemon-reload
systemctl enable xrayr-agent
if ! systemctl restart xrayr-agent; then
  echo "error: systemctl restart xrayr-agent failed (see: journalctl -u xrayr-agent -e)" >&2
  exit 1
fi

echo "xrayr-agent installed and started. Center endpoint: ${ENDPOINT}"
