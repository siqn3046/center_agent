package discovery

import (
	"os/exec"
	"strings"

	"github.com/XrayR-project/XrayR/xrayr-agent/internal/agentcfg"
)

func IsServiceActive(cfg *agentcfg.File) bool {
	sn := cfg.XrayR.ServiceName
	if sn == "" {
		sn = "xrayr"
	}
	out, err := exec.Command("systemctl", "is-active", sn).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "active"
}
