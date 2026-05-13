package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

func validArtifactToken(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

func (s *Server) getPublicArtifactFile(w http.ResponseWriter, r *http.Request) {
	version := chi.URLParam(r, "version")
	file := chi.URLParam(r, "file")
	if !validArtifactToken(version) || !validArtifactToken(file) {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	binName := s.cfg.AgentBinaryName
	if binName == "" {
		binName = "xrayr-agent"
	}
	if file == "sha256" {
		s.serveArtifactSHA(w, r, version, binName)
		return
	}
	if file != binName {
		http.Error(w, "forbidden file name", http.StatusForbidden)
		return
	}
	if s.cfg.ArtifactDir == "" {
		http.Error(w, "artifact hosting disabled", http.StatusNotFound)
		return
	}
	base := filepath.Clean(s.cfg.ArtifactDir)
	full := filepath.Join(base, version, file)
	if rel, err := filepath.Rel(base, full); err != nil || strings.HasPrefix(rel, "..") {
		http.Error(w, "bad path", http.StatusBadRequest)
		return
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeFile(w, r, full)
}

func (s *Server) serveArtifactSHA(w http.ResponseWriter, r *http.Request, version, binName string) {
	if s.cfg.ArtifactDir == "" {
		http.Error(w, "artifact hosting disabled", http.StatusNotFound)
		return
	}
	base := filepath.Clean(s.cfg.ArtifactDir)
	binPath := filepath.Join(base, version, binName)
	shaPath := filepath.Join(base, version, "sha256")
	sum, err := artifactPublicSHA(binPath, shaPath, s.cfg.AgentSHA256)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(sum + "\n"))
}

func artifactPublicSHA(binPath, shaFile, envFallback string) (string, error) {
	if b, err := os.ReadFile(shaFile); err == nil {
		line := strings.TrimSpace(string(b))
		line = strings.Fields(line)[0]
		if len(line) == 64 {
			return strings.ToLower(line), nil
		}
	}
	if envFallback != "" && len(strings.TrimSpace(envFallback)) == 64 {
		return strings.ToLower(strings.TrimSpace(envFallback)), nil
	}
	f, err := os.Open(binPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, 256<<20)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
