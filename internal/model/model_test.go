package model

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

// ddlColumns extracts the column names of each CREATE TABLE block from the
// embedded original ispconfig3.sql, keyed by table name.
func ddlColumns(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile("../database/ispconfig3.sql")
	require.NoError(t, err)

	tables := map[string][]string{}
	reCreate := regexp.MustCompile("^CREATE TABLE (?:IF NOT EXISTS )?`(\\w+)`")
	reCol := regexp.MustCompile("^\\s*`(\\w+)`")
	var current string
	for _, line := range strings.Split(string(raw), "\n") {
		if m := reCreate.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if current == "" {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ")") {
			current = ""
			continue
		}
		if strings.HasPrefix(trimmed, "PRIMARY") || strings.HasPrefix(trimmed, "UNIQUE") ||
			strings.HasPrefix(trimmed, "KEY") || strings.HasPrefix(trimmed, "FULLTEXT") {
			continue
		}
		if m := reCol.FindStringSubmatch(line); m != nil {
			tables[current] = append(tables[current], m[1])
		}
	}
	return tables
}

// TestModelsMatchDDL asserts that every GORM model maps exactly the columns
// of its table in the original ISPConfig3 DDL — same names, none missing,
// none invented.
func TestModelsMatchDDL(t *testing.T) {
	ddl := ddlColumns(t)

	models := []any{
		SysUser{}, SysGroup{}, SysDatalog{}, SysRemoteAction{}, SysConfig{},
		SysIni{}, SysLog{}, SysSession{}, Server{}, ServerIP{}, ServerPHP{},
		Client{}, WebDomain{}, WebFolder{}, WebFolderUser{},
		DNSSoa{}, DNSRr{}, DNSSlave{}, DNSTemplate{},
	}

	cache := &sync.Map{}
	for _, m := range models {
		s, err := schema.Parse(m, cache, schema.NamingStrategy{})
		require.NoError(t, err)

		want := ddl[s.Table]
		require.NotEmptyf(t, want, "table %s not found in ispconfig3.sql", s.Table)

		var got []string
		for _, f := range s.Fields {
			got = append(got, f.DBName)
		}
		sort.Strings(want)
		sort.Strings(got)
		assert.Equalf(t, want, got, "column mismatch for table %s", s.Table)
	}
}
