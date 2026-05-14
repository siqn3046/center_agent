package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reInstallNodeServiceName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	reInstallNodeNics         = regexp.MustCompile(`^[A-Za-z0-9_.,-]*$`)
)

type installNodeOpts struct {
	DisableRemoteControl bool
	DisableAutoUpdate    bool
	InsecureTLS          bool
	GithubProxy          string
	InstallDir           string
	ServiceName          string
	AgentOnly            bool
	ForceReplaceXrayR    bool
	KeepConfig           bool
	UseCenterConfig      bool
	Verbose              bool
	IncludeNICs          string
	ExcludeNICs          string
	MountPoints          string
	Interval             int
}

func badInstallNodeParam(msg string) error {
	return fmt.Errorf("%s", msg)
}

func validateInstallNodeQuery(q url.Values) (installNodeOpts, error) {
	var o installNodeOpts
	allowed := map[string]struct{}{
		"token": {}, "os": {}, "disable_remote_control": {}, "disable_auto_update": {}, "insecure_tls": {},
		"github_proxy": {}, "install_dir": {}, "service_name": {}, "agent_only": {}, "force_replace_xrayr": {},
		"keep_config": {}, "use_center_config": {}, "verbose": {}, "include_nics": {}, "exclude_nics": {},
		"mount_points": {}, "interval": {},
	}
	for k := range q {
		if _, ok := allowed[k]; !ok {
			return o, badInstallNodeParam("unsupported query parameter: " + k)
		}
	}
	tok := strings.TrimSpace(q.Get("token"))
	if tok == "" {
		return o, badInstallNodeParam("query token required")
	}
	osv := strings.ToLower(strings.TrimSpace(q.Get("os")))
	if osv == "" {
		osv = "linux"
	}
	if osv != "linux" {
		if osv == "windows" || osv == "macos" || osv == "darwin" {
			return o, badInstallNodeParam("暂未支持")
		}
		return o, badInstallNodeParam("unsupported os")
	}

	parseBool := func(key string, def bool) (bool, error) {
		v := q.Get(key)
		if v == "" {
			return def, nil
		}
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return false, badInstallNodeParam("invalid boolean for " + key)
		}
	}
	var err error
	if o.DisableRemoteControl, err = parseBool("disable_remote_control", false); err != nil {
		return o, err
	}
	if o.DisableAutoUpdate, err = parseBool("disable_auto_update", false); err != nil {
		return o, err
	}
	if o.InsecureTLS, err = parseBool("insecure_tls", false); err != nil {
		return o, err
	}
	if o.AgentOnly, err = parseBool("agent_only", false); err != nil {
		return o, err
	}
	if o.ForceReplaceXrayR, err = parseBool("force_replace_xrayr", false); err != nil {
		return o, err
	}
	if o.KeepConfig, err = parseBool("keep_config", false); err != nil {
		return o, err
	}
	if o.UseCenterConfig, err = parseBool("use_center_config", false); err != nil {
		return o, err
	}
	if o.Verbose, err = parseBool("verbose", false); err != nil {
		return o, err
	}

	o.GithubProxy = strings.TrimSpace(q.Get("github_proxy"))
	if o.GithubProxy != "" {
		if err := validateGithubProxyPrefix(o.GithubProxy); err != nil {
			return o, err
		}
	}

	installDir := strings.TrimSpace(q.Get("install_dir"))
	if installDir == "" {
		installDir = "/opt/xrayr"
	}
	if err := validateAbsoluteSafePath(installDir, false); err != nil {
		return o, err
	}
	o.InstallDir = installDir

	svc := strings.TrimSpace(q.Get("service_name"))
	if svc == "" {
		svc = "XrayR"
	}
	if !reInstallNodeServiceName.MatchString(svc) {
		return o, badInstallNodeParam("invalid service_name")
	}
	o.ServiceName = svc

	inc := strings.TrimSpace(q.Get("include_nics"))
	if inc != "" && !reInstallNodeNics.MatchString(inc) {
		return o, badInstallNodeParam("invalid include_nics")
	}
	o.IncludeNICs = inc

	exc := strings.TrimSpace(q.Get("exclude_nics"))
	if exc == "" {
		exc = "lo,docker0,br-"
	}
	if !reInstallNodeNics.MatchString(exc) {
		return o, badInstallNodeParam("invalid exclude_nics")
	}
	o.ExcludeNICs = exc

	mp := strings.TrimSpace(q.Get("mount_points"))
	if mp == "" {
		mp = "/"
	}
	if err := validateMountPointsList(mp); err != nil {
		return o, err
	}
	o.MountPoints = mp

	iv := strings.TrimSpace(q.Get("interval"))
	if iv == "" {
		o.Interval = 10
	} else {
		n, e := strconv.Atoi(iv)
		if e != nil || n < 5 || n > 300 {
			return o, badInstallNodeParam("interval must be integer 5-300")
		}
		o.Interval = n
	}

	return o, nil
}

func validateAbsoluteSafePath(s string, allowCommaList bool) error {
	if s == "" || s[0] != '/' {
		return badInstallNodeParam("path must be absolute")
	}
	forbidden := ";&|`$<>\n\r\\"
	for _, ch := range s {
		if strings.ContainsRune(forbidden, ch) {
			return badInstallNodeParam("path contains forbidden character")
		}
	}
	if allowCommaList {
		for _, part := range strings.Split(s, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			if err := validateAbsoluteSafePath(p, false); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

func validateMountPointsList(s string) error {
	parts := strings.Split(s, ",")
	for _, part := range parts {
		p := strings.TrimSpace(part)
		if p == "" {
			return badInstallNodeParam("empty mount_points segment")
		}
		if err := validateAbsoluteSafePath(p, false); err != nil {
			return err
		}
	}
	return nil
}

func validateGithubProxyPrefix(s string) error {
	if strings.ContainsAny(s, "\n\r`$;&|<>\\") {
		return badInstallNodeParam("invalid github_proxy")
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return badInstallNodeParam("github_proxy must be http(s) URL with host")
	}
	return nil
}

func joinAgentDownloadURL(proxyPrefix, agentURL string) string {
	proxyPrefix = strings.TrimSpace(proxyPrefix)
	if proxyPrefix == "" {
		return agentURL
	}
	return strings.TrimSuffix(proxyPrefix, "/") + "/" + agentURL
}

// installNodePublicScript GET /api/public/install-node.sh
func (s *Server) installNodePublicScript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	opts, err := validateInstallNodeQuery(r.URL.Query())
	if err != nil {
		msg := err.Error()
		if msg == "暂未支持" {
			http.Error(w, msg, http.StatusNotImplemented)
			return
		}
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	tok := strings.TrimSpace(r.URL.Query().Get("token"))
	th := hashToken(tok)
	var nid int64
	var exp time.Time
	var used *time.Time
	err = s.pool.QueryRow(r.Context(), `SELECT node_id, expires_at, used_at FROM node_install_token WHERE token_hash=$1 AND revoked_at IS NULL`, th).Scan(&nid, &exp, &used)
	if err != nil || nid <= 0 {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}
	if used != nil {
		http.Error(w, "token already used", http.StatusForbidden)
		return
	}
	if time.Now().After(exp) {
		http.Error(w, "token expired", http.StatusForbidden)
		return
	}
	var nodeCode string
	if err := s.pool.QueryRow(r.Context(), `SELECT node_code FROM node WHERE id=$1 AND disabled=false`, nid).Scan(&nodeCode); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_ = nodeCode

	centerBase := stringsTrimRightSlash(s.publicInstallBaseURL(r))
	agentURL, sha, _, errDl := s.cfg.ResolveAgentDownload()
	if errDl != nil {
		http.Error(w, errDl.Error(), http.StatusInternalServerError)
		return
	}
	agentFetchURL := joinAgentDownloadURL(opts.GithubProxy, agentURL)
	if !strings.HasPrefix(agentFetchURL, "http://") && !strings.HasPrefix(agentFetchURL, "https://") {
		http.Error(w, "invalid agent download URL", http.StatusInternalServerError)
		return
	}

	detail, _ := json.Marshal(map[string]any{"node_id": nid, "has_proxy": opts.GithubProxy != ""})
	_, _ = s.pool.Exec(r.Context(), `INSERT INTO operation_log(actor_admin_id, action, target_type, target_id, detail_json, success) VALUES (NULL,'node.public_install_script','node',$1,$2::jsonb,true)`,
		fmt.Sprint(nid), detail)

	yamlBody := buildInstallNodeAgentYAML(centerBase, tok, opts)

	script := buildInstallNodeShell(centerBase, nid, tok, agentFetchURL, strings.ToLower(strings.TrimSpace(sha)), yamlBody, opts)

	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(script))
}

func buildInstallNodeAgentYAML(centerURL, registerToken string, o installNodeOpts) string {
	tlsVerify := "true"
	if o.InsecureTLS {
		tlsVerify = "false"
	}
	dr := "false"
	if o.DisableRemoteControl {
		dr = "true"
	}
	du := "false"
	if o.DisableAutoUpdate {
		du = "true"
	}
	// 扩展字段：当前 xrayr-agent 可能忽略未声明键；monitor 供后续采集逻辑使用。
	return fmt.Sprintf(`center:
  url: %q
  node_id: ""
  node_secret: ""
  tls_verify: %s
register_token: %q
install_dir: %q
service_name: %q
agent:
  heartbeat_interval: 30s
  report_interval: 60s
  command_poll_interval: 30s
  log_tail_limit: 300
  backup_dir: "/var/backups/xrayr-agent"
  artifact_cache_dir: "/var/lib/xrayr-agent/cache"
  disable_remote_control: %s
  disable_auto_update: %s
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
  service_name: %q
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
monitor:
  interval: %d
  include_nics: %q
  exclude_nics: %q
  mount_points: %q
`,
		centerURL, tlsVerify, registerToken, o.InstallDir, o.ServiceName, dr, du, o.ServiceName,
		o.Interval, o.IncludeNICs, o.ExcludeNICs, o.MountPoints)
}

func buildInstallNodeShell(centerBase string, nodeID int64, registerToken, agentURL, agentSHA256, yamlBody string, o installNodeOpts) string {
	curlExtra := ""
	if o.InsecureTLS {
		curlExtra = " -k"
	}
	verboseX := ""
	if o.Verbose {
		verboseX = "set -x\n"
	}

	head := fmt.Sprintf(`#!/usr/bin/env bash
%s%s
echo "XrayR Center node installer"
if [[ "$(id -u)" != "0" ]]; then echo "must run as root" >&2; exit 1; fi
if [[ "$(uname -s)" != "Linux" ]]; then echo "Linux required" >&2; exit 1; fi
if ! command -v systemctl >/dev/null 2>&1; then echo "systemd required" >&2; exit 1; fi
ARCH="$(uname -m)"
if [[ "$ARCH" != "x86_64" && "$ARCH" != "amd64" && "$ARCH" != "aarch64" && "$ARCH" != "arm64" ]]; then echo "unsupported arch: $ARCH" >&2; exit 1; fi
CENTER_URL=%q
NODE_ID=%d
REGISTER_TOKEN=%q
AGENT_URL=%q
AGENT_SHA256=%q
INSTALL_DIR=%q
SERVICE_NAME=%q
AGENT_ONLY=%d
FORCE_REPLACE=%d
USE_CENTER_CONFIG=%d
CURL_EXTRA=%q
HAS_XRAYR=0
if command -v XrayR >/dev/null 2>&1; then HAS_XRAYR=1; fi
if [[ -f /usr/local/bin/XrayR ]]; then HAS_XRAYR=1; fi
if [[ -f /etc/XrayR/config.yml ]]; then HAS_XRAYR=1; fi
if systemctl status "$SERVICE_NAME" >/dev/null 2>&1; then HAS_XRAYR=1; fi
if systemctl cat "$SERVICE_NAME.service" >/dev/null 2>&1; then HAS_XRAYR=1; fi
NEED_XRAYR_REPLACE=0
if [[ $AGENT_ONLY -eq 1 ]]; then
  NEED_XRAYR_REPLACE=0
elif [[ $HAS_XRAYR -eq 1 && $FORCE_REPLACE -eq 1 ]]; then
  NEED_XRAYR_REPLACE=1
else
  NEED_XRAYR_REPLACE=0
fi
if [[ $NEED_XRAYR_REPLACE -eq 1 ]]; then
  echo "XrayR artifact is not configured" >&2
  exit 1
fi
if [[ $USE_CENTER_CONFIG -eq 1 ]]; then
  echo "Note: use_center_config requires config deploy via Center after agent registers; skipping automatic fetch." >&2
fi
STAMP="$(date +%%Y%%m%%d%%H%%M%%S)"
BACKUP_ROOT="/opt/xrayr-backup/$STAMP"
if [[ $FORCE_REPLACE -eq 1 && $HAS_XRAYR -eq 1 ]]; then
  install -d -m 0755 -o root -g root "$BACKUP_ROOT"
  for f in /usr/local/bin/XrayR /etc/XrayR/config.yml "/etc/systemd/system/${SERVICE_NAME}.service"; do
    if [[ -e "$f" ]]; then install -p -m 0644 "$f" "$BACKUP_ROOT/" 2>/dev/null || cp -a "$f" "$BACKUP_ROOT/" || true; fi
  done
fi
getent group xrayr-agent >/dev/null || groupadd --system xrayr-agent
id xrayr-agent >/dev/null 2>&1 || useradd --system --gid xrayr-agent --shell /usr/sbin/nologin --home /var/lib/xrayr-agent --no-create-home xrayr-agent
install -d -m 0755 -o root -g root /etc/xrayr-agent /var/lib/xrayr-agent /var/log/xrayr-agent /var/backups/xrayr-agent /etc/XrayR /var/log/xrayr
TMP="$(mktemp)"
curl -fsSL${CURL_EXTRA} "$AGENT_URL" -o "$TMP"
echo "$AGENT_SHA256  $TMP" | sha256sum -c -
install -m 0755 -o root -g root "$TMP" /usr/local/bin/xrayr-agent
rm -f "$TMP"
cat >/etc/xrayr-agent/agent.yml <<'AGENTYAML'
`,
		verboseX,
		"set -euo pipefail\n",
		centerBase, nodeID, registerToken, agentURL, agentSHA256, o.InstallDir, o.ServiceName,
		bool01(o.AgentOnly), bool01(o.ForceReplaceXrayR), bool01(o.UseCenterConfig),
		curlExtra,
	)

	mid := yamlBody + "\nAGENTYAML\n"

	tail := fmt.Sprintf(`chown root:root /etc/xrayr-agent/agent.yml
chmod 0600 /etc/xrayr-agent/agent.yml
UNIT_PATH="/etc/systemd/system/xrayr-agent.service"
cat >"$UNIT_PATH" <<'UNIT'
[Unit]
Description=XrayR Agent
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
User=xrayr-agent
Group=xrayr-agent
ExecStart=/usr/local/bin/xrayr-agent -config /etc/xrayr-agent/agent.yml
Restart=always
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=/etc/xrayr-agent /etc/XrayR /var/log/xrayr /var/lib/xrayr-agent /var/backups/xrayr-agent
[Install]
WantedBy=multi-user.target
UNIT
SUDO_PATH="/etc/sudoers.d/xrayr-agent"
cat >"$SUDO_PATH" <<SUDOEND
Defaults:xrayr-agent secure_path=/sbin:/bin:/usr/sbin:/usr/bin
xrayr-agent ALL=(root) NOPASSWD: /bin/systemctl restart %s
xrayr-agent ALL=(root) NOPASSWD: /bin/systemctl status %s
xrayr-agent ALL=(root) NOPASSWD: /bin/systemctl is-active %s
xrayr-agent ALL=(root) NOPASSWD: /bin/systemctl daemon-reload
SUDOEND
chmod 0440 "$SUDO_PATH"
visudo -cf "$SUDO_PATH"
systemctl daemon-reload
systemctl enable --now xrayr-agent
echo "xrayr-agent installed. NODE_ID=$NODE_ID"
`, o.ServiceName, o.ServiceName, o.ServiceName)

	return head + mid + tail
}

func bool01(b bool) int {
	if b {
		return 1
	}
	return 0
}
