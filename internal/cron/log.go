package cron

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// sys_log loglevel values (ISPConfig LOGLEVEL_* / engine constants).
const (
	logLevelInfo  int8 = 0
	logLevelWarn  int8 = 1
	logLevelError int8 = 2
)

// maxLogOutput is the bounded tail stored in the sys_log message (design D6).
const maxLogOutput = 4 * 1024

// RunLogInput is the data needed to write one cron_run sys_log row.
type RunLogInput struct {
	ServerID       uint32
	CronID         uint32
	ParentDomainID uint32
	Type           string
	// Log is the cron.log flag ("y"/"n"). Security aborts still log when "n".
	Log    string
	Result RunResult
	// Security is true for privilege-drop failures and similar aborts that
	// must be logged even when log='n' (design D6).
	Security bool
}

// FormatRunMessage builds the single-line sys_log message convention
// (design D6): cron_run id=… parent_domain_id=… type=… status=… exit=…
// start=… end=… output=…
func FormatRunMessage(in RunLogInput) string {
	out := sanitizeOutput(in.Result.Output, maxLogOutput)
	start := in.Result.Start
	end := in.Result.End
	if start.IsZero() {
		start = time.Now()
	}
	if end.IsZero() {
		end = start
	}
	return fmt.Sprintf(
		"cron_run id=%d parent_domain_id=%d type=%s status=%s exit=%d start=%d end=%d output=%s",
		in.CronID,
		in.ParentDomainID,
		in.Type,
		in.Result.Status,
		in.Result.ExitCode,
		start.Unix(),
		end.Unix(),
		out,
	)
}

// ShouldLogRun reports whether a finished run should produce a sys_log row:
// always when log='y'; always on security aborts; otherwise only on
// non-ok status when log='y' is already covered — when log='n', successful
// runs produce nothing; security failures still log.
func ShouldLogRun(logFlag string, status string, security bool) bool {
	if security {
		return true
	}
	if logFlag == "y" {
		return true
	}
	return false
}

// WriteRunLog inserts a sys_log row when ShouldLogRun is true. datalog_id is
// always 0 (not tied to a datalog row). Returns nil when nothing is written.
func WriteRunLog(ctx context.Context, db *gorm.DB, in RunLogInput) error {
	if !ShouldLogRun(in.Log, in.Result.Status, in.Security) {
		return nil
	}
	if db == nil {
		return fmt.Errorf("cron: WriteRunLog requires a database")
	}
	level := logLevelInfo
	switch {
	case in.Security || in.Result.Status == StatusError:
		level = logLevelError
	case in.Result.Status == StatusTimeout || in.Result.Status == StatusExit:
		level = logLevelWarn
	}
	entry := model.SysLog{
		ServerID:  in.ServerID,
		DatalogID: 0,
		Loglevel:  level,
		Tstamp:    uint32(time.Now().Unix()),
		Message:   FormatRunMessage(in),
	}
	if err := db.WithContext(ctx).Create(&entry).Error; err != nil {
		return fmt.Errorf("cron: writing sys_log run row: %w", err)
	}
	return nil
}

// RunMessagePrefix returns the LIKE/prefix filter used by the run-history API.
func RunMessagePrefix(cronID uint32) string {
	return fmt.Sprintf("cron_run id=%d ", cronID)
}

func sanitizeOutput(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if len(s) > max {
		s = s[len(s)-max:]
	}
	// Avoid empty output tokens that break simple scanners.
	if s == "" {
		return "-"
	}
	return s
}
