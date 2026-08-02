// Package importer is the legacy ISPConfig3 import engine: it maps
// entities fetched through the legacy remote-API client into the local
// go-ispconfig database — clients (with recreated sys_user/sys_group),
// web domains/folders/folder users and DNS zones/records/slaves/templates
// — with plan/apply two-phase runs, natural-key idempotency, ID remapping
// and datalog emission (design doc: openspec add-legacy-migration).
package importer

import (
	"context"
	"reflect"
	"sync"

	"gorm.io/gorm/schema"

	"go-ispconfig/internal/legacy/client"
)

// schemaCache caches gorm schema parses across mapper calls.
var schemaCache = &sync.Map{}

// namer is the naming strategy for schema parsing; every model carries
// explicit column tags and a TableName method, so the default is exact.
var namer = schema.NamingStrategy{IdentifierMaxLength: 64}

// parseSchema resolves the gorm schema of a model.
func parseSchema(rec any) (*schema.Schema, error) {
	return schema.Parse(rec, schemaCache, namer)
}

// shouldMap reports whether a legacy record column feeds a model field:
// the column must exist in the record, must not be the primary key (legacy
// ids are never preserved), and an empty legacy value only maps onto
// string fields (for numeric/time fields it keeps the zero value or DB
// default, mirroring how the legacy DB serializes NULL as "").
func shouldMap(rec client.Record, f *schema.Field) (string, bool) {
	if f.DBName == "" || f.PrimaryKey {
		return "", false
	}
	val, ok := rec[f.DBName]
	if !ok {
		return "", false
	}
	if val == "" && f.FieldType.Kind() != reflect.String {
		return "", false
	}
	return val, true
}

// mapRecord fills out (a model struct pointer) from a legacy record by
// column name. Unknown legacy columns are ignored, as are values that do
// not parse into the field type (minor-version drift tolerance): the
// engine asserts only the fields it maps.
func mapRecord(rec client.Record, out any) error {
	s, err := parseSchema(out)
	if err != nil {
		return err
	}
	ctx := context.Background()
	rv := reflect.ValueOf(out).Elem()
	for _, f := range s.Fields {
		val, ok := shouldMap(rec, f)
		if !ok {
			continue
		}
		// Tolerant: unparseable legacy values keep the zero value.
		_ = f.Set(ctx, rv, val)
	}
	return nil
}
