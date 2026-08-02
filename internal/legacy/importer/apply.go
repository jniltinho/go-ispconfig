package importer

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"

	"gorm.io/gorm"

	"go-ispconfig/internal/datalog"
	"go-ispconfig/internal/model"
)

// applyUser is the username stamped into sys_datalog rows written by the
// import (the migration runs with admin authority).
const applyUser = "admin"

// applyPhases groups plan tables into transactions, in dependency order.
// The client cluster (client + derived sys_group/sys_user) is one
// transaction because the three records reference each other.
var applyPhases = [][]string{
	{"client", "sys_group", "sys_user"},
	{"web_domain"},
	{"web_folder"},
	{"web_folder_user"},
	{"dns_soa"},
	{"dns_rr"},
	{"dns_slave"},
	{"dns_template"},
}

// Apply executes the plan: per-entity transactions writing every insert
// and update through the foundation's datalog writer (sys_datalog rows
// with {old,new} JSON, correct dbtable/dbidx/action/server_id), skipping
// conflicts. It returns the per-table tally of what was done.
func Apply(ctx context.Context, db *gorm.DB, plan *Plan) (map[string]EntityCount, error) {
	counts := map[string]EntityCount{}
	for _, tables := range applyPhases {
		items := itemsOf(plan, tables)
		if len(items) == 0 {
			continue
		}
		// Tallied per phase and merged only after the commit, so a
		// failed (rolled-back) phase never inflates the returned counts.
		phase := map[string]EntityCount{}
		nctx, flush := datalog.NotifyAfterCommit(ctx)
		err := db.WithContext(nctx).Transaction(func(tx *gorm.DB) error {
			if tables[0] == "client" {
				return applyClientCluster(tx, plan, items, phase)
			}
			return applyItems(tx, plan, items, phase)
		})
		if err != nil {
			return counts, fmt.Errorf("applying %s: %w", tables[0], err)
		}
		flush()
		for table, tally := range phase {
			c := counts[table]
			c.Created += tally.Created
			c.Updated += tally.Updated
			c.Skipped += tally.Skipped
			c.Conflicts += tally.Conflicts
			counts[table] = c
		}
	}
	return counts, nil
}

// itemsOf selects the plan items of the given tables, preserving order.
func itemsOf(plan *Plan, tables []string) []*Item {
	want := map[string]bool{}
	for _, t := range tables {
		want[t] = true
	}
	var out []*Item
	for i := range plan.Items {
		if want[plan.Items[i].Table] {
			out = append(out, &plan.Items[i])
		}
	}
	return out
}

// count tallies one item action.
func count(counts map[string]EntityCount, it *Item) {
	c := counts[it.Table]
	switch it.Action {
	case ActionCreate:
		c.Created++
	case ActionUpdate:
		c.Updated++
	case ActionSkip:
		c.Skipped++
	case ActionConflict:
		c.Conflicts++
	}
	counts[it.Table] = c
}

// applyClientCluster applies the client phase inside one transaction:
// clients first (parents before children, temporary admin ownership),
// then their sys_groups, then their sys_users, and finally each created
// client row is re-owned by its own recreated user/group before its
// datalog insert row is written — so the journaled record carries the
// final values.
func applyClientCluster(tx *gorm.DB, plan *Plan, items []*Item, counts map[string]EntityCount) error {
	var createdClients []*Item
	created := map[int]bool{} // legacy client ids created in this run

	for _, it := range items {
		if it.Table != "client" {
			continue
		}
		switch it.Action {
		case ActionCreate:
			rec := it.rec.(*model.Client)
			if it.legacyParent > 0 {
				id, ok := plan.getRemap("client", it.legacyParent)
				if !ok {
					return fmt.Errorf("client %s: parent (legacy id %d) was not created", it.Key, it.legacyParent)
				}
				rec.ParentClientID = id
			}
			rec.SysUserID, rec.SysGroupID = 1, 1 // temporary, re-owned below
			if err := tx.Create(rec).Error; err != nil {
				return fmt.Errorf("creating client %s: %w", it.Key, err)
			}
			plan.setRemap("client", it.LegacyID, rec.ClientID)
			createdClients = append(createdClients, it)
			created[it.LegacyID] = true
		case ActionUpdate:
			// A pending reseller parent resolves only now: the plan
			// diffed with parent_client_id 0, which must never be
			// written (it would break the hierarchy of an existing
			// client whose parent is created in this run).
			if it.legacyParent > 0 {
				id, ok := plan.getRemap("client", it.legacyParent)
				if !ok {
					return fmt.Errorf("client %s: parent (legacy id %d) was not created", it.Key, it.legacyParent)
				}
				it.rec.(*model.Client).ParentClientID = id
				if !slices.Contains(it.cols, "parent_client_id") {
					it.cols = append(it.cols, "parent_client_id")
				}
			}
			if err := applyUpdate(tx, it); err != nil {
				return err
			}
		}
		count(counts, it)
	}

	for _, it := range items {
		if it.Table != "sys_group" {
			continue
		}
		if it.Action == ActionCreate {
			rec := it.rec.(*model.SysGroup)
			cid, ok := plan.getRemap("client", it.LegacyID)
			if !ok {
				return fmt.Errorf("sys_group %s: its client (legacy id %d) was not created", it.Key, it.LegacyID)
			}
			rec.ClientID = cid
			if err := tx.Create(rec).Error; err != nil {
				return fmt.Errorf("creating sys_group %s: %w", it.Key, err)
			}
			plan.setRemap("sys_group", it.LegacyID, rec.GroupID)
		}
		if err := adoptFreeRow(tx, plan, it, created, "sys_group", "groupid"); err != nil {
			return err
		}
		count(counts, it)
	}

	for _, it := range items {
		if it.Table != "sys_user" {
			continue
		}
		if it.Action == ActionCreate {
			rec := it.rec.(*model.SysUser)
			gid, gok := plan.getRemap("sys_group", it.LegacyID)
			cid, cok := plan.getRemap("client", it.LegacyID)
			if !gok || !cok {
				return fmt.Errorf("sys_user %s: its client/group (legacy id %d) was not created", it.Key, it.LegacyID)
			}
			rec.SysGroupID = gid
			rec.Groups = strconv.FormatUint(uint64(gid), 10)
			rec.DefaultGroup = gid
			rec.ClientID = cid
			if err := tx.Create(rec).Error; err != nil {
				return fmt.Errorf("creating sys_user %s: %w", it.Key, err)
			}
			plan.setRemap("sys_user", it.LegacyID, rec.UserID)
		}
		if err := adoptFreeRow(tx, plan, it, created, "sys_user", "userid"); err != nil {
			return err
		}
		count(counts, it)
	}

	// Re-own created client rows by their recreated user/group and only
	// then journal the insert, so sys_datalog carries the final record.
	for _, it := range createdClients {
		rec := it.rec.(*model.Client)
		user, uok := plan.getRemap("sys_user", it.LegacyID)
		group, gok := plan.getRemap("sys_group", it.LegacyID)
		if !uok || !gok {
			// The planner guarantees a coherent cluster; a missing half
			// is a bug, never something to paper over.
			return fmt.Errorf("client %s: recreated sys_user/sys_group missing after create", it.Key)
		}
		rec.SysUserID, rec.SysGroupID = user, group
		err := tx.Model(&model.Client{}).Where("client_id = ?", rec.ClientID).
			Updates(map[string]any{"sys_userid": user, "sys_groupid": group}).Error
		if err != nil {
			return fmt.Errorf("re-owning client %s: %w", it.Key, err)
		}
		if err := datalog.LogInsert(tx, rec, applyUser); err != nil {
			return err
		}
	}
	return nil
}

// adoptFreeRow links a reused free sys_group/sys_user row (client_id 0)
// to the client created for it in this run, so the reseller graph and
// panel navigation see the association. Rows already tied to the same
// client are left untouched.
func adoptFreeRow(tx *gorm.DB, plan *Plan, it *Item, created map[int]bool, table, pkCol string) error {
	if it.Action != ActionSkip || !created[it.LegacyID] {
		return nil
	}
	cid, ok := plan.getRemap("client", it.LegacyID)
	if !ok {
		return nil
	}
	err := tx.Table(table).Where(pkCol+" = ? AND client_id = 0", it.localID).
		Update("client_id", cid).Error
	if err != nil {
		return fmt.Errorf("linking reused %s %s to its client: %w", table, it.Key, err)
	}
	return nil
}

// applyItems applies one non-client phase: creates (with pending FK/owner
// resolution) and updates, journaling each through the datalog writer.
func applyItems(tx *gorm.DB, plan *Plan, items []*Item, counts map[string]EntityCount) error {
	for _, it := range items {
		switch it.Action {
		case ActionCreate:
			if err := finalizeItem(plan, it); err != nil {
				return err
			}
			if err := tx.Create(it.rec).Error; err != nil {
				return fmt.Errorf("creating %s %s: %w", it.Table, it.Key, err)
			}
			pk, err := primaryKey(it.rec)
			if err != nil {
				return err
			}
			plan.setRemap(it.Table, it.LegacyID, pk)
			if err := datalog.LogInsert(tx, it.rec, applyUser); err != nil {
				return err
			}
		case ActionUpdate:
			if err := applyUpdate(tx, it); err != nil {
				return err
			}
		}
		count(counts, it)
	}
	return nil
}

// finalizeItem resolves the pending owner and parent references of a
// create item through the remap filled by earlier phases.
func finalizeItem(plan *Plan, it *Item) error {
	var user, group uint32
	if it.legacyOwnerGroup > 0 {
		var state ownerState
		user, group, state, _ = plan.resolveOwner(it.legacyOwnerGroup)
		if state != ownerOK {
			return fmt.Errorf("%s %s: owner (legacy sys_groupid %d) was not created", it.Table, it.Key, it.legacyOwnerGroup)
		}
	}

	parent := func(entity string) (uint32, error) {
		id, ok := plan.getRemap(entity, it.legacyParent)
		if !ok {
			return 0, fmt.Errorf("%s %s: parent %s (legacy id %d) was not created", it.Table, it.Key, entity, it.legacyParent)
		}
		return id, nil
	}

	switch rec := it.rec.(type) {
	case *model.WebDomain:
		if user != 0 {
			rec.SysUserID, rec.SysGroupID = user, group
		}
		if it.legacyParent > 0 {
			id, err := parent("web_domain")
			if err != nil {
				return err
			}
			rec.ParentDomainID = id
		}
	case *model.WebFolder:
		if user != 0 {
			rec.SysUserID, rec.SysGroupID = int32(user), int32(group)
		}
		if it.legacyParent > 0 {
			id, err := parent("web_domain")
			if err != nil {
				return err
			}
			rec.ParentDomainID = int32(id)
		}
	case *model.WebFolderUser:
		if user != 0 {
			rec.SysUserID, rec.SysGroupID = int32(user), int32(group)
		}
		if it.legacyParent > 0 {
			id, err := parent("web_folder")
			if err != nil {
				return err
			}
			rec.WebFolderID = int32(id)
		}
	case *model.DNSSoa:
		if user != 0 {
			rec.SysUserID, rec.SysGroupID = user, group
		}
	case *model.DNSRr:
		if user != 0 {
			rec.SysUserID, rec.SysGroupID = user, group
		}
		if it.legacyParent > 0 {
			id, err := parent("dns_soa")
			if err != nil {
				return err
			}
			rec.Zone = id
		}
	case *model.DNSSlave:
		if user != 0 {
			rec.SysUserID, rec.SysGroupID = user, group
		}
	}
	return nil
}

// applyUpdate loads the current local row, overwrites exactly the changed
// columns with the desired values and journals the {old,new} diff.
func applyUpdate(tx *gorm.DB, it *Item) error {
	old, err := emptyModel(it.Table)
	if err != nil {
		return err
	}
	s, err := parseSchema(old)
	if err != nil {
		return err
	}
	pkCol := s.PrioritizedPrimaryField.DBName
	if err := tx.Where(pkCol+" = ?", it.localID).First(old).Error; err != nil {
		return fmt.Errorf("loading %s %s for update: %w", it.Table, it.Key, err)
	}

	updated := clone(old)
	if err := copyColumns(it.cols, it.rec, updated); err != nil {
		return err
	}
	res := tx.Model(updated).Where(pkCol+" = ?", it.localID).Select(it.cols).Updates(updated)
	if res.Error != nil {
		return fmt.Errorf("updating %s %s: %w", it.Table, it.Key, res.Error)
	}
	return datalog.LogUpdate(tx, old, updated, applyUser)
}

// emptyModel returns a zero model pointer for a plan table.
func emptyModel(table string) (any, error) {
	switch table {
	case "client":
		return &model.Client{}, nil
	case "sys_user":
		return &model.SysUser{}, nil
	case "sys_group":
		return &model.SysGroup{}, nil
	case "web_domain":
		return &model.WebDomain{}, nil
	case "web_folder":
		return &model.WebFolder{}, nil
	case "web_folder_user":
		return &model.WebFolderUser{}, nil
	case "dns_soa":
		return &model.DNSSoa{}, nil
	case "dns_rr":
		return &model.DNSRr{}, nil
	case "dns_slave":
		return &model.DNSSlave{}, nil
	case "dns_template":
		return &model.DNSTemplate{}, nil
	default:
		return nil, fmt.Errorf("importer: unknown plan table %q", table)
	}
}

// clone returns a new pointer holding a shallow copy of the model src
// points to.
func clone(src any) any {
	v := reflect.ValueOf(src).Elem()
	dst := reflect.New(v.Type())
	dst.Elem().Set(v)
	return dst.Interface()
}

// copyColumns copies the named columns from one model to another of the
// same type via the parsed schema.
func copyColumns(cols []string, from, to any) error {
	s, err := parseSchema(from)
	if err != nil {
		return err
	}
	ctx := context.Background()
	fv := reflect.ValueOf(from)
	tv := reflect.ValueOf(to).Elem()
	for _, col := range cols {
		f := s.LookUpField(col)
		if f == nil {
			return fmt.Errorf("importer: %s has no column %s", s.Table, col)
		}
		val, _ := f.ValueOf(ctx, fv)
		if err := f.Set(ctx, tv, val); err != nil {
			return fmt.Errorf("importer: setting %s.%s: %w", s.Table, col, err)
		}
	}
	return nil
}

// primaryKey reads a model's primary key value after insert.
func primaryKey(rec any) (uint32, error) {
	s, err := parseSchema(rec)
	if err != nil {
		return 0, err
	}
	v, _ := s.PrioritizedPrimaryField.ValueOf(context.Background(), reflect.ValueOf(rec))
	switch n := v.(type) {
	case uint32:
		return n, nil
	case int64:
		return uint32(n), nil
	case uint64:
		return uint32(n), nil
	case int32:
		return uint32(n), nil
	case int:
		return uint32(n), nil
	default:
		return 0, fmt.Errorf("importer: unsupported primary key type %T", v)
	}
}
