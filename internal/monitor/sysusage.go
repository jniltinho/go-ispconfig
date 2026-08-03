package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// MaxSysUsagePoints caps rolling series length (PHP max_data_points = 15).
const MaxSysUsagePoints = 15

// SysUsagePayload is the JSON body for type sys_usage.
type SysUsagePayload struct {
	Tstamp int64       `json:"tstamp"`
	Load   []float64   `json:"load"`
	Mem    []float64   `json:"mem"`
	Time   []string    `json:"time"`
	Net    []NetPoint  `json:"net,omitempty"`
	Rx     uint64      `json:"rx"`
	Tx     uint64      `json:"tx"`
}

// NetPoint is one rx/tx sample in KB/s.
type NetPoint struct {
	Rx float64 `json:"rx"`
	Tx float64 `json:"tx"`
}

// AppendCapped appends v to series and drops the oldest when over max.
func AppendCapped[T any](series []T, v T, max int) []T {
	if max <= 0 {
		max = MaxSysUsagePoints
	}
	series = append(series, v)
	if len(series) > max {
		series = series[len(series)-max:]
	}
	return series
}

// CollectSysUsage builds the next sys_usage payload from previous state.
func CollectSysUsage(ctx context.Context, prev *SysUsagePayload) (*SysUsagePayload, error) {
	now := time.Now()
	out := &SysUsagePayload{
		Tstamp: now.Unix(),
		Load:   []float64{},
		Mem:    []float64{},
		Time:   []string{},
		Net:    []NetPoint{},
	}
	if prev != nil {
		out.Load = append([]float64{}, prev.Load...)
		out.Mem = append([]float64{}, prev.Mem...)
		out.Time = append([]string{}, prev.Time...)
		out.Net = append([]NetPoint{}, prev.Net...)
		out.Rx = prev.Rx
		out.Tx = prev.Tx
	}

	interval := 60.0
	if prev != nil && prev.Tstamp > 0 {
		interval = float64(now.Unix() - prev.Tstamp)
		if interval <= 0 {
			interval = 60
		}
	}

	// Load % = load_1 / cores * 100
	avg, err := load.AvgWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("load: %w", err)
	}
	cores, err := cpu.CountsWithContext(ctx, true)
	if err != nil || cores < 1 {
		cores = 1
	}
	loadPct := round2((avg.Load1 / float64(cores)) * 100)
	out.Load = AppendCapped(out.Load, loadPct, MaxSysUsagePoints)
	out.Time = AppendCapped(out.Time, now.Format("15:04"), MaxSysUsagePoints)

	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("mem: %w", err)
	}
	memPct := 0.0
	if v.Total > 0 {
		used := v.Total - v.Available
		memPct = round2(float64(used) / float64(v.Total) * 100)
	}
	out.Mem = AppendCapped(out.Mem, memPct, MaxSysUsagePoints)

	rx, tx := networkBytes(ctx)
	if prev != nil && (prev.Rx > 0 || prev.Tx > 0) {
		out.Net = AppendCapped(out.Net, NetPoint{
			Rx: abs(float64(rx-prev.Rx)) / interval / 1024,
			Tx: abs(float64(tx-prev.Tx)) / interval / 1024,
		}, MaxSysUsagePoints)
	}
	out.Rx = rx
	out.Tx = tx
	return out, nil
}

func networkBytes(ctx context.Context) (rx, tx uint64) {
	stats, err := net.IOCountersWithContext(ctx, true)
	if err != nil {
		return 0, 0
	}
	for _, s := range stats {
		if s.Name == "lo" || s.Name == "lo0" {
			continue
		}
		rx += s.BytesRecv
		tx += s.BytesSent
	}
	return rx, tx
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

// RunSysUsageCollector loads previous payload, appends points, upserts.
func RunSysUsageCollector(ctx context.Context, db *gorm.DB, serverID uint32) error {
	var prev *SysUsagePayload
	var row model.MonitorData
	err := db.WithContext(ctx).
		Where("type = ? AND server_id = ?", "sys_usage", serverID).
		Order("created DESC").
		First(&row).Error
	if err == nil && row.Data != "" {
		dec := DecodePayload(row.Data)
		if dec.DecodeError == "" {
			if m, ok := asStringMap(dec.Value); ok {
				prev = mapToSysUsage(m)
			}
		}
	}
	payload, err := CollectSysUsage(ctx, prev)
	if err != nil {
		return err
	}
	// PHP uses state ok for sys_usage; design says no_state — keep ok for dashlet parity with PHP write path.
	return UpsertType(ctx, db, serverID, "sys_usage", payload, "ok")
}

func mapToSysUsage(m map[string]any) *SysUsagePayload {
	p := &SysUsagePayload{}
	if v, ok := m["tstamp"].(float64); ok {
		p.Tstamp = int64(v)
	}
	p.Load = floatSlice(m["load"])
	p.Mem = floatSlice(m["mem"])
	p.Time = stringSlice(m["time"])
	if v, ok := m["rx"].(float64); ok {
		p.Rx = uint64(v)
	}
	if v, ok := m["tx"].(float64); ok {
		p.Tx = uint64(v)
	}
	if raw, ok := m["net"].([]any); ok {
		for _, item := range raw {
			if nm, ok := asStringMap(item); ok {
				pt := NetPoint{}
				if v, ok := nm["rx"].(float64); ok {
					pt.Rx = v
				}
				if v, ok := nm["tx"].(float64); ok {
					pt.Tx = v
				}
				p.Net = append(p.Net, pt)
			}
		}
	}
	return p
}

func floatSlice(v any) []float64 {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]float64, 0, len(arr))
	for _, x := range arr {
		if f, ok := x.(float64); ok {
			out = append(out, f)
		}
	}
	return out
}

func stringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
