package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func (s *Server) getDashboardSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var total, online int64
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM node WHERE disabled=false`).Scan(&total)
	thr := time.Now().Add(-2 * time.Minute)
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM node WHERE disabled=false AND last_seen_at >= $1`, thr).Scan(&online)
	var regions int64
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT NULLIF(trim(region),'')) FROM node WHERE disabled=false`).Scan(&regions)

	rows, err := s.pool.Query(ctx, `
WITH ranked AS (
  SELECT node_id, id, created_at, payload_json,
    ROW_NUMBER() OVER (PARTITION BY node_id ORDER BY id DESC) AS rn
  FROM node_monitor_snapshot
)
SELECT node_id, id, created_at, payload_json FROM ranked WHERE rn <= 2`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type snap struct {
		id   int64
		t    time.Time
		pay  map[string]any
	}
	byNode := make(map[int64][]snap)
	for rows.Next() {
		var nid, sid int64
		var ct time.Time
		var raw []byte
		if rows.Scan(&nid, &sid, &ct, &raw) != nil {
			continue
		}
		var pay map[string]any
		_ = json.Unmarshal(raw, &pay)
		byNode[nid] = append(byNode[nid], snap{id: sid, t: ct, pay: pay})
	}
	var sumUp, sumDown float64
	var totalIn, totalOut float64
	for _, snaps := range byNode {
		if len(snaps) >= 1 {
			in1, out1 := netBytes(snaps[0].pay)
			totalIn += in1
			totalOut += out1
		}
		if len(snaps) >= 2 {
			in1, out1 := netBytes(snaps[0].pay)
			in2, out2 := netBytes(snaps[1].pay)
			dt := snaps[0].t.Sub(snaps[1].t).Seconds()
			if dt > 0.5 {
				sumUp += math.Max(0, (out1-out2)/dt)
				sumDown += math.Max(0, (in1-in2)/dt)
			}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"total_nodes":       total,
		"online_nodes":      online,
		"offline_nodes":     total - online,
		"region_count":      regions,
		"total_traffic_up":   totalOut,
		"total_traffic_down": totalIn,
		"total_speed_up":     sumUp,
		"total_speed_down":   sumDown,
		"server_time":        time.Now().UTC().Format(time.RFC3339),
	})
}

func netBytes(pay map[string]any) (in, out float64) {
	if pay == nil {
		return 0, 0
	}
	if v, ok := pay["net_bytes_in"].(float64); ok {
		in = v
	}
	if v, ok := pay["net_bytes_out"].(float64); ok {
		out = v
	}
	return in, out
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	rows, err := s.pool.Query(ctx, `
SELECT n.id, n.node_code, n.node_name, n.region, n.public_ip, n.manage_status, n.install_state, n.last_seen_at,
  n.agent_os, n.agent_arch, n.hostname, n.discovered_service_name,
  m.cpu_pct, m.mem_pct, m.disk_pct, m.payload_json, m.created_at AS monitor_at,
  h.xrayr_running,
  x.systemd_active, x.created_at AS xrayr_at
FROM node n
LEFT JOIN LATERAL (
  SELECT cpu_pct, mem_pct, disk_pct, payload_json, created_at FROM node_monitor_snapshot WHERE node_id=n.id ORDER BY id DESC LIMIT 1
) m ON true
LEFT JOIN LATERAL (
  SELECT xrayr_running FROM node_heartbeat WHERE node_id=n.id ORDER BY id DESC LIMIT 1
) h ON true
LEFT JOIN LATERAL (
  SELECT systemd_active, created_at FROM xrayr_status_snapshot WHERE node_id=n.id ORDER BY id DESC LIMIT 1
) x ON true
WHERE n.disabled=false AND ($1 = '' OR n.node_name ILIKE '%'||$1||'%' OR n.node_code ILIKE '%'||$1||'%' OR COALESCE(n.public_ip,'') ILIKE '%'||$1||'%' OR COALESCE(n.region,'') ILIKE '%'||$1||'%' OR COALESCE(n.agent_os,'') ILIKE '%'||$1||'%')
ORDER BY n.id DESC`, q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	thr := time.Now().Add(-2 * time.Minute)
	var out []map[string]any
	for rows.Next() {
		var id int64
		var code, name string
		var region, pip, ms *string
		var inst *string
		var last *time.Time
		var aos, aarch, host, dsvc *string
		var cpu, mem, disk *float64
		var payload []byte
		var monAt *time.Time
		var xrun *bool
		var sysActive *bool
		var xrayAt *time.Time
		_ = rows.Scan(&id, &code, &name, &region, &pip, &ms, &inst, &last, &aos, &aarch, &host, &dsvc, &cpu, &mem, &disk, &payload, &monAt, &xrun, &sysActive, &xrayAt)
		online := last != nil && last.After(thr)
		var pay map[string]any
		_ = json.Unmarshal(payload, &pay)
		inB, outB := netBytes(pay)
		su, sd := s.nodeNetSpeed(ctx, id)
		xr := false
		if xrun != nil {
			xr = *xrun
		}
		if sysActive != nil && *sysActive {
			xr = true
		}
		out = append(out, map[string]any{
			"id":                 id,
			"name":               name,
			"node_code":          code,
			"region":             region,
			"public_ip":          pip,
			"online_status":      map[bool]string{true: "online", false: "offline"}[online],
			"os":                 aos,
			"arch":               aarch,
			"uptime_sec":         floatFromPay(pay, "uptime_sec"),
			"cpu_percent":        derefFloat(cpu),
			"ram_percent":        derefFloat(mem),
			"disk_percent":       derefFloat(disk),
			"net_up_speed":       su,
			"net_down_speed":     sd,
			"traffic_up":         outB,
			"traffic_down":       inB,
			"xrayr_running":      xr,
			"install_state":      inst,
			"manage_status":      ms,
			"last_heartbeat_at":  last,
			"hostname":           host,
			"service_name":       dsvc,
			"last_monitor_at":    monAt,
			"last_xrayr_status_at": xrayAt,
		})
	}
	_ = json.NewEncoder(w).Encode(out)
}

func derefFloat(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func floatFromPay(pay map[string]any, k string) float64 {
	if pay == nil {
		return 0
	}
	v, ok := pay[k].(float64)
	if !ok {
		return 0
	}
	return v
}

func (s *Server) nodeNetSpeed(ctx context.Context, nodeID int64) (up, down float64) {
	rows, err := s.pool.Query(ctx, `SELECT created_at, payload_json FROM node_monitor_snapshot WHERE node_id=$1 ORDER BY id DESC LIMIT 2`, nodeID)
	if err != nil {
		return 0, 0
	}
	defer rows.Close()
	var snaps []struct {
		t   time.Time
		pay map[string]any
	}
	for rows.Next() {
		var ct time.Time
		var raw []byte
		if rows.Scan(&ct, &raw) != nil {
			continue
		}
		var pay map[string]any
		_ = json.Unmarshal(raw, &pay)
		snaps = append(snaps, struct {
			t   time.Time
			pay map[string]any
		}{ct, pay})
	}
	if len(snaps) < 2 {
		return 0, 0
	}
	in1, out1 := netBytes(snaps[0].pay)
	in2, out2 := netBytes(snaps[1].pay)
	dt := snaps[0].t.Sub(snaps[1].t).Seconds()
	if dt < 0.5 {
		return 0, 0
	}
	return math.Max(0, (out1-out2)/dt), math.Max(0, (in1-in2)/dt)
}

func (s *Server) listMonitorSnapshots(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rows, err := s.pool.Query(r.Context(), `SELECT id, cpu_pct, mem_pct, disk_pct, load1, uptime_sec, payload_json, created_at
		FROM node_monitor_snapshot WHERE node_id=$1 ORDER BY id DESC LIMIT 144`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var sid int64
		var cpu, mem, disk, l1, up *float64
		var raw []byte
		var ct time.Time
		_ = rows.Scan(&sid, &cpu, &mem, &disk, &l1, &up, &raw, &ct)
		var pay any
		_ = json.Unmarshal(raw, &pay)
		out = append(out, map[string]any{
			"id": sid, "cpu_pct": cpu, "mem_pct": mem, "disk_pct": disk, "load1": l1, "uptime_sec": up,
			"payload_json": pay, "created_at": ct,
		})
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) listNodeConfigVersions(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rows, err := s.pool.Query(r.Context(), `
SELECT DISTINCT v.id, v.version, v.display_name, v.remark, v.content_sha256, v.source_type, v.imported_from_node_id, v.created_at,
  (SELECT username FROM admin_user u WHERE u.id = v.created_by) AS created_by_name
FROM config_version v
LEFT JOIN config_deploy_task d ON d.config_version_id = v.id AND d.node_id = $1
WHERE v.imported_from_node_id = $1 OR d.node_id = $1
ORDER BY v.id DESC`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var vid int64
		var ver, dname, remark, sha, stype *string
		var imp *int64
		var ct time.Time
		var creator *string
		_ = rows.Scan(&vid, &ver, &dname, &remark, &sha, &stype, &imp, &ct, &creator)
		out = append(out, map[string]any{
			"id": vid, "version": ver, "display_name": dname, "remark": remark, "content_sha256": sha,
			"source_type": stype, "imported_from_node_id": imp, "created_at": ct, "created_by": creator,
		})
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) getConfigVersionByID(w http.ResponseWriter, r *http.Request) {
	vid, _ := strconv.ParseInt(chi.URLParam(r, "versionId"), 10, 64)
	var ver, dname, remark, sha, stype string
	var imp *int64
	var ct time.Time
	var creator *string
	err := s.pool.QueryRow(r.Context(), `
SELECT v.version, COALESCE(v.display_name,''), COALESCE(v.remark,''), v.content_sha256, v.source_type, v.imported_from_node_id, v.created_at,
  (SELECT username FROM admin_user u WHERE u.id = v.created_by) FROM config_version v WHERE v.id=$1`, vid).Scan(&ver, &dname, &remark, &sha, &stype, &imp, &ct, &creator)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var yaml string
	_ = s.pool.QueryRow(r.Context(), `SELECT content_yaml FROM config_version WHERE id=$1`, vid).Scan(&yaml)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": vid, "version": ver, "display_name": dname, "remark": remark, "content_sha256": sha, "source_type": stype,
		"imported_from_node_id": imp, "created_at": ct, "created_by": creator, "content_yaml": yaml,
	})
}

func (s *Server) postNodeConfigVersion(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	nid, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req struct {
		Name        string `json:"name"`
		Remark      string `json:"remark"`
		ConfigYAML  string `json:"config_yaml"`
		VersionSlug string `json:"version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ConfigYAML == "" {
		http.Error(w, "config_yaml required", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	verSlug := req.VersionSlug
	if verSlug == "" {
		verSlug = fmt.Sprintf("ui-%d", time.Now().Unix())
	}
	tplName := fmt.Sprintf("__node_%d__", nid)
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())
	var tid int64
	err = tx.QueryRow(r.Context(), `SELECT id FROM config_template WHERE name=$1`, tplName).Scan(&tid)
	if err != nil {
		err = tx.QueryRow(r.Context(), `INSERT INTO config_template(name, description, created_by) VALUES ($1,'per-node', $2) RETURNING id`, tplName, adminID).Scan(&tid)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h := sha256SumHex([]byte(req.ConfigYAML))
	var vid int64
	err = tx.QueryRow(r.Context(), `INSERT INTO config_version(template_id, version, display_name, remark, content_yaml, content_sha256, source_type, imported_from_node_id, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,'NODE_UI',$7,$8) RETURNING id`,
		tid, verSlug, req.Name, req.Remark, req.ConfigYAML, h, nid, adminID).Scan(&vid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, _ = tx.Exec(r.Context(), `INSERT INTO operation_log(actor_admin_id, action, target_type, target_id, detail_json, success) VALUES ($1,'config_version.create_node','config_version',$2,$3::jsonb,true)`,
		adminID, fmt.Sprint(vid), mustJSON(map[string]any{"node_id": nid}))
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": vid, "content_sha256": h})
}

func (s *Server) postNodeConfigVersionDeploy(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	nid, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	vid, _ := strconv.ParseInt(chi.URLParam(r, "versionId"), 10, 64)
	cid, deployID, err := s.applyConfigDeployFlow(r.Context(), adminID, nid, vid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"command_id": cid, "config_deploy_task_id": deployID})
}

func (s *Server) patchNodeSettings(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req struct {
		NodeName               *string `json:"node_name"`
		Region                 *string `json:"region"`
		NodeGroupID            *int64  `json:"node_group_id"`
		Remark                 *string `json:"remark"`
		ManageStatus           *string `json:"manage_status"`
		AllowRestart           *bool   `json:"allow_restart"`
		AllowInstall           *bool   `json:"allow_install"`
		AllowConfigApply       *bool   `json:"allow_config_apply"`
		AllowUpgrade           *bool   `json:"allow_upgrade"`
		PendingDeployVersionID *int64  `json:"pending_deploy_version_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	_, err := s.pool.Exec(r.Context(), `UPDATE node SET
		node_name = COALESCE($2, node_name),
		region = COALESCE($3, region),
		node_group_id = COALESCE($4, node_group_id),
		remark = COALESCE($5, remark),
		manage_status = COALESCE($6, manage_status),
		allow_restart = COALESCE($7, allow_restart),
		allow_config_apply = COALESCE($8, allow_config_apply),
		allow_upgrade = COALESCE($9, allow_upgrade),
		pending_deploy_version_id = COALESCE($10, pending_deploy_version_id),
		allow_install = COALESCE($11, allow_install),
		updated_at = now()
		WHERE id=$1`,
		id, req.NodeName, req.Region, req.NodeGroupID, req.Remark, req.ManageStatus,
		req.AllowRestart, req.AllowConfigApply, req.AllowUpgrade, req.PendingDeployVersionID, req.AllowInstall)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.opLog(r.Context(), adminID, "node.patch", "node", fmt.Sprint(id), req, true, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listConfigVersionsGlobal(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT v.id, v.version, v.display_name, v.remark, v.content_sha256, v.source_type, v.imported_from_node_id, v.created_at FROM config_version v ORDER BY v.id DESC LIMIT 200`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var vid int64
		var ver, dname, remark, sha, stype *string
		var imp *int64
		var ct time.Time
		_ = rows.Scan(&vid, &ver, &dname, &remark, &sha, &stype, &imp, &ct)
		out = append(out, map[string]any{
			"id": vid, "version": ver, "display_name": dname, "remark": remark, "content_sha256": sha,
			"source_type": stype, "imported_from_node_id": imp, "created_at": ct,
		})
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) listConfigDeployTasks(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rows, err := s.pool.Query(r.Context(), `SELECT id, config_version_id, status, old_config_hash, new_config_hash, backup_path, error_message, progress_log, created_at, finished_at, related_command_id::text
		FROM config_deploy_task WHERE node_id=$1 ORDER BY id DESC LIMIT 50`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var did, cvid int64
		var st string
		var oldh, newh, bp, errm, plog, rcmd *string
		var ca time.Time
		var fi *time.Time
		_ = rows.Scan(&did, &cvid, &st, &oldh, &newh, &bp, &errm, &plog, &ca, &fi, &rcmd)
		out = append(out, map[string]any{
			"id": did, "config_version_id": cvid, "status": st, "before_config_hash": oldh, "after_config_hash": newh,
			"backup_path": bp, "error_message": errm, "progress_log": plog, "created_at": ca, "finished_at": fi, "related_command_id": rcmd,
		})
	}
	_ = json.NewEncoder(w).Encode(out)
}
