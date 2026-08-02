package importer

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	"gorm.io/gorm"

	"go-ispconfig/internal/legacy/client"
	"go-ispconfig/internal/model"
)

// Action classifies one plan item.
type Action string

// Plan item classifications (design D6).
const (
	// ActionCreate inserts a new local record.
	ActionCreate Action = "create"
	// ActionUpdate rewrites the changed columns of an existing record.
	ActionUpdate Action = "update"
	// ActionSkip leaves an identical existing record untouched.
	ActionSkip Action = "skip-identical"
	// ActionConflict marks a record apply must not touch; Reason names why.
	ActionConflict Action = "conflict"
)

// Options configures a plan run.
type Options struct {
	// Selection is the entity subset to import (dependency-ordered:
	// clients before sites/dns).
	Selection Selection
	// TargetServerID is the local server every legacy server_id maps to.
	TargetServerID uint32
	// AssignOrphanZonesToAdmin assigns DNS zones (and slave zones) whose
	// owning client is absent to admin (sys_userid 1) instead of
	// conflicting, when explicitly requested.
	AssignOrphanZonesToAdmin bool
}

// Item is one planned record.
type Item struct {
	// Table is the local table the record belongs to.
	Table string `json:"table"`
	// Key is the natural key, for display and reports.
	Key string `json:"key"`
	// LegacyID is the legacy primary key (for sys_user/sys_group items it
	// is the legacy client id they are derived from).
	LegacyID int `json:"legacy_id"`
	// Action is the classification.
	Action Action `json:"action"`
	// Reason names the conflict cause (conflict items only).
	Reason string `json:"reason,omitempty"`

	rec     any      // desired model pointer
	localID uint32   // existing local pk (update/skip)
	cols    []string // changed columns (update)

	// Pending references resolved during apply (create items only; zero
	// when the value was already rewritten at plan time).
	legacyParent     int // legacy id of the parent record (per-table entity)
	legacyOwnerGroup int // legacy sys_groupid still needing owner resolution
}

// Plan is the full dry-run/apply plan: every fetched record classified,
// in apply order, plus the remap state apply needs.
type Plan struct {
	// Items lists every planned record in apply order.
	Items []Item
	// Warnings collects non-blocking notes (orphan zones assigned to
	// admin, SSL re-issue, ...).
	Warnings []string

	opts Options
	// remap maps entity → legacy id → existing/created local id. For
	// sys_user/sys_group the legacy id is the legacy client id.
	remap map[string]map[int]uint32
	// pending marks entity legacy ids planned as create (local id known
	// only during apply).
	pending map[string]map[int]bool
	// groupOwner maps a legacy sys_groupid to its legacy client id.
	groupOwner map[int]int
}

// EntityCount is the per-table create/update/skip/conflict tally.
type EntityCount struct {
	// Created counts ActionCreate items.
	Created int `json:"created"`
	// Updated counts ActionUpdate items.
	Updated int `json:"updated"`
	// Skipped counts ActionSkip items.
	Skipped int `json:"skipped"`
	// Conflicts counts ActionConflict items.
	Conflicts int `json:"conflicts"`
}

// Counts tallies the plan per local table.
func (p *Plan) Counts() map[string]EntityCount {
	out := map[string]EntityCount{}
	for _, it := range p.Items {
		c := out[it.Table]
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
		out[it.Table] = c
	}
	return out
}

// Conflicts returns the conflict items.
func (p *Plan) Conflicts() []Item {
	var out []Item
	for _, it := range p.Items {
		if it.Action == ActionConflict {
			out = append(out, it)
		}
	}
	return out
}

// diffExclude lists columns never compared or updated per table:
// daemon-managed artifacts and volatile timestamps.
var diffExclude = map[string]map[string]bool{
	"dns_soa": {
		"rendered_zone":      true,
		"dnssec_initialized": true,
		"dnssec_last_signed": true,
		"dnssec_info":        true,
	},
	"dns_rr": {"stamp": true},
}

// localState holds every local row the planner matches against, loaded
// once (read-only) and keyed by natural key.
type localState struct {
	clients     map[string]*model.Client        // username
	groupByName map[string]*model.SysGroup      // name
	userByName  map[string]*model.SysUser       // username
	groupOfCli  map[uint32]*model.SysGroup      // sys_group.client_id
	userOfCli   map[uint32]*model.SysUser       // sys_user.client_id
	domains     map[string]*model.WebDomain     // domain|type
	folders     map[string]*model.WebFolder     // parent_domain_id|path
	folderUsers map[string]*model.WebFolderUser // web_folder_id|username
	zones       map[string]*model.DNSSoa        // origin
	rrs         map[string]*model.DNSRr         // zone|name|type|data
	slaves      map[string]*model.DNSSlave      // origin
	templates   map[string]*model.DNSTemplate   // name
}

// newLocalState returns an empty localState with initialized maps.
func newLocalState() *localState {
	return &localState{
		clients:     map[string]*model.Client{},
		groupByName: map[string]*model.SysGroup{},
		userByName:  map[string]*model.SysUser{},
		groupOfCli:  map[uint32]*model.SysGroup{},
		userOfCli:   map[uint32]*model.SysUser{},
		domains:     map[string]*model.WebDomain{},
		folders:     map[string]*model.WebFolder{},
		folderUsers: map[string]*model.WebFolderUser{},
		zones:       map[string]*model.DNSSoa{},
		rrs:         map[string]*model.DNSRr{},
		slaves:      map[string]*model.DNSSlave{},
		templates:   map[string]*model.DNSTemplate{},
	}
}

// loadLocalState bulk-loads the natural-key lookup maps.
// ponytail: full-table loads; switch to per-key SELECTs if local panels
// ever grow beyond memory.
func loadLocalState(ctx context.Context, db *gorm.DB) (*localState, error) {
	st := newLocalState()
	var clients []model.Client
	if err := db.WithContext(ctx).Find(&clients).Error; err != nil {
		return nil, fmt.Errorf("loading local clients: %w", err)
	}
	for i := range clients {
		st.clients[clients[i].Username] = &clients[i]
	}
	var groups []model.SysGroup
	if err := db.WithContext(ctx).Find(&groups).Error; err != nil {
		return nil, fmt.Errorf("loading local sys_groups: %w", err)
	}
	for i := range groups {
		st.groupByName[groups[i].Name] = &groups[i]
		if groups[i].ClientID != 0 {
			st.groupOfCli[groups[i].ClientID] = &groups[i]
		}
	}
	var users []model.SysUser
	if err := db.WithContext(ctx).Find(&users).Error; err != nil {
		return nil, fmt.Errorf("loading local sys_users: %w", err)
	}
	for i := range users {
		st.userByName[users[i].Username] = &users[i]
		if users[i].ClientID != 0 {
			st.userOfCli[users[i].ClientID] = &users[i]
		}
	}
	var domains []model.WebDomain
	if err := db.WithContext(ctx).Find(&domains).Error; err != nil {
		return nil, fmt.Errorf("loading local web domains: %w", err)
	}
	for i := range domains {
		st.domains[domains[i].Domain+"|"+domains[i].Type] = &domains[i]
	}
	var folders []model.WebFolder
	if err := db.WithContext(ctx).Find(&folders).Error; err != nil {
		return nil, fmt.Errorf("loading local web folders: %w", err)
	}
	for i := range folders {
		st.folders[fmt.Sprintf("%d|%s", folders[i].ParentDomainID, folders[i].Path)] = &folders[i]
	}
	var folderUsers []model.WebFolderUser
	if err := db.WithContext(ctx).Find(&folderUsers).Error; err != nil {
		return nil, fmt.Errorf("loading local web folder users: %w", err)
	}
	for i := range folderUsers {
		st.folderUsers[fmt.Sprintf("%d|%s", folderUsers[i].WebFolderID, folderUsers[i].Username)] = &folderUsers[i]
	}
	var zones []model.DNSSoa
	if err := db.WithContext(ctx).Find(&zones).Error; err != nil {
		return nil, fmt.Errorf("loading local dns zones: %w", err)
	}
	for i := range zones {
		st.zones[zones[i].Origin] = &zones[i]
	}
	var rrs []model.DNSRr
	if err := db.WithContext(ctx).Find(&rrs).Error; err != nil {
		return nil, fmt.Errorf("loading local dns records: %w", err)
	}
	for i := range rrs {
		st.rrs[rrKey(rrs[i].Zone, rrs[i].Name, rrs[i].Type, rrs[i].Data, rrs[i].Aux)] = &rrs[i]
	}
	var slaves []model.DNSSlave
	if err := db.WithContext(ctx).Find(&slaves).Error; err != nil {
		return nil, fmt.Errorf("loading local slave zones: %w", err)
	}
	for i := range slaves {
		st.slaves[slaves[i].Origin] = &slaves[i]
	}
	var templates []model.DNSTemplate
	if err := db.WithContext(ctx).Find(&templates).Error; err != nil {
		return nil, fmt.Errorf("loading local dns templates: %w", err)
	}
	for i := range templates {
		st.templates[templates[i].Name] = &templates[i]
	}
	return st, nil
}

// rrKey builds the dns_rr natural key. Aux extends the spec's
// (zone, name, type, data) so MX/SRV records differing only in priority
// never collapse onto one another.
func rrKey(zone uint32, name, typ, data string, aux uint32) string {
	return fmt.Sprintf("%d|%s|%s|%s|%d", zone, name, typ, data, aux)
}

// ownerState is the result of resolving a legacy owner group.
type ownerState int

const (
	ownerOK      ownerState = iota // local ids known
	ownerPending                   // owning client planned as create
	ownerMissing                   // owner unknown locally and not planned
)

// planner carries the working state of one BuildPlan run.
type planner struct {
	snap  *Snapshot
	local *localState
	plan  *Plan
}

// BuildPlan classifies every snapshot record against the local database
// (read-only) as create/update/skip-identical/conflict, in dependency
// order: clients (resellers before their clients) with derived
// sys_group/sys_user, then web domains/folders/folder users, then DNS
// zones/records/slaves/templates. Foreign keys of update/skip items are
// rewritten through the remap immediately; create items keep legacy ids
// until apply resolves them.
func BuildPlan(ctx context.Context, db *gorm.DB, snap *Snapshot, opts Options) (*Plan, error) {
	local, err := loadLocalState(ctx, db)
	if err != nil {
		return nil, err
	}
	return buildPlan(snap, local, opts)
}

// buildPlan is BuildPlan after the local state has been loaded; split out
// so classification is unit-testable without a database.
func buildPlan(snap *Snapshot, local *localState, opts Options) (*Plan, error) {
	if opts.TargetServerID == 0 {
		return nil, fmt.Errorf("importer: a target local server id is required")
	}
	p := &planner{
		snap:  snap,
		local: local,
		plan: &Plan{
			opts:       opts,
			remap:      map[string]map[int]uint32{},
			pending:    map[string]map[int]bool{},
			groupOwner: map[int]int{},
		},
	}
	for legacyID, rec := range snap.Clients {
		if gid := rec.Int("sys_groupid"); gid > 1 {
			p.plan.groupOwner[gid] = legacyID
		}
	}
	// 3.3.x panels leave admin-created clients' own records owned by
	// admin (sys_groupid 1), so the loop above cannot map their groups.
	// Entity records carry the owning client's user AND group; the
	// snapshot's client_get_id resolution closes the mapping.
	for _, recs := range [][]client.Record{snap.Domains, snap.Folders, snap.FolderUsers, snap.Zones, snap.Slaves} {
		for _, rec := range recs {
			gid := rec.Int("sys_groupid")
			if gid <= 1 || p.plan.groupOwner[gid] != 0 {
				continue
			}
			if cid := snap.UserClient[rec.Int("sys_userid")]; cid > 0 {
				p.plan.groupOwner[gid] = cid
			}
		}
	}

	if err := p.planClients(opts.Selection.Clients); err != nil {
		return nil, err
	}
	if opts.Selection.Sites {
		if err := p.planSites(); err != nil {
			return nil, err
		}
	}
	if opts.Selection.DNS {
		if err := p.planDNS(); err != nil {
			return nil, err
		}
	}
	return p.plan, nil
}

// setRemap records an existing local id for a legacy id.
func (p *Plan) setRemap(entity string, legacyID int, localID uint32) {
	if p.remap[entity] == nil {
		p.remap[entity] = map[int]uint32{}
	}
	p.remap[entity][legacyID] = localID
}

// getRemap looks up the local id mapped to a legacy id.
func (p *Plan) getRemap(entity string, legacyID int) (uint32, bool) {
	id, ok := p.remap[entity][legacyID]
	return id, ok
}

// setPending marks a legacy id as planned-create.
func (p *Plan) setPending(entity string, legacyID int) {
	if p.pending[entity] == nil {
		p.pending[entity] = map[int]bool{}
	}
	p.pending[entity][legacyID] = true
}

// isPending reports whether a legacy id is planned as create.
func (p *Plan) isPending(entity string, legacyID int) bool {
	return p.pending[entity][legacyID]
}

// resolveOwner maps a legacy sys_groupid to the local owner sys_user and
// sys_group ids. Legacy group ids <= 1 are admin. The returned client id
// is the legacy owning client (0 for admin).
func (p *Plan) resolveOwner(legacyGroupID int) (user, group uint32, state ownerState, legacyClientID int) {
	if legacyGroupID <= 1 {
		return 1, 1, ownerOK, 0
	}
	cid, ok := p.groupOwner[legacyGroupID]
	if !ok {
		return 0, 0, ownerMissing, 0
	}
	g, gok := p.getRemap("sys_group", cid)
	u, uok := p.getRemap("sys_user", cid)
	if gok && uok {
		return u, g, ownerOK, cid
	}
	if p.isPending("client", cid) {
		return 0, 0, ownerPending, cid
	}
	return 0, 0, ownerMissing, cid
}

// orderClients returns legacy client ids parents-first (resellers before
// the clients that reference them), cycle-tolerant.
func orderClients(clients map[int]client.Record) []int {
	ids := make([]int, 0, len(clients))
	for id := range clients {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	emitted := map[int]bool{}
	var order []int
	for len(order) < len(ids) {
		progress := false
		for _, id := range ids {
			if emitted[id] {
				continue
			}
			parent := clients[id].Int("parent_client_id")
			if parent == 0 || parent == id || emitted[parent] || clients[parent] == nil {
				order = append(order, id)
				emitted[id] = true
				progress = true
			}
		}
		if !progress { // parent cycle: emit the rest in id order
			for _, id := range ids {
				if !emitted[id] {
					order = append(order, id)
					emitted[id] = true
				}
			}
		}
	}
	return order
}

// planClients classifies clients and their derived sys_group/sys_user.
// When doImport is false (clients deselected) it only fills the remap
// from existing local clients so sites/dns can resolve owners. A client
// created new whose derived sys_user/sys_group conflicts is downgraded
// to conflict as a whole — a client without its panel user/group would
// be an inconsistent import.
func (p *planner) planClients(doImport bool) error {
	for legacyID, rec := range p.snap.Clients {
		if local, ok := p.local.clients[rec["username"]]; ok {
			p.plan.setRemap("client", legacyID, local.ClientID)
			if g := p.local.groupOfCli[local.ClientID]; g != nil {
				p.plan.setRemap("sys_group", legacyID, g.GroupID)
			}
			if u := p.local.userOfCli[local.ClientID]; u != nil {
				p.plan.setRemap("sys_user", legacyID, u.UserID)
			}
		}
	}
	if !doImport {
		return nil
	}

	for _, legacyID := range orderClients(p.snap.Clients) {
		rec := p.snap.Clients[legacyID]
		mapped, err := MapClient(rec)
		if err != nil {
			return fmt.Errorf("mapping legacy client %d: %w", legacyID, err)
		}
		item := Item{Table: "client", Key: mapped.Username, LegacyID: legacyID}

		// Rewrite parent_client_id through the remap.
		legacyParent := rec.Int("parent_client_id")
		if legacyParent == 0 || legacyParent == legacyID {
			mapped.ParentClientID = 0
		} else {
			if id, ok := p.plan.getRemap("client", legacyParent); ok {
				mapped.ParentClientID = id
			} else if p.plan.isPending("client", legacyParent) {
				mapped.ParentClientID = 0 // resolved during apply
				item.legacyParent = legacyParent
			} else {
				item.Action = ActionConflict
				item.Reason = fmt.Sprintf("parent client (legacy id %d) is not imported and not present locally", legacyParent)
				p.plan.Items = append(p.plan.Items, item)
				continue
			}
		}

		local, exists := p.local.clients[mapped.Username]
		if !exists {
			gItem := p.deriveGroupItem(legacyID, mapped)
			uItem := p.deriveUserItem(legacyID, mapped)
			if gItem.Action == ActionConflict || uItem.Action == ActionConflict {
				reason := gItem.Reason
				if reason == "" {
					reason = uItem.Reason
				}
				item.Action = ActionConflict
				item.Reason = "cannot recreate panel user/group: " + reason
				downgradeDerived(&gItem, &uItem)
			} else {
				item.Action = ActionCreate
				item.rec = mapped
				p.plan.setPending("client", legacyID)
				if gItem.Action == ActionCreate {
					p.plan.setPending("sys_group", legacyID)
				}
				if uItem.Action == ActionCreate {
					p.plan.setPending("sys_user", legacyID)
				}
			}
			p.plan.Items = append(p.plan.Items, item, gItem, uItem)
			continue
		}

		// Existing client: it owns itself locally.
		if g := p.local.groupOfCli[local.ClientID]; g != nil {
			mapped.SysGroupID = g.GroupID
		} else {
			mapped.SysGroupID = local.SysGroupID
		}
		if u := p.local.userOfCli[local.ClientID]; u != nil {
			mapped.SysUserID = u.UserID
		} else {
			mapped.SysUserID = local.SysUserID
		}
		p.classifyExisting(&item, rec, mapped, local, local.ClientID)
		gItem := p.deriveGroupItem(legacyID, mapped)
		uItem := p.deriveUserItem(legacyID, mapped)
		if gItem.Action == ActionCreate {
			p.plan.setPending("sys_group", legacyID)
		}
		if uItem.Action == ActionCreate {
			p.plan.setPending("sys_user", legacyID)
		}
		p.plan.Items = append(p.plan.Items, item, gItem, uItem)
	}
	return nil
}

// downgradeDerived turns the non-conflict derived items of a conflicted
// client into conflicts too, so apply creates neither half of the pair.
func downgradeDerived(items ...*Item) {
	for _, it := range items {
		if it.Action != ActionConflict {
			it.Action = ActionConflict
			it.Reason = "owning client is not imported (derived sys_user/sys_group conflict)"
			it.rec = nil
		}
	}
}

// sameLocalClient reports whether clientID is the local client matched by
// username (0 means "free", also acceptable).
func (p *planner) sameLocalClient(clientID uint32, username string) bool {
	if clientID == 0 {
		return true
	}
	local, ok := p.local.clients[username]
	return ok && local.ClientID == clientID
}

// deriveGroupItem plans the sys_group recreated for a client. The caller
// appends the item and marks the pending state once the client cluster is
// known to be coherent.
func (p *planner) deriveGroupItem(legacyID int, c *model.Client) Item {
	item := Item{Table: "sys_group", Key: c.Username, LegacyID: legacyID}
	if existing, ok := p.local.groupByName[c.Username]; ok {
		// Reuse only when the group is free or tied to the same client.
		if !p.sameLocalClient(existing.ClientID, c.Username) {
			item.Action = ActionConflict
			item.Reason = fmt.Sprintf("sys_group %q belongs to a different local client (%d)", c.Username, existing.ClientID)
		} else {
			item.Action = ActionSkip
			item.localID = existing.GroupID
			p.plan.setRemap("sys_group", legacyID, existing.GroupID)
		}
	} else {
		item.Action = ActionCreate
		item.rec = DeriveSysGroup(c)
	}
	return item
}

// deriveUserItem plans the panel sys_user recreated for a client. Newly
// created users carry the unusable placeholder and are flagged for the
// password-reset flow; existing users are never modified.
func (p *planner) deriveUserItem(legacyID int, c *model.Client) Item {
	item := Item{Table: "sys_user", Key: c.Username, LegacyID: legacyID}
	if existing, ok := p.local.userByName[c.Username]; ok {
		if !p.sameLocalClient(existing.ClientID, c.Username) {
			item.Action = ActionConflict
			item.Reason = fmt.Sprintf("sys_user %q belongs to a different local client (%d)", c.Username, existing.ClientID)
		} else {
			item.Action = ActionSkip
			item.localID = existing.UserID
			p.plan.setRemap("sys_user", legacyID, existing.UserID)
		}
	} else {
		item.Action = ActionCreate
		item.rec = DeriveSysUser(c)
	}
	return item
}

// classifyExisting diffs a mapped record (FKs already rewritten) against
// the matching local row and fills the item as update or skip-identical.
func (p *planner) classifyExisting(item *Item, rec client.Record, mapped, local any, localID uint32) {
	item.rec = mapped // kept on skips too (report rsync suggestions)
	item.localID = localID
	cols, err := diffCols(rec, mapped, local, diffExclude[item.Table])
	if err != nil {
		// Cannot happen with well-formed models; never guess an update.
		item.Action = ActionConflict
		item.Reason = "internal: comparing with the local record failed: " + err.Error()
		return
	}
	if len(cols) > 0 {
		item.Action = ActionUpdate
		item.cols = cols
		return
	}
	item.Action = ActionSkip
}

// diffCols compares desired vs local for every column the legacy record
// carries (same predicate as the mapper), returning the changed column
// names.
func diffCols(rec client.Record, desired, local any, exclude map[string]bool) ([]string, error) {
	s, err := parseSchema(desired)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	dv := reflect.ValueOf(desired)
	lv := reflect.ValueOf(local)
	var cols []string
	for _, f := range s.Fields {
		if _, ok := shouldMap(rec, f); !ok {
			continue
		}
		if exclude[f.DBName] {
			continue
		}
		dVal, _ := f.ValueOf(ctx, dv)
		lVal, _ := f.ValueOf(ctx, lv)
		if !reflect.DeepEqual(dVal, lVal) {
			cols = append(cols, f.DBName)
		}
	}
	return cols, nil
}

// planSites classifies web domains, folders and folder users.
func (p *planner) planSites() error {
	for _, rec := range p.snap.Domains {
		legacyID := rec.Int("domain_id")
		mapped, err := MapWebDomain(rec)
		if err != nil {
			return err
		}
		item := Item{Table: "web_domain", Key: mapped.Domain + " (" + mapped.Type + ")", LegacyID: legacyID}
		mapped.ServerID = p.plan.opts.TargetServerID

		user, group, state, ownerCli := p.plan.resolveOwner(rec.Int("sys_groupid"))
		if state == ownerMissing {
			item.Action = ActionConflict
			item.Reason = ownerMissingReason(rec.Int("sys_groupid"), ownerCli, p.snap)
			p.plan.Items = append(p.plan.Items, item)
			continue
		}

		// Parent domain (vhostsubdomain/vhostalias) through the remap.
		legacyParent := rec.Int("parent_domain_id")
		parentPending := false
		if legacyParent != 0 {
			if id, ok := p.plan.getRemap("web_domain", legacyParent); ok {
				mapped.ParentDomainID = id
			} else if p.plan.isPending("web_domain", legacyParent) {
				parentPending = true
			} else {
				item.Action = ActionConflict
				item.Reason = fmt.Sprintf("parent domain (legacy id %d) is not imported and not present locally", legacyParent)
				p.plan.Items = append(p.plan.Items, item)
				continue
			}
		}

		local, exists := p.local.domains[mapped.Domain+"|"+mapped.Type]
		if !exists {
			item.Action = ActionCreate
			item.rec = mapped
			if state == ownerOK {
				mapped.SysUserID, mapped.SysGroupID = user, group
			} else { // pending: apply resolves the owner
				item.legacyOwnerGroup = rec.Int("sys_groupid")
			}
			if parentPending {
				item.legacyParent = legacyParent
			}
			p.plan.setPending("web_domain", legacyID)
			p.plan.Items = append(p.plan.Items, item)
			continue
		}
		switch {
		case state == ownerPending:
			item.Action = ActionConflict
			item.Reason = fmt.Sprintf("domain exists locally but its legacy owner client (legacy id %d) is only being created now — owned by a different user", ownerCli)
		case group != local.SysGroupID:
			item.Action = ActionConflict
			item.Reason = fmt.Sprintf("owned by a different user (local sys_groupid %d, imported owner %d)", local.SysGroupID, group)
		case parentPending:
			item.Action = ActionConflict
			item.Reason = "domain exists locally but its parent domain is only being created now"
		default:
			mapped.SysUserID, mapped.SysGroupID = user, group
			p.classifyExisting(&item, rec, mapped, local, local.DomainID)
			p.plan.setRemap("web_domain", legacyID, local.DomainID)
		}
		p.plan.Items = append(p.plan.Items, item)
	}

	for _, rec := range p.snap.Folders {
		if err := p.planChildRecord(rec, "web_folder"); err != nil {
			return err
		}
	}
	for _, rec := range p.snap.FolderUsers {
		if err := p.planChildRecord(rec, "web_folder_user"); err != nil {
			return err
		}
	}
	return nil
}

// planChildRecord classifies a web_folder (parent: web_domain) or
// web_folder_user (parent: web_folder) record: shared owner/parent/key
// logic.
func (p *planner) planChildRecord(rec client.Record, table string) error {
	var (
		legacyID     int
		parentEntity string
		legacyParent int
	)
	switch table {
	case "web_folder":
		legacyID = rec.Int("web_folder_id")
		parentEntity = "web_domain"
		legacyParent = rec.Int("parent_domain_id")
	default: // web_folder_user
		legacyID = rec.Int("web_folder_user_id")
		parentEntity = "web_folder"
		legacyParent = rec.Int("web_folder_id")
	}
	item := Item{Table: table, LegacyID: legacyID}

	user, group, state, ownerCli := p.plan.resolveOwner(rec.Int("sys_groupid"))
	if state == ownerMissing {
		item.Key = rec["path"] + rec["username"]
		item.Action = ActionConflict
		item.Reason = ownerMissingReason(rec.Int("sys_groupid"), ownerCli, p.snap)
		p.plan.Items = append(p.plan.Items, item)
		return nil
	}

	parentID, parentOK := p.plan.getRemap(parentEntity, legacyParent)
	parentPending := p.plan.isPending(parentEntity, legacyParent)
	if !parentOK && !parentPending {
		item.Key = rec["path"] + rec["username"]
		item.Action = ActionConflict
		item.Reason = fmt.Sprintf("parent %s (legacy id %d) is not imported and not present locally", parentEntity, legacyParent)
		p.plan.Items = append(p.plan.Items, item)
		return nil
	}

	if table == "web_folder" {
		mapped, err := MapWebFolder(rec)
		if err != nil {
			return err
		}
		mapped.ServerID = int32(p.plan.opts.TargetServerID)
		item.Key = fmt.Sprintf("%s (domain %d)", mapped.Path, legacyParent)
		if parentOK {
			mapped.ParentDomainID = int32(parentID)
			if local, exists := p.local.folders[fmt.Sprintf("%d|%s", parentID, mapped.Path)]; exists {
				if state == ownerPending {
					item.Action = ActionConflict
					item.Reason = fmt.Sprintf("folder exists locally but its legacy owner client (legacy id %d) is only being created now — owned by a different user", ownerCli)
				} else if group != uint32(local.SysGroupID) {
					item.Action = ActionConflict
					item.Reason = fmt.Sprintf("owned by a different user (local sys_groupid %d, imported owner %d)", local.SysGroupID, group)
				} else {
					mapped.SysUserID, mapped.SysGroupID = int32(user), int32(group)
					p.classifyExisting(&item, rec, mapped, local, uint32(local.WebFolderID))
					p.plan.setRemap("web_folder", legacyID, uint32(local.WebFolderID))
				}
				p.plan.Items = append(p.plan.Items, item)
				return nil
			}
		}
		if state == ownerOK {
			mapped.SysUserID, mapped.SysGroupID = int32(user), int32(group)
		} else {
			item.legacyOwnerGroup = rec.Int("sys_groupid")
		}
		if !parentOK {
			item.legacyParent = legacyParent
		}
		item.Action = ActionCreate
		item.rec = mapped
		p.plan.setPending("web_folder", legacyID)
		p.plan.Items = append(p.plan.Items, item)
		return nil
	}

	mapped, err := MapWebFolderUser(rec)
	if err != nil {
		return err
	}
	mapped.ServerID = int32(p.plan.opts.TargetServerID)
	item.Key = fmt.Sprintf("%s (folder %d)", mapped.Username, legacyParent)
	if parentOK {
		mapped.WebFolderID = int32(parentID)
		if local, exists := p.local.folderUsers[fmt.Sprintf("%d|%s", parentID, mapped.Username)]; exists {
			if state == ownerPending {
				item.Action = ActionConflict
				item.Reason = fmt.Sprintf("folder user exists locally but its legacy owner client (legacy id %d) is only being created now — owned by a different user", ownerCli)
			} else if group != uint32(local.SysGroupID) {
				item.Action = ActionConflict
				item.Reason = fmt.Sprintf("owned by a different user (local sys_groupid %d, imported owner %d)", local.SysGroupID, group)
			} else {
				mapped.SysUserID, mapped.SysGroupID = int32(user), int32(group)
				p.classifyExisting(&item, rec, mapped, local, uint32(local.WebFolderUserID))
				p.plan.setRemap("web_folder_user", legacyID, uint32(local.WebFolderUserID))
			}
			p.plan.Items = append(p.plan.Items, item)
			return nil
		}
	}
	if state == ownerOK {
		mapped.SysUserID, mapped.SysGroupID = int32(user), int32(group)
	} else {
		item.legacyOwnerGroup = rec.Int("sys_groupid")
	}
	if !parentOK {
		item.legacyParent = legacyParent
	}
	item.Action = ActionCreate
	item.rec = mapped
	p.plan.setPending("web_folder_user", legacyID)
	p.plan.Items = append(p.plan.Items, item)
	return nil
}

// planDNS classifies zones, records, slave zones and templates.
func (p *planner) planDNS() error {
	conflictedZones := map[int]bool{}

	for _, rec := range p.snap.Zones {
		legacyID := rec.Int("id")
		mapped, err := MapDNSSoa(rec)
		if err != nil {
			return err
		}
		item := Item{Table: "dns_soa", Key: mapped.Origin, LegacyID: legacyID}
		mapped.ServerID = int32(p.plan.opts.TargetServerID)

		user, group, state, ownerCli := p.plan.resolveOwner(rec.Int("sys_groupid"))
		if state == ownerMissing {
			if !p.plan.opts.AssignOrphanZonesToAdmin {
				item.Action = ActionConflict
				item.Reason = ownerMissingReason(rec.Int("sys_groupid"), ownerCli, p.snap)
				conflictedZones[legacyID] = true
				p.plan.Items = append(p.plan.Items, item)
				continue
			}
			user, group, state = 1, 1, ownerOK
			p.plan.Warnings = append(p.plan.Warnings,
				fmt.Sprintf("zone %s: owner not found, assigned to admin", mapped.Origin))
		}

		local, exists := p.local.zones[mapped.Origin]
		if !exists {
			if state == ownerOK {
				mapped.SysUserID, mapped.SysGroupID = user, group
			} else {
				item.legacyOwnerGroup = rec.Int("sys_groupid")
			}
			item.Action = ActionCreate
			item.rec = mapped
			p.plan.setPending("dns_soa", legacyID)
			p.plan.Items = append(p.plan.Items, item)
			continue
		}
		switch {
		case state == ownerPending:
			item.Action = ActionConflict
			item.Reason = fmt.Sprintf("zone exists locally but its legacy owner client (legacy id %d) is only being created now — owned by a different user", ownerCli)
			conflictedZones[legacyID] = true
		case group != local.SysGroupID:
			item.Action = ActionConflict
			item.Reason = fmt.Sprintf("owned by a different user (local sys_groupid %d, imported owner %d)", local.SysGroupID, group)
			conflictedZones[legacyID] = true
		default:
			mapped.SysUserID, mapped.SysGroupID = user, group
			p.classifyExisting(&item, rec, mapped, local, local.ID)
			p.plan.setRemap("dns_soa", legacyID, local.ID)
		}
		p.plan.Items = append(p.plan.Items, item)
	}

	// zoneGroup remembers each zone's legacy owner group so its records
	// can inherit it when their own owner cannot be resolved.
	zoneGroup := map[int]int{}
	for _, rec := range p.snap.Zones {
		zoneGroup[rec.Int("id")] = rec.Int("sys_groupid")
	}

	for legacyZone, rrs := range p.snap.RRs {
		for _, rec := range rrs {
			mapped, err := MapDNSRr(rec)
			if err != nil {
				return err
			}
			item := Item{Table: "dns_rr", LegacyID: rec.Int("id")}
			item.Key = fmt.Sprintf("%s %s %s", rec["name"], rec["type"], rec["data"])
			mapped.ServerID = int32(p.plan.opts.TargetServerID)

			zoneID, zoneOK := p.plan.getRemap("dns_soa", legacyZone)
			zonePending := p.plan.isPending("dns_soa", legacyZone)
			if conflictedZones[legacyZone] || (!zoneOK && !zonePending) {
				item.Action = ActionConflict
				item.Reason = fmt.Sprintf("zone (legacy id %d) is not imported and not present locally", legacyZone)
				p.plan.Items = append(p.plan.Items, item)
				continue
			}

			// RR owner; unknown owners inherit the zone's owner, and the
			// orphan-to-admin option applies zone-wide.
			ownerGroup := rec.Int("sys_groupid")
			user, group, state, _ := p.plan.resolveOwner(ownerGroup)
			if state == ownerMissing {
				ownerGroup = zoneGroup[legacyZone]
				user, group, state, _ = p.plan.resolveOwner(ownerGroup)
			}
			if state == ownerMissing && p.plan.opts.AssignOrphanZonesToAdmin {
				user, group, state = 1, 1, ownerOK
			}

			if zoneOK {
				mapped.Zone = zoneID
				if local, exists := p.local.rrs[rrKey(zoneID, mapped.Name, mapped.Type, mapped.Data, mapped.Aux)]; exists {
					if state == ownerOK {
						mapped.SysUserID, mapped.SysGroupID = user, group
					} else {
						mapped.SysUserID, mapped.SysGroupID = local.SysUserID, local.SysGroupID
					}
					p.classifyExisting(&item, rec, mapped, local, local.ID)
					p.plan.Items = append(p.plan.Items, item)
					continue
				}
			} else {
				item.legacyParent = legacyZone
			}
			if state == ownerOK {
				mapped.SysUserID, mapped.SysGroupID = user, group
			} else {
				item.legacyOwnerGroup = ownerGroup
			}
			item.Action = ActionCreate
			item.rec = mapped
			p.plan.Items = append(p.plan.Items, item)
		}
	}

	for _, rec := range p.snap.Slaves {
		legacyID := rec.Int("id")
		mapped, err := MapDNSSlave(rec)
		if err != nil {
			return err
		}
		item := Item{Table: "dns_slave", Key: mapped.Origin, LegacyID: legacyID}
		mapped.ServerID = int32(p.plan.opts.TargetServerID)

		user, group, state, ownerCli := p.plan.resolveOwner(rec.Int("sys_groupid"))
		if state == ownerMissing {
			if !p.plan.opts.AssignOrphanZonesToAdmin {
				item.Action = ActionConflict
				item.Reason = ownerMissingReason(rec.Int("sys_groupid"), ownerCli, p.snap)
				p.plan.Items = append(p.plan.Items, item)
				continue
			}
			user, group, state = 1, 1, ownerOK
			p.plan.Warnings = append(p.plan.Warnings,
				fmt.Sprintf("slave zone %s: owner not found, assigned to admin", mapped.Origin))
		}

		local, exists := p.local.slaves[mapped.Origin]
		if !exists {
			if state == ownerOK {
				mapped.SysUserID, mapped.SysGroupID = user, group
			} else {
				item.legacyOwnerGroup = rec.Int("sys_groupid")
			}
			item.Action = ActionCreate
			item.rec = mapped
			p.plan.Items = append(p.plan.Items, item)
			continue
		}
		switch {
		case state == ownerPending:
			item.Action = ActionConflict
			item.Reason = fmt.Sprintf("slave zone exists locally but its legacy owner client (legacy id %d) is only being created now — owned by a different user", ownerCli)
		case group != local.SysGroupID:
			item.Action = ActionConflict
			item.Reason = fmt.Sprintf("owned by a different user (local sys_groupid %d, imported owner %d)", local.SysGroupID, group)
		default:
			mapped.SysUserID, mapped.SysGroupID = user, group
			p.classifyExisting(&item, rec, mapped, local, local.ID)
		}
		p.plan.Items = append(p.plan.Items, item)
	}

	for _, rec := range p.snap.Templates {
		legacyID := rec.Int("template_id")
		mapped, err := MapDNSTemplate(rec)
		if err != nil {
			return err
		}
		item := Item{Table: "dns_template", Key: mapped.Name, LegacyID: legacyID}
		// Zone templates are panel-global objects: always admin-owned
		// locally.
		mapped.SysUserID, mapped.SysGroupID = 1, 1
		if mapped.SysPermUser == "" {
			mapped.SysPermUser, mapped.SysPermGroup = "riud", "riud"
		}

		if local, exists := p.local.templates[mapped.Name]; exists {
			p.classifyExisting(&item, rec, mapped, local, local.TemplateID)
		} else {
			item.Action = ActionCreate
			item.rec = mapped
		}
		p.plan.Items = append(p.plan.Items, item)
	}
	return nil
}

// ownerMissingReason names why a legacy owner could not be mapped.
func ownerMissingReason(legacyGroupID, legacyClientID int, snap *Snapshot) string {
	if legacyClientID == 0 {
		return fmt.Sprintf("owner (legacy sys_groupid %d) does not belong to any legacy client", legacyGroupID)
	}
	username := snap.Clients[legacyClientID]["username"]
	return fmt.Sprintf("owner client %q (legacy id %d) is not imported and not present locally", username, legacyClientID)
}
