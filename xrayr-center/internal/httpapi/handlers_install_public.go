package httpapi

import (
	_ "embed"
	"net/http"
	"strings"
)

//go:embed static/install-agent.sh
var installAgentShellTemplate string

const installTokenPlaceholder = "<REGISTER_TOKEN>"

func trimPublicBase(s string) string {
	s = strings.TrimSpace(s)
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// publicInstallBaseURL 用于生成安装命令与脚本内 center.url。优先级：CENTER_PUBLIC_BASE_URL → 请求 Host → 本地回退。
func (s *Server) publicInstallBaseURL(r *http.Request) string {
	if b := trimPublicBase(s.cfg.PublicBaseURL); b != "" {
		return b
	}
	if r == nil {
		return "http://127.0.0.1:8080"
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if xf := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); xf == "https" || xf == "http" {
		scheme = xf
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return "http://127.0.0.1:8080"
	}
	return scheme + "://" + host
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func (s *Server) agentInstallCommandsForRequest(r *http.Request, registerToken string) (wget string, curl string) {
	base := s.publicInstallBaseURL(r)
	scriptURL := base + "/install-agent.sh"
	wget = "wget -qO- " + shellSingleQuote(scriptURL) + " | sudo bash -s -- -e " + shellSingleQuote(base) + " -t " + shellSingleQuote(registerToken)
	curl = "curl -fsSL " + shellSingleQuote(scriptURL) + " | sudo bash -s -- -e " + shellSingleQuote(base) + " -t " + shellSingleQuote(registerToken)
	return wget, curl
}

func (s *Server) serveInstallAgentSh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	agentURL, sha, _, err := s.cfg.ResolveAgentDownload()
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	body := installAgentShellTemplate
	if err != nil {
		agentURL = ""
		sha = ""
	}
	urlLit := "''"
	shaLit := "''"
	if agentURL != "" && sha != "" {
		urlLit = shellSingleQuote(agentURL)
		shaLit = shellSingleQuote(strings.ToLower(strings.TrimSpace(sha)))
	}
	body = strings.ReplaceAll(body, "__XRAYR_CENTER_INJECT_AGENT_URL__", urlLit)
	body = strings.ReplaceAll(body, "__XRAYR_CENTER_INJECT_AGENT_SHA256__", shaLit)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}
