package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
	"gorm.io/gorm/utils/tests"

	"go-ispconfig/internal/model"
	"go-ispconfig/internal/repository"
)

// dryDomainHandlers builds web-domain entity handlers on a connection-less
// DryRun GORM handle (UNIQUE and CRUD paths run in the integration suite).
func dryDomainHandlers(t *testing.T) *entityHandlers[model.WebDomain] {
	t.Helper()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	s, err := schema.Parse(&model.WebDomain{}, entitySchemaCache, db.NamingStrategy)
	require.NoError(t, err)
	ent := webDomainEntity()
	ent.Table = s.Table
	ent.PK = s.PrioritizedPrimaryField.DBName
	// RegisterEntity normally propagates tab-level AdminOnly to the fields.
	for ti := range ent.Tabs {
		if ent.Tabs[ti].AdminOnly {
			for fi := range ent.Tabs[ti].Fields {
				ent.Tabs[ti].Fields[fi].AdminOnly = true
			}
		}
	}
	// Strip the UNIQUE rule (needs a DB) so validate runs offline.
	for f := range ent.fields {
		kept := f.Validators[:0]
		for _, r := range f.Validators {
			if r.Type != "UNIQUE" {
				kept = append(kept, r)
			}
		}
		f.Validators = kept
	}
	return &entityHandlers[model.WebDomain]{deps: &Deps{DB: db}, ent: ent, schema: s}
}

// validDomain returns a record passing every web-domain validator.
func validDomain() *model.WebDomain {
	return &model.WebDomain{
		ServerID: 1, Domain: "example.com", Type: "vhost",
		HdQuota: -1, TrafficQuota: -1, AllowOverride: "All",
		PMMaxChildren: 10, PMStartServers: 2, PMMinSpareServers: 1,
		PMMaxSpareServers: 5, PMProcessIdleTimeout: 10,
		HTTPPort: 80, HTTPSPort: 443, LogRetention: 30,
	}
}

func TestWebDomainEntityDeclaresKnownColumnsOnly(t *testing.T) {
	// The dry handler construction re-runs the schema lookup RegisterEntity
	// performs; a typoed column would panic schema.Parse or fail here.
	h := dryDomainHandlers(t)
	for f := range h.ent.fields {
		require.NotNil(t, h.schema.LookUpField(f.Name), "unknown column %s", f.Name)
	}
}

func TestWebDomainDefaults(t *testing.T) {
	h := dryDomainHandlers(t)
	ctx := context.Background()
	rec := &model.WebDomain{}
	body := map[string]any{"server_id": float64(1), "domain": "example.com"}
	require.NoError(t, h.applyDefaults(ctx, rec, body))
	require.NoError(t, h.applyBody(ctx, rec, body, &repository.Identity{Typ: "admin"}))

	require.Equal(t, "vhost", rec.Type)
	require.Equal(t, "www", rec.Subdomain)
	require.Equal(t, "php-fpm", rec.PHP)
	require.Equal(t, "y", rec.Active)
	require.Equal(t, "n", rec.SSL)
	require.EqualValues(t, -1, rec.HdQuota)
	require.EqualValues(t, -1, rec.TrafficQuota)
	require.Equal(t, "ondemand", rec.PM)
	require.EqualValues(t, 80, rec.HTTPPort)
	require.EqualValues(t, 443, rec.HTTPSPort)
	require.EqualValues(t, 30, rec.LogRetention)
}

func TestWebDomainAdminOnlyFieldsIgnoredForClients(t *testing.T) {
	h := dryDomainHandlers(t)
	ctx := context.Background()
	client := &repository.Identity{Typ: "user", UserID: 2}

	rec := &model.WebDomain{}
	body := map[string]any{
		"domain":        "example.com",
		"document_root": "/etc/hacked",
		"system_user":   "root",
	}
	require.NoError(t, h.applyBody(ctx, rec, body, client))
	require.Equal(t, "example.com", rec.Domain)
	require.Empty(t, rec.DocumentRoot, "admin-only field must be ignored for clients")
	require.Empty(t, rec.SystemUser, "admin-only field must be ignored for clients")

	admin := &repository.Identity{Typ: "admin"}
	require.NoError(t, h.applyBody(ctx, rec, body, admin))
	require.Equal(t, "/etc/hacked", rec.DocumentRoot, "admins may set Options-tab fields")
}

func TestWebDomainValidate(t *testing.T) {
	h := dryDomainHandlers(t)
	ctx := context.Background()

	t.Run("valid record passes", func(t *testing.T) {
		require.NoError(t, h.validate(ctx, validDomain(), nil))
	})

	t.Run("empty domain fails NOTEMPTY", func(t *testing.T) {
		rec := validDomain()
		rec.Domain = ""
		err := h.validate(ctx, rec, nil)
		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		require.Contains(t, valErr.Fields["domain"], "domain_error_empty")
	})

	t.Run("invalid domain fails REGEX", func(t *testing.T) {
		for _, bad := range []string{"not a domain", "-bad.com", "single", "exa_mple.com"} {
			rec := validDomain()
			rec.Domain = bad
			err := h.validate(ctx, rec, nil)
			var valErr *ValidationError
			require.ErrorAs(t, err, &valErr, "domain %q must fail", bad)
			require.Contains(t, valErr.Fields["domain"], "domain_error_regex")
		}
	})

	t.Run("zero server_id fails", func(t *testing.T) {
		rec := validDomain()
		rec.ServerID = 0
		err := h.validate(ctx, rec, nil)
		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		require.Contains(t, valErr.Fields["server_id"], "no_server_error")
	})

	t.Run("blacklisted nginx directive fails", func(t *testing.T) {
		rec := validDomain()
		rec.NginxDirectives = "gzip on;\nload_module /tmp/evil.so;"
		err := h.validate(ctx, rec, nil)
		var valErr *ValidationError
		require.ErrorAs(t, err, &valErr)
		require.Len(t, valErr.Fields["nginx_directives"], 1)
		require.Contains(t, valErr.Fields["nginx_directives"][0], "nginx_directive_blocked_error")
		require.Contains(t, valErr.Fields["nginx_directives"][0], "load_module",
			"error must name the offending directive")
	})

	t.Run("harmless nginx directives pass", func(t *testing.T) {
		rec := validDomain()
		rec.NginxDirectives = "gzip on;\nlocation /foo { return 404; }"
		require.NoError(t, h.validate(ctx, rec, nil))
	})
}

func TestSitesCustomValidators(t *testing.T) {
	t.Run("web_folder", func(t *testing.T) {
		require.Empty(t, checkWebFolder(nil, ""))
		require.Empty(t, checkWebFolder(nil, "sub/folder"))
		for _, bad := range []string{"/abs", "a/../b", "./x", "a//b", "with space"} {
			require.Equal(t, "web_folder_error_regex", checkWebFolder(nil, bad), "value %q", bad)
		}
	})

	t.Run("redirect_path", func(t *testing.T) {
		require.Empty(t, checkRedirectPath(nil, ""))
		require.Empty(t, checkRedirectPath(nil, "https://example.com/target"))
		require.Empty(t, checkRedirectPath(nil, "/sub/dir/"))
		for _, bad := range []string{"/no-trailing-slash", "relative/", "/a/../b/", "javascript:alert(1)"} {
			require.Equal(t, "redirect_error_regex", checkRedirectPath(nil, bad), "value %q", bad)
		}
	})

	t.Run("backup_excludes", func(t *testing.T) {
		require.Empty(t, checkBackupExcludes(nil, ""))
		require.Empty(t, checkBackupExcludes(nil, "tmp/*, cache/"))
		require.Equal(t, "backup_excludes_error_regex", checkBackupExcludes(nil, "../../etc"))
	})
}

func TestWebFolderUserEntityValidators(t *testing.T) {
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	s, err := schema.Parse(&model.WebFolderUser{}, entitySchemaCache, db.NamingStrategy)
	require.NoError(t, err)
	ent := webFolderUserEntity()
	ent.Table = s.Table
	ent.PK = s.PrioritizedPrimaryField.DBName
	h := &entityHandlers[model.WebFolderUser]{deps: &Deps{DB: db}, ent: ent, schema: s}
	ctx := context.Background()

	rec := &model.WebFolderUser{WebFolderID: 1, Username: "user1", Password: "$6$abc", Active: "y"}
	require.NoError(t, h.validate(ctx, rec, nil))

	rec = &model.WebFolderUser{WebFolderID: 0, Username: "bad user!", Password: ""}
	errv := h.validate(ctx, rec, nil)
	var valErr *ValidationError
	require.ErrorAs(t, errv, &valErr)
	require.Contains(t, valErr.Fields["web_folder_id"], "folder_error_empty")
	require.Contains(t, valErr.Fields["username"], "username_error_regex")
	require.Contains(t, valErr.Fields["password"], "password_error_empty")
}

func TestCryptBodyPassword(t *testing.T) {
	body := map[string]any{"password": "secret"}
	require.NoError(t, cryptBodyPassword(body, "password"))
	hashed, _ := body["password"].(string)
	require.True(t, len(hashed) > 20 && hashed[:3] == "$6$", "expected SHA-512 crypt hash, got %q", hashed)

	// Already-crypted values are kept verbatim (GET → PUT round trip).
	body = map[string]any{"password": hashed}
	require.NoError(t, cryptBodyPassword(body, "password"))
	require.Equal(t, hashed, body["password"])

	// Absent or empty values are untouched.
	body = map[string]any{}
	require.NoError(t, cryptBodyPassword(body, "password"))
	require.NotContains(t, body, "password")
}
