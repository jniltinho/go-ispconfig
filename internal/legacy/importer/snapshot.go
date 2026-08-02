package importer

import (
	"context"
	"fmt"
	"sort"

	"go-ispconfig/internal/legacy/client"
)

// Selection is the entity subset to import. Sites and DNS depend on
// clients for ownership: client records are always fetched (they resolve
// owners), but only imported when Clients is set.
type Selection struct {
	// Clients selects client (+ derived sys_user/sys_group) import.
	Clients bool
	// Sites selects web domain/folder/folder user import.
	Sites bool
	// DNS selects DNS zone/record/slave/template import.
	DNS bool
}

// All reports whether nothing was deselected.
func (s Selection) All() bool { return s.Clients && s.Sites && s.DNS }

// Snapshot is one consistent read of the legacy panel: every record the
// selected entities need, fetched through the read-only client. Client
// records are always present regardless of selection (owner resolution).
type Snapshot struct {
	// Clients maps legacy client_id to its record.
	Clients map[int]client.Record
	// Domains holds web_domain records: vhost, then vhostsubdomain, then
	// vhostalias passes (design D3).
	Domains []client.Record
	// Folders holds web_folder records.
	Folders []client.Record
	// FolderUsers holds web_folder_user records.
	FolderUsers []client.Record
	// Zones holds dns_soa records.
	Zones []client.Record
	// RRs maps legacy zone id to its dns_rr records.
	RRs map[int][]client.Record
	// Slaves holds dns_slave records.
	Slaves []client.Record
	// Templates holds dns_template records.
	Templates []client.Record
	// Servers is the legacy server list (server_get_all rows).
	Servers []client.Record
	// UserClient maps a legacy sys_userid to its client_id
	// (client_get_id), resolved for every owner userid seen on fetched
	// site/DNS records. Needed because 3.3.x panels leave admin-created
	// clients' own records owned by admin (sys_groupid 1), so the client
	// rows alone cannot map entity owner groups to clients.
	UserClient map[int]int
	// Selection records what was fetched for the sites/dns entities.
	Selection Selection
}

// domainTypes are the web_domain types imported, in pass order: parents
// (vhost) before the children that reference them (design D3).
var domainTypes = []string{"vhost", "vhostsubdomain", "vhostalias"}

// FetchSnapshot reads everything the selection needs from the legacy
// panel. It uses only *_get calls.
func FetchSnapshot(ctx context.Context, c *client.Client, sel Selection) (*Snapshot, error) {
	snap := &Snapshot{
		Clients:    map[int]client.Record{},
		RRs:        map[int][]client.Record{},
		UserClient: map[int]int{},
		Selection:  sel,
	}

	servers, err := c.ServerGetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching legacy server list: %w", err)
	}
	snap.Servers = servers

	// Clients are always fetched: sites/dns need them to resolve owners.
	ids, err := c.ClientGetAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching legacy client ids: %w", err)
	}
	sort.Ints(ids)
	for _, id := range ids {
		rec, err := c.ClientGet(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("fetching legacy client %d: %w", id, err)
		}
		snap.Clients[id] = rec
	}

	if sel.Sites {
		for _, typ := range domainTypes {
			page, err := c.SitesWebDomainGet(ctx, client.Filter{"type": typ})
			if err != nil {
				return nil, fmt.Errorf("fetching legacy %s domains: %w", typ, err)
			}
			snap.Domains = append(snap.Domains, page...)
		}
		if snap.Folders, err = c.SitesWebFolderGet(ctx, nil); err != nil {
			return nil, fmt.Errorf("fetching legacy web folders: %w", err)
		}
		if snap.FolderUsers, err = c.SitesWebFolderUserGet(ctx, nil); err != nil {
			return nil, fmt.Errorf("fetching legacy web folder users: %w", err)
		}
	}

	if sel.DNS {
		if snap.Zones, err = c.DNSZoneGetAll(ctx); err != nil {
			return nil, fmt.Errorf("fetching legacy DNS zones: %w", err)
		}
		for _, zone := range snap.Zones {
			id := zone.Int("id")
			rrs, err := c.DNSRRGetAllByZone(ctx, id)
			if err != nil {
				return nil, fmt.Errorf("fetching records of legacy zone %d (%s): %w", id, zone["origin"], err)
			}
			snap.RRs[id] = rrs
		}
		if snap.Slaves, err = c.DNSSlaveGetAll(ctx); err != nil {
			return nil, fmt.Errorf("fetching legacy slave zones: %w", err)
		}
		if snap.Templates, err = c.DNSTemplateZoneGetAll(ctx); err != nil {
			return nil, fmt.Errorf("fetching legacy zone templates: %w", err)
		}
	}

	// Resolve entity owner userids to clients (see Snapshot.UserClient).
	for _, recs := range [][]client.Record{snap.Domains, snap.Folders, snap.FolderUsers, snap.Zones, snap.Slaves} {
		for _, rec := range recs {
			uid := rec.Int("sys_userid")
			if uid <= 1 {
				continue // admin-owned; resolveOwner maps group<=1 itself
			}
			if _, done := snap.UserClient[uid]; done {
				continue
			}
			cid, err := c.ClientGetID(ctx, uid)
			if err != nil {
				return nil, fmt.Errorf("resolving owner of legacy sys_userid %d (client_get_id): %w", uid, err)
			}
			snap.UserClient[uid] = cid
		}
	}

	return snap, nil
}

// Inventory is the per-entity record count of a legacy panel plus its
// server list, shown before planning (wizard inventory step, CLI table).
type Inventory struct {
	// Clients is the legacy client count.
	Clients int `json:"clients"`
	// WebDomains counts web_domain records of all imported types.
	WebDomains int `json:"web_domains"`
	// WebFolders counts web_folder records.
	WebFolders int `json:"web_folders"`
	// WebFolderUsers counts web_folder_user records.
	WebFolderUsers int `json:"web_folder_users"`
	// DNSZones counts dns_soa records.
	DNSZones int `json:"dns_zones"`
	// DNSRecords counts dns_rr records across all zones.
	DNSRecords int `json:"dns_records"`
	// DNSSlaveZones counts dns_slave records.
	DNSSlaveZones int `json:"dns_slave_zones"`
	// DNSTemplates counts dns_template records.
	DNSTemplates int `json:"dns_templates"`
	// Servers is the legacy server list (server_id, server_name rows).
	Servers []client.Record `json:"servers"`
	// MultiServer flags a legacy panel with more than one server: the run
	// must be blocked until the operator explicitly confirms mapping
	// everything onto the single local server (design D3 guard).
	MultiServer bool `json:"multi_server"`
}

// Inventory derives the per-entity counts from the snapshot without
// touching the local database.
func (s *Snapshot) Inventory() *Inventory {
	inv := &Inventory{
		Clients:        len(s.Clients),
		WebDomains:     len(s.Domains),
		WebFolders:     len(s.Folders),
		WebFolderUsers: len(s.FolderUsers),
		DNSZones:       len(s.Zones),
		DNSSlaveZones:  len(s.Slaves),
		DNSTemplates:   len(s.Templates),
		Servers:        s.Servers,
		MultiServer:    len(s.Servers) > 1,
	}
	for _, rrs := range s.RRs {
		inv.DNSRecords += len(rrs)
	}
	return inv
}
