package discovery

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/XrayR-project/XrayR/xrayr-agent/internal/agentcfg"
)

type Result struct {
	InstallState         string
	BinaryPaths          map[string]bool
	ConfigPaths          map[string]bool
	ServiceStatus        map[string]string
	ProcessStatus        map[string]string
	VersionInfo          map[string]string
	ConfigHash           string
	ConfigYAML           string
	ErrorTail            string
	DiscoveredBinaryPath string
	DiscoveredConfigPath string
	ServiceName          string
	ImportedBackupPath   string
}

func runCmd(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	_ = cmd.Run()
	return buf.String()
}

func Run(cfg *agentcfg.File) (*Result, error) {
	sn := cfg.XrayR.ServiceName
	active := strings.TrimSpace(runCmd("systemctl", "is-active", sn))
	statusOut := runCmd("systemctl", "status", sn, "--no-pager", "-l")
	catOut := runCmd("systemctl", "cat", sn)
	svc := map[string]string{"is_active": active, "status": statusOut, "cat": catOut}

	binFound := map[string]bool{}
	var binPath string
	for _, p := range cfg.XrayR.BinarySearchPaths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode().Perm()&0111 != 0 {
			binFound[p] = true
			if binPath == "" {
				binPath = p
			}
		}
	}

	cfgFound := map[string]bool{}
	var cfgPath string
	var cfgContent []byte
	for _, p := range cfg.XrayR.ConfigSearchPaths {
		b, err := os.ReadFile(p)
		if err == nil && len(bytes.TrimSpace(b)) > 0 {
			cfgFound[p] = true
			if cfgPath == "" {
				cfgPath = p
				cfgContent = b
			}
		}
	}

	proc := strings.TrimSpace(runCmd("pgrep", "-x", "XrayR"))
	procMap := map[string]string{"pgrep_xrayr": proc}

	ver := map[string]string{}
	if binPath != "" {
		out := runCmd(binPath, "version")
		if strings.TrimSpace(out) != "" {
			ver["xrayr_version_output"] = truncate(out, 500)
		}
	}

	hash := ""
	if len(cfgContent) > 0 {
		h := sha256.Sum256(cfgContent)
		hash = hex.EncodeToString(h[:])
	}
	tail := tailFile(cfg.XrayR.ErrorLogPath, min(80, cfg.Agent.LogTailLimit))

	state := classify(active, statusOut, binFound, cfgFound, proc)

	res := &Result{
		InstallState:         state,
		BinaryPaths:          binFound,
		ConfigPaths:          cfgFound,
		ServiceStatus:        svc,
		ProcessStatus:        procMap,
		VersionInfo:          ver,
		ConfigHash:           hash,
		ConfigYAML:           string(cfgContent),
		ErrorTail:            tail,
		DiscoveredBinaryPath: binPath,
		DiscoveredConfigPath: cfgPath,
		ServiceName:          sn,
	}

	if state == "INSTALLED_RUNNING" && len(cfgContent) > 0 {
		_ = os.MkdirAll(cfg.XrayR.ConfigBackupDir, 0755)
		short := hash
		if len(short) > 12 {
			short = short[:12]
		}
		bp := filepath.Join(cfg.XrayR.ConfigBackupDir, fmt.Sprintf("imported-%d-%s.yml", time.Now().Unix(), short))
		if err := os.WriteFile(bp, cfgContent, 0644); err == nil {
			res.ImportedBackupPath = bp
		}
	}

	return res, nil
}

func classify(active, status string, binFound map[string]bool, cfgFound map[string]bool, pgrep string) string {
	hasBin := false
	for _, v := range binFound {
		if v {
			hasBin = true
			break
		}
	}
	hasCfg := false
	for _, v := range cfgFound {
		if v {
			hasCfg = true
			break
		}
	}
	procOK := pgrep != ""

	if strings.Contains(status, "could not be found") || strings.Contains(strings.ToLower(status), "not loaded") {
		if hasCfg && !hasBin {
			return "CONFIG_ONLY"
		}
		if hasBin && !hasCfg {
			return "BINARY_ONLY"
		}
		if !hasBin && !hasCfg {
			return "NOT_INSTALLED"
		}
	}

	if active == "active" {
		if hasBin && hasCfg && procOK {
			return "INSTALLED_RUNNING"
		}
		if strings.Contains(strings.ToLower(status), "failed") || (!procOK && hasCfg) {
			return "INSTALLED_BROKEN"
		}
		if hasCfg && !hasBin {
			return "SERVICE_ONLY"
		}
		return "UNKNOWN"
	}

	if active == "failed" {
		return "INSTALLED_BROKEN"
	}

	if active == "inactive" || active == "dead" {
		if hasBin || hasCfg {
			return "INSTALLED_STOPPED"
		}
		return "NOT_INSTALLED"
	}

	if hasCfg && !hasBin {
		return "CONFIG_ONLY"
	}
	if hasBin && !hasCfg {
		return "BINARY_ONLY"
	}
	if !hasBin && !hasCfg {
		return "NOT_INSTALLED"
	}
	return "UNKNOWN"
}

func tailFile(path string, maxLines int) string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) <= maxLines {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-maxLines:], "\n")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
