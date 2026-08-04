package api

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"gorm.io/gorm/utils/tests"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/shell"
	"go-ispconfig/internal/system"
	"go-ispconfig/internal/validator"
)

// dryFTPHandlers builds FTP-user entity handlers on a connection-less
// DryRun GORM handle (UNIQUE / Prepare paths that need a real DB run in
// the integration suite).
func dryFTPHandlers(t *testing.T) *entityHandlers[model.FTPUser] {
	t.Helper()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	s, err := schema.Parse(&model.FTPUser{}, entitySchemaCache, db.NamingStrategy)
	require.NoError(t, err)
	ent := ftpUserEntity()
	ent.Table = s.Table
	ent.PK = s.PrioritizedPrimaryField.DBName
	for ti := range ent.Tabs {
		if ent.Tabs[ti].AdminOnly {
			for fi := range ent.Tabs[ti].Fields {
				ent.Tabs[ti].Fields[fi].AdminOnly = true
			}
		}
	}
	for f := range ent.fields {
		kept := f.Validators[:0]
		for _, r := range f.Validators {
			if r.Type != "UNIQUE" {
				kept = append(kept, r)
			}
		}
		f.Validators = kept
	}
	return &entityHandlers[model.FTPUser]{deps: &Deps{DB: db}, ent: ent, schema: s}
}

// dryShellHandlers is the shell-user counterpart of dryFTPHandlers.
func dryShellHandlers(t *testing.T) *entityHandlers[model.ShellUser] {
	t.Helper()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	s, err := schema.Parse(&model.ShellUser{}, entitySchemaCache, db.NamingStrategy)
	require.NoError(t, err)
	ent := shellUserEntity()
	ent.Table = s.Table
	ent.PK = s.PrioritizedPrimaryField.DBName
	for ti := range ent.Tabs {
		if ent.Tabs[ti].AdminOnly {
			for fi := range ent.Tabs[ti].Fields {
				ent.Tabs[ti].Fields[fi].AdminOnly = true
			}
		}
	}
	for f := range ent.fields {
		kept := f.Validators[:0]
		for _, r := range f.Validators {
			if r.Type != "UNIQUE" {
				kept = append(kept, r)
			}
		}
		f.Validators = kept
	}
	return &entityHandlers[model.ShellUser]{deps: &Deps{DB: db}, ent: ent, schema: s}
}

func validFTPUser() *model.FTPUser {
	return &model.FTPUser{
		ServerID: 1, ParentDomainID: 1, Username: "c1_alice",
		UsernamePrefix: "c1_", Password: "$6$dummyhash",
		QuotaSize: -1, Active: "y", UID: "web1", GID: "client1",
		Dir: "/var/www/clients/client1/web1", UserType: "user",
	}
}

func validShellUser() *model.ShellUser {
	return &model.ShellUser{
		ServerID: 1, ParentDomainID: 1, Username: "c1_alice",
		UsernamePrefix: "c1_", Password: "$6$dummyhash",
		QuotaSize: -1, Active: "y", PUser: "web1", PGroup: "client1",
		Shell: "/bin/bash", Dir: "/var/www/clients/client1/web1",
	}
}

// --- task 5.1: FTP entity declaration + validators ---

func TestFTPUserEntityDeclaresKnownColumnsOnly(t *testing.T) {
	h := dryFTPHandlers(t)
	for f := range h.ent.fields {
		require.NotNil(t, h.schema.LookUpField(f.Name), "unknown column %s", f.Name)
	}
}

func TestFTPUserAdminOnlyFieldsIgnoredForClients(t *testing.T) {
	h := dryFTPHandlers(t)
	ctx := context.Background()
	client := &repository.Identity{Typ: "user", UserID: 2}

	rec := &model.FTPUser{}
	body := map[string]any{
		"username":    "alice",
		"uid":         "root",
		"gid":         "root",
		"quota_files": float64(99),
		"ul_ratio":    float64(5),
	}
	require.NoError(t, h.applyBody(ctx, rec, body, client))
	require.Equal(t, "alice", rec.Username)
	require.Empty(t, rec.UID, "admin-only uid must be ignored for clients")
	require.Empty(t, rec.GID, "admin-only gid must be ignored for clients")
	require.Zero(t, rec.QuotaFiles)
	require.Zero(t, rec.ULRatio)

	admin := &repository.Identity{Typ: "admin"}
	require.NoError(t, h.applyBody(ctx, rec, body, admin))
	require.Equal(t, "root", rec.UID)
	require.Equal(t, "root", rec.GID)
	require.EqualValues(t, 99, rec.QuotaFiles)
}

func TestFTPUserDefaults(t *testing.T) {
	h := dryFTPHandlers(t)
	ctx := context.Background()
	rec := &model.FTPUser{}
	body := map[string]any{"username": "alice", "parent_domain_id": float64(1)}
	require.NoError(t, h.applyDefaults(ctx, rec, body))
	require.Equal(t, "y", rec.Active)
	require.EqualValues(t, -1, rec.QuotaSize)
	require.Zero(t, rec.QuotaFiles, "PHP advanced default is 0")
}

func TestFTPUserValidate(t *testing.T) {
	h := dryFTPHandlers(t)
	ctx := context.Background()

	t.Run("valid record passes", func(t *testing.T) {
		require.NoError(t, h.validate(ctx, validFTPUser(), nil))
	})

	t.Run("empty username fails", func(t *testing.T) {
		rec := validFTPUser()
		rec.Username = ""
		err := h.validate(ctx, rec, nil)
		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		require.Contains(t, valErr.Fields["username"], "username_error_empty")
	})

	t.Run("bad username regex", func(t *testing.T) {
		for _, bad := range []string{"has space", "bad/name", strings.Repeat("a", 65)} {
			rec := validFTPUser()
			rec.Username = bad
			err := h.validate(ctx, rec, nil)
			var valErr *ValidationError
			require.ErrorAs(t, err, &valErr, "username %q must fail", bad)
			require.Contains(t, valErr.Fields["username"], "username_error_regex")
		}
	})

	t.Run("quota_size regex", func(t *testing.T) {
		rec := validFTPUser()
		rec.QuotaSize = -2
		err := h.validate(ctx, rec, nil)
		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		require.Contains(t, valErr.Fields["quota_size"], "quota_size_error_regex")
	})

	t.Run("empty dir fails", func(t *testing.T) {
		rec := validFTPUser()
		rec.Dir = ""
		err := h.validate(ctx, rec, nil)
		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		require.Contains(t, valErr.Fields["dir"], "directory_error_empty")
	})
}

func TestFTPUserDirUnderDocroot(t *testing.T) {
	// Table-driven guard used by ftpUserPrepare (system.UnderDocroot +
	// ".." rejection) — pure path rules without a DB.
	docroot := "/var/www/clients/client1/web1"
	tests := []struct {
		dir  string
		want bool
	}{
		{docroot, true},
		{docroot + "/uploads", true},
		{docroot + "/../web2", false},
		{"/etc/passwd", false},
		{"/tmp", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, system.UnderDocroot(tt.dir, docroot), tt.dir)
	}
}

// --- task 5.2: Shell entity declaration + validators ---

func TestShellUserEntityDeclaresKnownColumnsOnly(t *testing.T) {
	h := dryShellHandlers(t)
	for f := range h.ent.fields {
		require.NotNil(t, h.schema.LookUpField(f.Name), "unknown column %s", f.Name)
	}
}

func TestShellUserAdminOnlyAdvancedTab(t *testing.T) {
	h := dryShellHandlers(t)
	ctx := context.Background()
	client := &repository.Identity{Typ: "user", UserID: 2}

	rec := &model.ShellUser{}
	body := map[string]any{
		"username": "alice",
		"puser":    "root",
		"pgroup":   "root",
		"shell":    "/bin/sh",
		"dir":      "/etc",
	}
	require.NoError(t, h.applyBody(ctx, rec, body, client))
	require.Equal(t, "alice", rec.Username)
	require.Empty(t, rec.PUser, "admin Options tab ignored for clients")
	require.Empty(t, rec.PGroup)
	require.Empty(t, rec.Shell)
	require.Empty(t, rec.Dir)

	admin := &repository.Identity{Typ: "admin"}
	require.NoError(t, h.applyBody(ctx, rec, body, admin))
	require.Equal(t, "root", rec.PUser)
	require.Equal(t, "/bin/sh", rec.Shell)
	require.Equal(t, "/etc", rec.Dir)
}

func TestShellUserDefaults(t *testing.T) {
	h := dryShellHandlers(t)
	ctx := context.Background()
	rec := &model.ShellUser{}
	body := map[string]any{"username": "alice", "parent_domain_id": float64(1)}
	require.NoError(t, h.applyDefaults(ctx, rec, body))
	require.Equal(t, "y", rec.Active)
	require.EqualValues(t, -1, rec.QuotaSize)
}

func TestShellUserValidate(t *testing.T) {
	h := dryShellHandlers(t)
	ctx := context.Background()

	t.Run("valid record passes", func(t *testing.T) {
		require.NoError(t, h.validate(ctx, validShellUser(), nil))
	})

	t.Run("username regex 32-char max", func(t *testing.T) {
		rec := validShellUser()
		rec.Username = strings.Repeat("a", 33)
		err := h.validate(ctx, rec, nil)
		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		require.Contains(t, valErr.Fields["username"], "username_error_regex")
	})

	t.Run("shell regex", func(t *testing.T) {
		rec := validShellUser()
		rec.Shell = "/usr/bin/evil"
		err := h.validate(ctx, rec, nil)
		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		require.Contains(t, valErr.Fields["shell"], "shell_error_regex")
	})

	t.Run("allowed shells pass", func(t *testing.T) {
		for _, sh := range []string{"/bin/bash", "/bin/sh", "/bin/false", "/usr/sbin/jk_chrootsh", "/bin/rbash"} {
			rec := validShellUser()
			rec.Shell = sh
			require.NoError(t, h.validate(ctx, rec, nil), sh)
		}
	})
}

func TestShellUserBlacklistAndLength(t *testing.T) {
	// Prepare-level rules exercised as pure helpers (blacklist + 32-char
	// cap after prefix) — same logic as shellUserPrepare without a DB.
	tests := []struct {
		name   string
		prefix string
		suffix string
		want   string // error key or ""
	}{
		{"ok", "c1_", "alice", ""},
		{"blacklisted unprefixed", "", "root", "username_error_blacklist"},
		{"blacklisted full", "c1_", "www-data", "username_error_blacklist"},
		// prefix 12 + suffix 21 = 33
		{"too long", "c1234567890_", strings.Repeat("a", 21), "username_error_len"},
		{"exactly 32", "c1_", strings.Repeat("a", 29), ""}, // 3+29=32
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkName := tt.suffix
			full := tt.prefix + checkName
			var key string
			if shell.Blacklisted(checkName) || shell.Blacklisted(full) {
				key = "username_error_blacklist"
			} else if len(full) > 32 {
				key = "username_error_len"
			}
			assert.Equal(t, tt.want, key)
		})
	}
}

func TestSSHAuthModeHelpers(t *testing.T) {
	// Nil DB → empty mode (both password and key allowed).
	assert.Empty(t, sshAuthMode(nil))
}

func TestClientSSHChrootAdminAlwaysAllowed(t *testing.T) {
	ok, err := clientSSHChroot(context.Background(), nil, &repository.Identity{Typ: "admin"})
	require.NoError(t, err)
	assert.True(t, ok)
}

// fieldRules collects validator ErrKeys of a named entity field.
func fieldRules(ent *Entity, name string) []validator.Rule {
	for f := range ent.fields {
		if f.Name == name {
			return f.Validators
		}
	}
	return nil
}

func TestFTPUserUsernameRulesShape(t *testing.T) {
	rules := fieldRules(ftpUserEntity(), "username")
	types := map[string]bool{}
	for _, r := range rules {
		types[r.Type] = true
	}
	assert.True(t, types["NOTEMPTY"])
	assert.True(t, types["UNIQUE"])
	assert.True(t, types["REGEX"])
}

func TestShellUserChrootOptions(t *testing.T) {
	ent := shellUserEntity()
	var chroot *Field
	for f := range ent.fields {
		if f.Name == "chroot" {
			chroot = f
			break
		}
	}
	require.NotNil(t, chroot)
	values := map[string]bool{}
	for _, o := range chroot.Options {
		values[o.Value] = true
	}
	assert.True(t, values[""] || values["no"])
	assert.True(t, values["jailkit"])
}
