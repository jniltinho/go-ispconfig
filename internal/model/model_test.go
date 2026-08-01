package model

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

// ddlColumn is one column definition parsed from the embedded DDL.
type ddlColumn struct {
	baseType string // lowercased type keyword, e.g. "int", "varchar", "date"
	nullable bool
	unsigned bool
}

// ddlColumns extracts the column definitions of each CREATE TABLE block from
// the embedded original ispconfig3.sql, keyed by table then column name.
func ddlColumns(t *testing.T) map[string]map[string]ddlColumn {
	t.Helper()
	raw, err := os.ReadFile("../database/ispconfig3.sql")
	require.NoError(t, err)

	tables := map[string]map[string]ddlColumn{}
	reCreate := regexp.MustCompile("^CREATE TABLE (?:IF NOT EXISTS )?`(\\w+)`")
	reCol := regexp.MustCompile("^\\s*`(\\w+)`\\s+(\\w+)(.*)$")
	var current string
	for _, line := range strings.Split(string(raw), "\n") {
		if m := reCreate.FindStringSubmatch(line); m != nil {
			current = m[1]
			tables[current] = map[string]ddlColumn{}
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
			rest := strings.ToUpper(m[3])
			tables[current][m[1]] = ddlColumn{
				baseType: strings.ToLower(m[2]),
				nullable: !strings.Contains(rest, "NOT NULL"),
				unsigned: strings.Contains(rest, "UNSIGNED"),
			}
		}
	}
	return tables
}

var allModels = []any{
	SysUser{}, SysGroup{}, SysDatalog{}, SysRemoteAction{}, SysConfig{},
	SysIni{}, SysLog{}, SysSession{}, Server{}, ServerIP{}, ServerPHP{},
	Client{}, WebDomain{}, WebFolder{}, WebFolderUser{},
	DNSSoa{}, DNSRr{}, DNSSlave{}, DNSTemplate{},
}

// TestModelsMatchDDL asserts that every GORM model maps exactly the columns
// of its table in the original ISPConfig3 DDL — same names, none missing,
// none invented.
func TestModelsMatchDDL(t *testing.T) {
	ddl := ddlColumns(t)

	cache := &sync.Map{}
	for _, m := range allModels {
		s, err := schema.Parse(m, cache, schema.NamingStrategy{})
		require.NoError(t, err)

		require.NotEmptyf(t, ddl[s.Table], "table %s not found in ispconfig3.sql", s.Table)
		var want []string
		for col := range ddl[s.Table] {
			want = append(want, col)
		}

		var got []string
		for _, f := range s.Fields {
			got = append(got, f.DBName)
		}
		sort.Strings(want)
		sort.Strings(got)
		assert.Equalf(t, want, got, "column mismatch for table %s", s.Table)
	}
}

// ddlTypeClass buckets a DDL base type for comparison with a Go field type.
func ddlTypeClass(baseType string) string {
	switch baseType {
	case "tinyint", "smallint", "mediumint", "int", "bigint":
		return "int"
	case "date", "datetime", "timestamp":
		return "time"
	case "decimal", "float", "double":
		return "float"
	default:
		return "text"
	}
}

// goTypeClass buckets a Go field type (pointers dereferenced).
func goTypeClass(typ reflect.Type) string {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeOf(time.Time{}) {
		return "time"
	}
	switch typ.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "float"
	default:
		return "text"
	}
}

// TestModelTypesMatchDDL goes beyond column names for the tables the daemon
// writes through: every field's Go type class must match the DDL type, and
// nullable numeric/time columns must be pointers so a database NULL survives
// a scan/insert round trip (e.g. dns_rr.serial, client.created_at).
func TestModelTypesMatchDDL(t *testing.T) {
	ddl := ddlColumns(t)
	critical := map[string]any{
		"sys_datalog": SysDatalog{},
		"server":      Server{},
		"web_domain":  WebDomain{},
		"dns_soa":     DNSSoa{},
		"dns_rr":      DNSRr{},
		"client":      Client{},
	}

	cache := &sync.Map{}
	for table, m := range critical {
		s, err := schema.Parse(m, cache, schema.NamingStrategy{})
		require.NoError(t, err)

		for _, f := range s.Fields {
			col, ok := ddl[table][f.DBName]
			if !ok {
				continue // name coverage is TestModelsMatchDDL's job
			}
			want := ddlTypeClass(col.baseType)
			assert.Equalf(t, want, goTypeClass(f.FieldType),
				"%s.%s: DDL type %s does not match Go type %s",
				table, f.DBName, col.baseType, f.FieldType)

			if col.nullable && want != "text" {
				assert.Equalf(t, reflect.Pointer, f.FieldType.Kind(),
					"%s.%s is nullable %s in the DDL and must be a pointer, got %s",
					table, f.DBName, col.baseType, f.FieldType)
			}
		}
	}
}
