// Package repository provides the permission-checked data access layer:
// a GORM scope porting ISPConfig3's riud record permission model
// (tform_base getAuthSQL/checkPerm, design D4), a generic repository base
// that applies it on every query, and the attempts_login brute-force
// lockout. No handler may query user-data tables outside this package.
package repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"

	"go-ispconfig/internal/datalog"
)

// Permission flags, one per operation (Unix-style riud strings stored in
// sys_perm_user / sys_perm_group / sys_perm_other).
const (
	// PermRead is required to select a record.
	PermRead byte = 'r'
	// PermInsert is required to create a record.
	PermInsert byte = 'i'
	// PermUpdate is required to modify a record.
	PermUpdate byte = 'u'
	// PermDelete is required to remove a record.
	PermDelete byte = 'd'
)

// ErrPermissionDenied is returned when the requesting identity lacks the
// required riud flag on a record — or the record does not exist at all
// (ISPConfig semantics: the permission WHERE clause simply matches nothing,
// so denied and missing are indistinguishable by design).
var ErrPermissionDenied = errors.New("repository: permission denied")

// WithPerm is the GORM scope porting tform_base::getAuthSQL. It restricts a
// query to records the identity may access with the given permission flag:
//
//	(sys_userid = user AND sys_perm_user LIKE '%f%')
//	OR (sys_groupid IN (groups) AND sys_perm_group LIKE '%f%')
//	OR sys_perm_other LIKE '%f%'
//
// Admins bypass the filter entirely (getAuthSQL returns '1'); a nil
// identity matches nothing (deny by default).
func WithPerm(id *Identity, perm byte) func(*gorm.DB) *gorm.DB {
	return func(tx *gorm.DB) *gorm.DB {
		if id == nil {
			return tx.Where("1 = 0")
		}
		if id.IsAdmin() {
			return tx
		}
		like := "%" + string(perm) + "%"
		cond := tx.Session(&gorm.Session{NewDB: true}).
			Where("sys_userid = ? AND sys_perm_user LIKE ?", id.UserID, like)
		if len(id.Groups) > 0 {
			cond = cond.Or("sys_groupid IN ? AND sys_perm_group LIKE ?", id.Groups, like)
		}
		cond = cond.Or("sys_perm_other LIKE ?", like)
		return tx.Where(cond)
	}
}

// sysColumns are the record permission columns every permissioned table
// carries (design D4).
var sysColumns = []string{"sys_userid", "sys_groupid", "sys_perm_user", "sys_perm_group", "sys_perm_other"}

// schemaCache is shared across Repo instances (gorm schema.Parse cache).
var schemaCache = &sync.Map{}

// Repo is the generic permission-checked repository for one model type T.
// Every read applies WithPerm(r); update and delete verify the flag via a
// ported checkPerm before touching the row; insert validates the flag
// against the record's own sys_ permission preset.
type Repo[T any] struct {
	db     *gorm.DB
	schema *schema.Schema
	pk     string
}

// New builds a repository for T. It fails when T has no primary key or
// lacks the sys_ permission columns — non-permissioned tables must not go
// through this layer.
func New[T any](db *gorm.DB) (*Repo[T], error) {
	s, err := schema.Parse(new(T), schemaCache, db.NamingStrategy)
	if err != nil {
		return nil, fmt.Errorf("parsing schema for %T: %w", *new(T), err)
	}
	if s.PrioritizedPrimaryField == nil {
		return nil, fmt.Errorf("repository: %s has no primary key", s.Table)
	}
	for _, col := range sysColumns {
		if s.LookUpField(col) == nil {
			return nil, fmt.Errorf("repository: %s has no %s column (not a permissioned table)", s.Table, col)
		}
	}
	return &Repo[T]{db: db, schema: s, pk: s.PrioritizedPrimaryField.DBName}, nil
}

// List loads every record the identity may read, with optional extra
// conditions (cond/args as in gorm Where). A nil identity returns
// ErrPermissionDenied, matching Insert/Update/Delete — an unauthenticated
// caller is a bug in the caller, not an empty result set.
func (r *Repo[T]) List(ctx context.Context, id *Identity, dest *[]T, conds ...any) error {
	if id == nil {
		return ErrPermissionDenied
	}
	tx := r.db.WithContext(ctx).Scopes(WithPerm(id, PermRead))
	if len(conds) > 0 {
		tx = tx.Where(conds[0], conds[1:]...)
	}
	return tx.Find(dest).Error
}

// Get loads one record by primary key if the identity may read it;
// otherwise it returns ErrPermissionDenied.
func (r *Repo[T]) Get(ctx context.Context, id *Identity, pk any, dest *T) error {
	err := r.db.WithContext(ctx).Scopes(WithPerm(id, PermRead)).
		Where(r.pk+" = ?", pk).First(dest).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrPermissionDenied
	}
	return err
}

// CheckPerm reports whether the identity holds the permission flag on the
// record with the given primary key (port of tform::checkPerm for existing
// records: SELECT under the auth WHERE clause, empty result = denied).
// Unlike List/Insert/Update/Delete, a nil identity is not an error here:
// this function's contract is to answer the permission question, and for a
// nil identity the answer is simply false (deny by default) — the error
// return is reserved for query failures.
func (r *Repo[T]) CheckPerm(ctx context.Context, id *Identity, pk any, perm byte) (bool, error) {
	if id == nil {
		return false, nil
	}
	var n int64
	err := r.db.WithContext(ctx).Model(new(T)).Scopes(WithPerm(id, perm)).
		Where(r.pk+" = ?", pk).Count(&n).Error
	return n > 0, err
}

// Insert creates a record after stamping its ownership server-side and
// checking the i flag against the entity's permission preset (port of
// tform::checkPerm for record_id 0 with the form auth_preset). Non-admin
// callers can never choose the sys_userid/sys_groupid/sys_perm_* values:
// the owner is the caller, the group is the caller's default group (or a
// requested group the caller belongs to) and the permissions come from the
// preset — the record body is never trusted for them. When T is
// datalog.Tracked, a sys_datalog row is written in the same transaction, so
// a rollback removes both the record and its journal entry.
func (r *Repo[T]) Insert(ctx context.Context, id *Identity, rec *T) error {
	return r.InsertFn(ctx, id, rec, nil)
}

// InsertFn is Insert with an optional fixup running inside the same
// transaction after the row exists (primary key assigned) and before the
// datalog insert row is written: derived defaults set there (ISPConfig
// onAfterInsert semantics, e.g. document_root = .../web<id>) land in both
// the row and the journaled record. fixup must persist its own changes on
// tx and mutate rec to match.
func (r *Repo[T]) InsertFn(ctx context.Context, id *Identity, rec *T, fixup func(tx *gorm.DB) error) error {
	if id == nil {
		return ErrPermissionDenied
	}
	if !id.IsAdmin() {
		if err := r.stampOwnership(ctx, id, rec); err != nil {
			return err
		}
	}
	if !r.canInsert(id, rec) {
		return ErrPermissionDenied
	}
	return r.txn(ctx, func(tx *gorm.DB) error {
		if err := tx.Create(rec).Error; err != nil {
			return err
		}
		if fixup != nil {
			if err := fixup(tx); err != nil {
				return err
			}
		}
		return datalog.LogInsert(tx, rec, id.Username)
	})
}

// txn runs fn in a transaction under a datalog.NotifyAfterCommit context and
// fires the datalog:ready notifier only after a successful commit — never
// before it and never on rollback (design D12 instant wake).
func (r *Repo[T]) txn(ctx context.Context, fn func(tx *gorm.DB) error) error {
	ctx, flush := datalog.NotifyAfterCommit(ctx)
	if err := r.db.WithContext(ctx).Transaction(fn); err != nil {
		return err
	}
	flush()
	return nil
}

// Update saves a record. The stored row is loaded under the u-flag
// permission scope (no row = denied), non-admin callers keep the stored
// sys_ ownership/permission columns (mass-assignment protection), and the
// UPDATE itself carries the permission scope in its WHERE clause with
// RowsAffected 0 treated as denied — no check-then-write window. When T is
// datalog.Tracked, the changed-fields diff against the stored row is
// journaled to sys_datalog in the same transaction.
func (r *Repo[T]) Update(ctx context.Context, id *Identity, rec *T) error {
	return r.UpdateFn(ctx, id, rec, nil)
}

// UpdateFn is Update with an optional fixup running inside the transaction
// after the stored row has been loaded (SELECT ... FOR UPDATE) and the sys_
// columns protected, and before the change detection: derived values
// computed from the locked stored state (e.g. the DNS SOA serial bump,
// design D7) land in the row and the datalog diff race-free. fixup may
// mutate rec; old is the locked stored row.
func (r *Repo[T]) UpdateFn(ctx context.Context, id *Identity, rec *T, fixup func(tx *gorm.DB, old *T) error) error {
	if id == nil {
		return ErrPermissionDenied
	}
	pk, _ := r.fieldValue(ctx, rec, r.pk)
	return r.txn(ctx, func(tx *gorm.DB) error {
		var old T
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Scopes(WithPerm(id, PermUpdate)).Where(r.pk+" = ?", pk).First(&old).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPermissionDenied
		}
		if err != nil {
			return err
		}
		if !id.IsAdmin() {
			if err := r.copySysColumns(ctx, &old, rec); err != nil {
				return err
			}
		}
		if fixup != nil {
			if err := fixup(tx, &old); err != nil {
				return err
			}
		}
		if reflect.DeepEqual(&old, rec) {
			return nil // nothing changed; no UPDATE, no datalog row
		}
		res := tx.Model(new(T)).Scopes(WithPerm(id, PermUpdate)).
			Where(r.pk+" = ?", pk).Select("*").Updates(rec)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrPermissionDenied
		}
		return datalog.LogUpdate(tx, &old, rec, id.Username)
	})
}

// Delete removes the record with the given primary key. The DELETE carries
// the d-flag permission scope in its WHERE clause and RowsAffected 0 is
// treated as denied — no check-then-write window. When T is
// datalog.Tracked, the full old record is journaled to sys_datalog in the
// same transaction.
func (r *Repo[T]) Delete(ctx context.Context, id *Identity, pk any) error {
	return r.DeleteFn(ctx, id, pk, nil)
}

// DeleteFn is Delete with an optional fixup running inside the transaction
// after the row has been loaded under the d-flag scope and before it is
// removed: cascades (e.g. journaling and deleting a zone's records) commit
// or roll back atomically with the parent delete.
func (r *Repo[T]) DeleteFn(ctx context.Context, id *Identity, pk any, fixup func(tx *gorm.DB, old *T) error) error {
	if id == nil {
		return ErrPermissionDenied
	}
	return r.txn(ctx, func(tx *gorm.DB) error {
		var old T
		err := tx.Scopes(WithPerm(id, PermDelete)).Where(r.pk+" = ?", pk).First(&old).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPermissionDenied
		}
		if err != nil {
			return err
		}
		if fixup != nil {
			if err := fixup(tx, &old); err != nil {
				return err
			}
		}
		res := tx.Scopes(WithPerm(id, PermDelete)).Where(r.pk+" = ?", pk).Delete(new(T))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrPermissionDenied
		}
		return datalog.LogDelete(tx, &old, id.Username)
	})
}

// PermPresetter is optionally implemented by models to override the default
// insert permission preset (the tform auth_preset perm_user/perm_group/
// perm_other values applied to new records).
type PermPresetter interface {
	// PermPreset returns the sys_perm_user, sys_perm_group and
	// sys_perm_other values stamped onto inserted records.
	PermPreset() (user, group, other string)
}

// permPreset returns the entity preset for rec, defaulting to the common
// tform preset riud/riud/"".
func permPreset[T any](rec *T) (user, group, other string) {
	if p, ok := any(rec).(PermPresetter); ok {
		return p.PermPreset()
	}
	return "riud", "riud", ""
}

// stampOwnership overwrites the sys_ columns of a record being inserted by
// a non-admin: owner = caller, group = a requested group the caller belongs
// to or the caller's default group, permissions = the entity preset. It
// fails with ErrPermissionDenied when no group can be determined.
func (r *Repo[T]) stampOwnership(ctx context.Context, id *Identity, rec *T) error {
	group := toUint32(r.mustFieldValue(ctx, rec, "sys_groupid"))
	if group == 0 || !id.InGroup(group) {
		group = id.DefaultGroup
	}
	if group == 0 && len(id.Groups) > 0 {
		group = id.Groups[0]
	}
	if group == 0 {
		return ErrPermissionDenied
	}
	permUser, permGroup, permOther := permPreset(rec)
	for col, val := range map[string]any{
		"sys_userid":     id.UserID,
		"sys_groupid":    group,
		"sys_perm_user":  permUser,
		"sys_perm_group": permGroup,
		"sys_perm_other": permOther,
	} {
		if err := r.setFieldValue(ctx, rec, col, val); err != nil {
			return err
		}
	}
	return nil
}

// canInsert ports checkPerm(0, 'i') against the entity permission preset:
// after stampOwnership the caller owns the new record, so insert is allowed
// when the preset grants i to the owner, the group or everyone (perm_other),
// matching the three branches of the PHP auth SQL. The record body is never
// consulted (Insert stamps it for non-admins before this check).
func (r *Repo[T]) canInsert(id *Identity, rec *T) bool {
	if id == nil {
		return false
	}
	if id.IsAdmin() {
		return true
	}
	permUser, permGroup, permOther := permPreset(rec)
	flag := string(PermInsert)
	return strings.Contains(permUser, flag) || strings.Contains(permGroup, flag) ||
		strings.Contains(permOther, flag)
}

// copySysColumns overwrites the sys_ ownership/permission columns of rec
// with the stored row's values, so a non-admin update can never move a
// record to another owner/group or escalate its permission strings.
func (r *Repo[T]) copySysColumns(ctx context.Context, from, to *T) error {
	for _, col := range sysColumns {
		v, _ := r.fieldValue(ctx, from, col)
		if err := r.setFieldValue(ctx, to, col, v); err != nil {
			return err
		}
	}
	return nil
}

// setFieldValue writes a model field by DB column name via the parsed
// schema.
func (r *Repo[T]) setFieldValue(ctx context.Context, rec *T, column string, value any) error {
	f := r.schema.LookUpField(column)
	if f == nil {
		return fmt.Errorf("repository: %s has no %s column", r.schema.Table, column)
	}
	if err := f.Set(ctx, reflect.ValueOf(rec).Elem(), value); err != nil {
		return fmt.Errorf("repository: setting %s.%s: %w", r.schema.Table, column, err)
	}
	return nil
}

// fieldValue reads a model field by DB column name via the parsed schema.
func (r *Repo[T]) fieldValue(ctx context.Context, rec *T, column string) (any, bool) {
	f := r.schema.LookUpField(column)
	if f == nil {
		return nil, false
	}
	v, zero := f.ValueOf(ctx, reflect.ValueOf(rec))
	return v, !zero
}

// mustFieldValue is fieldValue for columns New already verified to exist.
func (r *Repo[T]) mustFieldValue(ctx context.Context, rec *T, column string) any {
	v, _ := r.fieldValue(ctx, rec, column)
	return v
}

// toUint32 normalizes the numeric types GORM may return for id columns.
func toUint32(v any) uint32 {
	switch n := v.(type) {
	case uint32:
		return n
	case int32:
		return uint32(n)
	case uint64:
		return uint32(n)
	case int64:
		return uint32(n)
	case int:
		return uint32(n)
	case uint:
		return uint32(n)
	default:
		return 0
	}
}
