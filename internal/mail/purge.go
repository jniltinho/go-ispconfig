package mail

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// softDeleteSuffixPrefix is the marker mailPlugin.removeTree appends to a
// soft-deleted maildir (<path>-deleted-<YmdHis>).
const softDeleteSuffixPrefix = "-deleted-"

// RegisterPurgeJob registers the daily soft-delete purge (PHP cron
// 500-clean_mailboxes analogue): remove soft-deleted maildir/domain
// trees under homedir_path older than mailbox_soft_delete days. A
// retention of 0/off disables the removal (the job still runs, no-op).
func (p *Plugin) RegisterPurgeJob(s *engine.Scheduler) error {
	return s.Register("mail_soft_delete_purge", "@daily", p.purgeJob)
}

// purgeJob scans homedir_path for "-deleted-<timestamp>" trees and
// removes those whose timestamp is older than the retention window.
func (p *Plugin) purgeJob(ctx context.Context) error {
	cfg, err := p.config(ctx)
	if err != nil {
		p.log.Warn("mail: using default [mail] config for purge", "error", err)
	}
	days := retentionDays(cfg.MailboxSoftDelete)
	if days <= 0 {
		return nil // soft delete off, or "keep forever"
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	home := strings.TrimSuffix(cfg.HomedirPath, "/")

	// Soft-deleted trees live one level under homedir_path (mailbox:
	// homedir/<domain>/<local>-deleted-*) and directly under it (domain:
	// homedir/<domain>-deleted-*). Walk both levels.
	p.purgeDir(ctx, cfg, home, cutoff)
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil // homedir absent on a non-mail run
	}
	for _, e := range entries {
		if e.IsDir() && !strings.Contains(e.Name(), softDeleteSuffixPrefix) {
			p.purgeDir(ctx, cfg, home+"/"+e.Name(), cutoff)
		}
	}
	return nil
}

// purgeDir removes soft-deleted children of dir older than cutoff,
// re-checking the delete guards on every path.
func (p *Plugin) purgeDir(ctx context.Context, cfg getconf.MailConfig, dir string, cutoff time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ts, ok := softDeleteTimestamp(e.Name())
		if !ok || !ts.Before(cutoff) {
			continue
		}
		path := dir + "/" + e.Name()
		if !safeMailPath(path, cfg.HomedirPath) {
			p.log.Error("mail: purge refused unsafe path", "path", path)
			continue
		}
		if _, err := p.runner.Run(ctx, "rm", "-rf", path); err != nil {
			p.log.Error("mail: purge delete failed", "path", path, "error", err)
			continue
		}
		p.log.Info("mail: purged soft-deleted tree", "path", path)
	}
}

// softDeleteTimestamp parses the trailing -deleted-<YmdHis> stamp.
func softDeleteTimestamp(name string) (time.Time, bool) {
	i := strings.LastIndex(name, softDeleteSuffixPrefix)
	if i < 0 {
		return time.Time{}, false
	}
	stamp := name[i+len(softDeleteSuffixPrefix):]
	ts, err := time.ParseInLocation("20060102150405", stamp, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

// retentionDays reads mailbox_soft_delete as a day count. 'y' means the
// checkbox is on with no age window in PHP; here it maps to the daemon
// default retention so the purge still fires.
func retentionDays(v string) int {
	switch v {
	case "", "0", "n":
		return 0
	case "y":
		return defaultSoftDeleteDays
	default:
		n, err := strconv.Atoi(v)
		if err != nil {
			return 0
		}
		return n
	}
}

// defaultSoftDeleteDays is the retention used when soft delete is on via
// the plain 'y' flag (no explicit day count).
const defaultSoftDeleteDays = 30
