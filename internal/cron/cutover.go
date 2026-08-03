package cron

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"go-ispconfig/internal/getconf"
)

// DefaultCrontabDir is the PHP getconf cron.crontab_dir fallback.
const DefaultCrontabDir = "/etc/cron.d"

// CrontabDir returns getconf [cron] crontab_dir, or DefaultCrontabDir.
func CrontabDir(cfg *getconf.ServerConfig) string {
	if cfg != nil {
		if sec := cfg.Raw["cron"]; sec != nil {
			if d := strings.TrimSpace(sec["crontab_dir"]); d != "" {
				return d
			}
		}
	}
	return DefaultCrontabDir
}

// isLegacyISPCronFile reports whether name is an ISPConfig-written crontab
// under crontab_dir: ispc_* / ispc_chrooted_* (optional Gentoo .cron suffix).
func isLegacyISPCronFile(name string) bool {
	base := filepath.Base(name)
	return strings.HasPrefix(base, "ispc_") || strings.HasPrefix(base, "ispc_chrooted_")
}

// RemoveLegacyCrontabs deletes ispc_* / ispc_chrooted_* files under dir
// (design D7). Missing dir is a no-op. Returns the basenames removed.
func RemoveLegacyCrontabs(dir string, log *slog.Logger) ([]string, error) {
	if log == nil {
		log = slog.Default()
	}
	if dir == "" {
		return nil, fmt.Errorf("cron: empty crontab_dir")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cron: listing crontab_dir %s: %w", dir, err)
	}
	var removed []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isLegacyISPCronFile(name) {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return removed, fmt.Errorf("cron: removing legacy crontab %s: %w", path, err)
		}
		log.Info("cron: removed legacy crontab", "path", path)
		removed = append(removed, name)
	}
	return removed, nil
}
