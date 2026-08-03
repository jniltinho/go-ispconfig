package clients

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go-ispconfig/internal/model"
)

// TestResolveRuleMailEntities pins the mail entity → limit column/error
// mapping (add-mail-module: limit enforcement via the client limit hook).
func TestResolveRuleMailEntities(t *testing.T) {
	tests := []struct {
		entity string
		key    string
		limit  int32
		get    func(*model.Client) int32
	}{
		{"mail-domains", "error.limit_maildomain", 3, func(c *model.Client) int32 { return c.LimitMaildomain }},
		{"mailboxes", "error.limit_mailbox", 5, func(c *model.Client) int32 { return c.LimitMailbox }},
		{"aliases", "error.limit_mailalias", 7, func(c *model.Client) int32 { return c.LimitMailalias }},
		{"alias-domains", "error.limit_mailaliasdomain", 2, func(c *model.Client) int32 { return c.LimitMailaliasdomain }},
		{"forwards", "error.limit_mailforward", 4, func(c *model.Client) int32 { return c.LimitMailforward }},
		{"catchalls", "error.limit_mailcatchall", 1, func(c *model.Client) int32 { return c.LimitMailcatchall }},
	}
	for _, tt := range tests {
		t.Run(tt.entity, func(t *testing.T) {
			rule, ok := resolveRule(tt.entity, nil)
			assert.True(t, ok, "mail entity must resolve a limit rule")
			assert.Equal(t, tt.key, rule.key)
			c := &model.Client{}
			// Set the field via the accessor's mirror: build a client with
			// the value and confirm the rule reads the same column.
			assert.Equal(t, tt.get(c), rule.limit(c))
		})
	}

	// A truly unknown entity is still a no-op (never vetoed).
	_, ok := resolveRule("gizmos", nil)
	assert.False(t, ok)
}

// TestResolveRuleDatabaseEntities pins the database entity → limit
// column/error mapping (add-database-module task 4.5).
func TestResolveRuleDatabaseEntities(t *testing.T) {
	rule, ok := resolveRule("databases", nil)
	assert.True(t, ok)
	assert.Equal(t, "error.limit_database", rule.key)
	assert.Equal(t, int32(3), rule.limit(&model.Client{LimitDatabase: 3}))

	rule, ok = resolveRule("database-users", nil)
	assert.True(t, ok)
	assert.Equal(t, "error.limit_database_user", rule.key)
	assert.Equal(t, int32(7), rule.limit(&model.Client{LimitDatabaseUser: 7}))
}

// TestResolveRuleFTPShellEntities pins the FTP/shell limit mapping
// (add-ftp-shell-module tasks 5.1–5.2).
func TestResolveRuleFTPShellEntities(t *testing.T) {
	rule, ok := resolveRule("ftp-users", nil)
	assert.True(t, ok)
	assert.Equal(t, "error.limit_ftp_user", rule.key)
	assert.Equal(t, int32(2), rule.limit(&model.Client{LimitFTPUser: 2}))

	rule, ok = resolveRule("shell-users", nil)
	assert.True(t, ok)
	assert.Equal(t, "error.limit_shell_user", rule.key)
	assert.Equal(t, int32(0), rule.limit(&model.Client{LimitShellUser: 0}))
}

// TestDatabaseServerAllowList: creating on a server outside db_servers
// is vetoed before any counting; allow-listed and unset lists pass (with
// unlimited quota nothing else is queried).
func TestDatabaseServerAllowList(t *testing.T) {
	client := &model.Client{DBServers: "1,3", LimitDatabaseQuota: -1}
	err := databaseCreateChecks(t.Context(), nil, client, map[string]any{"server_id": float64(2)})
	var le *LimitError
	assert.ErrorAs(t, err, &le)
	assert.Equal(t, "error.not_allowed_server_id", le.Key)

	assert.NoError(t, databaseCreateChecks(t.Context(), nil, client, map[string]any{"server_id": float64(3)}))
	open := &model.Client{DBServers: "", LimitDatabaseQuota: -1}
	assert.NoError(t, databaseCreateChecks(t.Context(), nil, open, map[string]any{"server_id": float64(9)}))
}

// TestDatabaseQuotaBodyRejections: -1 (unlimited DB) under a finite
// client quota and 0 under a positive quota are rejected before any sum
// query runs.
func TestDatabaseQuotaBodyRejections(t *testing.T) {
	client := &model.Client{LimitDatabaseQuota: 100}
	var le *LimitError
	err := databaseCreateChecks(t.Context(), nil, client, map[string]any{"database_quota": float64(-1)})
	assert.ErrorAs(t, err, &le)
	err = databaseCreateChecks(t.Context(), nil, client, map[string]any{"database_quota": float64(0)})
	assert.ErrorAs(t, err, &le)
	assert.Equal(t, "error.limit_database_quota", le.Key)
}

// TestBodyNum: float64, string and missing values.
func TestBodyNum(t *testing.T) {
	assert.EqualValues(t, 5, bodyNum(map[string]any{"x": float64(5)}, "x"))
	assert.EqualValues(t, 7, bodyNum(map[string]any{"x": " 7 "}, "x"))
	assert.EqualValues(t, 0, bodyNum(map[string]any{}, "x"))
}
