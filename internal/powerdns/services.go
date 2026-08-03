package powerdns

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gorm.io/gorm"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/model"
)

// DefaultAXFRConf is the default allow-axfr-ips include path.
const DefaultAXFRConf = getconf.DefaultPowerDNSAXFRConf

// systemd unit dirs for unit-file presence (shared idea with bind).
var systemdUnitDirs = []string{
	"/etc/systemd/system",
	"/lib/systemd/system",
	"/usr/lib/systemd/system",
}

// Executor wraps the services Executor with PowerDNS unit resolution and
// AXFR allow-list rewrite on restart (port of restartPowerDNS).
type Executor struct {
	// Inner performs the actual service action.
	Inner engine.Executor
	// PanelDB is used to collect active xfer values on restart.
	PanelDB *gorm.DB
	// ServerID scopes xfer collection.
	ServerID uint32
	// AXFRPath overrides the written allow-axfr file (empty → getconf default).
	AXFRPath string
	// UnitExists reports whether a systemd unit exists; nil means standard dirs.
	UnitExists func(unit string) bool
	// WriteFile overrides atomic file write (tests).
	WriteFile func(path string, data []byte, perm os.FileMode) error

	once sync.Once
	unit string
}

// Run implements engine.Executor.
func (e *Executor) Run(ctx context.Context, service, action string) error {
	if service != ServiceName {
		return e.Inner.Run(ctx, service, action)
	}
	e.once.Do(func() {
		exists := e.UnitExists
		if exists == nil {
			exists = unitFileExists
		}
		// PHP: powerdns if present, else pdns.
		e.unit = "pdns"
		if exists("powerdns") {
			e.unit = "powerdns"
		}
	})
	if action == engine.ActionRestart {
		if err := e.rewriteAXFR(ctx); err != nil {
			// Log path: return error so the services flush records it.
			return fmt.Errorf("powerdns: rewrite axfr allow-list: %w", err)
		}
	}
	return e.Inner.Run(ctx, e.unit, action)
}

func (e *Executor) rewriteAXFR(ctx context.Context) error {
	path := e.AXFRPath
	if path == "" {
		path = DefaultAXFRConf
	}
	ips := []string{"127.0.0.1"}
	if e.PanelDB != nil {
		ips = append(ips, collectXferIPs(ctx, e.PanelDB, e.ServerID)...)
	}
	line := "allow-axfr-ips=" + strings.Join(uniqueIPs(ips), ",") + "\n"
	write := e.WriteFile
	if write == nil {
		write = func(p string, data []byte, perm os.FileMode) error {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			tmp := p + ".tmp"
			if err := os.WriteFile(tmp, data, perm); err != nil {
				return err
			}
			return os.Rename(tmp, p)
		}
	}
	return write(path, []byte(line), 0o644)
}

// collectXferIPs gathers non-empty xfer fields from active dns_soa and
// dns_slave for this server (comma/space separated lists).
func collectXferIPs(ctx context.Context, db *gorm.DB, serverID uint32) []string {
	var out []string
	var soas []model.DNSSoa
	if err := db.WithContext(ctx).
		Select("xfer").
		Where("server_id = ? AND active = ?", serverID, "Y").
		Find(&soas).Error; err == nil {
		for _, s := range soas {
			out = append(out, splitXfer(s.Xfer)...)
		}
	}
	var slaves []model.DNSSlave
	if err := db.WithContext(ctx).
		Select("xfer").
		Where("server_id = ? AND active = ?", serverID, "Y").
		Find(&slaves).Error; err == nil {
		for _, s := range slaves {
			out = append(out, splitXfer(s.Xfer)...)
		}
	}
	return out
}

func splitXfer(xfer string) []string {
	xfer = strings.TrimSpace(xfer)
	if xfer == "" {
		return nil
	}
	// Accept comma and whitespace separators.
	fields := strings.FieldsFunc(xfer, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == ';'
	})
	var out []string
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// uniqueIPs returns ips with duplicates removed, preserving order; always
// keeps 127.0.0.1 first when present.
func uniqueIPs(ips []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ip := range ips {
		ip = strings.TrimSpace(ip)
		if ip == "" || seen[ip] {
			continue
		}
		seen[ip] = true
		out = append(out, ip)
	}
	return out
}

func unitFileExists(unit string) bool {
	for _, dir := range systemdUnitDirs {
		if _, err := os.Stat(dir + "/" + unit + ".service"); err == nil {
			return true
		}
	}
	return false
}

// RegisterServices declares the powerdns service in the delayed-restart registry.
func RegisterServices(s *engine.Services) {
	s.Register(ServiceName)
}

// BuildAXFRLine is exported for tests: localhost + unique IPs from xfer lists.
func BuildAXFRLine(xferLists ...string) string {
	ips := []string{"127.0.0.1"}
	for _, x := range xferLists {
		ips = append(ips, splitXfer(x)...)
	}
	return "allow-axfr-ips=" + strings.Join(uniqueIPs(ips), ",")
}
