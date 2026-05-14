package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/XrayR-project/XrayR/xrayr-center/internal/authadmin"
	"github.com/XrayR-project/XrayR/xrayr-center/internal/config"
	"github.com/XrayR-project/XrayR/xrayr-center/internal/hsign"
	"github.com/XrayR-project/XrayR/xrayr-center/internal/noncecache"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Server struct {
	cfg   *config.Config
	pool  *pgxpool.Pool
	nonce *noncecache.Cache
	hub   *agentHub
}

func New(cfg *config.Config, pool *pgxpool.Pool) *Server {
	return &Server{
		cfg:   cfg,
		pool:  pool,
		nonce: noncecache.New(10*time.Minute, 90*time.Second),
		hub:   newAgentHub(),
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/api/public/artifacts/xrayr-agent/{version}/{file}", s.getPublicArtifactFile)

	r.Post("/api/admin/login", s.postAdminLogin)

	r.Route("/api/admin", func(r chi.Router) {
		r.Use(s.requireAdminJWT)
		r.Get("/me", s.getMe)
		r.Get("/dashboard/summary", s.getDashboardSummary)
		r.Get("/node-groups", s.listNodeGroups)
		r.Post("/node-groups", s.createNodeGroup)
		r.Get("/nodes", s.listNodes)
		r.Post("/nodes", s.createNode)
		r.Get("/nodes/{id}", s.getNode)
		r.Patch("/nodes/{id}/settings", s.patchNodeSettings)
		r.Get("/nodes/{id}/install-script.sh", s.installScript)
		r.Get("/nodes/{id}/discovery-reports", s.listDiscovery)
		r.Get("/nodes/{id}/monitor-snapshots", s.listMonitorSnapshots)
		r.Get("/nodes/{id}/commands", s.listCommands)
		r.Get("/nodes/{id}/config-versions", s.listNodeConfigVersions)
		r.Post("/nodes/{id}/config-versions", s.postNodeConfigVersion)
		r.Post("/nodes/{id}/config-versions/{versionId}/deploy", s.postNodeConfigVersionDeploy)
		r.Get("/nodes/{id}/config-deploy-tasks", s.listConfigDeployTasks)
		r.Post("/nodes/{id}/enable-writable", s.enableWritable)
		r.Post("/nodes/{id}/commands/restart-xrayr", s.postCmdRestart)
		r.Post("/nodes/{id}/commands/status-xrayr", s.postCmdStatus)
		r.Post("/nodes/{id}/commands/apply-config", s.postCmdApplyConfig)
		r.Post("/nodes/{id}/commands/install-xrayr", s.postCmdInstallXrayr)
		r.Post("/nodes/{id}/commands/upgrade-xrayr", s.postCmdUpgradeXrayr)
		r.Get("/artifacts", s.listXrayrArtifacts)
		r.Get("/artifacts/{id}", s.getXrayrArtifact)
		r.Post("/artifacts", s.postXrayrArtifact)
		r.Get("/config-versions", s.listConfigVersionsGlobal)
		r.Get("/config-versions/{versionId}", s.getConfigVersionByID)
		r.Post("/config-versions", s.createConfigVersion)
	})

	r.Post("/api/agent/register", s.agentRegister)

	r.Route("/api/agent", func(r chi.Router) {
		r.Use(s.requireAgentHMAC)
		r.Post("/heartbeat", s.agentHeartbeat)
		r.Post("/monitor/report", s.agentMonitorReport)
		r.Post("/xrayr/status", s.agentXrayrStatus)
		r.Post("/discovery/report", s.agentDiscoveryReport)
		r.Get("/config-deploy/{deployId}/yaml", s.getConfigDeployYAML)
		r.Get("/artifacts/{id}/download", s.getAgentArtifactDownload)
		r.Post("/command/progress", s.postAgentCommandProgress)
		r.Post("/command/result", s.postAgentCommandResult)
	})

	r.Get("/ws/agent", s.agentWebSocket)

	r.Get("/", s.serveIndex)
	r.Get("/static/app.js", s.serveJS)
	r.Get("/static/style.css", s.serveCSS)
	return r
}

func (s *Server) serveCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write([]byte(styleCSS))
}

func (s *Server) requireAdminJWT(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		const p = "Bearer "
		if !strings.HasPrefix(h, p) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		c, err := authadmin.ParseJWT(strings.TrimPrefix(h, p), s.cfg.JWTSecret)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxAdminID, c.AdminID)
		ctx = context.WithValue(ctx, ctxAdminName, c.Username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type ctxKey string

const ctxAdminID ctxKey = "adminID"
const ctxAdminName ctxKey = "adminName"
const ctxNodeID ctxKey = "agentNodeID"
const ctxNodeSecret ctxKey = "agentNodeSecret"

func (s *Server) requireAgentHMAC(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nodeIDStr := r.Header.Get("X-Node-Id")
		tsStr := r.Header.Get("X-Timestamp")
		nonce := r.Header.Get("X-Nonce")
		bh := r.Header.Get("X-Body-Hash")
		sig := r.Header.Get("X-Signature")
		if nodeIDStr == "" || tsStr == "" || nonce == "" || bh == "" || sig == "" {
			http.Error(w, "missing auth headers", http.StatusUnauthorized)
			return
		}
		ts, err := strconv.ParseInt(tsStr, 10, 64)
		if err != nil {
			http.Error(w, "bad timestamp", http.StatusUnauthorized)
			return
		}
		nodeID, err := strconv.ParseInt(nodeIDStr, 10, 64)
		if err != nil {
			http.Error(w, "bad node id", http.StatusUnauthorized)
			return
		}
		var secret string
		err = s.pool.QueryRow(r.Context(), `SELECT node_hmac_key FROM node WHERE id=$1 AND disabled=false`, nodeID).Scan(&secret)
		if err != nil || secret == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		path := r.URL.Path
		if err := hsign.Verify(secret, r.Method, path, ts, nonce, bh, sig, body, s.nonce.Skew(), nil); err != nil {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		if s.nonce.Seen(nonce) {
			http.Error(w, "nonce replay", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxNodeID, nodeID)
		ctx = context.WithValue(ctx, ctxNodeSecret, secret)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func hashToken(plain string) string {
	h := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(h[:])
}

func (s *Server) postAdminLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	var id int64
	var hash string
	err := s.pool.QueryRow(r.Context(), `SELECT id, password_hash FROM admin_user WHERE username=$1 AND disabled=false`, req.Username).Scan(&id, &hash)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	tok, err := authadmin.IssueJWT(id, req.Username, s.cfg.JWTSecret, s.cfg.JWTTTL)
	if err != nil {
		http.Error(w, "token error", http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"token": tok, "expires_in": int(s.cfg.JWTTTL.Seconds())})
}

func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	id := r.Context().Value(ctxAdminID).(int64)
	name := r.Context().Value(ctxAdminName).(string)
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "username": name})
}

func (s *Server) listNodeGroups(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `SELECT id, name, description FROM node_group ORDER BY id`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var name, desc string
		_ = rows.Scan(&id, &name, &desc)
		out = append(out, map[string]any{"id": id, "name": name, "description": desc})
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) createNodeGroup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	var id int64
	err := s.pool.QueryRow(r.Context(), `INSERT INTO node_group(name, description) VALUES($1,$2) RETURNING id`, req.Name, req.Description).Scan(&id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
}

func (s *Server) createNode(w http.ResponseWriter, r *http.Request) {
	adminID := r.Context().Value(ctxAdminID).(int64)
	var req struct {
		NodeName                     string `json:"node_name"`
		NodeCode                     string `json:"node_code"`
		NodeGroupID                  *int64 `json:"node_group_id"`
		ExpiresHours                 int    `json:"expires_hours"`
		ImportMode                   string `json:"import_mode"`
		InstallXrayrIfMissing        *bool  `json:"install_xrayr_if_missing"`
		UpgradeXrayrIfExists         *bool  `json:"upgrade_xrayr_if_exists"`
		RepairIfBroken               *bool  `json:"repair_if_broken"`
		TargetXrayrVersion           string `json:"target_xrayr_version"`
		TargetDownloadURL            string `json:"target_download_url"`
		TargetSha256                 string `json:"target_sha256"`
		AutoStartAfterInstall        *bool  `json:"auto_start_after_install"`
		AutoEnableManageAfterSuccess *bool  `json:"auto_enable_manage_after_success"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.NodeName == "" || req.NodeCode == "" {
		http.Error(w, "node_name and node_code required", http.StatusBadRequest)
		return
	}
	if req.ExpiresHours <= 0 {
		req.ExpiresHours = 72
	}
	im := req.ImportMode
	if im == "" {
		im = "AUTO_DETECT"
	}
	ix := true
	if req.InstallXrayrIfMissing != nil {
		ix = *req.InstallXrayrIfMissing
	}
	ux := false
	if req.UpgradeXrayrIfExists != nil {
		ux = *req.UpgradeXrayrIfExists
	}
	rb := false
	if req.RepairIfBroken != nil {
		rb = *req.RepairIfBroken
	}
	as := true
	if req.AutoStartAfterInstall != nil {
		as = *req.AutoStartAfterInstall
	}
	aem := false
	if req.AutoEnableManageAfterSuccess != nil {
		aem = *req.AutoEnableManageAfterSuccess
	}

	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	var nid int64
	err = tx.QueryRow(r.Context(),
		`INSERT INTO node(node_code, node_name, node_group_id, manage_status, import_mode)
		 VALUES($1,$2,$3,'DISCOVERED',$4) RETURNING id`,
		req.NodeCode, req.NodeName, req.NodeGroupID, im).Scan(&nid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	plainTok := randomHex(24)
	th := hashToken(plainTok)
	exp := time.Now().Add(time.Duration(req.ExpiresHours) * time.Hour)
	_, err = tx.Exec(r.Context(), `INSERT INTO node_install_token(
		node_id, token_hash, import_mode, install_xrayr_if_missing, upgrade_xrayr_if_exists, repair_if_broken,
		target_xrayr_version, target_download_url, target_sha256, auto_start_after_install, auto_enable_manage_after_success,
		expires_at, created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		nid, th, im, ix, ux, rb, nullStr(req.TargetXrayrVersion), nullStr(req.TargetDownloadURL), nullStr(req.TargetSha256), as, aem, exp, adminID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	detail, _ := json.Marshal(map[string]any{"node_code": req.NodeCode})
	_, _ = tx.Exec(r.Context(), `INSERT INTO operation_log(actor_admin_id, action, target_type, target_id, detail_json, success) VALUES ($1,'node.create','node',$2,$3::jsonb,true)`,
		adminID, fmt.Sprint(nid), detail)
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"node_id":         nid,
		"register_token": plainTok,
		"expires_at":      exp.UTC().Format(time.RFC3339),
	})
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
