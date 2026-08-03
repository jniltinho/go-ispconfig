package monitor

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"gorm.io/gorm"
)

// Version is set by the daemon bootstrap to the go-ispconfig version string
// for ispc_info collection (defaults to "dev").
var Version = "dev"

// CollectCPUInfo gathers cpu_info (PHP-compatible multi-key map from /proc
// style fields). State is always no_state.
func CollectCPUInfo(ctx context.Context) (map[string]any, string, error) {
	infos, err := cpu.InfoWithContext(ctx)
	if err != nil {
		return map[string]any{"output": ""}, "no_state", nil
	}
	if len(infos) == 0 {
		return map[string]any{"output": ""}, "no_state", nil
	}
	data := map[string]any{}
	for i, info := range infos {
		// Match PHP keys: "model name N", "cpu cores N", "processor N", …
		data[fmt.Sprintf("processor %d", i)] = fmt.Sprintf("%d", i)
		if info.ModelName != "" {
			data[fmt.Sprintf("model name %d", i)] = info.ModelName
		}
		if info.VendorID != "" {
			data[fmt.Sprintf("vendor_id %d", i)] = info.VendorID
		}
		if info.Cores > 0 {
			data[fmt.Sprintf("cpu cores %d", i)] = fmt.Sprintf("%d", info.Cores)
		}
		if info.Mhz > 0 {
			data[fmt.Sprintf("cpu MHz %d", i)] = fmt.Sprintf("%.3f", info.Mhz)
		}
		if info.Family != "" {
			data[fmt.Sprintf("cpu family %d", i)] = info.Family
		}
		if info.Model != "" {
			data[fmt.Sprintf("model %d", i)] = info.Model
		}
	}
	// Convenience fields for UI.
	data["model"] = infos[0].ModelName
	data["cores"] = runtime.NumCPU()
	return data, "no_state", nil
}

// CollectMemUsage gathers mem_usage with PHP /proc/meminfo key names (bytes).
func CollectMemUsage(ctx context.Context) (map[string]any, string, error) {
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, "no_state", fmt.Errorf("mem: %w", err)
	}
	swap, _ := mem.SwapMemoryWithContext(ctx)
	data := map[string]any{
		"MemTotal":     int64(v.Total),
		"MemFree":      int64(v.Free),
		"MemAvailable": int64(v.Available),
		"Buffers":      int64(v.Buffers),
		"Cached":       int64(v.Cached),
		"Active":       int64(v.Active),
		"Inactive":     int64(v.Inactive),
	}
	if swap != nil {
		data["SwapTotal"] = int64(swap.Total)
		data["SwapFree"] = int64(swap.Free)
	}
	return data, "no_state", nil
}

// CollectServerLoad gathers server_load with load_1/5/15 and uptime fields.
// Thresholds: load_1 >20 info, >50 warning, >100 critical, >150 error.
func CollectServerLoad(ctx context.Context) (map[string]any, string, error) {
	avg, err := load.AvgWithContext(ctx)
	if err != nil {
		return nil, "unknown", fmt.Errorf("load: %w", err)
	}
	uptimeSec, _ := host.UptimeWithContext(ctx)
	upDays := uptimeSec / 86400
	upHours := (uptimeSec - upDays*86400) / 3600
	upMinutes := (uptimeSec - upDays*86400 - upHours*3600) / 60

	users := 0
	if u, err := host.UsersWithContext(ctx); err == nil {
		users = len(u)
	}

	data := map[string]any{
		"load_1":      avg.Load1,
		"load_5":      avg.Load5,
		"load_15":     avg.Load15,
		"up_days":     upDays,
		"up_hours":    upHours,
		"up_minutes":  upMinutes,
		"user_online": users,
		"uptime": fmt.Sprintf(
			" up %d days, %d:%02d, %d users, load average: %.2f, %.2f, %.2f",
			upDays, upHours, upMinutes, users, avg.Load1, avg.Load5, avg.Load15,
		),
	}
	return data, LoadState(avg.Load1), nil
}

// LoadState maps load_1 to severity (PHP 100-monitor_server thresholds).
func LoadState(load1 float64) string {
	state := "ok"
	if load1 > 20 {
		state = "info"
	}
	if load1 > 50 {
		state = "warning"
	}
	if load1 > 100 {
		state = "critical"
	}
	if load1 > 150 {
		state = "error"
	}
	return state
}

// CollectOSInfo gathers os_info name/version via gopsutil host.
func CollectOSInfo(ctx context.Context) (map[string]any, string, error) {
	info, err := host.InfoWithContext(ctx)
	if err != nil {
		return map[string]any{"name": runtime.GOOS, "version": ""}, "no_state", nil
	}
	name := info.Platform
	if name == "" {
		name = info.OS
	}
	version := info.PlatformVersion
	if version == "" {
		version = info.KernelVersion
	}
	return map[string]any{
		"name":    name,
		"version": version,
	}, "no_state", nil
}

// CollectKernelInfo gathers kernel_info (uname-style).
func CollectKernelInfo(ctx context.Context) (map[string]any, string, error) {
	info, err := host.InfoWithContext(ctx)
	if err != nil {
		return map[string]any{
			"kernel": runtime.GOOS,
			"output": runtime.GOOS + " " + runtime.GOARCH,
		}, "no_state", nil
	}
	return map[string]any{
		"kernel":       info.KernelVersion,
		"hostname":     info.Hostname,
		"os":           info.OS,
		"platform":     info.Platform,
		"architecture": info.KernelArch,
		"output":       strings.TrimSpace(info.Platform + " " + info.KernelVersion + " " + info.KernelArch),
	}, "no_state", nil
}

// CollectISPCInfo gathers ispc_info with the go-ispconfig version string.
func CollectISPCInfo(_ context.Context) (map[string]any, string, error) {
	return map[string]any{
		"version": Version,
		"name":    "go-ispconfig",
	}, "no_state", nil
}

// RunBasicCollectors executes the no_state / load host collectors and stores
// each result. serverID is the local server row.
func RunBasicCollectors(ctx context.Context, db *gorm.DB, serverID uint32) error {
	type job struct {
		typ string
		fn  func(context.Context) (map[string]any, string, error)
	}
	jobs := []job{
		{"cpu_info", CollectCPUInfo},
		{"mem_usage", CollectMemUsage},
		{"server_load", CollectServerLoad},
		{"os_info", CollectOSInfo},
		{"kernel_info", CollectKernelInfo},
		{"ispc_info", CollectISPCInfo},
	}
	var first error
	for _, j := range jobs {
		data, state, err := j.fn(ctx)
		if err != nil {
			if first == nil {
				first = fmt.Errorf("%s: %w", j.typ, err)
			}
			continue
		}
		if err := Store(ctx, db, serverID, j.typ, data, state, 0); err != nil && first == nil {
			first = err
		}
	}
	return first
}
