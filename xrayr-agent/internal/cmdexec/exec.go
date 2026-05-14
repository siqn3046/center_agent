package cmdexec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/XrayR-project/XrayR/xrayr-agent/internal/agentcfg"
	"github.com/XrayR-project/XrayR/xrayr-agent/internal/capi"
	"github.com/XrayR-project/XrayR/xrayr-agent/internal/discovery"
)

func Allowed(list []string, cmd string) bool {
	for _, x := range list {
		if strings.EqualFold(strings.TrimSpace(x), cmd) {
			return true
		}
	}
	return false
}

func progress(client *capi.Client, commandID, step, msg string) {
	_ = client.SignedJSON(http.MethodPost, "/api/agent/command/progress", map[string]any{
		"command_id": commandID,
		"step":       step,
		"message":    msg,
	})
}

func result(client *capi.Client, commandID, status, log string, err error, extra map[string]any) {
	em := ""
	if err != nil {
		em = err.Error()
	}
	if extra == nil {
		extra = map[string]any{}
	}
	_ = client.SignedJSON(http.MethodPost, "/api/agent/command/result", map[string]any{
		"command_id":     commandID,
		"status":         status,
		"log_summary":      log,
		"error_message":  em,
		"result_json":    extra,
	})
}

func RunStatus(client *capi.Client, cfg *agentcfg.File, commandID string) {
	if !Allowed(cfg.Agent.AllowCommands, "STATUS_XRAYR") {
		result(client, commandID, "FAILED", "", fmt.Errorf("STATUS_XRAYR 未在 allow_commands"), nil)
		return
	}
	active := discovery.IsServiceActive(cfg)
	cfgPath := cfg.XrayR.ConfigPath
	h := ""
	if b, err := os.ReadFile(cfgPath); err == nil {
		sum := sha256.Sum256(b)
		h = hex.EncodeToString(sum[:])
	}
	result(client, commandID, "SUCCESS", "systemctl is-active 完成", nil, map[string]any{
		"systemd_active": active, "config_hash": h,
	})
}

func RunRestart(client *capi.Client, cfg *agentcfg.File, commandID string) {
	if !Allowed(cfg.Agent.AllowCommands, "RESTART_XRAYR") {
		result(client, commandID, "FAILED", "", fmt.Errorf("RESTART_XRAYR 未在 allow_commands"), nil)
		return
	}
	sn := cfg.XrayR.ServiceName
	progress(client, commandID, "restart", "执行 systemctl restart")
	cmd := exec.Command("sudo", "-n", "systemctl", "restart", sn)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err != nil {
		result(client, commandID, "FAILED", buf.String(), err, nil)
		return
	}
	active := discovery.IsServiceActive(cfg)
	if !active {
		result(client, commandID, "FAILED", "重启后未 active", fmt.Errorf("inactive"), nil)
		return
	}
	result(client, commandID, "SUCCESS", buf.String(), nil, map[string]any{"systemd_active": true})
}

func RunApplyConfig(client *capi.Client, cfg *agentcfg.File, commandID string, payload map[string]any) {
	if !Allowed(cfg.Agent.AllowCommands, "APPLY_CONFIG") {
		result(client, commandID, "FAILED", "", fmt.Errorf("APPLY_CONFIG 不在 allow_commands 中"), nil)
		return
	}
	progress(client, commandID, "precheck", "校验参数与路径")
	wantHash, _ := payload["config_hash"].(string)
	if wantHash == "" {
		wantHash, _ = payload["content_sha256"].(string)
	}
	wantHash = strings.ToLower(strings.TrimSpace(wantHash))
	yamlPath, _ := payload["yaml_path"].(string)
	if yamlPath == "" || wantHash == "" {
		result(client, commandID, "FAILED", "", fmt.Errorf("缺少 yaml_path 或 config_hash"), nil)
		return
	}
	target, _ := payload["target_config_path"].(string)
	target = strings.TrimSpace(target)
	local := strings.TrimSpace(cfg.XrayR.ConfigPath)
	if target != "" && target != local {
		result(client, commandID, "FAILED", "", fmt.Errorf("Center 下发的 target_config_path 与 agent.yml 的 config_path 不一致"), map[string]any{
			"expected": local, "got": target,
		})
		return
	}
	cfgFile := local
	progress(client, commandID, "download", "拉取 YAML")
	raw, err := client.SignedGET(yamlPath)
	if err != nil {
		result(client, commandID, "FAILED", "", fmt.Errorf("下载配置: %w", err), nil)
		return
	}
	sum := sha256.Sum256(raw)
	got := hex.EncodeToString(sum[:])
	if got != wantHash {
		result(client, commandID, "FAILED", "", fmt.Errorf("sha256 不匹配"), map[string]any{"want": wantHash, "got": got})
		return
	}
	_ = os.MkdirAll(cfg.Agent.BackupDir, 0755)
	backupName := fmt.Sprintf("config-%s.yml", time.Now().Format("20060102-150405"))
	backupPath := filepath.Join(cfg.Agent.BackupDir, backupName)
	oldBytes, errRead := os.ReadFile(cfgFile)
	beforeHash := ""
	if errRead == nil && len(oldBytes) > 0 {
		h0 := sha256.Sum256(oldBytes)
		beforeHash = hex.EncodeToString(h0[:])
	}
	if errRead == nil {
		if err := os.WriteFile(backupPath, oldBytes, 0600); err != nil {
			result(client, commandID, "FAILED", "", fmt.Errorf("备份失败: %w", err), nil)
			return
		}
	}
	progress(client, commandID, "write", "写入临时文件并原子替换")
	tmp := cfgFile + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		result(client, commandID, "FAILED", "", fmt.Errorf("写临时文件: %w", err), nil)
		return
	}
	if err := os.Rename(tmp, cfgFile); err != nil {
		_ = os.Remove(tmp)
		result(client, commandID, "FAILED", "", fmt.Errorf("替换配置: %w", err), nil)
		return
	}
	progress(client, commandID, "restart", "重启 XrayR")
	_ = exec.Command("sudo", "-n", "systemctl", "restart", cfg.XrayR.ServiceName).Run()
	time.Sleep(2 * time.Second)
	active := discovery.IsServiceActive(cfg)
	if active {
		ah := ""
		if nb, err := os.ReadFile(cfgFile); err == nil {
			h := sha256.Sum256(nb)
			ah = hex.EncodeToString(h[:])
		}
		result(client, commandID, "SUCCESS", "配置已应用且服务 active", nil, map[string]any{
			"backup_path": backupPath, "before_config_hash": beforeHash, "after_config_hash": ah,
		})
		return
	}
	progress(client, commandID, "rollback", "健康检查失败，回滚旧配置")
	if errRead == nil && len(oldBytes) > 0 {
		_ = os.WriteFile(cfgFile, oldBytes, 0600)
	}
	_ = exec.Command("sudo", "-n", "systemctl", "restart", cfg.XrayR.ServiceName).Run()
	result(client, commandID, "FAILED", "新配置启动失败，已回滚旧配置", fmt.Errorf("systemctl inactive after apply"), map[string]any{
		"backup_path": backupPath, "before_config_hash": beforeHash, "rolled_back": true,
	})
}

