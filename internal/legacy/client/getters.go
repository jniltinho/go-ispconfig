package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// DefaultPageSize is the #LIMIT# used by paged getters when
// Options.PageSize is zero.
const DefaultPageSize = 500

// ClientGetAll returns the ids of all clients on the legacy panel
// (client_get_all).
func (c *Client) ClientGetAll(ctx context.Context) ([]int, error) {
	var raw []flexInt
	if err := c.call(ctx, "client_get_all", nil, &raw); err != nil {
		return nil, err
	}
	ids := make([]int, len(raw))
	for i, id := range raw {
		ids[i] = int(id)
	}
	return ids, nil
}

// ClientGet returns the full record of one client (client_get).
func (c *Client) ClientGet(ctx context.Context, clientID int) (Record, error) {
	var rec Record
	if err := c.call(ctx, "client_get", map[string]any{"client_id": clientID}, &rec); err != nil {
		return nil, err
	}
	// The panel answers false instead of a record in some not-found paths.
	if rec == nil {
		return nil, fmt.Errorf("legacy: client_get: no record for client %d", clientID)
	}
	return rec, nil
}

// ClientGetID maps a legacy sys_user userid to its client_id
// (client_get_id). A legacy fault (no sys_user for that id, or a
// userid without a client) resolves to 0 — the caller treats 0 as
// "not owned by any client".
func (c *Client) ClientGetID(ctx context.Context, sysUserID int) (int, error) {
	var id flexInt
	err := c.call(ctx, "client_get_id", map[string]any{"sys_userid": sysUserID}, &id)
	if err != nil {
		var f *Fault
		if errors.As(err, &f) {
			return 0, nil
		}
		return 0, err
	}
	return int(id), nil
}

// SitesWebDomainGet returns all web domain records matching filter
// (sites_web_domain_get), iterating #OFFSET#/#LIMIT# pages until a page
// comes back shorter than the page size.
func (c *Client) SitesWebDomainGet(ctx context.Context, filter Filter) ([]Record, error) {
	return c.pagedGet(ctx, "sites_web_domain_get", filter)
}

// SitesWebFolderGet returns all web folder records matching filter
// (sites_web_folder_get), paged like SitesWebDomainGet.
func (c *Client) SitesWebFolderGet(ctx context.Context, filter Filter) ([]Record, error) {
	return c.pagedGet(ctx, "sites_web_folder_get", filter)
}

// SitesWebFolderUserGet returns all web folder user records matching
// filter (sites_web_folder_user_get), paged like SitesWebDomainGet.
func (c *Client) SitesWebFolderUserGet(ctx context.Context, filter Filter) ([]Record, error) {
	return c.pagedGet(ctx, "sites_web_folder_user_get", filter)
}

// DNSZoneGetAll returns all DNS zone (dns_soa) records
// (dns_zone_get with primary_id = -1).
func (c *Client) DNSZoneGetAll(ctx context.Context) ([]Record, error) {
	var zones []Record
	if err := c.call(ctx, "dns_zone_get", map[string]any{"primary_id": -1}, &zones); err != nil {
		return nil, err
	}
	return zones, nil
}

// DNSRRGetAllByZone returns all resource records of one zone
// (dns_rr_get_all_by_zone).
func (c *Client) DNSRRGetAllByZone(ctx context.Context, zoneID int) ([]Record, error) {
	var rrs []Record
	if err := c.call(ctx, "dns_rr_get_all_by_zone", map[string]any{"zone_id": zoneID}, &rrs); err != nil {
		return nil, err
	}
	return rrs, nil
}

// DNSSlaveGetAll returns all slave zone records
// (dns_slave_get with primary_id = -1).
func (c *Client) DNSSlaveGetAll(ctx context.Context) ([]Record, error) {
	var slaves []Record
	if err := c.call(ctx, "dns_slave_get", map[string]any{"primary_id": -1}, &slaves); err != nil {
		return nil, err
	}
	return slaves, nil
}

// DNSTemplateZoneGetAll returns all DNS zone template records
// (dns_templatezone_get_all).
func (c *Client) DNSTemplateZoneGetAll(ctx context.Context) ([]Record, error) {
	var templates []Record
	if err := c.call(ctx, "dns_templatezone_get_all", nil, &templates); err != nil {
		return nil, err
	}
	return templates, nil
}

// ServerGetAll returns the legacy server list (server_get_all), one
// record per server with server_id and server_name.
func (c *Client) ServerGetAll(ctx context.Context) ([]Record, error) {
	var servers []Record
	if err := c.call(ctx, "server_get_all", nil, &servers); err != nil {
		return nil, err
	}
	return servers, nil
}

// ServerGet returns one section of a legacy server's configuration
// (server_get). Section must be non-empty (e.g. "server", "web", "dns");
// the legacy panel returns nested per-section arrays for an empty
// section, which this client does not model.
func (c *Client) ServerGet(ctx context.Context, serverID int, section string) (Record, error) {
	var rec Record
	err := c.call(ctx, "server_get", map[string]any{"server_id": serverID, "section": section}, &rec)
	if err != nil {
		return nil, err
	}
	// The panel answers false instead of a record in some not-found paths.
	if rec == nil {
		return nil, fmt.Errorf("legacy: server_get: no %q config for server %d", section, serverID)
	}
	return rec, nil
}

// GetFunctionList returns the remote-API function names available to the
// remote user (get_function_list).
func (c *Client) GetFunctionList(ctx context.Context) ([]string, error) {
	var functions []string
	if err := c.call(ctx, "get_function_list", nil, &functions); err != nil {
		return nil, err
	}
	return functions, nil
}

// maxPages bounds paged iteration: at the default page size this allows
// one million records per entity, far beyond any real panel. It guards
// against a broken panel that ignores #OFFSET# and repeats full pages
// forever.
const maxPages = 2000

// pagedGet fetches every record matching filter from a *_get method that
// implements the legacy filter-object semantics, injecting #OFFSET# and
// #LIMIT# and iterating until a page returns fewer records than the page
// size.
func (c *Client) pagedGet(ctx context.Context, method string, filter Filter) ([]Record, error) {
	limit := c.pageSize
	var all []Record
	for offset, pages := 0, 0; ; offset, pages = offset+limit, pages+1 {
		if pages >= maxPages {
			return nil, fmt.Errorf("legacy: %s: pagination did not terminate after %d pages; the panel seems to ignore #OFFSET#", method, maxPages)
		}
		var page []Record
		primaryID := pagedFilter{filter: filter, offset: offset, limit: limit}
		if err := c.call(ctx, method, map[string]any{"primary_id": primaryID}, &page); err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < limit {
			return all, nil
		}
	}
}

// pagedFilter marshals a filter plus #OFFSET#/#LIMIT# as a JSON object
// whose filter keys come FIRST. The order is load-bearing: the legacy
// remoting_lib getDataRecord appends every key/value — including
// #OFFSET#/#LIMIT# — to the SQL parameter list, so any key seen before a
// filter key shifts the WHERE placeholders onto the wrong values and the
// query silently matches nothing. Go maps marshal keys alphabetically,
// which put "#LIMIT#" first and broke every filtered fetch against real
// panels (found against the ISPConfig 3.3.1p1 lab VMs).
type pagedFilter struct {
	filter Filter
	offset int
	limit  int
}

// MarshalJSON implements json.Marshaler with the ordering contract above.
func (p pagedFilter) MarshalJSON() ([]byte, error) {
	keys := make([]string, 0, len(p.filter))
	for k := range p.filter {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.WriteByte('{')
	for _, k := range keys {
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		vb, err := json.Marshal(p.filter[k])
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
		b.WriteByte(',')
	}
	fmt.Fprintf(&b, `"#OFFSET#":%d,"#LIMIT#":%d}`, p.offset, p.limit)
	return b.Bytes(), nil
}
