package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr       string
	DatabaseURL      string
	JWTSecret        string
	JWTTTL           time.Duration
	AgentDownloadURL string
	AgentSHA256      string
	PublicBaseURL    string
	ArtifactDir      string
	AgentVersion     string
	AgentBinaryName  string
}

func Load() *Config {
	jwtTTL := 24 * time.Hour
	if s := os.Getenv("CENTER_JWT_TTL_HOURS"); s != "" {
		if h, err := strconv.Atoi(s); err == nil && h > 0 {
			jwtTTL = time.Duration(h) * time.Hour
		}
	}
	binName := getenv("CENTER_AGENT_BINARY_NAME", "xrayr-agent")
	if binName == "" {
		binName = "xrayr-agent"
	}
	return &Config{
		ListenAddr:       getenv("CENTER_LISTEN", ":8080"),
		DatabaseURL:      getenv("DATABASE_URL", "postgres://center:center@localhost:5432/center?sslmode=disable"),
		JWTSecret:        getenv("CENTER_JWT_SECRET", "dev-change-me-change-me-32chars!!"),
		JWTTTL:           jwtTTL,
		AgentDownloadURL: getenv("CENTER_AGENT_DOWNLOAD_URL", ""),
		AgentSHA256:      getenv("CENTER_AGENT_SHA256", ""),
		PublicBaseURL:    getenv("CENTER_PUBLIC_BASE_URL", ""),
		ArtifactDir:      getenv("CENTER_ARTIFACT_DIR", ""),
		AgentVersion:     getenv("CENTER_AGENT_VERSION", ""),
		AgentBinaryName:  binName,
	}
}

func trimRightSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// ResolveAgentDownload 返回安装脚本使用的下载 URL 与 sha256。
// 优先级：1) CENTER_ARTIFACT_DIR 下已放置的版本化二进制（必须能解析出 sha256）；2) CENTER_AGENT_DOWNLOAD_URL + CENTER_AGENT_SHA256。
func (c *Config) ResolveAgentDownload() (downloadURL string, sha256hex string, source string, err error) {
	base := trimRightSlash(c.PublicBaseURL)
	if c.ArtifactDir != "" && c.AgentVersion != "" {
		if base == "" {
			return "", "", "", fmt.Errorf("使用 CENTER_ARTIFACT_DIR 托管 Agent 时必须设置 CENTER_PUBLIC_BASE_URL（对外可访问的 Center 根 URL）")
		}
		binPath := filepath.Join(c.ArtifactDir, c.AgentVersion, c.AgentBinaryName)
		if st, e := os.Stat(binPath); e == nil && !st.IsDir() {
			sum, e2 := artifactSHA256(binPath, filepath.Join(c.ArtifactDir, c.AgentVersion, "sha256"), c.AgentSHA256)
			if e2 != nil {
				return "", "", "", e2
			}
			u := fmt.Sprintf("%s/api/public/artifacts/xrayr-agent/%s/%s", base, urlPathSeg(c.AgentVersion), urlPathSeg(c.AgentBinaryName))
			return u, sum, "center_artifact", nil
		}
	}
	if c.AgentDownloadURL != "" && c.AgentSHA256 != "" {
		return c.AgentDownloadURL, c.AgentSHA256, "external_url", nil
	}
	return "", "", "", fmt.Errorf("未配置可用 Agent 下载源：请在 CENTER_ARTIFACT_DIR 放置 %s/%s/%s 与 sha256 文件，或同时设置 CENTER_AGENT_DOWNLOAD_URL 与 CENTER_AGENT_SHA256", c.ArtifactDir, c.AgentVersion, c.AgentBinaryName)
}

func urlPathSeg(s string) string {
	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '_' {
			continue
		}
		return "invalid"
	}
	return s
}

func artifactSHA256(binPath, shaFile string, envFallback string) (string, error) {
	if b, err := os.ReadFile(shaFile); err == nil {
		line := strings.TrimSpace(string(b))
		line = strings.Fields(line)[0]
		if len(line) == 64 {
			return strings.ToLower(line), nil
		}
	}
	if envFallback != "" && len(envFallback) == 64 {
		return strings.ToLower(envFallback), nil
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

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
