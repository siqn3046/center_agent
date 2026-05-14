#!/usr/bin/env bash
# XrayR Center 傻瓜式一键安装向导（Ubuntu / Debian，需 root）
# UTF-8
set -euo pipefail

REPO_URL_DEFAULT="https://github.com/siqn3046/center_agent.git"
BRANCH_DEFAULT="main"
INSTALL_DIR_DEFAULT="/opt/xrayr-center"
PORT_DEFAULT="8080"

REPO_URL="${REPO_URL_DEFAULT}"
BRANCH="${BRANCH_DEFAULT}"
INSTALL_DIR="${INSTALL_DIR_DEFAULT}"
PUBLIC_URL=""
PORT="${PORT_DEFAULT}"
AGENT_URL=""
AGENT_SHA=""
ASSUME_YES=0
START_CENTER=1

usage() {
  cat <<'EOF'
XrayR Center 一键安装向导

用法:
  sudo bash install-center.sh
  sudo bash install-center.sh --public-url http://1.2.3.4:8080 --port 8080 \
    --agent-url https://example.com/xrayr-agent-linux-amd64 \
    --agent-sha256 <64位hex> --yes

参数:
  --repo-url URL       仓库地址（默认 https://github.com/siqn3046/center_agent.git）
  --branch NAME        分支（默认 main）
  --install-dir PATH   安装根目录（默认 /opt/xrayr-center）
  --public-url URL     Center 对外访问地址（必填）
  --port N             端口（默认 8080）
  --agent-url URL      Agent 二进制下载地址（必填）
  --agent-sha256 HEX   Agent sha256，64 位十六进制（必填）
  --yes                非交互；缺少必填项则退出
  -h, --help           显示本帮助
EOF
}

log() { echo "[install-center] $*"; }
die() { echo "[install-center] error: $*" >&2; exit 1; }

require_root() {
  if [[ "$(id -u)" != "0" ]]; then
    die "请使用 root 执行，例如: sudo bash install-center.sh"
  fi
}

require_linux() {
  if [[ "$(uname -s)" != "Linux" ]]; then
    die "仅支持 Linux"
  fi
}

check_os_supported() {
  if [[ ! -f /etc/os-release ]]; then
    die "无法识别系统（缺少 /etc/os-release），仅支持 Ubuntu / Debian"
  fi
  # shellcheck source=/dev/null
  source /etc/os-release
  local id_lc="${ID,,}"
  local like_lc="${ID_LIKE:-}"
  like_lc="${like_lc,,}"
  if [[ "${id_lc}" != "ubuntu" && "${id_lc}" != "debian" ]] && [[ "${like_lc}" != *"debian"* && "${like_lc}" != *"ubuntu"* ]]; then
    die "暂仅支持 Ubuntu / Debian 系发行版（当前 ID=${ID:-?}）。其它系统请改用手动 Docker 安装文档。"
  fi
}

trim_slash() {
  local s="$1"
  while [[ "${s}" == */ ]]; do s="${s%/}"; done
  echo -n "${s}"
}

is_hex64() {
  local x="$1"
  [[ "${#x}" -eq 64 ]] && [[ "${x}" =~ ^[0-9a-fA-F]{64}$ ]]
}

normalize_sha_lower() {
  echo -n "$1" | tr '[:upper:]' '[:lower:]'
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --repo-url)
        [[ -n "${2:-}" ]] || die "--repo-url 需要参数"
        REPO_URL="$2"
        shift 2
        ;;
      --branch)
        [[ -n "${2:-}" ]] || die "--branch 需要参数"
        BRANCH="$2"
        shift 2
        ;;
      --install-dir)
        [[ -n "${2:-}" ]] || die "--install-dir 需要参数"
        INSTALL_DIR="$2"
        shift 2
        ;;
      --public-url)
        [[ -n "${2:-}" ]] || die "--public-url 需要参数"
        PUBLIC_URL="$2"
        shift 2
        ;;
      --port)
        [[ -n "${2:-}" ]] || die "--port 需要参数"
        PORT="$2"
        shift 2
        ;;
      --agent-url)
        [[ -n "${2:-}" ]] || die "--agent-url 需要参数"
        AGENT_URL="$2"
        shift 2
        ;;
      --agent-sha256)
        [[ -n "${2:-}" ]] || die "--agent-sha256 需要参数"
        AGENT_SHA="$2"
        shift 2
        ;;
      --yes) ASSUME_YES=1; shift ;;
      -h|--help) usage; exit 0 ;;
      *) die "未知参数: $1（使用 --help）" ;;
    esac
  done
}

prompt_required() {
  local var_name="$1" prompt_text="$2"
  local val=""
  while true; do
    read -r -p "${prompt_text}" val || true
    val="$(echo -n "${val}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
    if [[ -n "${val}" ]]; then
      printf -v "${var_name}" '%s' "${val}"
      return 0
    fi
    echo "该项不能为空，请重新输入。"
  done
}

prompt_port() {
  local val=""
  while true; do
    read -r -p "请输入 Center 端口，默认 ${PORT_DEFAULT}：" val || true
    val="$(echo -n "${val}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
    if [[ -z "${val}" ]]; then
      PORT="${PORT_DEFAULT}"
      return 0
    fi
    if [[ "${val}" =~ ^[0-9]+$ ]] && [[ "${val}" -ge 1 ]] && [[ "${val}" -le 65535 ]]; then
      PORT="${val}"
      return 0
    fi
    echo "端口必须是 1-65535 的数字。"
  done
}

prompt_start_now() {
  local ans=""
  read -r -p "是否立即启动 Center？[Y/n]：" ans || true
  ans="$(echo -n "${ans}" | tr '[:upper:]' '[:lower:]')"
  if [[ -z "${ans}" || "${ans}" == "y" || "${ans}" == "yes" ]]; then
    START_CENTER=1
  elif [[ "${ans}" == "n" || "${ans}" == "no" ]]; then
    START_CENTER=0
  else
    START_CENTER=1
  fi
}

ensure_packages() {
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y ca-certificates curl gnupg git openssl >/dev/null
  else
    die "未找到 apt-get，无法自动安装依赖"
  fi
}

ensure_docker() {
  if command -v docker >/dev/null 2>&1; then
    log "Docker 已安装，跳过 get.docker.com"
  else
    log "正在安装 Docker …"
    curl -fsSL https://get.docker.com | sh
  fi
  systemctl enable docker >/dev/null 2>&1 || true
  systemctl start docker >/dev/null 2>&1 || true
  if ! docker compose version >/dev/null 2>&1; then
    log "正在安装 docker-compose-plugin …"
    apt-get install -y docker-compose-plugin
  fi
}

prepare_repo_dir() {
  local center_sub="${INSTALL_DIR}/xrayr-center"
  if [[ ! -d "${INSTALL_DIR}" ]]; then
    log "克隆仓库到 ${INSTALL_DIR} …"
    git clone --depth 1 -b "${BRANCH}" "${REPO_URL}" "${INSTALL_DIR}"
  elif [[ -d "${INSTALL_DIR}/.git" ]]; then
    log "更新已有仓库 ${INSTALL_DIR} …"
    git -C "${INSTALL_DIR}" fetch --all --prune
    if ! git -C "${INSTALL_DIR}" checkout "${BRANCH}"; then
      die "无法切换到分支 ${BRANCH}"
    fi
    git -C "${INSTALL_DIR}" pull --ff-only || git -C "${INSTALL_DIR}" pull
  else
    if [[ "${ASSUME_YES}" == "1" ]]; then
      die "目录 ${INSTALL_DIR} 已存在且不是 git 仓库，请先清空或更换 --install-dir"
    fi
    read -r -p "目录 ${INSTALL_DIR} 已存在但不是 git 仓库。是否删除该目录下全部内容并重新克隆？[y/N]：" ans || true
    ans="$(echo -n "${ans}" | tr '[:upper:]' '[:lower:]')"
    if [[ "${ans}" != "y" && "${ans}" != "yes" ]]; then
      die "已取消"
    fi
    rm -rf "${INSTALL_DIR:?}"/*
    log "克隆仓库到 ${INSTALL_DIR} …"
    git clone --depth 1 -b "${BRANCH}" "${REPO_URL}" "${INSTALL_DIR}"
  fi
  if [[ ! -d "${center_sub}" ]]; then
    die "未找到 ${center_sub}，请确认仓库结构包含 xrayr-center 子目录"
  fi
}

backup_env_if_needed() {
  local envf="$1"
  if [[ ! -f "${envf}" ]]; then
    return 0
  fi
  local bak="${envf}.bak.$(date +%Y%m%d%H%M%S)"
  if [[ "${ASSUME_YES}" == "1" ]]; then
    cp -a "${envf}" "${bak}"
    log "已备份现有 .env -> ${bak}"
    return 0
  fi
  read -r -p ".env 已存在，是否覆盖（会先备份）？[y/N]：" ans || true
  ans="$(echo -n "${ans}" | tr '[:upper:]' '[:lower:]')"
  if [[ "${ans}" != "y" && "${ans}" != "yes" ]]; then
    die "已取消（保留现有 .env）"
  fi
  cp -a "${envf}" "${bak}"
  log "已备份现有 .env -> ${bak}"
}

write_env_file() {
  local envf="$1"
  local pg_pass="$2"
  local jwt_sec="$3"
  umask 077
  {
    printf 'CENTER_PORT=%s\n' "${PORT}"
    printf 'CENTER_PUBLIC_BASE_URL=%s\n' "${PUBLIC_URL}"
    printf '%s\n' "POSTGRES_DB=xrayr_center"
    printf '%s\n' "POSTGRES_USER=xrayr"
    printf 'POSTGRES_PASSWORD=%s\n' "${pg_pass}"
    printf 'CENTER_JWT_SECRET=%s\n' "${jwt_sec}"
    printf 'CENTER_AGENT_DOWNLOAD_URL=%s\n' "${AGENT_URL}"
    printf 'CENTER_AGENT_SHA256=%s\n' "${AGENT_SHA}"
    printf '%s\n' "CENTER_AGENT_VERSION="
    printf '%s\n' "CENTER_AGENT_BINARY_NAME=xrayr-agent"
  } >"${envf}"
  chmod 600 "${envf}" || true
}

validate_noninteractive() {
  [[ -n "${PUBLIC_URL}" ]] || die "--public-url 必填"
  [[ -n "${AGENT_URL}" ]] || die "--agent-url 必填"
  [[ -n "${AGENT_SHA}" ]] || die "--agent-sha256 必填"
  AGENT_SHA="$(normalize_sha_lower "${AGENT_SHA}")"
  is_hex64 "${AGENT_SHA}" || die "--agent-sha256 须为 64 位十六进制"
  [[ "${PORT}" =~ ^[0-9]+$ ]] || die "--port 须为数字"
  [[ "${PORT}" -ge 1 && "${PORT}" -le 65535 ]] || die "--port 须在 1-65535"
  PUBLIC_URL="$(trim_slash "${PUBLIC_URL}")"
}

interactive_collect() {
  prompt_required PUBLIC_URL "请输入 Center 对外访问地址，例如 http://1.2.3.4:8080："
  PUBLIC_URL="$(trim_slash "${PUBLIC_URL}")"
  prompt_port
  prompt_required AGENT_URL "请输入 Agent 二进制下载地址："
  prompt_required AGENT_SHA "请输入 Agent 二进制 sha256（64 位十六进制）："
  AGENT_SHA="$(normalize_sha_lower "${AGENT_SHA}")"
  if ! is_hex64 "${AGENT_SHA}"; then
    die "sha256 格式不正确，须为 64 位十六进制"
  fi
  prompt_start_now
}

main() {
  parse_args "$@"
  require_root
  require_linux
  check_os_supported

  if [[ "${ASSUME_YES}" == "1" ]]; then
    validate_noninteractive
    START_CENTER=1
  else
    interactive_collect
  fi

  ensure_packages
  ensure_docker

  prepare_repo_dir

  local center_sub="${INSTALL_DIR}/xrayr-center"
  local envf="${center_sub}/.env"

  local pg_pass jwt_sec
  pg_pass="$(openssl rand -base64 24 | tr -d '\n' | tr '/+' '_-')"
  jwt_sec="$(openssl rand -hex 32)"

  backup_env_if_needed "${envf}"
  write_env_file "${envf}" "${pg_pass}" "${jwt_sec}"

  if [[ "${START_CENTER}" != "1" ]]; then
    log "已按选择跳过启动。请稍后执行:"
    echo "  cd ${center_sub} && docker compose up -d --build"
    cat <<EOF

配置文件已写入（请妥善保存，勿泄露）：
${envf}
EOF
    exit 0
  fi

  log "正在启动 Center（docker compose）…"
  if ! (cd "${center_sub}" && docker compose up -d --build); then
    echo "[install-center] docker compose 启动失败，最近日志：" >&2
    (cd "${center_sub}" && docker compose logs --tail=120 xrayr-center) 2>&1 || true
    die "请检查上方日志与 ${envf} 配置"
  fi

  (cd "${center_sub}" && docker compose ps) || log "警告: docker compose ps 执行异常"

  cat <<EOF

========================================
XrayR Center 安装完成。

访问地址：
${PUBLIC_URL}

常用命令：
cd ${center_sub}
docker compose ps
docker compose logs -f xrayr-center
docker compose restart xrayr-center
docker compose down

配置文件（内含数据库密码与 JWT，请勿泄露）：
${envf}
请妥善保存该文件；本输出不重复打印密钥。

下一步：
1. 打开 Center 后台（默认 admin / admin123，登录后请立即改密）。
2. 创建节点。
3. 复制页面生成的 Agent 一键安装命令。
4. 到目标 VPS 执行 Agent 安装命令。
========================================
EOF
}

main "$@"
