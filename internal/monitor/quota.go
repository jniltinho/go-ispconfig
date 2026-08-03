package monitor

// Quota collectors for the monitor_data types the legacy dashboard dashlets
// read: harddisk_quota (per website), email_quota (per mailbox) and
// database_size (per client database). Each stores one rolling row per
// server via UpsertType, so the dashboard always reads the latest sample.

import (
	"bufio"
	"context"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// quotaCmdTimeout bounds every external quota command.
const quotaCmdTimeout = 60 * time.Second

// HDQuotaEntry is one website's disk usage in KB (soft/hard 0 = unlimited).
type HDQuotaEntry struct {
	Domain string `json:"domain"`
	User   string `json:"user"`
	Used   int64  `json:"used"`
	Soft   int64  `json:"soft"`
	Hard   int64  `json:"hard"`
}

// MailQuotaEntry is one mailbox's usage in bytes (quota 0 = unlimited).
type MailQuotaEntry struct {
	Email string `json:"email"`
	Used  int64  `json:"used"`
	Quota int64  `json:"quota"`
}

// DatabaseSizeEntry is one client database's size in bytes (quota 0 = unlimited).
type DatabaseSizeEntry struct {
	DatabaseName string `json:"database_name"`
	Size         int64  `json:"size"`
	Quota        int64  `json:"quota"`
}

// CollectDatabaseSize sums data_length+index_length per schema and joins the
// result with the server's web_database rows. One information_schema scan.
func CollectDatabaseSize(ctx context.Context, db *gorm.DB, serverID uint32) ([]DatabaseSizeEntry, error) {
	var dbs []model.WebDatabase
	if err := db.WithContext(ctx).
		Where("server_id = ?", serverID).
		Order("database_name").Find(&dbs).Error; err != nil {
		return nil, err
	}
	if len(dbs) == 0 {
		return []DatabaseSizeEntry{}, nil
	}

	var sizes []struct {
		Name string
		Size int64
	}
	if err := db.WithContext(ctx).Raw(
		"SELECT table_schema AS name, COALESCE(SUM(data_length+index_length),0) AS size "+
			"FROM information_schema.tables GROUP BY table_schema").Scan(&sizes).Error; err != nil {
		return nil, err
	}
	bySchema := make(map[string]int64, len(sizes))
	for _, s := range sizes {
		bySchema[s.Name] = s.Size
	}

	out := make([]DatabaseSizeEntry, 0, len(dbs))
	for _, d := range dbs {
		var quota int64
		if d.DatabaseQuota != nil && *d.DatabaseQuota > 0 {
			quota = int64(*d.DatabaseQuota) * 1024 * 1024 // stored as MB
		}
		out = append(out, DatabaseSizeEntry{
			DatabaseName: d.DatabaseName,
			Size:         bySchema[d.DatabaseName],
			Quota:        quota,
		})
	}
	return out, nil
}

// CollectHarddiskQuota reports per-website disk usage. It prefers repquota
// (filesystem quotas, cheap) and falls back to du on each document_root.
func CollectHarddiskQuota(ctx context.Context, db *gorm.DB, serverID uint32) ([]HDQuotaEntry, error) {
	var sites []model.WebDomain
	if err := db.WithContext(ctx).
		Where("server_id = ? AND type IN ('vhost','vhostsubdomain','vhostalias')", serverID).
		Order("domain").Find(&sites).Error; err != nil {
		return nil, err
	}
	if len(sites) == 0 {
		return []HDQuotaEntry{}, nil
	}

	byUser := repquotaUsers(ctx)
	out := make([]HDQuotaEntry, 0, len(sites))
	for _, s := range sites {
		e := HDQuotaEntry{Domain: s.Domain, User: s.SystemUser}
		if s.HdQuota > 0 {
			e.Soft = s.HdQuota * 1024 // hd_quota is MB
			e.Hard = e.Soft
		}
		if q, ok := byUser[s.SystemUser]; ok {
			e.Used = q.Used
			if q.Soft > 0 {
				e.Soft, e.Hard = q.Soft, q.Hard
			}
		} else if s.DocumentRoot != "" {
			e.Used = duKB(ctx, s.DocumentRoot)
		}
		out = append(out, e)
	}
	return out, nil
}

// repquotaUsers parses `repquota -au` block limits (KB) keyed by user.
// An empty map means quotas are unavailable on this host.
func repquotaUsers(ctx context.Context) map[string]HDQuotaEntry {
	out := map[string]HDQuotaEntry{}
	if _, err := exec.LookPath("repquota"); err != nil {
		return out
	}
	cctx, cancel := context.WithTimeout(ctx, quotaCmdTimeout)
	defer cancel()
	// -au only: the group section would collide with same-named users in
	// the flat map, and websites are tracked per system_user.
	raw, err := exec.CommandContext(cctx, "repquota", "-au").Output()
	if err != nil {
		return out
	}
	return parseRepquota(string(raw))
}

// parseRepquota reads repquota's block-limit columns (KB) keyed by user.
func parseRepquota(raw string) map[string]HDQuotaEntry {
	out := map[string]HDQuotaEntry{}
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		// user  --  used  soft  hard  grace  ...
		if len(f) < 5 || len(f[1]) != 2 || strings.Trim(f[1], "-+") != "" {
			continue
		}
		out[f[0]] = HDQuotaEntry{
			User: f[0],
			Used: atoi64(f[2]), Soft: atoi64(f[3]), Hard: atoi64(f[4]),
		}
	}
	return out
}

// duKB returns the apparent size of dir in KB, 0 when du fails.
// ponytail: full walk per site; only runs when repquota is absent.
func duKB(ctx context.Context, dir string) int64 {
	// document_root comes from the DB, so treat it as untrusted: only ever
	// walk below the web root.
	dir = filepath.Clean(dir)
	if !strings.HasPrefix(dir, "/var/www/") {
		slog.Warn("skipping du for document_root outside /var/www", "dir", dir)
		return 0
	}
	cctx, cancel := context.WithTimeout(ctx, quotaCmdTimeout)
	defer cancel()
	raw, err := exec.CommandContext(cctx, "du", "-sk", dir).Output()
	if err != nil {
		return 0
	}
	f := strings.Fields(string(raw))
	if len(f) == 0 {
		return 0
	}
	return atoi64(f[0])
}

// CollectEmailQuota reports per-mailbox usage from `doveadm quota get -A`,
// falling back to zero usage (quota limits still come from mail_user).
func CollectEmailQuota(ctx context.Context, db *gorm.DB, serverID uint32) ([]MailQuotaEntry, error) {
	var users []model.MailUser
	if err := db.WithContext(ctx).
		Where("server_id = ?", serverID).
		Order("email").Find(&users).Error; err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return []MailQuotaEntry{}, nil
	}

	used := doveadmStorage(ctx)
	out := make([]MailQuotaEntry, 0, len(users))
	for _, u := range users {
		out = append(out, MailQuotaEntry{
			Email: u.Email,
			Used:  used[u.Email],
			Quota: u.Quota, // bytes, 0 = unlimited
		})
	}
	return out, nil
}

// doveadmStorage parses STORAGE rows of `doveadm quota get -A` into bytes.
func doveadmStorage(ctx context.Context) map[string]int64 {
	out := map[string]int64{}
	if _, err := exec.LookPath("doveadm"); err != nil {
		return out
	}
	cctx, cancel := context.WithTimeout(ctx, quotaCmdTimeout)
	defer cancel()
	raw, err := exec.CommandContext(cctx, "doveadm", "quota", "get", "-A").Output()
	if err != nil {
		return out
	}
	return parseDoveadm(string(raw))
}

// parseDoveadm reads STORAGE rows of `doveadm quota get -A` into bytes.
func parseDoveadm(raw string) map[string]int64 {
	out := map[string]int64{}
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		// Username  Type  Value  Limit  %
		f := strings.Fields(sc.Text())
		if len(f) < 3 || f[1] != "STORAGE" {
			continue
		}
		out[f[0]] = atoi64(f[2]) * 1024 // doveadm reports KB
	}
	return out
}

func atoi64(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSuffix(s, "*"), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// RunQuotaCollectors stores all three quota types, keeping the newest sample
// per type. A failing collector does not stop the others.
func RunQuotaCollectors(ctx context.Context, db *gorm.DB, serverID uint32) error {
	var firstErr error
	keep := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if rows, err := CollectDatabaseSize(ctx, db, serverID); err != nil {
		keep(err)
	} else {
		keep(UpsertType(ctx, db, serverID, "database_size", rows, "ok"))
	}
	if rows, err := CollectHarddiskQuota(ctx, db, serverID); err != nil {
		keep(err)
	} else {
		keep(UpsertType(ctx, db, serverID, "harddisk_quota", rows, "ok"))
	}
	if rows, err := CollectEmailQuota(ctx, db, serverID); err != nil {
		keep(err)
	} else {
		keep(UpsertType(ctx, db, serverID, "email_quota", rows, "ok"))
	}
	return firstErr
}
