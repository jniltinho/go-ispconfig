package monitor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// MaxLogLines is the default tail size (PHP tail -n default via _getLogData).
const MaxLogLines = 100

// LogPaths holds daemon-side log file locations for tail collectors.
type LogPaths struct {
	// ISPConfig is the panel/daemon log (default /var/log/ispconfig/ispconfig.log).
	ISPConfig string
	// LetsEncrypt is the ACME/LE log.
	LetsEncrypt string
	// Messages is the system syslog (/var/log/syslog on Debian/Ubuntu).
	Messages string
}

// DefaultLogPaths returns distro-typical paths for go-ispconfig installs.
func DefaultLogPaths() LogPaths {
	return LogPaths{
		ISPConfig:   "/var/log/ispconfig/ispconfig.log",
		LetsEncrypt: "/var/log/letsencrypt/letsencrypt.log",
		Messages:    "/var/log/syslog",
	}
}

// TailFile returns the last maxLines of path. Safe against path traversal:
// only absolute paths under /var/log are accepted. Missing files return a
// readable message string (PHP parity).
func TailFile(path string, maxLines int) (string, error) {
	if maxLines <= 0 {
		maxLines = MaxLogLines
	}
	if err := validateLogPath(path); err != nil {
		return "Logfile path error.", nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) || os.IsPermission(err) {
			return "Unable to read " + path, nil
		}
		return "", err
	}
	defer f.Close() //nolint:errcheck // read-only file

	var lines []string
	sc := bufio.NewScanner(f)
	// Allow long log lines (up to 1 MiB).
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > maxLines {
			lines = lines[1:]
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}

func validateLogPath(path string) error {
	if path == "" || strings.Contains(path, "..") || strings.Contains(path, ";") {
		return fmt.Errorf("invalid log path")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("log path must be absolute")
	}
	clean := filepath.Clean(path)
	if !strings.HasPrefix(clean, "/var/log/") && !strings.HasPrefix(clean, "/tmp/") {
		// /tmp allowed only for tests; production uses /var/log.
		return fmt.Errorf("log path outside /var/log")
	}
	return nil
}

// CollectLogTail reads a log file and returns data as {output: text} for storage.
func CollectLogTail(path string, maxLines int) (map[string]any, string, error) {
	text, err := TailFile(path, maxLines)
	if err != nil {
		return nil, "unknown", err
	}
	return map[string]any{"output": text}, "ok", nil
}

// CollectSysLogState rolls open sys_log rows (loglevel > 0) into a monitor
// state for type sys_log. Payload body is empty/minimal (PHP parity).
func CollectSysLogState(ctx context.Context, db *gorm.DB, serverID uint32) (map[string]any, string, error) {
	var rows []model.SysLog
	err := db.WithContext(ctx).
		Where("server_id = ? AND loglevel > 0", serverID).
		Order("loglevel DESC").
		Limit(50).
		Find(&rows).Error
	if err != nil {
		return nil, "unknown", err
	}
	state := "ok"
	// ISPConfig: loglevel 0=debug cleared, 1=warning, 2=error (daemon constants).
	for _, r := range rows {
		switch {
		case r.Loglevel >= 2:
			state = SetState(state, "error")
		case r.Loglevel == 1:
			state = SetState(state, "warning")
		}
	}
	return map[string]any{
		"open_count": len(rows),
	}, state, nil
}

// RunLogCollectors tails configured logs and stores log_* + sys_log samples.
func RunLogCollectors(ctx context.Context, db *gorm.DB, serverID uint32, paths LogPaths) error {
	var first error
	store := func(typ, state string, data map[string]any) {
		if err := Store(ctx, db, serverID, typ, data, state, 0); err != nil && first == nil {
			first = err
		}
	}

	// log_ispconfig
	if paths.ISPConfig != "" {
		data, state, err := CollectLogTail(paths.ISPConfig, MaxLogLines)
		if err != nil && first == nil {
			first = err
		} else if err == nil {
			store("log_ispconfig", state, data)
		}
	}
	// log_letsencrypt — no_state in design
	if paths.LetsEncrypt != "" {
		data, _, err := CollectLogTail(paths.LetsEncrypt, MaxLogLines)
		if err != nil && first == nil {
			first = err
		} else if err == nil {
			store("log_letsencrypt", "no_state", data)
		}
	}
	// log_messages
	if paths.Messages != "" {
		data, _, err := CollectLogTail(paths.Messages, MaxLogLines)
		if err != nil && first == nil {
			first = err
		} else if err == nil {
			store("log_messages", "no_state", data)
		}
	}

	data, state, err := CollectSysLogState(ctx, db, serverID)
	if err != nil && first == nil {
		first = err
	} else if err == nil {
		store("sys_log", state, data)
	}
	return first
}
