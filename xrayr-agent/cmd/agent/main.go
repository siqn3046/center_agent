package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/XrayR-project/XrayR/xrayr-agent/internal/agentcfg"
	"github.com/XrayR-project/XrayR/xrayr-agent/internal/capi"
	"github.com/XrayR-project/XrayR/xrayr-agent/internal/discovery"
	"github.com/XrayR-project/XrayR/xrayr-agent/internal/monitor"
)

func main() {
	cfgPath := flag.String("config", "/etc/xrayr-agent/agent.yml", "agent config path")
	flag.Parse()
	cfg, err := agentcfg.Load(*cfgPath)
	if err != nil {
		log.Fatal("load config: ", err)
	}

	if cfg.RegisterToken != "" && cfg.Center.NodeSecret == "" {
		cl0 := capi.New(cfg)
		out, err := cl0.Register(map[string]any{
			"register_token": cfg.RegisterToken,
			"hostname":       hostname(),
			"os":             runtime.GOOS,
			"arch":           runtime.GOARCH,
			"agent_version":  "0.1.0-mvp",
		})
		if err != nil {
			log.Fatal("register: ", err)
		}
		nid := fmtInt(out["node_id"])
		sec, _ := out["node_secret"].(string)
		cfg.Center.NodeID = nid
		cfg.Center.NodeSecret = sec
		cfg.RegisterToken = ""
		if err := cfg.Save(*cfgPath); err != nil {
			log.Fatal("save config: ", err)
		}
		log.Println("registered node_id=", nid)
	}

	if cfg.Center.NodeID == "" || cfg.Center.NodeSecret == "" {
		log.Fatal("missing node_id/node_secret; provide register_token for first boot")
	}

	client := capi.New(cfg)
	hb := agentcfg.ParseDur(cfg.Agent.HeartbeatInterval, 30*time.Second)
	rep := agentcfg.ParseDur(cfg.Agent.ReportInterval, 60*time.Second)

	res, err := discovery.Run(cfg)
	if err != nil {
		log.Fatal("discovery: ", err)
	}
	if err := postDiscovery(client, cfg, res); err != nil {
		log.Fatal("discovery report: ", err)
	}

	tick := time.NewTicker(hb)
	tickRep := time.NewTicker(rep)
	defer tick.Stop()
	defer tickRep.Stop()

	go func() {
		for range tick.C {
			_ = client.SignedJSON(http.MethodPost, "/api/agent/heartbeat", map[string]any{
				"uptime_sec":    uptime(),
				"agent_version": "0.1.0-mvp",
				"xrayr_running": res.InstallState == "INSTALLED_RUNNING",
			})
		}
	}()
	go func() {
		for range tickRep.C {
			_ = client.SignedJSON(http.MethodPost, "/api/agent/monitor/report", monitor.Snapshot())
			active := res.ServiceStatus["is_active"] == "active"
			_ = client.SignedJSON(http.MethodPost, "/api/agent/xrayr/status", map[string]any{
				"systemd_active": active,
				"config_hash":      res.ConfigHash,
				"xrayr_version":    res.VersionInfo["xrayr_version_output"],
				"error_tail":       truncate(res.ErrorTail, 2000),
			})
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
}

func postDiscovery(client *capi.Client, cfg *agentcfg.File, res *discovery.Result) error {
	body := map[string]any{
		"install_state":              res.InstallState,
		"binary_paths":               res.BinaryPaths,
		"config_paths":               res.ConfigPaths,
		"service_status":             res.ServiceStatus,
		"process_status":             res.ProcessStatus,
		"version_info":               res.VersionInfo,
		"config_hash":                res.ConfigHash,
		"error_tail":                 res.ErrorTail,
		"config_yaml":                res.ConfigYAML,
		"imported_backup_path":       res.ImportedBackupPath,
		"discovered_binary_path":     res.DiscoveredBinaryPath,
		"discovered_config_path":     res.DiscoveredConfigPath,
		"discovered_service_name":    res.ServiceName,
		"discovered_xrayr_version":   firstLine(res.VersionInfo["xrayr_version_output"]),
		"discovered_xray_core_version": "",
	}
	if res.InstallState == "INSTALLED_RUNNING" {
		body["imported_config_hash"] = res.ConfigHash
	}
	return client.SignedJSON(http.MethodPost, "/api/agent/discovery/report", body)
}

func firstLine(s string) string {
	for i := range s {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func fmtInt(v any) string {
	switch t := v.(type) {
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case string:
		return t
	default:
		return ""
	}
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

func uptime() int64 {
	// 简化：进程启动时长
	return int64(time.Since(startTime).Seconds())
}

var startTime = time.Now()
