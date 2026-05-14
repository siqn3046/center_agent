package agentcfg

import (
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type File struct {
	Center         CenterYAML         `yaml:"center"`
	RegisterToken  string             `yaml:"register_token"`
	Agent          AgentYAML          `yaml:"agent"`
	XrayR          XrayRYAML          `yaml:"xrayr"`
}

type CenterYAML struct {
	URL        string `yaml:"url"`
	NodeID     string `yaml:"node_id"`
	NodeSecret string `yaml:"node_secret"`
	TLSVerify  bool   `yaml:"tls_verify"`
}

type AgentYAML struct {
	HeartbeatInterval   string   `yaml:"heartbeat_interval"`
	ReportInterval      string   `yaml:"report_interval"`
	CommandPollInterval string   `yaml:"command_poll_interval"`
	LogTailLimit        int      `yaml:"log_tail_limit"`
	AllowCommands       []string `yaml:"allow_commands"`
	BackupDir           string   `yaml:"backup_dir"`
	ArtifactCacheDir    string   `yaml:"artifact_cache_dir"`
}

type XrayRYAML struct {
	Mode              string           `yaml:"mode"`
	ServiceName       string           `yaml:"service_name"`
	ServicePath       string           `yaml:"service_path"`
	BinaryPath        string           `yaml:"binary_path"`
	ConfigPath        string           `yaml:"config_path"`
	ConfigBackupDir   string           `yaml:"config_backup_dir"`
	BackupDir         string           `yaml:"backup_dir"`
	ErrorLogPath      string           `yaml:"error_log_path"`
	AccessLogPath     string           `yaml:"access_log_path"`
	BinarySearchPaths []string         `yaml:"binary_search_paths"`
	ConfigSearchPaths []string         `yaml:"config_search_paths"`
	HealthCheck       HealthCheckYAML  `yaml:"health_check"`
}

type HealthCheckYAML struct {
	Type    string `yaml:"type"`
	Timeout string `yaml:"timeout"`
	Ports   []uint16 `yaml:"ports"`
}

func Load(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	if f.XrayR.ServiceName == "" {
		f.XrayR.ServiceName = "xrayr"
	}
	if f.XrayR.BinaryPath == "" {
		f.XrayR.BinaryPath = "/usr/local/bin/XrayR"
	}
	if f.XrayR.ServicePath == "" {
		f.XrayR.ServicePath = "/etc/systemd/system/" + f.XrayR.ServiceName + ".service"
	}
	if len(f.XrayR.BinarySearchPaths) == 0 {
		f.XrayR.BinarySearchPaths = []string{"/usr/local/bin/XrayR", "/usr/bin/XrayR", "/opt/XrayR/XrayR"}
	}
	if len(f.XrayR.ConfigSearchPaths) == 0 {
		f.XrayR.ConfigSearchPaths = []string{"/etc/XrayR/config.yml", "/etc/XrayR/config.yaml"}
	}
	if f.XrayR.ConfigPath == "" {
		f.XrayR.ConfigPath = "/etc/XrayR/config.yml"
	}
	if f.XrayR.ConfigBackupDir == "" {
		f.XrayR.ConfigBackupDir = "/etc/XrayR/backups"
	}
	if f.XrayR.BackupDir == "" {
		f.XrayR.BackupDir = f.XrayR.ConfigBackupDir
	}
	if f.Agent.BackupDir == "" {
		f.Agent.BackupDir = "/var/lib/xrayr-agent/backups"
	}
	if f.Agent.ArtifactCacheDir == "" {
		f.Agent.ArtifactCacheDir = "/var/lib/xrayr-agent/artifacts"
	}
	if f.XrayR.ErrorLogPath == "" {
		f.XrayR.ErrorLogPath = "/var/log/xrayr/error.log"
	}
	if f.Agent.LogTailLimit == 0 {
		f.Agent.LogTailLimit = 300
	}
	return &f, nil
}

func (f *File) Save(path string) error {
	b, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0600)
}

func ParseDur(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}

// EffectiveBinaryPath 返回 Agent 本地用于安装/升级的二进制路径（仅来自配置，不信任 Center 任意路径）。
func (f *File) EffectiveBinaryPath() string {
	p := strings.TrimSpace(f.XrayR.BinaryPath)
	if p != "" {
		return p
	}
	return "/usr/local/bin/XrayR"
}

// EffectiveServicePath 返回 systemd unit 文件路径。
func (f *File) EffectiveServicePath() string {
	p := strings.TrimSpace(f.XrayR.ServicePath)
	if p != "" {
		return p
	}
	sn := strings.TrimSpace(f.XrayR.ServiceName)
	if sn == "" {
		sn = "xrayr"
	}
	return "/etc/systemd/system/" + sn + ".service"
}
