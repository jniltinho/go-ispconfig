package powerdns

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/database"
)

func TestEmbeddedSchemaSQLCreatesPowerDNSTables(t *testing.T) {
	require.NotEmpty(t, SchemaSQL, "powerdns.sql must be embedded")

	stmts, err := database.SplitStatements(SchemaSQL)
	require.NoError(t, err)
	require.NotEmpty(t, stmts)

	// Collect CREATE TABLE targets (case-insensitive, strip IF NOT EXISTS + backticks).
	creates := map[string]string{}
	for _, s := range stmts {
		upper := strings.ToUpper(strings.TrimSpace(s))
		if !strings.HasPrefix(upper, "CREATE TABLE") {
			continue
		}
		// CREATE TABLE IF NOT EXISTS `name` (
		rest := strings.TrimSpace(s[len("CREATE TABLE"):])
		rest = strings.TrimPrefix(rest, "IF NOT EXISTS")
		rest = strings.TrimSpace(rest)
		rest = strings.TrimPrefix(rest, "`")
		name, _, ok := strings.Cut(rest, "`")
		if !ok {
			// unquoted name up to space or (
			for i, r := range rest {
				if r == ' ' || r == '(' || r == '\n' || r == '\t' {
					name = rest[:i]
					break
				}
			}
		}
		name = strings.ToLower(strings.TrimSpace(name))
		creates[name] = s
	}

	for _, table := range []string{"domains", "records", "supermasters", "domainmetadata"} {
		body, ok := creates[table]
		require.Truef(t, ok, "embedded SQL must CREATE TABLE %s", table)
		assert.NotEmpty(t, body)
	}

	// Bridge columns used by powerdns_plugin.inc.php to map panel dns_* ids.
	assert.Contains(t, creates["domains"], "`ispconfig_id`")
	assert.Contains(t, creates["records"], "`ispconfig_id`")
	// Core PowerDNS columns the plugin writes.
	assert.Contains(t, creates["domains"], "`name`")
	assert.Contains(t, creates["domains"], "`type`")
	assert.Contains(t, creates["domains"], "`notified_serial`")
	assert.Contains(t, creates["records"], "`domain_id`")
	assert.Contains(t, creates["records"], "`content`")
	assert.Contains(t, creates["records"], "`ttl`")
	assert.Contains(t, creates["records"], "`prio`")
}

func TestEmbeddedLocalMasterTemplate(t *testing.T) {
	require.NotEmpty(t, LocalMasterTemplate)
	assert.Contains(t, LocalMasterTemplate, "launch=gmysql")
	assert.Contains(t, LocalMasterTemplate, "gmysql-host={mysql_server_host}")
	assert.Contains(t, LocalMasterTemplate, "gmysql-user={mysql_server_ispconfig_user}")
	assert.Contains(t, LocalMasterTemplate, "gmysql-password={mysql_server_ispconfig_password}")
	assert.Contains(t, LocalMasterTemplate, "gmysql-dbname={powerdns_database}")
	assert.Contains(t, LocalMasterTemplate, "gmysql-port={mysql_server_port}")
	assert.Equal(t, "powerdns", DatabaseName)
}
