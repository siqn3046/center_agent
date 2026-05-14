package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func newCommandUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], b[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], b[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], b[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], b[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], b[10:16])
	return string(dst)
}

func (s *Server) listCommands(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	rows, err := s.pool.Query(r.Context(), `SELECT command_id::text, command_type, status, expires_at, error_message, log_summary, result_json, created_at, started_at, finished_at
		FROM command_task WHERE node_id=$1 ORDER BY id DESC LIMIT 200`, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var cid, ctype, status string
		var exp time.Time
		var errm, logsum *string
		var res []byte
		var ca, st, fi *time.Time
		_ = rows.Scan(&cid, &ctype, &status, &exp, &errm, &logsum, &res, &ca, &st, &fi)
		var rj any
		if len(res) > 0 {
			_ = json.Unmarshal(res, &rj)
		}
		out = append(out, map[string]any{
			"command_id": cid, "command_type": ctype, "status": status, "expires_at": exp,
			"error_message": errm, "log_summary": logsum, "result_json": rj,
			"created_at": ca, "started_at": st, "finished_at": fi,
		})
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) opLog(ctx context.Context, adminID int64, action, targetType, targetID string, detail any, ok bool, errm string) {
	var dj []byte
	if detail != nil {
		dj, _ = json.Marshal(detail)
	}
	var em any
	if errm != "" {
		em = errm
	}
	_, _ = s.pool.Exec(ctx, `INSERT INTO operation_log(actor_admin_id, action, target_type, target_id, detail_json, success, error_message) VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7)`,
		adminID, action, targetType, targetID, dj, ok, em)
}

type nodePermRow struct {
	ManageStatus       string
	AllowRestart       bool
	AllowConfigApply   bool
	AllowUpgrade       bool
	AllowInstall       bool
	ImportedConfigHash *string
}

func (s *Server) loadNodePerms(ctx context.Context, nodeID int64) (*nodePermRow, error) {
	var r nodePermRow
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(manage_status,''), allow_restart, allow_config_apply, allow_upgrade, COALESCE(allow_install,false), imported_config_hash FROM node WHERE id=$1 AND disabled=false`,
		nodeID).Scan(&r.ManageStatus, &r.AllowRestart, &r.AllowConfigApply, &r.AllowUpgrade, &r.AllowInstall, &r.ImportedConfigHash)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func isWritable(ms string) bool {
	return ms == "MANAGED_WRITABLE"
}

func isReadonlyManaged(ms string) bool {
	return ms == "MANAGED_READONLY"
}

func (s *Server) assertCommandAllowed(ctx context.Context, cmd string, np *nodePermRow) error {
	readOnly := isReadonlyManaged(np.ManageStatus) || (!isWritable(np.ManageStatus) && np.ManageStatus != "DISCOVERED")
	switch cmd {
	case "STATUS_XRAYR":
		return nil
	case "RESTART_XRAYR":
		if readOnly || !isWritable(np.ManageStatus) || !np.AllowRestart {
			return fmt.Errorf("MANAGED_READONLY 或未 enable-writable / allow_restart 禁止执行 RESTART")
		}
	case "APPLY_CONFIG":
		if readOnly || !isWritable(np.ManageStatus) || !np.AllowConfigApply {
			return fmt.Errorf("MANAGED_READONLY 或未 enable-writable / allow_config_apply 禁止下发配置")
		}
	case "INSTALL_XRAYR":
		if readOnly || !isWritable(np.ManageStatus) || !np.AllowInstall {
			return fmt.Errorf("MANAGED_READONLY 或未 enable-writable / allow_install 禁止安装 XrayR")
		}
	case "UPGRADE_XRAYR":
		if readOnly || !isWritable(np.ManageStatus) || !np.AllowUpgrade {
			return fmt.Errorf("MANAGED_READONLY 或未 enable-writable / allow_upgrade 禁止升级 XrayR")
		}
	default:
		return fmt.Errorf("unknown command")
	}
	return nil
}

func dangerousCommand(cmd string) bool {
	switch cmd {
	case "RESTART_XRAYR", "APPLY_CONFIG", "INSTALL_XRAYR", "UPGRADE_XRAYR":
		return true
	default:
		return false
	}
}

func (s *Server) hasConcurrentDangerous(ctx context.Context, nodeID int64, cmd string) (bool, error) {
	if !dangerousCommand(cmd) {
		return false, nil
	}
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM command_task WHERE node_id=$1 AND command_type=$2 AND status IN ('PENDING','SENT','RUNNING')`,
		nodeID, cmd).Scan(&n)
	return n > 0, err
}

func (s *Server) enqueueCommand(ctx context.Context, adminID, nodeID int64, cmdType string, payload map[string]any, idem string) (string, error) {
	np, err := s.loadNodePerms(ctx, nodeID)
	if err != nil {
		return "", err
	}
	if err := s.assertCommandAllowed(ctx, cmdType, np); err != nil {
		return "", err
	}
	dup, err := s.hasConcurrentDangerous(ctx, nodeID, cmdType)
	if err != nil {
		return "", err
	}
	if dup {
		return "", fmt.Errorf("同节点同类型危险命令已在执行队列中，请等待完成")
	}
	if idem == "" {
		idem = fmt.Sprintf("%s-%d", cmdType, time.Now().UnixNano())
	}
	cid := newCommandUUID()
	exp := time.Now().Add(10 * time.Minute)
	payloadJSON, _ := json.Marshal(payload)
	_, err = s.pool.Exec(ctx, `INSERT INTO command_task(command_id, node_id, command_type, payload_json, idempotency_key, expires_at, status, created_by)
		VALUES ($1::uuid, $2, $3, $4::jsonb, $5, $6, 'PENDING', $7)`,
		cid, nodeID, cmdType, payloadJSON, idem, exp, adminID)
	if err != nil {
		return "", err
	}
	s.opLog(ctx, adminID, "command.enqueue", "node", fmt.Sprint(nodeID), map[string]any{"command_id": cid, "command_type": cmdType}, true, "")
	msg := map[string]any{
		"type":          "command_push",
		"command_id":    cid,
		"command_type":  cmdType,
		"payload":       payload,
		"expires_at":    exp.UTC().Format(time.RFC3339Nano),
	}
	if s.hub.sendJSON(nodeID, msg) {
		_, _ = s.pool.Exec(ctx, `UPDATE command_task SET status='SENT', started_at=COALESCE(started_at, now()), updated_at=now() WHERE command_id=$1::uuid`, cid)
	}
	return cid, nil
}

func (s *Server) flushPendingCommands(ctx context.Context, nodeID int64) {
	rows, err := s.pool.Query(ctx, `SELECT command_id::text, command_type, payload_json, expires_at FROM command_task WHERE node_id=$1 AND status='PENDING' ORDER BY id ASC`, nodeID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var cid, ctype string
		var payload []byte
		var exp time.Time
		if rows.Scan(&cid, &ctype, &payload, &exp) != nil {
			continue
		}
		var pay map[string]any
		_ = json.Unmarshal(payload, &pay)
		msg := map[string]any{
			"type":         "command_push",
			"command_id":   cid,
			"command_type": ctype,
			"payload":      pay,
			"expires_at":   exp.UTC().Format(time.RFC3339Nano),
		}
		if s.hub.sendJSON(nodeID, msg) {
			_, _ = s.pool.Exec(ctx, `UPDATE command_task SET status='SENT', started_at=COALESCE(started_at, now()), updated_at=now() WHERE command_id=$1::uuid`, cid)
		}
	}
}

func (s *Server) postCmdRestart(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	cid, err := s.enqueueCommand(r.Context(), adminID, id, "RESTART_XRAYR", map[string]any{}, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"command_id": cid})
}

func (s *Server) postCmdStatus(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	cid, err := s.enqueueCommand(r.Context(), adminID, id, "STATUS_XRAYR", map[string]any{}, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"command_id": cid})
}

func (s *Server) postCmdApplyConfig(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req struct {
		ConfigVersionID int64 `json:"config_version_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ConfigVersionID == 0 {
		http.Error(w, "config_version_id required", http.StatusBadRequest)
		return
	}
	cid, deployID, err := s.applyConfigDeployFlow(r.Context(), adminID, id, req.ConfigVersionID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"command_id": cid, "config_deploy_task_id": deployID})
}

func (s *Server) applyConfigDeployFlow(ctx context.Context, adminID, nodeID, configVersionID int64) (commandID string, deployID int64, err error) {
	var sha string
	err = s.pool.QueryRow(ctx, `SELECT content_sha256 FROM config_version WHERE id=$1`, configVersionID).Scan(&sha)
	if err != nil {
		return "", 0, fmt.Errorf("config version not found")
	}
	var before *string
	_ = s.pool.QueryRow(ctx, `SELECT imported_config_hash FROM node WHERE id=$1`, nodeID).Scan(&before)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback(ctx)

	err = tx.QueryRow(ctx, `INSERT INTO config_deploy_task(node_id, config_version_id, status, old_config_hash, new_config_hash, created_by)
		VALUES ($1,$2,'pending',$3,$4,$5) RETURNING id`,
		nodeID, configVersionID, before, sha, adminID).Scan(&deployID)
	if err != nil {
		return "", 0, err
	}

	cid := newCommandUUID()
	exp := time.Now().Add(15 * time.Minute)
	var targetPath *string
	_ = tx.QueryRow(ctx, `SELECT discovered_config_path FROM node WHERE id=$1`, nodeID).Scan(&targetPath)
	tp := ""
	if targetPath != nil {
		tp = *targetPath
	}
	payload := map[string]any{
		"config_deploy_task_id": deployID,
		"config_version_id":     configVersionID,
		"config_hash":           sha,
		"content_sha256":        sha,
		"config_yaml":           "",
		"yaml_path":             fmt.Sprintf("/api/agent/config-deploy/%d/yaml", deployID),
		"target_config_path":    tp,
	}
	payloadJSON, _ := json.Marshal(payload)
	idem := fmt.Sprintf("apply-%d-%d", nodeID, time.Now().UnixNano())
	_, err = tx.Exec(ctx, `INSERT INTO command_task(command_id, node_id, command_type, payload_json, idempotency_key, expires_at, status, created_by)
		VALUES ($1::uuid,$2,'APPLY_CONFIG',$3::jsonb,$4,$5,'PENDING',$6)`,
		cid, nodeID, payloadJSON, idem, exp, adminID)
	if err != nil {
		return "", 0, err
	}
	_, _ = tx.Exec(ctx, `UPDATE config_deploy_task SET related_command_id=$1::uuid, updated_at=now() WHERE id=$2`, cid, deployID)
	detail, _ := json.Marshal(map[string]any{"command_id": cid, "deploy_id": deployID})
	_, _ = tx.Exec(ctx, `INSERT INTO operation_log(actor_admin_id, action, target_type, target_id, detail_json, success) VALUES ($1,'command.enqueue','node',$2,$3::jsonb,true)`,
		adminID, fmt.Sprint(nodeID), detail)
	if cerr := tx.Commit(ctx); cerr != nil {
		return "", 0, cerr
	}
	msg := map[string]any{
		"type": "command_push", "command_id": cid, "command_type": "APPLY_CONFIG",
		"payload": payload, "expires_at": exp.UTC().Format(time.RFC3339Nano),
	}
	if s.hub.sendJSON(nodeID, msg) {
		_, _ = s.pool.Exec(ctx, `UPDATE command_task SET status='SENT', started_at=COALESCE(started_at, now()), updated_at=now() WHERE command_id=$1::uuid`, cid)
	}
	return cid, deployID, nil
}

func (s *Server) postCmdInstallXrayr(w http.ResponseWriter, r *http.Request) {
	s.postCmdInstallUpgrade(w, r, true)
}

func (s *Server) postCmdUpgradeXrayr(w http.ResponseWriter, r *http.Request) {
	s.postCmdInstallUpgrade(w, r, false)
}

func (s *Server) postCmdInstallUpgrade(w http.ResponseWriter, r *http.Request, install bool) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req struct {
		ArtifactID           int64  `json:"artifact_id"`
		InitialConfigYAML    string `json:"initial_config_yaml"`
		InitialConfigSha256  string `json:"initial_config_sha256"`
		OverwriteConfig      *bool  `json:"overwrite_config"`
		ConfigYAML           string `json:"config_yaml"`
		ConfigSha256         string `json:"config_sha256"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.ArtifactID <= 0 {
		http.Error(w, "artifact_id 必填", http.StatusBadRequest)
		return
	}
	var name, tos, ta, ver, sha, rel string
	var dis bool
	err := s.pool.QueryRow(r.Context(), `SELECT display_name, target_os, target_arch, COALESCE(version_label,''), sha256_hex, storage_relpath, disabled
		FROM xrayr_binary_artifact WHERE id=$1`, req.ArtifactID).Scan(&name, &tos, &ta, &ver, &sha, &rel, &dis)
	if err != nil || dis {
		http.Error(w, "制品不存在或已禁用", http.StatusBadRequest)
		return
	}
	var nos, narch, dbin, dcfg, dsvc *string
	if err := s.pool.QueryRow(r.Context(), `SELECT agent_os, agent_arch, discovered_binary_path, discovered_config_path, discovered_service_name FROM node WHERE id=$1 AND disabled=false`, id).Scan(&nos, &narch, &dbin, &dcfg, &dsvc); err != nil {
		http.Error(w, "节点不存在", http.StatusNotFound)
		return
	}
	if nos != nil && strings.TrimSpace(*nos) != "" && !strings.EqualFold(strings.TrimSpace(*nos), tos) {
		http.Error(w, "节点操作系统与制品 target_os 不匹配", http.StatusBadRequest)
		return
	}
	if narch != nil && strings.TrimSpace(*narch) != "" && !archMatchesAgent(strings.TrimSpace(*narch), ta) {
		http.Error(w, "节点架构与制品 target_arch 不匹配", http.StatusBadRequest)
		return
	}
	tb := "/usr/local/bin/XrayR"
	tc := "/etc/XrayR/config.yml"
	sn := "xrayr"
	if dbin != nil && strings.TrimSpace(*dbin) != "" {
		tb = strings.TrimSpace(*dbin)
	}
	if dcfg != nil && strings.TrimSpace(*dcfg) != "" {
		tc = strings.TrimSpace(*dcfg)
	}
	if dsvc != nil && strings.TrimSpace(*dsvc) != "" {
		sn = strings.TrimSuffix(strings.TrimSpace(*dsvc), ".service")
	}
	if sn == "" {
		sn = "xrayr"
	}
	ts := fmt.Sprintf("/etc/systemd/system/%s.service", sn)
	icy := strings.TrimSpace(req.InitialConfigYAML)
	if icy != "" {
		req.InitialConfigSha256 = strings.ToLower(strings.TrimSpace(req.InitialConfigSha256))
		if len(req.InitialConfigSha256) != 64 {
			http.Error(w, "initial_config_sha256 必须为 64 位十六进制", http.StatusBadRequest)
			return
		}
	}
	if !install && req.OverwriteConfig != nil && *req.OverwriteConfig {
		req.ConfigSha256 = strings.ToLower(strings.TrimSpace(req.ConfigSha256))
		if strings.TrimSpace(req.ConfigYAML) == "" || len(req.ConfigSha256) != 64 {
			http.Error(w, "overwrite_config 为 true 时必须提供 config_yaml 与 config_sha256", http.StatusBadRequest)
			return
		}
	}
	cmd := "UPGRADE_XRAYR"
	if install {
		cmd = "INSTALL_XRAYR"
	}
	payload := map[string]any{
		"artifact_id":             req.ArtifactID,
		"artifact_name":           name,
		"download_path":           fmt.Sprintf("/api/agent/artifacts/%d/download", req.ArtifactID),
		"sha256":                  sha,
		"target_os":               tos,
		"target_arch":             ta,
		"version_label":           nullStrOrPtr(ver),
		"service_name":            sn,
		"target_binary_path":      tb,
		"target_config_path":      tc,
		"target_service_path":     ts,
		"enable_service":          true,
		"restart_after_install":   true,
		"storage_relpath":         filepath.ToSlash(rel),
	}
	if icy != "" {
		payload["initial_config_yaml"] = icy
		payload["initial_config_sha256"] = req.InitialConfigSha256
	}
	if !install && req.OverwriteConfig != nil && *req.OverwriteConfig {
		payload["overwrite_config"] = true
		payload["config_yaml"] = req.ConfigYAML
		payload["config_sha256"] = req.ConfigSha256
	}
	cid, err := s.enqueueCommand(r.Context(), adminID, id, cmd, payload, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"command_id": cid})
}

func (s *Server) getConfigDeployYAML(w http.ResponseWriter, r *http.Request) {
	nid := r.Context().Value(ctxNodeID).(int64)
	tid, _ := strconv.ParseInt(chi.URLParam(r, "deployId"), 10, 64)
	var yaml string
	var nodeID int64
	err := s.pool.QueryRow(r.Context(), `SELECT d.node_id, v.content_yaml
		FROM config_deploy_task d JOIN config_version v ON v.id = d.config_version_id WHERE d.id=$1`, tid).Scan(&nodeID, &yaml)
	if err != nil || nodeID != nid {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write([]byte(yaml))
}

func (s *Server) postAgentCommandProgress(w http.ResponseWriter, r *http.Request) {
	nid := r.Context().Value(ctxNodeID).(int64)
	var req struct {
		CommandID string `json:"command_id"`
		Step      string `json:"step"`
		Message   string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CommandID == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	var cur []byte
	err := s.pool.QueryRow(r.Context(), `SELECT result_json FROM command_task WHERE command_id=$1::uuid AND node_id=$2`, req.CommandID, nid).Scan(&cur)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var root map[string]any
	if len(cur) > 0 {
		_ = json.Unmarshal(cur, &root)
	}
	if root == nil {
		root = map[string]any{}
	}
	var steps []any
	if arr, ok := root["progress"].([]any); ok {
		steps = arr
	}
	steps = append(steps, map[string]any{"t": time.Now().UTC().Format(time.RFC3339), "step": req.Step, "message": req.Message})
	root["progress"] = steps
	out, _ := json.Marshal(root)
	_, _ = s.pool.Exec(r.Context(), `UPDATE command_task SET status='RUNNING', result_json=$1::jsonb, updated_at=now(), started_at=COALESCE(started_at, now()) WHERE command_id=$2::uuid AND node_id=$3`,
		out, req.CommandID, nid)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) postAgentCommandResult(w http.ResponseWriter, r *http.Request) {
	nid := r.Context().Value(ctxNodeID).(int64)
	var req struct {
		CommandID    string         `json:"command_id"`
		Status       string         `json:"status"`
		LogSummary   string         `json:"log_summary"`
		ResultJSON   map[string]any `json:"result_json"`
		ErrorMessage string         `json:"error_message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.CommandID == "" {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	st := strings.ToUpper(strings.TrimSpace(req.Status))
	switch st {
	case "SUCCESS", "FAILED", "TIMEOUT", "CANCELED":
	default:
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	var curJSON []byte
	_ = s.pool.QueryRow(r.Context(), `SELECT COALESCE(result_json, '{}'::jsonb)::text FROM command_task WHERE command_id=$1::uuid AND node_id=$2`, req.CommandID, nid).Scan(&curJSON)
	merged := mergeCommandResultJSON(curJSON, req.ResultJSON)
	rj, _ := json.Marshal(merged)
	_, err := s.pool.Exec(r.Context(), `UPDATE command_task SET status=$1, log_summary=$2, result_json=$3::jsonb, error_message=$4, finished_at=now(), updated_at=now() WHERE command_id=$5::uuid AND node_id=$6`,
		st, nullStrOrPtr(req.LogSummary), rj, nullStrOrPtr(req.ErrorMessage), req.CommandID, nid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// APPLY_CONFIG: 同步 config_deploy_task
	var ctype string
	var payload []byte
	_ = s.pool.QueryRow(r.Context(), `SELECT command_type, payload_json FROM command_task WHERE command_id=$1::uuid`, req.CommandID).Scan(&ctype, &payload)
	if ctype == "APPLY_CONFIG" {
		var pay map[string]any
		_ = json.Unmarshal(payload, &pay)
		did := int64FromAny(pay["config_deploy_task_id"])
		if did > 0 {
			if st == "SUCCESS" {
				var after string
				if req.ResultJSON != nil {
					if v, ok := req.ResultJSON["after_config_hash"].(string); ok {
						after = v
					}
				}
				_, _ = s.pool.Exec(r.Context(), `UPDATE config_deploy_task SET status='success', new_config_hash=COALESCE($2, new_config_hash), backup_path=COALESCE($3, backup_path),
					error_message=NULL, progress_log=$4, finished_at=now(), updated_at=now() WHERE id=$1`,
					did, nullStrOrPtr(after), strFromResult(req.ResultJSON, "backup_path"), req.LogSummary)
			} else {
				_, _ = s.pool.Exec(r.Context(), `UPDATE config_deploy_task SET status='failed', error_message=$2, progress_log=$3, finished_at=now(), updated_at=now() WHERE id=$1`,
					did, req.ErrorMessage, req.LogSummary)
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func mergeCommandResultJSON(curJSON []byte, incoming map[string]any) map[string]any {
	var cur map[string]any
	_ = json.Unmarshal(curJSON, &cur)
	if cur == nil {
		cur = map[string]any{}
	}
	var prog any
	if p, ok := cur["progress"]; ok {
		prog = p
	}
	incomingHasProgress := false
	if incoming != nil {
		_, incomingHasProgress = incoming["progress"]
	}
	out := make(map[string]any)
	for k, v := range cur {
		out[k] = v
	}
	if incoming != nil {
		for k, v := range incoming {
			out[k] = v
		}
	}
	if !incomingHasProgress && prog != nil {
		out["progress"] = prog
	}
	return out
}

func strFromResult(m map[string]any, k string) any {
	if m == nil {
		return nil
	}
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

func nullStrOrPtr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func int64FromAny(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case json.Number:
		x, _ := t.Int64()
		return x
	case string:
		x, _ := strconv.ParseInt(t, 10, 64)
		return x
	default:
		return 0
	}
}

func (s *Server) createConfigVersion(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	var req struct {
		TemplateName string `json:"template_name"`
		Version      string `json:"version"`
		ContentYAML  string `json:"content_yaml"`
		DisplayName  string `json:"name"`
		Remark       string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ContentYAML == "" || req.Version == "" {
		http.Error(w, "version and content_yaml required", http.StatusBadRequest)
		return
	}
	tn := req.TemplateName
	if tn == "" {
		tn = "__manual__"
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())
	var tid int64
	err = tx.QueryRow(r.Context(), `SELECT id FROM config_template WHERE name=$1`, tn).Scan(&tid)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(r.Context(), `INSERT INTO config_template(name, description, created_by) VALUES ($1,'', $2) RETURNING id`, tn, adminID).Scan(&tid)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h := sha256SumHex([]byte(req.ContentYAML))
	var vid int64
	err = tx.QueryRow(r.Context(), `INSERT INTO config_version(template_id, version, display_name, remark, content_yaml, content_sha256, source_type, created_by) VALUES ($1,$2,$3,$4,$5,$6,'MANUAL',$7) RETURNING id`,
		tid, req.Version, nullStrOrPtr(req.DisplayName), nullStrOrPtr(req.Remark), req.ContentYAML, h, adminID).Scan(&vid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_, _ = tx.Exec(r.Context(), `INSERT INTO operation_log(actor_admin_id, action, target_type, target_id, detail_json, success) VALUES ($1,'config_version.create','config_version',$2,$3::jsonb,true)`,
		adminID, fmt.Sprint(vid), mustJSON(map[string]any{"version": req.Version}))
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": vid, "content_sha256": h})
}
