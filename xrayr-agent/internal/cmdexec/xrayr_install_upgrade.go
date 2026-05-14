package cmdexec

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/XrayR-project/XrayR/xrayr-agent/internal/agentcfg"
	"github.com/XrayR-project/XrayR/xrayr-agent/internal/capi"
	"github.com/XrayR-project/XrayR/xrayr-agent/internal/discovery"
)

func RunInstallXrayR(client *capi.Client, cfg *agentcfg.File, commandID string, payload map[string]any) {
	if !Allowed(cfg.Agent.AllowCommands, "INSTALL_XRAYR") {
		result(client, commandID, "FAILED", "", fmt.Errorf("INSTALL_XRAYR 不在 allow_commands"), nil)
		return
	}
	runXrayrInstallUpgrade(client, cfg, commandID, payload, true)
}

func RunUpgradeXrayR(client *capi.Client, cfg *agentcfg.File, commandID string, payload map[string]any) {
	if !Allowed(cfg.Agent.AllowCommands, "UPGRADE_XRAYR") {
		result(client, commandID, "FAILED", "", fmt.Errorf("UPGRADE_XRAYR 不在 allow_commands"), nil)
		return
	}
	runXrayrInstallUpgrade(client, cfg, commandID, payload, false)
}

func runXrayrInstallUpgrade(client *capi.Client, cfg *agentcfg.File, commandID string, payload map[string]any, install bool) {
	if runtime.GOOS != "linux" {
		result(client, commandID, "FAILED", "", fmt.Errorf("仅支持 Linux systemd 环境"), nil)
		return
	}
	progress(client, commandID, "CHECKING", "校验环境与 Center 载荷")
	if err := validateInstallPayload(cfg, payload); err != nil {
		result(client, commandID, "FAILED", "", err, nil)
		return
	}
	dlPath := strings.TrimSpace(strFrom(payload, "download_path"))
	wantSHA := strings.ToLower(strings.TrimSpace(strFrom(payload, "sha256")))
	binPath := cfg.EffectiveBinaryPath()
	cfgPath := strings.TrimSpace(cfg.XrayR.ConfigPath)
	svcPath := cfg.EffectiveServicePath()
	svc := strings.TrimSpace(cfg.XrayR.ServiceName)
	if svc == "" {
		svc = "xrayr"
	}
	if !allowedBinaryPath(binPath) || !allowedConfigPath(cfgPath) || !allowedServicePath(svcPath) {
		result(client, commandID, "FAILED", "", fmt.Errorf("agent.yml 中的路径不在白名单"), map[string]any{"binary": binPath, "config": cfgPath, "service": svcPath})
		return
	}
	if !dirAllowed(cfg.Agent.BackupDir) || !dirAllowed(cfg.Agent.ArtifactCacheDir) {
		result(client, commandID, "FAILED", "", fmt.Errorf("backup_dir 或 artifact_cache_dir 不在允许目录下"), nil)
		return
	}
	_ = os.MkdirAll(cfg.Agent.ArtifactCacheDir, 0755)
	_ = os.MkdirAll(cfg.Agent.BackupDir, 0755)

	binExists := fileExists(binPath)
	if install && binExists {
		result(client, commandID, "FAILED", "", fmt.Errorf("已检测到 XrayR 二进制，请使用 UPGRADE_XRAYR"), nil)
		return
	}
	if !install && !binExists {
		result(client, commandID, "FAILED", "", fmt.Errorf("未找到现有 XrayR 二进制，请使用 INSTALL_XRAYR"), nil)
		return
	}

	var backupBin, backupSvc, backupCfg string
	var hadOldSvc bool
	if !install {
		progress(client, commandID, "BACKUP", "备份旧二进制与 systemd unit")
		var err error
		backupBin, err = backupFileToDir(binPath, cfg.Agent.BackupDir, "xrayr-bin")
		if err != nil {
			result(client, commandID, "FAILED", "", fmt.Errorf("备份二进制: %w", err), nil)
			return
		}
		if b, err := readFileMaybeSudo(svcPath); err == nil && len(b) > 0 {
			hadOldSvc = true
			backupSvc, err = writeBackupBytes(b, cfg.Agent.BackupDir, "xrayr-unit")
			if err != nil {
				result(client, commandID, "FAILED", "", fmt.Errorf("备份 unit: %w", err), nil)
				return
			}
		}
	}

	artifactFile := filepath.Join(cfg.Agent.ArtifactCacheDir, fmt.Sprintf("artifact-%s-%d.bin", commandID, time.Now().UnixNano()))
	defer func() { _ = os.Remove(artifactFile) }()

	progress(client, commandID, "DOWNLOADING", "正在下载 XrayR 制品（签名通道）")
	if err := client.SignedGETToFile(dlPath, artifactFile); err != nil {
		result(client, commandID, "FAILED", "", fmt.Errorf("下载失败: %w", err), nil)
		return
	}

	progress(client, commandID, "VERIFYING", "校验 sha256")
	gotHash, err := sha256FileHex(artifactFile)
	if err != nil {
		result(client, commandID, "FAILED", "", fmt.Errorf("读取制品: %w", err), nil)
		return
	}
	if gotHash != wantSHA {
		result(client, commandID, "FAILED", "", fmt.Errorf("sha256 不匹配"), map[string]any{"want": wantSHA, "got": gotHash})
		return
	}

	if !install && payloadBool(payload, "overwrite_config") {
		progress(client, commandID, "BACKUP", "备份当前配置（升级覆盖）")
		yaml := strFrom(payload, "config_yaml")
		sumWant := strings.ToLower(strings.TrimSpace(strFrom(payload, "config_sha256")))
		raw := []byte(yaml)
		h := sha256.Sum256(raw)
		if hex.EncodeToString(h[:]) != sumWant {
			result(client, commandID, "FAILED", "", fmt.Errorf("config_sha256 与 config_yaml 不一致"), nil)
			return
		}
		old, _ := os.ReadFile(cfgPath)
		if len(old) > 0 {
			backupCfg, _ = writeBackupBytes(old, cfg.Agent.BackupDir, "xrayr-cfg")
		}
	}

	progress(client, commandID, "INSTALLING", "写入二进制与目录")
	cfgDir := filepath.Dir(cfgPath)
	if out, err := sudoCombined("mkdir", "-p", filepath.Dir(binPath), cfgDir); err != nil {
		result(client, commandID, "FAILED", out, fmt.Errorf("创建目录: %w", err), nil)
		return
	}
	tmpBin := binPath + ".new." + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := copyFileChmod(artifactFile, tmpBin, 0755); err != nil {
		result(client, commandID, "FAILED", "", fmt.Errorf("准备二进制: %w", err), nil)
		return
	}
	if out, err := sudoCombined("install", "-m", "0755", "-o", "root", "-g", "root", tmpBin, binPath); err != nil {
		_ = os.Remove(tmpBin)
		result(client, commandID, "FAILED", out, fmt.Errorf("安装二进制: %w", err), nil)
		return
	}
	_ = os.Remove(tmpBin)

	if install {
		icy := strings.TrimSpace(strFrom(payload, "initial_config_yaml"))
		if icy != "" {
			sum := strings.ToLower(strings.TrimSpace(strFrom(payload, "initial_config_sha256")))
			raw := []byte(icy)
			h := sha256.Sum256(raw)
			if hex.EncodeToString(h[:]) != sum {
				rollbackBin(binPath, backupBin)
				result(client, commandID, "FAILED", "", fmt.Errorf("initial_config_sha256 不匹配"), nil)
				return
			}
			if err := writeFileViaSudo(cfgPath, raw, 0600); err != nil {
				rollbackBin(binPath, backupBin)
				result(client, commandID, "FAILED", "", fmt.Errorf("写入初始配置: %w", err), nil)
				return
			}
		}
	} else if payloadBool(payload, "overwrite_config") {
		raw := []byte(strFrom(payload, "config_yaml"))
		if err := writeFileViaSudo(cfgPath, raw, 0600); err != nil {
			rollbackUpgrade(binPath, svcPath, backupBin, backupSvc, hadOldSvc)
			_ = systemctlRestart(client, commandID, svc)
			result(client, commandID, "FAILED", "", fmt.Errorf("写入配置: %w", err), nil)
			return
		}
	}

	unit := renderSystemdUnit(binPath, cfgPath, svc)
	progress(client, commandID, "SYSTEMD", "写入 systemd unit")
	if err := writeFileViaSudo(svcPath, []byte(unit), 0644); err != nil {
		rollbackInstall(binPath, svcPath, install, backupBin, backupSvc, hadOldSvc)
		_ = systemctlRestart(client, commandID, svc)
		result(client, commandID, "FAILED", "", fmt.Errorf("写入 unit: %w", err), nil)
		return
	}

	progress(client, commandID, "RESTARTING", "daemon-reload 并重启服务")
	if out, err := sudoSystemctl("daemon-reload"); err != nil {
		rollbackFull(binPath, svcPath, cfgPath, install, backupBin, backupSvc, backupCfg, hadOldSvc)
		_ = systemctlRestart(client, commandID, svc)
		result(client, commandID, "FAILED", out, fmt.Errorf("daemon-reload: %w", err), map[string]any{"rollback": true})
		return
	}
	if install || payloadBool(payload, "enable_service") {
		if out, err := sudoSystemctl("enable", svc); err != nil {
			// enable 失败不阻断，记录日志
			_ = out
		}
	}
	if out, err := sudoSystemctl("restart", svc); err != nil {
		rollbackFull(binPath, svcPath, cfgPath, install, backupBin, backupSvc, backupCfg, hadOldSvc)
		_, _ = sudoSystemctl("daemon-reload")
		_ = systemctlRestart(client, commandID, svc)
		result(client, commandID, "FAILED", out, fmt.Errorf("systemctl restart: %w", err), map[string]any{"rollback": true})
		return
	}
	time.Sleep(2 * time.Second)

	progress(client, commandID, "VERIFY_SERVICE", "检查 systemd is-active")
	active := discovery.IsServiceActive(cfg)
	if !active {
		progress(client, commandID, "ROLLBACK", "服务未 active，尝试回滚")
		rollbackFull(binPath, svcPath, cfgPath, install, backupBin, backupSvc, backupCfg, hadOldSvc)
		_, _ = sudoSystemctl("daemon-reload")
		rbErr := systemctlRestart(client, commandID, svc)
		active2 := discovery.IsServiceActive(cfg)
		em := fmt.Errorf("重启后仍未 active")
		if rbErr != nil {
			em = fmt.Errorf("回滚后重启失败: %v; 原错误: %w", rbErr, em)
		}
		result(client, commandID, "FAILED", "已尝试回滚旧文件", em, map[string]any{
			"rollback": true, "active_after_rollback": active2, "backup_binary": backupBin, "backup_service": backupSvc, "backup_config": backupCfg,
		})
		return
	}

	progress(client, commandID, "DONE", "安装/升级完成")
	result(client, commandID, "SUCCESS", "XrayR 已就绪", nil, map[string]any{
		"binary_path": binPath, "config_path": cfgPath, "service_path": svcPath, "backup_binary": backupBin,
	})
}

func validateInstallPayload(cfg *agentcfg.File, payload map[string]any) error {
	psn := strings.TrimSpace(strFrom(payload, "service_name"))
	lsn := strings.TrimSpace(cfg.XrayR.ServiceName)
	if lsn == "" {
		lsn = "xrayr"
	}
	if psn == "" {
		psn = "xrayr"
	}
	if !strings.EqualFold(psn, lsn) {
		return fmt.Errorf("service_name 与 agent.yml 不一致")
	}
	if strings.TrimSpace(strFrom(payload, "target_binary_path")) != cfg.EffectiveBinaryPath() {
		return fmt.Errorf("target_binary_path 与 agent.yml binary_path 不一致")
	}
	if strings.TrimSpace(strFrom(payload, "target_config_path")) != strings.TrimSpace(cfg.XrayR.ConfigPath) {
		return fmt.Errorf("target_config_path 与 agent.yml config_path 不一致")
	}
	if strings.TrimSpace(strFrom(payload, "target_service_path")) != cfg.EffectiveServicePath() {
		return fmt.Errorf("target_service_path 与 agent.yml service_path 不一致")
	}
	if !strings.EqualFold(strings.TrimSpace(strFrom(payload, "target_os")), runtime.GOOS) {
		return fmt.Errorf("target_os 与本机不符")
	}
	if !archMatchesPayload(strings.TrimSpace(strFrom(payload, "target_arch")), runtime.GOARCH) {
		return fmt.Errorf("target_arch 与本机不符")
	}
	dl := strings.TrimSpace(strFrom(payload, "download_path"))
	if dl == "" || !strings.HasPrefix(dl, "/api/agent/artifacts/") || !strings.HasSuffix(dl, "/download") {
		return fmt.Errorf("非法 download_path")
	}
	if len(strings.TrimSpace(strFrom(payload, "sha256"))) != 64 {
		return fmt.Errorf("sha256 无效")
	}
	return nil
}

func strFrom(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	v, ok := m[k]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func payloadBool(m map[string]any, k string) bool {
	s := strings.ToLower(strings.TrimSpace(strFrom(m, k)))
	return s == "1" || s == "true" || s == "yes"
}

func archMatchesPayload(artifactArch, hostArch string) bool {
	a := strings.ToLower(artifactArch)
	h := strings.ToLower(hostArch)
	if a == h {
		return true
	}
	if (a == "amd64" && h == "x86_64") || (a == "x86_64" && h == "amd64") {
		return true
	}
	return false
}

func allowedBinaryPath(p string) bool {
	for _, x := range []string{"/usr/local/bin/XrayR", "/usr/bin/XrayR", "/opt/XrayR/XrayR"} {
		if p == x {
			return true
		}
	}
	return false
}

func allowedConfigPath(p string) bool {
	if strings.Contains(p, "..") {
		return false
	}
	if !strings.HasPrefix(p, "/etc/XrayR/") {
		return false
	}
	base := filepath.Base(p)
	return base == "config.yml" || base == "config.yaml"
}

func allowedServicePath(p string) bool {
	if strings.Contains(p, "..") || !strings.HasPrefix(p, "/etc/systemd/system/") || !strings.HasSuffix(p, ".service") {
		return false
	}
	base := strings.TrimSuffix(filepath.Base(p), ".service")
	for _, ch := range base {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

func dirAllowed(dir string) bool {
	d := filepath.Clean(dir)
	for _, root := range []string{"/var/lib/xrayr-agent", "/var/backups/xrayr-agent", "/etc/XrayR"} {
		if d == root || strings.HasPrefix(d, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func backupFileToDir(src, backupDir, prefix string) (string, error) {
	b, err := readFileMaybeSudo(src)
	if err != nil {
		return "", err
	}
	return writeBackupBytes(b, backupDir, prefix)
}

func readFileMaybeSudo(p string) ([]byte, error) {
	b, err := os.ReadFile(p)
	if err == nil {
		return b, nil
	}
	cmd := exec.Command("sudo", "-n", "cat", p)
	return cmd.Output()
}

func writeBackupBytes(b []byte, backupDir, prefix string) (string, error) {
	_ = os.MkdirAll(backupDir, 0755)
	name := fmt.Sprintf("%s-%s.bak", prefix, time.Now().Format("20060102-150405"))
	dst := filepath.Join(backupDir, name)
	if err := os.WriteFile(dst, b, 0600); err != nil {
		return "", err
	}
	return dst, nil
}

func sha256FileHex(path string) (string, error) {
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

func copyFileChmod(src, dst string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, mode)
}

func sudoCombined(args ...string) (string, error) {
	cmd := exec.Command("sudo", append([]string{"-n"}, args...)...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func sudoSystemctl(sub string, unitParts ...string) (string, error) {
	args := []string{"-n", "systemctl", sub}
	args = append(args, unitParts...)
	cmd := exec.Command("sudo", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

func writeFileViaSudo(dest string, data []byte, mode os.FileMode) error {
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("xrayr-agent-wr-%d", time.Now().UnixNano()))
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()
	out, err := sudoCombined("install", "-m", fmt.Sprintf("%04o", mode&0777), "-o", "root", "-g", "root", tmp, dest)
	if err != nil {
		return fmt.Errorf("%s: %w", out, err)
	}
	return nil
}

func renderSystemdUnit(binPath, cfgPath, unitName string) string {
	return fmt.Sprintf(`[Unit]
Description=XrayR
After=network.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s --config %s
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
`, filepath.Dir(cfgPath), binPath, cfgPath)
}

func rollbackBin(binPath, backup string) {
	if backup != "" {
		_, _ = sudoCombined("install", "-m", "0755", "-o", "root", "-g", "root", backup, binPath)
		return
	}
	_, _ = sudoCombined("rm", "-f", binPath)
}

func rollbackUpgrade(binPath, svcPath, backupBin, backupSvc string, hadOldSvc bool) {
	if backupBin != "" {
		_, _ = sudoCombined("install", "-m", "0755", "-o", "root", "-g", "root", backupBin, binPath)
	}
	if hadOldSvc && backupSvc != "" {
		b, _ := os.ReadFile(backupSvc)
		if len(b) > 0 {
			_ = writeFileViaSudo(svcPath, b, 0644)
		}
	}
}

func rollbackInstall(binPath, svcPath string, install bool, backupBin, backupSvc string, hadOldSvc bool) {
	if !install {
		rollbackUpgrade(binPath, svcPath, backupBin, backupSvc, hadOldSvc)
		return
	}
	_, _ = sudoCombined("rm", "-f", binPath)
	_, _ = sudoCombined("rm", "-f", svcPath)
}

func rollbackFull(binPath, svcPath, cfgPath string, install bool, backupBin, backupSvc, backupCfg string, hadOldSvc bool) {
	if install {
		_, _ = sudoCombined("rm", "-f", binPath)
		_, _ = sudoCombined("rm", "-f", svcPath)
		return
	}
	rollbackUpgrade(binPath, svcPath, backupBin, backupSvc, hadOldSvc)
	if backupCfg != "" {
		b, _ := os.ReadFile(backupCfg)
		if len(b) > 0 {
			_ = writeFileViaSudo(cfgPath, b, 0600)
		}
	}
}

func systemctlRestart(client *capi.Client, commandID, svc string) error {
	cmd := exec.Command("sudo", "-n", "systemctl", "restart", svc)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err != nil {
		_ = client.SignedJSON(http.MethodPost, "/api/agent/command/progress", map[string]any{
			"command_id": commandID, "step": "ROLLBACK", "message": "回滚阶段 systemctl restart: " + buf.String(),
		})
	}
	return err
}
