package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const artifactMaxUploadBytes = 512 << 20

func (s *Server) resolveXrayrArtifactFile(rel string) (string, error) {
	if s.cfg.ArtifactDir == "" {
		return "", fmt.Errorf("CENTER_ARTIFACT_DIR 未配置")
	}
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || rel == "." || strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") {
		return "", fmt.Errorf("非法 storage_relpath")
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." || seg == "" {
			return "", fmt.Errorf("非法 storage_relpath")
		}
	}
	base, err := filepath.Abs(filepath.Clean(s.cfg.ArtifactDir))
	if err != nil {
		return "", fmt.Errorf("非法配置目录")
	}
	full, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(rel)))
	if err != nil {
		return "", fmt.Errorf("非法路径")
	}
	if !strings.HasPrefix(full, base+string(os.PathSeparator)) && full != base {
		return "", fmt.Errorf("路径越界")
	}
	return full, nil
}

func fileSHA256Hex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, artifactMaxUploadBytes)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range strings.ToLower(s) {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return false
	}
	return true
}

func artifactPathToken(s string, max int) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > max {
		return "", fmt.Errorf("非法路径片段")
	}
	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '_' {
			continue
		}
		return "", fmt.Errorf("路径片段含非法字符")
	}
	if strings.Contains(s, "..") {
		return "", fmt.Errorf("非法路径片段")
	}
	return s, nil
}

func normalizeArtifactArch(a string) (string, error) {
	a = strings.ToLower(strings.TrimSpace(a))
	switch a {
	case "amd64", "x86_64":
		return "amd64", nil
	case "arm64", "aarch64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("arch 仅支持 amd64 或 arm64")
	}
}

func artifactJSONRow(id int64, name, tos, ta, ver, sha, rel string, dis bool, size int64, ct time.Time) map[string]any {
	en := !dis
	return map[string]any{
		"id": id, "name": name, "version": nullStrOrPtr(ver), "os": tos, "arch": ta,
		"sha256": sha, "size_bytes": size, "enabled": en,
		"storage_relpath": rel, "download_path": fmt.Sprintf("/api/agent/artifacts/%d/download", id),
		"created_at": ct.UTC().Format(time.RFC3339Nano),
		// 兼容旧字段
		"display_name": name, "target_os": tos, "target_arch": ta, "version_label": nullStrOrPtr(ver),
	}
}

func (s *Server) listXrayrArtifacts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT id, display_name, target_os, target_arch, COALESCE(version_label,''), sha256_hex, storage_relpath, disabled, COALESCE(size_bytes,0), created_at
		FROM xrayr_binary_artifact ORDER BY id DESC`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var name, tos, ta, ver, sha, rel string
		var dis bool
		var sz int64
		var ct time.Time
		if rows.Scan(&id, &name, &tos, &ta, &ver, &sha, &rel, &dis, &sz, &ct) != nil {
			continue
		}
		out = append(out, artifactJSONRow(id, name, tos, ta, ver, sha, rel, dis, sz, ct))
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) getXrayrArtifact(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var name, tos, ta, ver, sha, rel string
	var dis bool
	var sz int64
	var ct time.Time
	err := s.pool.QueryRow(r.Context(), `SELECT display_name, target_os, target_arch, COALESCE(version_label,''), sha256_hex, storage_relpath, disabled, COALESCE(size_bytes,0), created_at
		FROM xrayr_binary_artifact WHERE id=$1`, id).Scan(&name, &tos, &ta, &ver, &sha, &rel, &dis, &sz, &ct)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	row := artifactJSONRow(id, name, tos, ta, ver, sha, rel, dis, sz, ct)
	_ = json.NewEncoder(w).Encode(row)
}

func pickStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if m == nil {
			break
		}
		v, ok := m[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if strings.TrimSpace(t) != "" {
				return strings.TrimSpace(t)
			}
		}
	}
	return ""
}

func (s *Server) postXrayrArtifact(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	name := pickStr(raw, "name", "display_name")
	ver := pickStr(raw, "version", "version_label")
	osStr := strings.ToLower(pickStr(raw, "os", "target_os"))
	archIn := pickStr(raw, "arch", "target_arch")
	if archIn == "" {
		archIn = "amd64"
	}
	sha := strings.ToLower(pickStr(raw, "sha256", "sha256_hex"))
	filename := pickStr(raw, "filename")
	relManual := strings.TrimSpace(pickStr(raw, "storage_relpath"))

	if name == "" {
		http.Error(w, "name 必填", http.StatusBadRequest)
		return
	}
	if ver == "" {
		http.Error(w, "version 必填", http.StatusBadRequest)
		return
	}
	if osStr != "linux" {
		http.Error(w, "os 仅支持 linux", http.StatusBadRequest)
		return
	}
	archNorm, err := normalizeArtifactArch(archIn)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !isHexSHA256(sha) {
		http.Error(w, "sha256 须为 64 位十六进制", http.StatusBadRequest)
		return
	}
	var rel string
	if relManual != "" {
		rel = filepath.ToSlash(relManual)
	} else {
		if filename == "" {
			http.Error(w, "未提供 storage_relpath 时必须填写 filename", http.StatusBadRequest)
			return
		}
		if filepath.IsAbs(filename) || strings.Contains(filename, "..") || strings.ContainsAny(filename, `/\`) {
			http.Error(w, "filename 非法", http.StatusBadRequest)
			return
		}
		fnTok, err := artifactPathToken(filepath.Base(filename), 256)
		if err != nil || fnTok != filepath.Base(filename) {
			http.Error(w, "filename 只能为安全文件名", http.StatusBadRequest)
			return
		}
		vTok, err := artifactPathToken(ver, 64)
		if err != nil {
			http.Error(w, "version 片段非法: "+err.Error(), http.StatusBadRequest)
			return
		}
		rel = filepath.ToSlash(filepath.Join(vTok, "linux-"+archNorm, fnTok))
	}

	full, err := s.resolveXrayrArtifactFile(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		http.Error(w, "制品文件不存在或不可读", http.StatusBadRequest)
		return
	}
	if st.Size() > artifactMaxUploadBytes {
		http.Error(w, "文件过大", http.StatusBadRequest)
		return
	}
	got, err := fileSHA256Hex(full)
	if err != nil {
		http.Error(w, "校验文件失败", http.StatusInternalServerError)
		return
	}
	if got != sha {
		http.Error(w, "sha256 与磁盘文件不一致", http.StatusBadRequest)
		return
	}

	disabled := false
	if v, ok := raw["enabled"]; ok {
		switch t := v.(type) {
		case bool:
			disabled = !t
		case string:
			disabled = strings.EqualFold(strings.TrimSpace(t), "false") || t == "0"
		}
	}

	var id int64
	err = s.pool.QueryRow(r.Context(), `INSERT INTO xrayr_binary_artifact(display_name, target_os, target_arch, version_label, sha256_hex, storage_relpath, disabled, size_bytes)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8) RETURNING id`,
		name, osStr, archNorm, ver, sha, rel, disabled, st.Size()).Scan(&id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.opLog(r.Context(), adminID, "artifact.create", "artifact", fmt.Sprint(id), map[string]any{"name": name}, true, "")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func (s *Server) patchXrayrArtifact(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if id <= 0 {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		http.Error(w, "需要 enabled 布尔字段", http.StatusBadRequest)
		return
	}
	dis := !*req.Enabled
	tag, err := s.pool.Exec(r.Context(), `UPDATE xrayr_binary_artifact SET disabled=$2, updated_at=now() WHERE id=$1`, id, dis)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if tag.RowsAffected() == 0 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.opLog(r.Context(), adminID, "artifact.patch", "artifact", fmt.Sprint(id), map[string]any{"enabled": *req.Enabled}, true, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) postXrayrArtifactUpload(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	if s.cfg.ArtifactDir == "" {
		http.Error(w, "CENTER_ARTIFACT_DIR 未配置", http.StatusBadRequest)
		return
	}
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "multipart 解析失败", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	name := strings.TrimSpace(r.FormValue("name"))
	ver := strings.TrimSpace(r.FormValue("version"))
	osStr := strings.ToLower(strings.TrimSpace(r.FormValue("os")))
	archIn := strings.TrimSpace(r.FormValue("arch"))
	if archIn == "" {
		archIn = "amd64"
	}
	shaExpect := strings.ToLower(strings.TrimSpace(r.FormValue("sha256")))

	if name == "" || ver == "" {
		http.Error(w, "name、version 必填", http.StatusBadRequest)
		return
	}
	if osStr == "" {
		osStr = "linux"
	}
	if osStr != "linux" {
		http.Error(w, "os 仅支持 linux", http.StatusBadRequest)
		return
	}
	archNorm, err := normalizeArtifactArch(archIn)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if shaExpect != "" && !isHexSHA256(shaExpect) {
		http.Error(w, "sha256 须为 64 位十六进制", http.StatusBadRequest)
		return
	}

	file, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "缺少 file 字段", http.StatusBadRequest)
		return
	}
	defer file.Close()
	baseName := filepath.Base(hdr.Filename)
	fnTok, err := artifactPathToken(baseName, 256)
	if err != nil || fnTok != baseName || baseName == "" || strings.Contains(baseName, "..") {
		http.Error(w, "非法文件名", http.StatusBadRequest)
		return
	}

	vTok, err := artifactPathToken(ver, 64)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rel := filepath.ToSlash(filepath.Join(vTok, "linux-"+archNorm, fnTok))
	full, err := s.resolveXrayrArtifactFile(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		http.Error(w, "创建目录失败", http.StatusInternalServerError)
		return
	}
	tmp := full + ".uploading"
	out, err := os.Create(tmp)
	if err != nil {
		http.Error(w, "无法写入临时文件", http.StatusInternalServerError)
		return
	}
	nw, err := io.Copy(out, io.LimitReader(file, artifactMaxUploadBytes))
	_ = out.Close()
	if err != nil {
		_ = os.Remove(tmp)
		http.Error(w, "写入失败", http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		http.Error(w, "保存失败", http.StatusInternalServerError)
		return
	}
	got, err := fileSHA256Hex(full)
	if err != nil {
		_ = os.Remove(full)
		http.Error(w, "校验失败", http.StatusInternalServerError)
		return
	}
	finalSHA := shaExpect
	if shaExpect == "" {
		finalSHA = got
	} else if got != shaExpect {
		_ = os.Remove(full)
		http.Error(w, "sha256 与上传内容不一致", http.StatusBadRequest)
		return
	}

	var id int64
	err = s.pool.QueryRow(r.Context(), `INSERT INTO xrayr_binary_artifact(display_name, target_os, target_arch, version_label, sha256_hex, storage_relpath, disabled, size_bytes)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,$6,false,$7) RETURNING id`,
		name, osStr, archNorm, ver, finalSHA, rel, nw).Scan(&id)
	if err != nil {
		_ = os.Remove(full)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.opLog(r.Context(), adminID, "artifact.upload", "artifact", fmt.Sprint(id), map[string]any{"name": name, "bytes": nw}, true, "")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "storage_relpath": rel, "size_bytes": nw, "sha256": finalSHA})
}

func (s *Server) getAgentArtifactDownload(w http.ResponseWriter, r *http.Request) {
	nid := r.Context().Value(ctxNodeID).(int64)
	aid, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if aid <= 0 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var tos, ta, sha, rel string
	var dis bool
	err := s.pool.QueryRow(r.Context(), `SELECT target_os, target_arch, sha256_hex, storage_relpath, disabled FROM xrayr_binary_artifact WHERE id=$1`, aid).Scan(&tos, &ta, &sha, &rel, &dis)
	if err != nil || dis {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !strings.EqualFold(strings.TrimSpace(tos), "linux") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var nos, narch *string
	_ = s.pool.QueryRow(r.Context(), `SELECT agent_os, agent_arch FROM node WHERE id=$1 AND disabled=false`, nid).Scan(&nos, &narch)
	if nos != nil && strings.TrimSpace(*nos) != "" && !strings.EqualFold(strings.TrimSpace(*nos), tos) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if narch != nil && strings.TrimSpace(*narch) != "" && !archMatchesAgent(strings.TrimSpace(*narch), ta) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	full, err := s.resolveXrayrArtifactFile(rel)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Artifact-Sha256", sha)
	w.Header().Set("X-Artifact-Id", strconv.FormatInt(aid, 10))
	http.ServeFile(w, r, full)
}

func archMatchesAgent(nodeArch, artifactArch string) bool {
	na := strings.ToLower(strings.TrimSpace(nodeArch))
	aa := strings.ToLower(strings.TrimSpace(artifactArch))
	if na == aa {
		return true
	}
	if (na == "amd64" && aa == "x86_64") || (na == "x86_64" && aa == "amd64") {
		return true
	}
	return false
}
