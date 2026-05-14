package httpapi

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
)

//go:embed static/index.html
var indexHTML string

//go:embed static/app.js
var appJS string

//go:embed static/style.css
var styleCSS string

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func (s *Server) serveJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = w.Write([]byte(appJS))
}

func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	row := s.pool.QueryRow(r.Context(), `SELECT id, node_code, node_name, node_group_id, region, remark, install_state, manage_status, imported_config_hash, imported_backup_path,
		allow_config_apply, allow_restart, allow_install, allow_cleanup, allow_upgrade, last_seen_at, hostname, discovered_xrayr_version,
		agent_os, agent_arch, virtualization, public_ip, discovered_binary_path, discovered_config_path, discovered_service_name, pending_deploy_version_id
		FROM node WHERE id=$1`, id)
	var nid int64
	var ngid *int64
	var code, name string
	var region, remark *string
	var inst, ms, ich, ibp, host, dxv *string
	var aca, ar, ai, ac, au *bool
	var last *time.Time
	var aos, aarch, virt, pip, dbin, dcfg, dsvc *string
	var pending *int64
	if err := row.Scan(&nid, &code, &name, &ngid, &region, &remark, &inst, &ms, &ich, &ibp, &aca, &ar, &ai, &ac, &au, &last, &host, &dxv, &aos, &aarch, &virt, &pip, &dbin, &dcfg, &dsvc, &pending); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var ichid *int64
	if ich != nil && *ich != "" {
		_ = s.pool.QueryRow(r.Context(), `SELECT cv.id FROM config_version cv WHERE cv.content_sha256 = $1 AND cv.imported_from_node_id = $2 ORDER BY cv.id DESC LIMIT 1`, *ich, id).Scan(&ichid)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": nid, "node_code": code, "node_name": name, "node_group_id": ngid, "region": region, "remark": remark,
		"install_state": inst, "manage_status": ms,
		"imported_config_hash": ich, "imported_config_version_id": ichid, "imported_backup_path": ibp,
		"allow_config_apply": aca, "allow_restart": ar, "allow_install": ai, "allow_cleanup": ac, "allow_upgrade": au,
		"last_seen_at": last, "hostname": host, "discovered_xrayr_version": dxv,
		"agent_os": aos, "agent_arch": aarch, "virtualization": virt, "public_ip": pip,
		"discovered_binary_path": dbin, "discovered_config_path": dcfg, "discovered_service_name": dsvc,
		"pending_deploy_version_id": pending,
	})
}

func (s *Server) installScript(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("register_token")
	if tok == "" {
		http.Error(w, "query register_token required", http.StatusBadRequest)
		return
	}
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	th := hashToken(tok)
	var nid int64
	var exp time.Time
	var used *time.Time
	err := s.pool.QueryRow(r.Context(), `SELECT node_id, expires_at, used_at FROM node_install_token WHERE token_hash=$1`, th).Scan(&nid, &exp, &used)
	if err != nil || nid != id {
		http.Error(w, "invalid token for node", http.StatusForbidden)
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
	var code string
	if err := s.pool.QueryRow(r.Context(), `SELECT node_code FROM node WHERE id=$1`, id).Scan(&code); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	base := stringsTrimRightSlash(s.cfg.PublicBaseURL)
	agentURL, sha, dlSrc, errDl := s.cfg.ResolveAgentDownload()
	if errDl != nil {
		http.Error(w, errDl.Error(), http.StatusInternalServerError)
		return
	}
	_ = dlSrc
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
if [[ "$(id -u)" != "0" ]]; then echo "must run as root"; exit 1; fi
if ! command -v systemctl >/dev/null 2>&1; then echo "systemd required"; exit 1; fi
CENTER_URL=%q
NODE_ID=%d
REGISTER_TOKEN=%q
AGENT_URL=%q
AGENT_SHA256=%q
if [[ -z "$AGENT_URL" || -z "$AGENT_SHA256" ]]; then echo "Center 未配置 Agent 下载地址与校验和"; exit 1; fi
getent group xrayr-agent >/dev/null || groupadd --system xrayr-agent
id xrayr-agent >/dev/null 2>&1 || useradd --system --gid xrayr-agent --shell /usr/sbin/nologin --home /var/lib/xrayr-agent --no-create-home xrayr-agent
install -d -m 0755 -o root -g root /etc/xrayr-agent /var/lib/xrayr-agent /var/log/xrayr-agent /var/backups/xrayr-agent
TMP="$(mktemp)"
curl -fsSL "$AGENT_URL" -o "$TMP"
echo "$AGENT_SHA256  $TMP" | sha256sum -c -
install -m 0755 -o root -g root "$TMP" /usr/local/bin/xrayr-agent
rm -f "$TMP"
cat >/etc/xrayr-agent/agent.yml <<EOF
center:
  url: "${CENTER_URL}"
  node_id: ""
  node_secret: ""
  tls_verify: true
register_token: "${REGISTER_TOKEN}"
agent:
  heartbeat_interval: 30s
  report_interval: 60s
  command_poll_interval: 30s
  log_tail_limit: 300
  backup_dir: "/var/backups/xrayr-agent"
  artifact_cache_dir: "/var/lib/xrayr-agent/cache"
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
chown root:root /etc/xrayr-agent/agent.yml
chmod 0600 /etc/xrayr-agent/agent.yml
cat >/etc/systemd/system/xrayr-agent.service <<'UNIT'
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
cat >/etc/sudoers.d/xrayr-agent <<'SUDO'
Defaults:xrayr-agent secure_path=/sbin:/bin:/usr/sbin:/usr/bin
xrayr-agent ALL=(root) NOPASSWD: /bin/systemctl restart xrayr
xrayr-agent ALL=(root) NOPASSWD: /bin/systemctl status xrayr
xrayr-agent ALL=(root) NOPASSWD: /bin/systemctl is-active xrayr
xrayr-agent ALL=(root) NOPASSWD: /bin/systemctl daemon-reload
SUDO
chmod 0440 /etc/sudoers.d/xrayr-agent
visudo -cf /etc/sudoers.d/xrayr-agent
systemctl daemon-reload
systemctl enable xrayr-agent
systemctl restart xrayr-agent
echo "xrayr-agent 已安装。节点代码=%s NODE_ID=%d。脚本未修改 XrayR。"
`, base, id, tok, agentURL, sha, code, id)
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write([]byte(script))
}

func stringsTrimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func (s *Server) listDiscovery(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rows, err := s.pool.Query(r.Context(), `SELECT dr.id, dr.install_state, dr.config_hash, dr.error_tail, dr.created_at, dr.raw_json,
		n.manage_status, n.discovered_service_name, n.discovered_binary_path, n.discovered_config_path, n.imported_config_hash, n.imported_backup_path
		FROM discovery_report dr JOIN node n ON n.id = dr.node_id WHERE dr.node_id=$1 ORDER BY dr.id DESC LIMIT 50`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var rid int64
		var st, ch, tail *string
		var ct time.Time
		var raw []byte
		var ms, svc, dbin, dcfg, ich, ibp *string
		_ = rows.Scan(&rid, &st, &ch, &tail, &ct, &raw, &ms, &svc, &dbin, &dcfg, &ich, &ibp)
		var rj any
		_ = json.Unmarshal(raw, &rj)
		out = append(out, map[string]any{
			"id": rid, "install_state": st, "config_hash": ch, "error_tail": tail, "created_at": ct, "raw_json": rj,
			"manage_status": ms, "service_name": svc, "binary_path": dbin, "config_path": dcfg,
			"imported_config_hash": ich, "imported_backup_path": ibp, "last_report_time": ct,
		})
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) enableWritable(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	_, err := s.pool.Exec(r.Context(), `UPDATE node SET manage_status='MANAGED_WRITABLE', allow_config_apply=true, allow_restart=true, allow_install=true, allow_cleanup=true, allow_upgrade=true, updated_at=now() WHERE id=$1`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	detail, _ := json.Marshal(map[string]any{"node_id": id})
	_, _ = s.pool.Exec(r.Context(), `INSERT INTO operation_log(actor_admin_id, action, target_type, target_id, detail_json, success) VALUES ($1,'node.enable_manage','node',$2,$3::jsonb,true)`,
		adminID, fmt.Sprint(id), detail)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) agentRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RegisterToken string `json:"register_token"`
		Hostname      string `json:"hostname"`
		OS            string `json:"os"`
		Arch          string `json:"arch"`
		AgentVersion  string `json:"agent_version"`
		PublicIP      string `json:"public_ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RegisterToken == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	th := hashToken(req.RegisterToken)
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	var nid int64
	var used *time.Time
	var exp time.Time
	err = tx.QueryRow(r.Context(), `SELECT node_id, used_at, expires_at FROM node_install_token WHERE token_hash=$1 FOR UPDATE`, th).Scan(&nid, &used, &exp)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	if used != nil {
		http.Error(w, "token already used", http.StatusUnauthorized)
		return
	}
	if time.Now().After(exp) {
		http.Error(w, "token expired", http.StatusUnauthorized)
		return
	}
	secret := randomHex(32)
	_, err = tx.Exec(r.Context(), `UPDATE node SET node_hmac_key=$2, hostname=COALESCE(NULLIF($3,''), hostname), public_ip=COALESCE(NULLIF($4,''), public_ip), agent_version=$5,
		agent_os=COALESCE(NULLIF($6,''), agent_os), agent_arch=COALESCE(NULLIF($7,''), agent_arch), last_seen_at=now(), updated_at=now() WHERE id=$1`,
		nid, secret, req.Hostname, req.PublicIP, req.AgentVersion, req.OS, req.Arch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = tx.Exec(r.Context(), `UPDATE node_install_token SET used_at=now() WHERE token_hash=$1`, th)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"node_id": nid, "node_secret": secret})
}

func (s *Server) agentHeartbeat(w http.ResponseWriter, r *http.Request) {
	nid := r.Context().Value(ctxNodeID).(int64)
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	raw, _ := json.Marshal(body)
	_, _ = s.pool.Exec(r.Context(), `INSERT INTO node_heartbeat(node_id, uptime_sec, agent_version, xrayr_running, raw_json) VALUES ($1,$2,$3,$4,$5::jsonb)`,
		nid, body["uptime_sec"], body["agent_version"], body["xrayr_running"], raw)
	_, _ = s.pool.Exec(r.Context(), `UPDATE node SET last_seen_at=now() WHERE id=$1`, nid)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) agentMonitorReport(w http.ResponseWriter, r *http.Request) {
	nid := r.Context().Value(ctxNodeID).(int64)
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	raw, _ := json.Marshal(body)
	var cpu, mem, disk, l1, up *float64
	if v, ok := body["cpu_pct"].(float64); ok {
		cpu = &v
	}
	if v, ok := body["mem_pct"].(float64); ok {
		mem = &v
	}
	if v, ok := body["disk_pct"].(float64); ok {
		disk = &v
	}
	if v, ok := body["load1"].(float64); ok {
		l1 = &v
	}
	if v, ok := body["uptime_sec"].(float64); ok {
		x := v
		up = &x
	}
	_, err := s.pool.Exec(r.Context(), `INSERT INTO node_monitor_snapshot(node_id, cpu_pct, mem_pct, disk_pct, load1, uptime_sec, payload_json) VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb)`,
		nid, cpu, mem, disk, l1, up, raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) agentXrayrStatus(w http.ResponseWriter, r *http.Request) {
	nid := r.Context().Value(ctxNodeID).(int64)
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	var active *bool
	if v, ok := body["systemd_active"].(bool); ok {
		active = &v
	}
	ch, _ := body["config_hash"].(string)
	xv, _ := body["xrayr_version"].(string)
	tail, _ := body["error_tail"].(string)
	_, _ = s.pool.Exec(r.Context(), `INSERT INTO xrayr_status_snapshot(node_id, systemd_active, config_hash, xrayr_version, error_tail) VALUES ($1,$2,$3,$4,$5)`,
		nid, active, nullStrPtr(ch), nullStrPtr(xv), nullStrPtr(tail))
	w.WriteHeader(http.StatusNoContent)
}

func nullStrPtr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Server) agentDiscoveryReport(w http.ResponseWriter, r *http.Request) {
	nid := r.Context().Value(ctxNodeID).(int64)
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	raw, _ := json.Marshal(body)
	inst, _ := body["install_state"].(string)
	ch, _ := body["config_hash"].(string)
	tail, _ := body["error_tail"].(string)
	bin, _ := json.Marshal(body["binary_paths"])
	cfg, _ := json.Marshal(body["config_paths"])
	svc, _ := json.Marshal(body["service_status"])
	proc, _ := json.Marshal(body["process_status"])
	ver, _ := json.Marshal(body["version_info"])

	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	_, err = tx.Exec(r.Context(), `INSERT INTO discovery_report(node_id, install_state, binary_paths, config_paths, service_status, process_status, version_info, config_hash, error_tail, raw_json) VALUES ($1,$2,$3::jsonb,$4::jsonb,$5::jsonb,$6::jsonb,$7::jsonb,$8,$9,$10::jsonb)`,
		nid, inst, bin, cfg, svc, proc, ver, nullStrPtr(ch), nullStrPtr(tail), raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 更新 node 展示字段
	_, _ = tx.Exec(r.Context(), `UPDATE node SET install_state=$2, discovered_at=now(), last_discovery_error=NULL,
		discovered_binary_path=$3, discovered_config_path=$4, discovered_service_name=$5,
		discovered_xrayr_version=$6, discovered_xray_core_version=$7, imported_config_hash=COALESCE($8, imported_config_hash),
		imported_backup_path=COALESCE($9, imported_backup_path), updated_at=now() WHERE id=$1`,
		nid, inst, strFromMap(body, "discovered_binary_path"), strFromMap(body, "discovered_config_path"), strFromMap(body, "discovered_service_name"),
		strFromMap(body, "discovered_xrayr_version"), strFromMap(body, "discovered_xray_core_version"),
		strOrNil(body, "imported_config_hash"), strOrNil(body, "imported_backup_path"))

	if inst == "INSTALLED_RUNNING" {
		yamlStr, _ := body["config_yaml"].(string)
		if yamlStr != "" && ch != "" {
			var tid int64
			err = tx.QueryRow(r.Context(), `SELECT id FROM config_template WHERE name='__imported__' LIMIT 1`).Scan(&tid)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					err = tx.QueryRow(r.Context(), `INSERT INTO config_template(name, description) VALUES ('__imported__', 'system') RETURNING id`).Scan(&tid)
				}
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			verLabel := fmt.Sprintf("imported-%d-%d", nid, time.Now().Unix())
			_, _ = tx.Exec(r.Context(), `INSERT INTO config_version(template_id, version, content_yaml, content_sha256, source_type, imported_from_node_id) VALUES ($1,$2,$3,$4,'IMPORTED',$5)`,
				tid, verLabel, yamlStr, ch, nid)
			_, _ = tx.Exec(r.Context(), `UPDATE node SET manage_status='MANAGED_READONLY', imported_config_hash=$2, imported_backup_path=COALESCE($3, imported_backup_path), updated_at=now() WHERE id=$1`,
				nid, ch, strOrNil(body, "imported_backup_path"))
		} else {
			_, _ = tx.Exec(r.Context(), `UPDATE node SET manage_status='MANAGED_READONLY', updated_at=now() WHERE id=$1`, nid)
		}
	} else if inst == "INSTALLED_BROKEN" || inst == "SERVICE_ONLY" {
		_, _ = tx.Exec(r.Context(), `UPDATE node SET manage_status='REPAIR_REQUIRED', updated_at=now() WHERE id=$1`, nid)
	} else if inst == "NOT_INSTALLED" || inst == "CONFIG_ONLY" || inst == "BINARY_ONLY" || inst == "INSTALLED_STOPPED" || inst == "UNKNOWN" {
		_, _ = tx.Exec(r.Context(), `UPDATE node SET manage_status='DISCOVERED', updated_at=now() WHERE id=$1`, nid)
	}

	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func strFromMap(m map[string]any, k string) any {
	v, ok := m[k]
	if !ok || v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return s
}

func strOrNil(m map[string]any, k string) any {
	v, ok := m[k]
	if !ok {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return s
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
