package monitor

import (
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

func Snapshot() map[string]any {
	out := map[string]any{}
	if p, err := cpu.Percent(0, false); err == nil && len(p) > 0 {
		out["cpu_pct"] = p[0]
	}
	if v, err := mem.VirtualMemory(); err == nil {
		out["mem_pct"] = v.UsedPercent
	}
	if d, err := disk.Usage("/"); err == nil {
		out["disk_pct"] = d.UsedPercent
	}
	if l, err := load.Avg(); err == nil {
		out["load1"] = l.Load1
	}
	if n, err := net.IOCounters(false); err == nil && len(n) > 0 {
		out["net_bytes_in"] = n[0].BytesRecv
		out["net_bytes_out"] = n[0].BytesSent
	}
	return out
}
