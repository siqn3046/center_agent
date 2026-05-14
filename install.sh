#!/usr/bin/env bash
# XrayR node one-click install from this repository (source build).
# Official XrayR-release install is discontinued; use this script instead.
set -euo pipefail

REPO="${XRAYR_INSTALL_REPO:-https://github.com/siqn3046/center_agent.git}"
BRANCH="${XRAYR_INSTALL_BRANCH:-}"
INSTALL_PATH="${XRAYR_INSTALL_PATH:-/usr/local/bin/XrayR}"

usage() {
  echo "Usage: curl ... | sudo bash   or   sudo bash install.sh" >&2
  echo "Environment:" >&2
  echo "  XRAYR_INSTALL_REPO    Git clone URL (default: ${REPO})" >&2
  echo "  XRAYR_INSTALL_BRANCH  Optional branch (default: remote default branch)" >&2
  echo "  XRAYR_INSTALL_PATH    Output binary path (default: ${INSTALL_PATH})" >&2
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

if [[ "$(id -u)" != "0" ]]; then
  echo "error: run as root to install to ${INSTALL_PATH}" >&2
  exit 1
fi

if ! command -v git >/dev/null 2>&1; then
  echo "error: git is required" >&2
  exit 1
fi
if ! command -v go >/dev/null 2>&1; then
  echo "error: Go toolchain is required (see repository go.mod; suggest Go 1.26+). Install Go, then re-run this script." >&2
  exit 1
fi

have="$(go env GOVERSION 2>/dev/null || echo unknown)"
if [[ "${have}" == unknown ]]; then
  echo "error: could not read Go version (go env GOVERSION)" >&2
  exit 1
fi
if [[ "${have}" != devel* ]]; then
  need="go1.26"
  if [[ "$(printf '%s\n%s' "${need}" "${have}" | sort -V | head -n1)" != "${need}" ]]; then
    echo "error: need Go >= 1.26, got ${have}" >&2
    exit 1
  fi
fi

TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

clone_args=(--depth 1)
if [[ -n "${BRANCH}" ]]; then
  clone_args+=(-b "${BRANCH}")
fi

echo "==> Cloning ${REPO} ..."
git clone "${clone_args[@]}" "${REPO}" "${TMP}/src"
cd "${TMP}/src"

echo "==> Building XrayR -> ${INSTALL_PATH} ..."
go build -trimpath -ldflags="-s -w" -o "${INSTALL_PATH}" .

echo "==> Installed: ${INSTALL_PATH}"
"${INSTALL_PATH}" version 2>/dev/null || true
echo "Done. Configure /etc/XrayR/config.yml and systemd unit per documentation."
