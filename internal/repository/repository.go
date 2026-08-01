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
	"gorm.io/gorm/schema"
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
// Admins bypass the filter entirely (getAuthSQL returns '1').
func WithPerm(id *Identity, perm byte) func(*gorm.DB) *gorm.DB {
	return func(tx *gorm.DB) *gorm.DB {
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
// conditions (cond/args as in gorm Where).
func (r *Repo[T]) List(ctx context.Context, id *Identity, dest *[]T, conds ...any) error {
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
func (r *Repo[T]) CheckPerm(ctx context.Context, id *Identity, pk any, perm byte) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(new(T)).Scopes(WithPerm(id, perm)).
		Where(r.pk+" = ?", pk).Count(&n).Error
	return n > 0, err
}

// Insert creates a record after checking the i flag against the record's
// own sys_ permission preset (port of tform::checkPerm for record_id 0).
func (r *Repo[T]) Insert(ctx context.Context, id *Identity, rec *T) error {
	if !r.canInsert(id, rec) {
		return ErrPermissionDenied
	}
	return r.db.WithContext(ctx).Create(rec).Error
}

// Update saves a record after verifying the identity holds the u flag on
// the stored row (checkPerm-then-update, as tform's "Update denied").
func (r *Repo[T]) Update(ctx context.Context, id *Identity, rec *T) error {
	pk, _ := r.fieldValue(ctx, rec, r.pk)
	ok, err := r.CheckPerm(ctx, id, pk, PermUpdate)
	if err != nil {
		return err
	}
	if !ok {
		return ErrPermissionDenied
	}
	return r.db.WithContext(ctx).Save(rec).Error
}

// Delete removes the record with the given primary key after verifying the
// d flag.
func (r *Repo[T]) Delete(ctx context.Context, id *Identity, pk any) error {
	ok, err := r.CheckPerm(ctx, id, pk, PermDelete)
	if err != nil {
		return err
	}
	if !ok {
		return ErrPermissionDenied
	}
	return r.db.WithContext(ctx).Where(r.pk+" = ?", pk).Delete(new(T)).Error
}

// canInsert ports the checkPerm(0, 'i') preset branch using the record's
// own sys_ fields as the auth preset: owner match + i in sys_perm_user,
// group membership + i in sys_perm_group, i in sys_perm_other, or the
// "preset 0/0 means everyone may insert" rule.
func (r *Repo[T]) canInsert(id *Identity, rec *T) bool {
	if id.IsAdmin() {
		return true
	}
	ctx := context.Background()
	sysUserID := toUint32(r.mustFieldValue(ctx, rec, "sys_userid"))
	sysGroupID := toUint32(r.mustFieldValue(ctx, rec, "sys_groupid"))
	permUser, _ := r.mustFieldValue(ctx, rec, "sys_perm_user").(string)
	permGroup, _ := r.mustFieldValue(ctx, rec, "sys_perm_group").(string)
	permOther, _ := r.mustFieldValue(ctx, rec, "sys_perm_other").(string)

	flag := string(PermInsert)
	switch {
	case sysUserID == id.UserID && sysUserID != 0 && strings.Contains(permUser, flag):
		return true
	case sysGroupID != 0 && id.InGroup(sysGroupID) && strings.Contains(permGroup, flag):
		return true
	case strings.Contains(permOther, flag):
		return true
	case sysUserID == 0 && sysGroupID == 0 &&
		(strings.Contains(permUser, flag) || strings.Contains(permGroup, flag)):
		return true
	}
	return false
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
