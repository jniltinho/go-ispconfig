package powerdns

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

// TestModelsMatchPowerDNSSQL asserts GORM models map every column of the
// embedded powerdns.sql CREATE TABLE blocks (names only; no AutoMigrate).
func TestModelsMatchPowerDNSSQL(t *testing.T) {
	ddlCols := powerdnsDDLColumns(t)

	cases := []struct {
		table string
		model any
	}{
		{"domains", Domain{}},
		{"records", Record{}},
		{"domainmetadata", DomainMetadata{}},
		{"supermasters", SuperMaster{}},
	}

	cache := &sync.Map{}
	for _, tc := range cases {
		t.Run(tc.table, func(t *testing.T) {
			wantMap, ok := ddlCols[tc.table]
			require.Truef(t, ok, "DDL missing table %s", tc.table)

			s, err := schema.Parse(tc.model, cache, schema.NamingStrategy{})
			require.NoError(t, err)
			require.Equal(t, tc.table, s.Table)

			var want, got []string
			for col := range wantMap {
				want = append(want, col)
			}
			for _, f := range s.Fields {
				if f.DBName != "" {
					got = append(got, f.DBName)
				}
			}
			sort.Strings(want)
			sort.Strings(got)
			assert.Equalf(t, want, got, "column mismatch for table %s", tc.table)
		})
	}
}

func powerdnsDDLColumns(t *testing.T) map[string]map[string]struct{} {
	t.Helper()
	tables := map[string]map[string]struct{}{}
	reCreate := regexp.MustCompile("(?i)^CREATE TABLE (?:IF NOT EXISTS )?`(\\w+)`")
	reCol := regexp.MustCompile("^\\s*`([\\w-]+)`\\s+\\w+")
	var current string
	for _, line := range strings.Split(SchemaSQL, "\n") {
		if m := reCreate.FindStringSubmatch(line); m != nil {
			current = strings.ToLower(m[1])
			tables[current] = map[string]struct{}{}
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
			tables[current][m[1]] = struct{}{}
		}
	}
	return tables
}
