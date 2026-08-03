package monitor

import (
	"context"
	"fmt"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
)

// skipFSTypes are filesystem types PHP ignores for fill thresholds.
var skipFSTypes = map[string]bool{
	"iso9660":  true,
	"cramfs":   true,
	"udf":      true,
	"tmpfs":    true,
	"devtmpfs": true,
	"udev":     true,
	"squashfs": true,
	"overlay":  false, // still report, apply thresholds
	"simfs":    true,
	"efivarfs": true,
}

// CollectDiskUsage gathers disk_usage partition rows with PHP keys
// (fs, type, size, used, available, percent, mounted) and applies fill
// thresholds: 75/80/90/95% with free-size gates 2000/1000/500/100 MiB.
func CollectDiskUsage(ctx context.Context) (map[string]any, string, error) {
	parts, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, "unknown", fmt.Errorf("partitions: %w", err)
	}
	state := "ok"
	data := map[string]any{}
	idx := 1
	for _, p := range parts {
		usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			continue
		}
		row := map[string]any{
			"fs":        p.Device,
			"type":      p.Fstype,
			"size":      humanSize(usage.Total),
			"used":      humanSize(usage.Used),
			"available": humanSize(usage.Free),
			"percent":   fmt.Sprintf("%.0f%%", usage.UsedPercent),
			"mounted":   p.Mountpoint,
			// numeric helpers for thresholds / UI
			"size_bytes":      usage.Total,
			"used_bytes":      usage.Used,
			"available_bytes": usage.Free,
			"use_percent":     usage.UsedPercent,
		}
		data[fmt.Sprintf("%d", idx)] = row
		idx++

		if skipFSTypes[strings.ToLower(p.Fstype)] {
			continue
		}
		state = SetState(state, DiskFillState(usage.UsedPercent, usage.Free))
	}
	return data, state, nil
}

// DiskFillState returns severity for one filesystem (PHP disk_usage thresholds).
// freeBytes is free space in bytes; free-size gates are in MiB.
func DiskFillState(usePercent float64, freeBytes uint64) string {
	freeMiB := float64(freeBytes) / (1024 * 1024)
	state := "ok"
	if usePercent > 75 && freeMiB < 2000 {
		state = SetState(state, "info")
	}
	if usePercent > 80 && freeMiB < 1000 {
		state = SetState(state, "warning")
	}
	if usePercent > 90 && freeMiB < 500 {
		state = SetState(state, "critical")
	}
	if usePercent > 95 && freeMiB < 100 {
		state = SetState(state, "error")
	}
	return state
}

// humanSize formats bytes like df -h (G/M/K) for PHP-compatible display.
func humanSize(b uint64) string {
	const (
		k = 1024
		m = k * 1024
		g = m * 1024
		t = g * 1024
	)
	switch {
	case b >= t:
		return fmt.Sprintf("%.1fT", float64(b)/float64(t))
	case b >= g:
		return fmt.Sprintf("%.1fG", float64(b)/float64(g))
	case b >= m:
		return fmt.Sprintf("%.1fM", float64(b)/float64(m))
	case b >= k:
		return fmt.Sprintf("%.1fK", float64(b)/float64(k))
	default:
		return fmt.Sprintf("%d", b)
	}
}
