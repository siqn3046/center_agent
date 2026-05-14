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

func (s *Server) resolveXrayrArtifactFile(rel string) (string, error) {
	if s.cfg.ArtifactDir == "" {
		return "", fmt.Errorf("CENTER_ARTIFACT_DIR 未配置")
	}
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || rel == "." || strings.Contains(rel, "..") {
		return "", fmt.Errorf("非法 storage_relpath")
	}
	base := filepath.Clean(s.cfg.ArtifactDir)
	full := filepath.Join(base, filepath.FromSlash(rel))
	if r, err := filepath.Rel(base, full); err != nil || strings.HasPrefix(r, "..") {
		return "", fmt.Errorf("非法路径")
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
	if _, err := io.Copy(h, io.LimitReader(f, 512<<20)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Server) listXrayrArtifacts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT id, display_name, target_os, target_arch, COALESCE(version_label,''), sha256_hex, storage_relpath, created_at
		FROM xrayr_binary_artifact WHERE disabled=false ORDER BY id DESC`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var name, tos, ta, ver, sha, rel string
		var ct time.Time
		if rows.Scan(&id, &name, &tos, &ta, &ver, &sha, &rel, &ct) != nil {
			continue
		}
		out = append(out, map[string]any{
			"id":               id,
			"display_name":     name,
			"target_os":        tos,
			"target_arch":      ta,
			"version_label":    nullStrOrPtr(ver),
			"sha256":           sha,
			"storage_relpath":  rel,
			"download_path":    fmt.Sprintf("/api/agent/artifacts/%d/download", id),
			"created_at":       ct.UTC().Format(time.RFC3339Nano),
		})
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
	err := s.pool.QueryRow(r.Context(), `SELECT display_name, target_os, target_arch, COALESCE(version_label,''), sha256_hex, storage_relpath, disabled
		FROM xrayr_binary_artifact WHERE id=$1`, id).Scan(&name, &tos, &ta, &ver, &sha, &rel, &dis)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if dis {
		http.Error(w, "disabled", http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": id, "display_name": name, "target_os": tos, "target_arch": ta,
		"version_label": nullStrOrPtr(ver), "sha256": sha, "storage_relpath": rel,
		"download_path": fmt.Sprintf("/api/agent/artifacts/%d/download", id),
	})
}

func (s *Server) postXrayrArtifact(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	var req struct {
		DisplayName     string `json:"display_name"`
		TargetOS        string `json:"target_os"`
		TargetArch      string `json:"target_arch"`
		VersionLabel    string `json:"version_label"`
		Sha256Hex       string `json:"sha256_hex"`
		StorageRelpath  string `json:"storage_relpath"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.TargetOS = strings.TrimSpace(req.TargetOS)
	req.TargetArch = strings.TrimSpace(req.TargetArch)
	req.Sha256Hex = strings.ToLower(strings.TrimSpace(req.Sha256Hex))
	req.StorageRelpath = strings.TrimSpace(req.StorageRelpath)
	if req.DisplayName == "" || req.TargetOS == "" || req.TargetArch == "" || len(req.Sha256Hex) != 64 {
		http.Error(w, "display_name、target_os、target_arch、sha256_hex(64) 必填", http.StatusBadRequest)
		return
	}
	full, err := s.resolveXrayrArtifactFile(req.StorageRelpath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		http.Error(w, "制品文件不存在或不可读", http.StatusBadRequest)
		return
	}
	got, err := fileSHA256Hex(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if got != req.Sha256Hex {
		http.Error(w, "sha256 与磁盘文件不一致", http.StatusBadRequest)
		return
	}
	var id int64
	err = s.pool.QueryRow(r.Context(), `INSERT INTO xrayr_binary_artifact(display_name, target_os, target_arch, version_label, sha256_hex, storage_relpath)
		VALUES ($1,$2,$3,NULLIF($4,''),$5,$6) RETURNING id`,
		req.DisplayName, req.TargetOS, req.TargetArch, req.VersionLabel, req.Sha256Hex, filepath.ToSlash(req.StorageRelpath)).Scan(&id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.opLog(r.Context(), adminID, "artifact.create", "artifact", fmt.Sprint(id), map[string]any{"name": req.DisplayName}, true, "")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func (s *Server) getAgentArtifactDownload(w http.ResponseWriter, r *http.Request) {
	nid := r.Context().Value(ctxNodeID).(int64)
	aid, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if aid <= 0 {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	var name, tos, ta, sha, rel string
	var dis bool
	err := s.pool.QueryRow(r.Context(), `SELECT display_name, target_os, target_arch, sha256_hex, storage_relpath, disabled FROM xrayr_binary_artifact WHERE id=$1`, aid).Scan(&name, &tos, &ta, &sha, &rel, &dis)
	if err != nil || dis {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var nos, narch *string
	_ = s.pool.QueryRow(r.Context(), `SELECT agent_os, agent_arch FROM node WHERE id=$1 AND disabled=false`, nid).Scan(&nos, &narch)
	if nos != nil && *nos != "" && !strings.EqualFold(*nos, tos) {
		http.Error(w, "制品 target_os 与节点不匹配", http.StatusForbidden)
		return
	}
	if narch != nil && *narch != "" && !archMatchesAgent(*narch, ta) {
		http.Error(w, "制品 target_arch 与节点不匹配", http.StatusForbidden)
		return
	}
	full, err := s.resolveXrayrArtifactFile(rel)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Artifact-Sha256", sha)
	w.Header().Set("X-Artifact-Name", name)
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
