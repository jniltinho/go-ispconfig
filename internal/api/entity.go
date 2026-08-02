package api

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"sync"

	"github.com/labstack/echo/v5"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/repository"
	"go-ispconfig/internal/validator"
)

// Field is one declarative entity form field (port of a tform field
// definition, design D5): DB column, datatype/formtype rendering hints,
// default value, select options and validators. The same definition drives
// API validation and the form metadata endpoint.
type Field struct {
	// Name is the DB column and JSON key of the field.
	Name string `json:"name"`
	// Label is the i18n label key shown by the SPA.
	Label string `json:"label"`
	// Datatype is the tform datatype (INTEGER, VARCHAR, TEXT, ...).
	Datatype string `json:"datatype"`
	// Formtype is the tform form control (TEXT, SELECT, CHECKBOX, ...).
	Formtype string `json:"formtype"`
	// Default is the value applied when a create request omits the field.
	Default any `json:"default,omitempty"`
	// Options lists the allowed values of SELECT/CHECKBOX fields.
	Options []Option `json:"options,omitempty"`
	// Validators are the declarative validation rules of the field.
	Validators []validator.Rule `json:"validators,omitempty"`
	// AdminOnly hides the field from non-admin form metadata and makes the
	// API ignore it in non-admin request bodies (tform admin-only tabs).
	AdminOnly bool `json:"admin_only,omitempty"`
}

// Option is one selectable value of a SELECT or CHECKBOX field.
type Option struct {
	// Value is the stored value.
	Value string `json:"value"`
	// Label is the i18n label key of the option.
	Label string `json:"label"`
}

// Tab is one form tab grouping fields (tform tabs).
type Tab struct {
	// Name identifies the tab.
	Name string `json:"name"`
	// Label is the i18n label key of the tab.
	Label string `json:"label"`
	// Fields are the fields rendered on the tab.
	Fields []Field `json:"fields"`
	// AdminOnly marks every field of the tab as AdminOnly and hides the
	// whole tab from non-admin form metadata (tform admin-only tabs).
	AdminOnly bool `json:"admin_only,omitempty"`
}

// Entity is a declarative CRUD entity definition (the Go replacement of a
// form/*.tform.php file): name, form tabs with validated fields and the
// optional security policy flag gating its routes. Table and PK are filled
// from the GORM model at registration.
type Entity struct {
	// Name is the URL segment and metadata key ("server_ip").
	Name string `json:"name"`
	// Title is the i18n title key of the form.
	Title string `json:"title"`
	// Table is the DB table, derived from the model at registration.
	Table string `json:"-"`
	// PK is the primary key column, derived from the model at registration.
	PK string `json:"-"`
	// Tabs are the form tabs with their fields.
	Tabs []Tab `json:"tabs"`
	// Policy is an optional security policy flag (auth.RequirePolicy)
	// enforced on every route of the entity.
	Policy string `json:"-"`
	// AdminOnly restricts every route of the entity to admin sessions
	// (entities that are panel-wide configuration, e.g. DNS templates).
	AdminOnly bool `json:"-"`
	// Prepare, when set, runs after JSON bind and before defaults and
	// validation on create and update. It may mutate the body (derive
	// fields, hash passwords, verify referenced parent records) or veto
	// the request by returning an error.
	Prepare func(c *echo.Context, d *Deps, id *repository.Identity, body map[string]any) error `json:"-"`
	// AfterInsert, when set, runs inside the create transaction after the
	// row exists (primary key assigned) and before the datalog insert row
	// is written, so derived defaults land in both the row and the journal.
	AfterInsert func(ctx context.Context, tx *gorm.DB, id *repository.Identity, rec any) error `json:"-"`
	// Decorate, when set, may add or remove keys (e.g. datalog state,
	// password redaction) on the JSON objects returned by list and get.
	Decorate func(ctx context.Context, db *gorm.DB, items []map[string]any) error `json:"-"`
	// ListScope, when set, constrains the generic list query — role
	// discriminators on shared tables (e.g. clients vs resellers on the
	// client table via limit_client).
	ListScope func(tx *gorm.DB) *gorm.DB `json:"-"`
	// BeforeUpdate, when set, runs inside the update transaction after the
	// stored row has been loaded (SELECT ... FOR UPDATE) and before the
	// change detection: derived values computed from the locked stored
	// state (e.g. the DNS SOA serial bump) land race-free in the row and
	// its datalog diff. old and rec are *T; body is the request body.
	BeforeUpdate func(ctx context.Context, tx *gorm.DB, id *repository.Identity, body map[string]any, old, rec any) error `json:"-"`
	// BeforeDelete, when set, runs inside the delete transaction after the
	// row has been loaded under the d-scope and before it is removed, so
	// cascades (journaled child deletes) are atomic with the parent.
	BeforeDelete func(ctx context.Context, tx *gorm.DB, id *repository.Identity, rec any) error `json:"-"`
}

// fields iterates over every field of every tab.
func (e *Entity) fields(yield func(*Field) bool) {
	for ti := range e.Tabs {
		for fi := range e.Tabs[ti].Fields {
			if !yield(&e.Tabs[ti].Fields[fi]) {
				return
			}
		}
	}
}

// LimitHook is consulted before every entity create (design D-limit): it
// receives the entity name, the requesting identity and the request body
// (read-only; needed for per-type limits like web_domain subtypes) and
// may veto the operation by returning a *LimitError (rendered as 403
// with its i18n key). add-client-module plugs limit_* enforcement here.
type LimitHook func(ctx context.Context, entity string, id *repository.Identity, body map[string]any) error

// limitHook is the registered hook; the default allows everything.
var limitHook LimitHook = func(context.Context, string, *repository.Identity, map[string]any) error { return nil }

// RegisterLimitHook replaces the create limit hook. Call it once at startup
// before Register.
func RegisterLimitHook(h LimitHook) { limitHook = h }

// registry holds the registered entities for the form metadata endpoint.
var (
	registryMu sync.RWMutex
	registry   = map[string]*Entity{}
)

// lookupEntity returns a registered entity definition by name.
func lookupEntity(name string) (*Entity, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	ent, ok := registry[name]
	return ent, ok
}

// entitySchemaCache caches gorm schema parses for the API layer.
var entitySchemaCache = &sync.Map{}

// ListResponse is the paginated body of every entity list endpoint.
type ListResponse struct {
	// Items are the records of the requested page, keyed by DB column.
	Items []map[string]any `json:"items"`
	// Total is the number of records matching the filters (all pages).
	Total int64 `json:"total"`
	// Page is the returned 1-based page number.
	Page int `json:"page"`
	// Limit is the applied page size.
	Limit int `json:"limit"`
}

// entityHandlers wires the generic CRUD flow of one registered entity:
// bind JSON → declarative validation → limit hook → permission-scoped
// repository (ownership stamping + datalog in one transaction).
type entityHandlers[T any] struct {
	deps   *Deps
	ent    *Entity
	repo   *repository.Repo[T]
	schema *schema.Schema
}

// RegisterEntity mounts the generic CRUD routes of ent for model T under g:
// GET /<name> (paginated list), POST /<name>, GET/PUT/DELETE /<name>/:id.
// The entity becomes visible to the form metadata endpoint.
func RegisterEntity[T any](g *echo.Group, d *Deps, ent *Entity) error {
	repo, err := repository.New[T](d.DB)
	if err != nil {
		return err
	}
	s, err := schema.Parse(new(T), entitySchemaCache, d.DB.NamingStrategy)
	if err != nil {
		return fmt.Errorf("api: parsing schema for %T: %w", *new(T), err)
	}
	ent.Table = s.Table
	ent.PK = s.PrioritizedPrimaryField.DBName
	for ti := range ent.Tabs {
		if !ent.Tabs[ti].AdminOnly {
			continue
		}
		for fi := range ent.Tabs[ti].Fields {
			ent.Tabs[ti].Fields[fi].AdminOnly = true
		}
	}
	for f := range ent.fields {
		if s.LookUpField(f.Name) == nil {
			return fmt.Errorf("api: entity %s declares unknown column %s", ent.Name, f.Name)
		}
	}

	registryMu.Lock()
	registry[ent.Name] = ent
	registryMu.Unlock()

	h := &entityHandlers[T]{deps: d, ent: ent, repo: repo, schema: s}
	var mw []echo.MiddlewareFunc
	if ent.Policy != "" {
		mw = append(mw, auth.RequirePolicy(d.DB, ent.Policy))
	}
	if ent.AdminOnly {
		mw = append(mw, requireAdmin)
	}
	grp := g.Group("/"+ent.Name, mw...)
	grp.GET("", h.list)
	grp.POST("", h.create)
	grp.GET("/:id", h.get)
	grp.PUT("/:id", h.update)
	grp.DELETE("/:id", h.delete)
	return nil
}

// identity builds the repository identity of the authenticated session.
func identity(c *echo.Context) *repository.Identity {
	sess := auth.FromContext(c)
	if sess == nil {
		return nil
	}
	return &repository.Identity{
		UserID:       sess.UserID,
		Username:     sess.Username,
		Typ:          sess.Typ,
		Groups:       sess.Groups,
		DefaultGroup: sess.DefaultGroup,
	}
}

// list serves GET /<entity>: server-side pagination (?page=&limit=) and
// per-field substring filters (?<field>=v), permission-scoped via WithPerm.
func (h *entityHandlers[T]) list(c *echo.Context) error {
	id := identity(c)
	page, _ := strconv.Atoi(c.QueryParamOr("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.QueryParamOr("limit", "25"))
	if limit < 1 || limit > 100 {
		limit = 25
	}

	// The entity framework is the single generic query path; WithPerm keeps
	// every list permission-scoped (design D4). Filters accept only
	// declared field names, so no request input reaches SQL identifiers.
	q := h.deps.DB.WithContext(c.Request().Context()).Model(new(T)).
		Scopes(repository.WithPerm(id, repository.PermRead))
	if h.ent.ListScope != nil {
		q = h.ent.ListScope(q)
	}
	for f := range h.ent.fields {
		if v := c.QueryParam(f.Name); v != "" {
			q = q.Where(fmt.Sprintf("`%s` LIKE ?", f.Name), "%"+v+"%")
		}
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return err
	}
	var recs []T
	if err := q.Order("`" + h.ent.PK + "`").
		Offset((page - 1) * limit).Limit(limit).Find(&recs).Error; err != nil {
		return err
	}
	items := make([]map[string]any, len(recs))
	for i := range recs {
		items[i] = h.toMap(c.Request().Context(), &recs[i])
	}
	if err := h.decorate(c.Request().Context(), items); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, ListResponse{Items: items, Total: total, Page: page, Limit: limit})
}

// get serves GET /<entity>/:id.
func (h *entityHandlers[T]) get(c *echo.Context) error {
	var rec T
	if err := h.repo.Get(c.Request().Context(), identity(c), c.Param("id"), &rec); err != nil {
		return err
	}
	item := h.toMap(c.Request().Context(), &rec)
	if err := h.decorate(c.Request().Context(), []map[string]any{item}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, item)
}

// decorate applies the entity's Decorate hook, if any.
func (h *entityHandlers[T]) decorate(ctx context.Context, items []map[string]any) error {
	if h.ent.Decorate == nil || len(items) == 0 {
		return nil
	}
	return h.ent.Decorate(ctx, h.deps.DB, items)
}

// create serves POST /<entity>: defaults + body over a fresh record,
// validation, limit hook, then permission-checked insert with ownership
// stamping and datalog in one transaction.
func (h *entityHandlers[T]) create(c *echo.Context) error {
	id := identity(c)
	body := map[string]any{}
	if err := c.Bind(&body); err != nil {
		return err
	}

	ctx := c.Request().Context()
	if h.ent.Prepare != nil {
		if err := h.ent.Prepare(c, h.deps, id, body); err != nil {
			return err
		}
	}
	rec := new(T)
	if err := h.applyDefaults(ctx, rec, body); err != nil {
		return err
	}
	if err := h.applyBody(ctx, rec, body, id); err != nil {
		return err
	}
	if err := h.validate(ctx, rec, nil); err != nil {
		return err
	}
	if err := limitHook(ctx, h.ent.Name, id, body); err != nil {
		return err
	}
	if id != nil && id.IsAdmin() {
		// Admin inserts bypass repository stamping; default the sys_
		// ownership columns to the admin identity and the entity preset.
		if err := h.stampAdmin(ctx, rec, id); err != nil {
			return err
		}
	}
	var fixup func(tx *gorm.DB) error
	if h.ent.AfterInsert != nil {
		fixup = func(tx *gorm.DB) error { return h.ent.AfterInsert(ctx, tx, id, rec) }
	}
	if err := h.repo.InsertFn(ctx, id, rec, fixup); err != nil {
		return err
	}
	item := h.toMap(ctx, rec)
	if err := h.decorate(ctx, []map[string]any{item}); err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, item)
}

// update serves PUT /<entity>/:id: the stored record is loaded under the
// read scope, declared body fields are merged over it, the merged state is
// validated and saved under the u-flag scope with a datalog diff.
func (h *entityHandlers[T]) update(c *echo.Context) error {
	id := identity(c)
	body := map[string]any{}
	if err := c.Bind(&body); err != nil {
		return err
	}

	ctx := c.Request().Context()
	if h.ent.Prepare != nil {
		if err := h.ent.Prepare(c, h.deps, id, body); err != nil {
			return err
		}
	}
	var rec T
	if err := h.repo.Get(ctx, id, c.Param("id"), &rec); err != nil {
		return err
	}
	if err := h.applyBody(ctx, &rec, body, id); err != nil {
		return err
	}
	if err := h.validate(ctx, &rec, c.Param("id")); err != nil {
		return err
	}
	var fixup func(tx *gorm.DB, old *T) error
	if h.ent.BeforeUpdate != nil {
		fixup = func(tx *gorm.DB, old *T) error {
			return h.ent.BeforeUpdate(ctx, tx, id, body, old, &rec)
		}
	}
	if err := h.repo.UpdateFn(ctx, id, &rec, fixup); err != nil {
		return err
	}
	item := h.toMap(ctx, &rec)
	if err := h.decorate(ctx, []map[string]any{item}); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, item)
}

// delete serves DELETE /<entity>/:id.
func (h *entityHandlers[T]) delete(c *echo.Context) error {
	ctx := c.Request().Context()
	id := identity(c)
	var fixup func(tx *gorm.DB, old *T) error
	if h.ent.BeforeDelete != nil {
		fixup = func(tx *gorm.DB, old *T) error {
			return h.ent.BeforeDelete(ctx, tx, id, old)
		}
	}
	if err := h.repo.DeleteFn(ctx, id, c.Param("id"), fixup); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// validate runs every declared field rule against the record's merged
// state and returns a *ValidationError with the per-field i18n keys. pk is
// nil on create; on update it scopes the UNIQUE check.
func (h *entityHandlers[T]) validate(ctx context.Context, rec *T, pk any) error {
	fields := map[string][]string{}
	for f := range h.ent.fields {
		if len(f.Validators) == 0 {
			continue
		}
		value := h.fieldString(ctx, rec, f.Name)
		vc := &validator.Context{
			Ctx:      ctx,
			DB:       h.deps.DB,
			Table:    h.ent.Table,
			Column:   f.Name,
			PKColumn: h.ent.PK,
			PK:       pk,
		}
		for _, rule := range f.Validators {
			key, err := rule.Validate(vc, value)
			if err != nil {
				return err
			}
			if key != "" {
				fields[f.Name] = append(fields[f.Name], key)
			}
		}
	}
	if len(fields) > 0 {
		return &ValidationError{Fields: fields}
	}
	return nil
}

// applyDefaults writes each declared field default onto a fresh record when
// the request body does not carry the field.
func (h *entityHandlers[T]) applyDefaults(ctx context.Context, rec *T, body map[string]any) error {
	for f := range h.ent.fields {
		if f.Default == nil {
			continue
		}
		if _, ok := body[f.Name]; ok {
			continue
		}
		if err := h.setField(ctx, rec, f.Name, f.Default); err != nil {
			return err
		}
	}
	return nil
}

// applyBody copies the declared entity fields present in the JSON body onto
// the record. Undeclared keys — including the sys_ ownership columns and
// the primary key — are ignored: the body can never set them. AdminOnly
// fields are ignored for non-admin identities (tform admin-only tabs).
func (h *entityHandlers[T]) applyBody(ctx context.Context, rec *T, body map[string]any, id *repository.Identity) error {
	admin := id != nil && id.IsAdmin()
	for f := range h.ent.fields {
		if f.AdminOnly && !admin {
			continue
		}
		v, ok := body[f.Name]
		if !ok || v == nil {
			continue
		}
		if err := h.setField(ctx, rec, f.Name, v); err != nil {
			return err
		}
	}
	return nil
}

// stampAdmin defaults the sys_ ownership/permission columns of an
// admin-inserted record: owner admin, admin default group, entity preset
// permissions (riud/riud/” unless the model overrides PermPreset).
func (h *entityHandlers[T]) stampAdmin(ctx context.Context, rec *T, id *repository.Identity) error {
	permUser, permGroup, permOther := "riud", "riud", ""
	if p, ok := any(rec).(repository.PermPresetter); ok {
		permUser, permGroup, permOther = p.PermPreset()
	}
	group := id.DefaultGroup
	if group == 0 {
		group = 1 // admin group
	}
	for col, val := range map[string]any{
		"sys_userid":     id.UserID,
		"sys_groupid":    group,
		"sys_perm_user":  permUser,
		"sys_perm_group": permGroup,
		"sys_perm_other": permOther,
	} {
		if err := h.setField(ctx, rec, col, val); err != nil {
			return err
		}
	}
	return nil
}

// setField writes a model field by DB column name via the parsed schema.
func (h *entityHandlers[T]) setField(ctx context.Context, rec *T, column string, value any) error {
	f := h.schema.LookUpField(column)
	if f == nil {
		return fmt.Errorf("api: %s has no %s column", h.schema.Table, column)
	}
	if err := f.Set(ctx, reflect.ValueOf(rec).Elem(), value); err != nil {
		return &ValidationError{Fields: map[string][]string{column: {"error.validation.type"}}}
	}
	return nil
}

// fieldString reads a model field by DB column name as the string the
// validators consume (tform validates string form values). A zero numeric
// field serializes as "0", never "": server_id=0 must reach ISPOSITIVE as
// "0" and fail, not vanish as an empty value.
func (h *entityHandlers[T]) fieldString(ctx context.Context, rec *T, column string) string {
	f := h.schema.LookUpField(column)
	if f == nil {
		return ""
	}
	v, zero := f.ValueOf(ctx, reflect.ValueOf(rec))
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return fmt.Sprintf("%v", v)
	default:
		if zero {
			return ""
		}
		return fmt.Sprintf("%v", v)
	}
}

// toMap renders a record as its canonical JSON object keyed by DB column.
func (h *entityHandlers[T]) toMap(ctx context.Context, rec *T) map[string]any {
	out := make(map[string]any, len(h.schema.Fields))
	rv := reflect.ValueOf(rec)
	for _, f := range h.schema.Fields {
		v, _ := f.ValueOf(ctx, rv)
		out[f.DBName] = v
	}
	return out
}
