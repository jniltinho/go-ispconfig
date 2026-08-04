package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

func TestCPUserModulesCSV(t *testing.T) {
	tests := []struct {
		name  string
		in    any
		want  string
		valid bool
	}{
		{"csv string", "sites,mail", "sites,mail", true},
		{"json array", []any{"sites", "dns"}, "sites,dns", true},
		{"drops unknown ids", "sites,vm,admin,mail", "sites,mail", true},
		{"dedupes", "sites,sites,mail", "sites,mail", true},
		{"trims and skips blanks", " sites , , mail ", "sites,mail", true},
		{"empty", "", "", true},
		{"non-string array element", []any{1}, "", false},
		{"wrong type", 42, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := cpUserModulesCSV(tc.in)
			assert.Equal(t, tc.valid, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestCPUserTypTransitions covers the rules ported from
// users_edit.php::onBeforeUpdate. They run before the hook touches the
// database, so a nil transaction is enough to exercise them.
func TestCPUserTypTransitions(t *testing.T) {
	tests := []struct {
		name   string
		stored model.SysUser
		body   map[string]any
		errKey string
	}{
		{
			name:   "client login cannot become admin",
			stored: model.SysUser{UserID: 5, Typ: "user", ClientID: 3},
			body:   map[string]any{"typ": "admin"},
			errKey: "cpuser_error_client_not_admin",
		},
		{
			name:   "standalone admin cannot become a plain user",
			stored: model.SysUser{UserID: 6, Typ: "admin"},
			body:   map[string]any{"typ": "user"},
			errKey: "cpuser_error_no_user_insert",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := cpUserBeforeUpdate(context.Background(), nil, nil, tc.body, &tc.stored, &model.SysUser{})
			var ve *ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Equal(t, []string{tc.errKey}, ve.Fields["typ"])
		})
	}

	t.Run("unchanged typ on a standalone admin is a no-op", func(t *testing.T) {
		stored := model.SysUser{UserID: 6, Typ: "admin"}
		err := cpUserBeforeUpdate(context.Background(), nil, nil, map[string]any{"typ": "admin"},
			&stored, &model.SysUser{UserID: 6, Typ: "admin", Username: stored.Username})
		assert.NoError(t, err)
	})
}

// TestRedactCPUserSecrets is the guard that no hash or key material can leak
// through a list or get response.
func TestRedactCPUserSecrets(t *testing.T) {
	items := []map[string]any{{
		"userid": 1, "username": "admin",
		"passwort": "$2a$10$hash", "id_rsa": "priv", "ssh_rsa": "pub",
		"otp_data": "seed", "otp_recovery": "codes", "lost_password_hash": "h",
	}}
	require.NoError(t, redactCPUserSecrets(context.Background(), nil, items))
	for _, secret := range []string{"passwort", "id_rsa", "ssh_rsa", "otp_data", "otp_recovery", "lost_password_hash"} {
		assert.NotContains(t, items[0], secret)
	}
	assert.Equal(t, "admin", items[0]["username"])
}

// TestCPUserEntityRendersOnlyConsumedFields mirrors the Server Config policy:
// a field the panel never reads would be a knob that silently does nothing.
func TestCPUserEntityRendersOnlyConsumedFields(t *testing.T) {
	ent := cpUserEntity()
	require.True(t, ent.AdminOnly, "CP Users is admin only")
	require.Len(t, ent.Tabs, 1)

	got := map[string]*Field{}
	for i := range ent.Tabs[0].Fields {
		f := &ent.Tabs[0].Fields[i]
		got[f.Name] = f
	}
	for _, name := range []string{"username", "passwort", "typ", "active", "modules", "language"} {
		assert.Contains(t, got, name)
	}
	for _, name := range []string{"startmodule", "app_theme", "otp_type", "lost_password_function"} {
		assert.NotContains(t, got, name, "%s has no consumer in this panel", name)
	}

	// Every module option must be a real panel module id, or the checkbox
	// would write a value AppShell can never match.
	known := map[string]bool{}
	for _, m := range cpUserPanelModules {
		known[m] = true
	}
	require.NotEmpty(t, got["modules"].Options)
	for _, o := range got["modules"].Options {
		assert.True(t, known[o.Value], "unknown module option %q", o.Value)
	}
}
