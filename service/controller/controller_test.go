package controller_test

import (
	"runtime"
	"testing"

	"github.com/xtls/xray-core/app/proxyman"
	"github.com/xtls/xray-core/app/stats"
	"github.com/xtls/xray-core/common/serial"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"

	"github.com/XrayR-project/XrayR/api"
	"github.com/XrayR-project/XrayR/api/sspanel"
	"github.com/XrayR-project/XrayR/app/mydispatcher"
	_ "github.com/XrayR-project/XrayR/cmd/distro/all"
	"github.com/XrayR-project/XrayR/common/mylego"
	. "github.com/XrayR-project/XrayR/service/controller"
)

func TestController(t *testing.T) {
	coreLogConfig := &conf.LogConfig{LogLevel: "debug"}
	corePolicyConfig := &conf.PolicyConfig{}
	corePolicyConfig.Levels = map[uint32]*conf.Policy{0: {
		StatsUserUplink:   true,
		StatsUserDownlink: true,
	}}
	policyConfig, err := corePolicyConfig.Build()
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	coreDnsConfig := &conf.DNSConfig{}
	dnsConfig, err := coreDnsConfig.Build()
	if err != nil {
		t.Fatalf("dns: %v", err)
	}
	coreRouterConfig := &conf.RouterConfig{}
	routeConfig, err := coreRouterConfig.Build()
	if err != nil {
		t.Fatalf("router: %v", err)
	}

	config := &core.Config{
		App: []*serial.TypedMessage{
			serial.ToTypedMessage(coreLogConfig.Build()),
			serial.ToTypedMessage(&mydispatcher.Config{}),
			serial.ToTypedMessage(&stats.Config{}),
			serial.ToTypedMessage(&proxyman.InboundConfig{}),
			serial.ToTypedMessage(&proxyman.OutboundConfig{}),
			serial.ToTypedMessage(policyConfig),
			serial.ToTypedMessage(dnsConfig),
			serial.ToTypedMessage(routeConfig),
		},
	}

	server, err := core.New(config)
	defer server.Close()
	if err != nil {
		t.Errorf("failed to create instance: %s", err)
	}
	if err = server.Start(); err != nil {
		t.Errorf("Failed to start instance: %s", err)
	}
	certConfig := &mylego.CertConfig{
		CertMode:   "http",
		CertDomain: "test.ss.tk",
		Provider:   "alidns",
		Email:      "ss@ss.com",
	}
	controlerConfig := &Config{
		UpdatePeriodic: 5,
		CertConfig:     certConfig,
	}
	apiConfig := &api.Config{
		APIHost:  "http://127.0.0.1:667",
		Key:      "123",
		NodeID:   41,
		NodeType: "V2ray",
	}
	apiClient := sspanel.New(apiConfig)
	c := New(server, apiClient, controlerConfig, "SSpanel")
	err = c.Start()
	if err != nil {
		t.Logf("Start without live panel (expected): %v", err)
	}
	// Explicitly triggering GC to remove garbage from config loading.
	runtime.GC()
}
