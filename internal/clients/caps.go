package clients

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gorm.io/gorm/schema"

	"go-ispconfig/internal/model"
)

// CapToParent clamps a child client's limits so it is never more
// permissive than its parent reseller (port of the tform valuelimit
// inheritance): numeric limits cap at the parent value when the parent
// is not unlimited (child -1 counts as exceeding), y/n feature flags are
// forced to n when the parent has n (force_suexec inversely forced to y
// when the parent enforces it), cron frequency cannot be more frequent
// than the parent's, cron type cannot be a less restrictive tform index,
// and server/option lists intersect with the parent's set. The child is
// mutated in place; the clamped column names are returned (sorted) for
// warnings. Default servers and text fields are not capped.
func CapToParent(child, parent *model.Client) ([]string, error) {
	if parent == nil {
		return nil, nil
	}
	s, err := schema.Parse(&model.Client{}, mergeSchemaCache, schema.NamingStrategy{})
	if err != nil {
		return nil, fmt.Errorf("clients: parsing client schema: %w", err)
	}
	ctx := context.Background()
	childV := reflect.ValueOf(child).Elem()
	parentV := reflect.ValueOf(parent)

	var clamped []string
	for _, f := range s.Fields {
		kind, ok := mergeKinds[f.DBName]
		if !ok {
			continue
		}
		pv, _ := f.ValueOf(ctx, parentV)
		cv, _ := f.ValueOf(ctx, reflect.ValueOf(child))

		switch kind {
		case kindNumeric:
			p, c := asInt(pv), asInt(cv)
			if p >= 0 && (c == -1 || c > p) {
				if err := f.Set(ctx, childV, p); err != nil {
					return nil, err
				}
				clamped = append(clamped, f.DBName)
			}
		case kindCronFrequency:
			// Lower minutes = more frequent = more permissive.
			p, c := asInt(pv), asInt(cv)
			if c < p {
				if err := f.Set(ctx, childV, p); err != nil {
					return nil, err
				}
				clamped = append(clamped, f.DBName)
			}
		case kindFlag:
			if asString(pv) == "n" && asString(cv) == "y" {
				if err := f.Set(ctx, childV, "n"); err != nil {
					return nil, err
				}
				clamped = append(clamped, f.DBName)
			}
		case kindFlagSuexec:
			if asString(pv) == "y" && asString(cv) == "n" {
				if err := f.Set(ctx, childV, "y"); err != nil {
					return nil, err
				}
				clamped = append(clamped, f.DBName)
			}
		case kindUnion:
			inter := intersectCSV(asString(cv), asString(pv))
			if inter != asString(cv) {
				if err := f.Set(ctx, childV, inter); err != nil {
					return nil, err
				}
				clamped = append(clamped, f.DBName)
			}
		case kindSelectCron:
			if cronIndex(asString(cv)) < cronIndex(asString(pv)) {
				if err := f.Set(ctx, childV, asString(pv)); err != nil {
					return nil, err
				}
				clamped = append(clamped, f.DBName)
			}
		case kindDefaultServer, kindMasterOnly:
			// not capped
		}
	}
	sort.Strings(clamped)
	return clamped, nil
}

// intersectCSV keeps only child entries also present in the parent set,
// preserving child order.
func intersectCSV(child, parent string) string {
	allowed := map[string]bool{}
	for _, p := range splitCSVList(parent) {
		allowed[p] = true
	}
	var out []string
	for _, c := range splitCSVList(child) {
		if allowed[c] {
			out = append(out, c)
		}
	}
	return strings.Join(out, ",")
}

// cronIndex returns the tform SELECT index of a cron type (unknown =
// most restrictive).
func cronIndex(v string) int {
	for i, o := range cronTypeOrder {
		if o == v {
			return i
		}
	}
	return len(cronTypeOrder) - 1
}
