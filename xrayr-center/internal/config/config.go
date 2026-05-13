package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddr     string
	DatabaseURL    string
	JWTSecret      string
	JWTTTL         time.Duration
	AgentDownloadURL string
	AgentSHA256    string
	PublicBaseURL  string
}

func Load() *Config {
	jwtTTL := 24 * time.Hour
	if s := os.Getenv("CENTER_JWT_TTL_HOURS"); s != "" {
		if h, err := strconv.Atoi(s); err == nil && h > 0 {
			jwtTTL = time.Duration(h) * time.Hour
		}
	}
	return &Config{
		ListenAddr:       getenv("CENTER_LISTEN", ":8080"),
		DatabaseURL:      getenv("DATABASE_URL", "postgres://center:center@localhost:5432/center?sslmode=disable"),
		JWTSecret:        getenv("CENTER_JWT_SECRET", "dev-change-me-change-me-32chars!!"),
		JWTTTL:           jwtTTL,
		AgentDownloadURL: getenv("CENTER_AGENT_DOWNLOAD_URL", ""),
		AgentSHA256:      getenv("CENTER_AGENT_SHA256", ""),
		PublicBaseURL:    getenv("CENTER_PUBLIC_BASE_URL", "http://127.0.0.1:8080"),
	}
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
