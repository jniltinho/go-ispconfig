package importer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/legacy/client"
	"go-ispconfig/internal/model"
)

// testSnapshot builds a small legacy panel: reseller (1) with child
// client (2), one vhost + one subdomain, a folder + folder user, one
// zone with one RR, a slave zone and a template.
func testSnapshot() *Snapshot {
	return &Snapshot{
		Selection: Selection{Clients: true, Sites: true, DNS: true},
		Clients: map[int]client.Record{
			1: {"client_id": "1", "username": "reseller1", "parent_client_id": "0",
				"sys_userid": "2", "sys_groupid": "3", "contact_name": "Reseller",
				"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
			2: {"client_id": "2", "username": "client2", "parent_client_id": "1",
				"sys_userid": "3", "sys_groupid": "4", "contact_name": "Client Two",
				"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
		},
		Domains: []client.Record{
			{"domain_id": "10", "domain": "example.com", "type": "vhost", "server_id": "9",
				"parent_domain_id": "0", "document_root": "/var/www/clients/client2/web10",
				"system_user": "web10", "system_group": "client2", "active": "y",
				"sys_userid": "3", "sys_groupid": "4",
				"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
			{"domain_id": "11", "domain": "sub.example.com", "type": "vhostsubdomain", "server_id": "9",
				"parent_domain_id": "10", "document_root": "/var/www/clients/client2/web10", "active": "y",
				"sys_userid": "3", "sys_groupid": "4",
				"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
		},
		Folders: []client.Record{
			{"web_folder_id": "20", "parent_domain_id": "10", "path": "protected",
				"server_id": "9", "active": "y", "sys_userid": "3", "sys_groupid": "4",
				"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
		},
		FolderUsers: []client.Record{
			{"web_folder_user_id": "30", "web_folder_id": "20", "username": "fuser",
				"password": "$6$abc$hash", "server_id": "9", "active": "y",
				"sys_userid": "3", "sys_groupid": "4",
				"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
		},
		Zones: []client.Record{
			{"id": "40", "origin": "example.com.", "ns": "ns1.example.com.",
				"mbox": "hostmaster.example.com.", "serial": "2024010101", "server_id": "9",
				"refresh": "7200", "retry": "540", "expire": "604800", "minimum": "3600",
				"ttl": "3600", "active": "Y", "sys_userid": "3", "sys_groupid": "4",
				"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
		},
		RRs: map[int][]client.Record{
			40: {
				{"id": "50", "zone": "40", "name": "www", "type": "A", "data": "192.0.2.1",
					"ttl": "3600", "active": "Y", "server_id": "9",
					"sys_userid": "3", "sys_groupid": "4",
					"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
			},
		},
		Slaves: []client.Record{
			{"id": "60", "origin": "slave.example.net.", "ns": "192.0.2.53", "active": "Y",
				"server_id": "9", "sys_userid": "1", "sys_groupid": "1",
				"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
		},
		Templates: []client.Record{
			{"template_id": "70", "name": "Default", "visible": "Y",
				"fields": "DOMAIN,IP", "template": "[ZONE]"},
		},
		Servers: []client.Record{{"server_id": "9", "server_name": "legacy1"}},
	}
}

// itemFor finds the plan item of one table+key.
func itemFor(t *testing.T, p *Plan, table, key string) Item {
	t.Helper()
	for _, it := range p.Items {
		if it.Table == table && it.Key == key {
			return it
		}
	}
	t.Fatalf("no %s item with key %q in plan", table, key)
	return Item{}
}

func TestPlanEmptyLocalPanelAllCreates(t *testing.T) {
	plan, err := buildPlan(testSnapshot(), newLocalState(), Options{
		Selection:      Selection{Clients: true, Sites: true, DNS: true},
		TargetServerID: 1,
	})
	require.NoError(t, err)

	counts := plan.Counts()
	require.Equal(t, EntityCount{Created: 2}, counts["client"])
	require.Equal(t, EntityCount{Created: 2}, counts["sys_group"])
	require.Equal(t, EntityCount{Created: 2}, counts["sys_user"])
	require.Equal(t, EntityCount{Created: 2}, counts["web_domain"])
	require.Equal(t, EntityCount{Created: 1}, counts["web_folder"])
	require.Equal(t, EntityCount{Created: 1}, counts["web_folder_user"])
	require.Equal(t, EntityCount{Created: 1}, counts["dns_soa"])
	require.Equal(t, EntityCount{Created: 1}, counts["dns_rr"])
	require.Equal(t, EntityCount{Created: 1}, counts["dns_slave"])
	require.Equal(t, EntityCount{Created: 1}, counts["dns_template"])
	require.Empty(t, plan.Conflicts())

	// Reseller ordered before its client.
	var clientKeys []string
	for _, it := range plan.Items {
		if it.Table == "client" {
			clientKeys = append(clientKeys, it.Key)
		}
	}
	require.Equal(t, []string{"reseller1", "client2"}, clientKeys)

	// Server ids are remapped to the target local server.
	dom := itemFor(t, plan, "web_domain", "example.com (vhost)")
	require.Equal(t, uint32(1), dom.rec.(*model.WebDomain).ServerID)
}

func TestPlanMissingTargetServer(t *testing.T) {
	_, err := buildPlan(testSnapshot(), newLocalState(), Options{})
	require.Error(t, err)
}

// localWithClient2 is a local panel already holding client2 with its
// sys_user/sys_group (local ids differ from legacy).
func localWithClient2() *localState {
	st := newLocalState()
	st.clients["client2"] = &model.Client{ClientID: 7, Username: "client2",
		SysUserID: 30, SysGroupID: 31, ContactName: "Client Two", ParentClientID: 5,
		SysPermUser: "riud", SysPermGroup: "riud"}
	st.clients["reseller1"] = &model.Client{ClientID: 5, Username: "reseller1",
		SysUserID: 28, SysGroupID: 29, ContactName: "Reseller",
		SysPermUser: "riud", SysPermGroup: "riud"}
	st.groupByName["client2"] = &model.SysGroup{GroupID: 31, Name: "client2", ClientID: 7}
	st.groupOfCli[7] = st.groupByName["client2"]
	st.userByName["client2"] = &model.SysUser{UserID: 30, Username: "client2", ClientID: 7}
	st.userOfCli[7] = st.userByName["client2"]
	st.groupByName["reseller1"] = &model.SysGroup{GroupID: 29, Name: "reseller1", ClientID: 5}
	st.groupOfCli[5] = st.groupByName["reseller1"]
	st.userByName["reseller1"] = &model.SysUser{UserID: 28, Username: "reseller1", ClientID: 5}
	st.userOfCli[5] = st.userByName["reseller1"]
	return st
}

func TestPlanExistingRecordsSkipOrUpdate(t *testing.T) {
	st := localWithClient2()
	st.zones["example.com."] = &model.DNSSoa{ID: 12, Origin: "example.com.",
		SysUserID: 30, SysGroupID: 31, ServerID: 1,
		NS: "ns1.example.com.", Mbox: "hostmaster.example.com.", Serial: 2024010101,
		Refresh: 7200, Retry: 540, Expire: 604800, Minimum: 3600, TTL: 3600, Active: "Y",
		SysPermUser: "riud", SysPermGroup: "riud"}
	st.rrs[rrKey(12, "www", "A", "192.0.2.1", 0)] = &model.DNSRr{ID: 90, Zone: 12,
		SysUserID: 30, SysGroupID: 31, ServerID: 1,
		Name: "www", Type: "A", Data: "192.0.2.1", TTL: 3600, Active: "Y",
		SysPermUser: "riud", SysPermGroup: "riud"}

	plan, err := buildPlan(testSnapshot(), st, Options{
		Selection:      Selection{Clients: true, DNS: true},
		TargetServerID: 1,
	})
	require.NoError(t, err)
	require.Empty(t, plan.Conflicts())

	zone := itemFor(t, plan, "dns_soa", "example.com.")
	require.Equal(t, ActionSkip, zone.Action, "identical zone must be skip-identical")
	rr := itemFor(t, plan, "dns_rr", "www A 192.0.2.1")
	require.Equal(t, ActionSkip, rr.Action)

	// A changed legacy field re-plans as update carrying only that field.
	snap := testSnapshot()
	snap.Zones[0]["serial"] = "2024020202"
	plan, err = buildPlan(snap, st, Options{
		Selection:      Selection{Clients: true, DNS: true},
		TargetServerID: 1,
	})
	require.NoError(t, err)
	zone = itemFor(t, plan, "dns_soa", "example.com.")
	require.Equal(t, ActionUpdate, zone.Action)
	require.Equal(t, []string{"serial"}, zone.cols)
	require.Equal(t, uint32(12), zone.localID)
}

func TestPlanConflictDifferentOwner(t *testing.T) {
	st := localWithClient2()
	// example.com exists locally but belongs to another group (99).
	st.domains["example.com|vhost"] = &model.WebDomain{DomainID: 55, Domain: "example.com",
		Type: "vhost", SysUserID: 98, SysGroupID: 99, ServerID: 1}

	plan, err := buildPlan(testSnapshot(), st, Options{
		Selection:      Selection{Clients: true, Sites: true},
		TargetServerID: 1,
	})
	require.NoError(t, err)

	dom := itemFor(t, plan, "web_domain", "example.com (vhost)")
	require.Equal(t, ActionConflict, dom.Action)
	require.Contains(t, dom.Reason, "owned by a different user")
}

func TestPlanConflictMissingOwner(t *testing.T) {
	// Sites selected without clients, empty local panel: owners missing.
	snap := testSnapshot()
	snap.Selection = Selection{Sites: true}
	plan, err := buildPlan(snap, newLocalState(), Options{
		Selection:      Selection{Sites: true},
		TargetServerID: 1,
	})
	require.NoError(t, err)

	dom := itemFor(t, plan, "web_domain", "example.com (vhost)")
	require.Equal(t, ActionConflict, dom.Action)
	require.Contains(t, dom.Reason, `owner client "client2"`)
}

func TestPlanConflictUnmappedZone(t *testing.T) {
	// DNS-only selection on an empty local panel: zone owner missing →
	// zone conflicts → its records conflict with an unmapped-zone reason.
	snap := testSnapshot()
	plan, err := buildPlan(snap, newLocalState(), Options{
		Selection:      Selection{DNS: true},
		TargetServerID: 1,
	})
	require.NoError(t, err)

	zone := itemFor(t, plan, "dns_soa", "example.com.")
	require.Equal(t, ActionConflict, zone.Action)
	rr := itemFor(t, plan, "dns_rr", "www A 192.0.2.1")
	require.Equal(t, ActionConflict, rr.Action)
	require.Contains(t, rr.Reason, "zone (legacy id 40) is not imported")
}

func TestPlanOrphanZonesToAdmin(t *testing.T) {
	plan, err := buildPlan(testSnapshot(), newLocalState(), Options{
		Selection:                Selection{DNS: true},
		TargetServerID:           1,
		AssignOrphanZonesToAdmin: true,
	})
	require.NoError(t, err)

	zone := itemFor(t, plan, "dns_soa", "example.com.")
	require.Equal(t, ActionCreate, zone.Action)
	soa := zone.rec.(*model.DNSSoa)
	require.Equal(t, uint32(1), soa.SysUserID)
	require.Equal(t, uint32(1), soa.SysGroupID)
	require.NotEmpty(t, plan.Warnings)

	rr := itemFor(t, plan, "dns_rr", "www A 192.0.2.1")
	require.Equal(t, ActionCreate, rr.Action)
}

func TestPlanConflictParentMissing(t *testing.T) {
	// Subdomain whose parent vhost is neither in the snapshot nor local.
	snap := testSnapshot()
	snap.Domains = snap.Domains[1:] // drop the parent vhost
	plan, err := buildPlan(snap, newLocalState(), Options{
		Selection:      Selection{Clients: true, Sites: true},
		TargetServerID: 1,
	})
	require.NoError(t, err)

	sub := itemFor(t, plan, "web_domain", "sub.example.com (vhostsubdomain)")
	require.Equal(t, ActionConflict, sub.Action)
	require.Contains(t, sub.Reason, "parent domain (legacy id 10)")
}

func TestPlanConflictSysUserTakenByOtherClient(t *testing.T) {
	st := newLocalState()
	// Username client2 already used by an unrelated local client (id 9).
	st.userByName["client2"] = &model.SysUser{UserID: 44, Username: "client2", ClientID: 9}

	plan, err := buildPlan(testSnapshot(), st, Options{
		Selection:      Selection{Clients: true},
		TargetServerID: 1,
	})
	require.NoError(t, err)

	user := itemFor(t, plan, "sys_user", "client2")
	require.Equal(t, ActionConflict, user.Action)
	require.Contains(t, user.Reason, "different local client")

	// The cluster is atomic: neither the client nor its group may be
	// created without the panel user.
	cli := itemFor(t, plan, "client", "client2")
	require.Equal(t, ActionConflict, cli.Action)
	require.Contains(t, cli.Reason, "cannot recreate panel user/group")
	group := itemFor(t, plan, "sys_group", "client2")
	require.Equal(t, ActionConflict, group.Action)

	// The unaffected reseller still imports normally.
	require.Equal(t, ActionCreate, itemFor(t, plan, "client", "reseller1").Action)
}

func TestPlanConflictExistingFolderPendingOwner(t *testing.T) {
	// Local panel has client2 with domain and folder; the legacy folder
	// claims an owner (reseller1's group) whose client is only being
	// created in this run — the existing folder must conflict, never be
	// re-owned to a zero/foreign owner.
	st := localWithClient2()
	st.domains["example.com|vhost"] = &model.WebDomain{DomainID: 55, Domain: "example.com",
		Type: "vhost", SysUserID: 30, SysGroupID: 31, ServerID: 1,
		ParentDomainID: 0, DocumentRoot: "/var/www/clients/client2/web10",
		SystemUser: "web10", SystemGroup: "client2", Active: "y",
		SysPermUser: "riud", SysPermGroup: "riud"}
	st.folders["55|protected"] = &model.WebFolder{WebFolderID: 77, ParentDomainID: 55,
		Path: "protected", SysUserID: 30, SysGroupID: 31, ServerID: 1, Active: "y",
		SysPermUser: "riud", SysPermGroup: "riud"}
	delete(st.clients, "reseller1") // reseller becomes a pending create
	delete(st.groupByName, "reseller1")
	delete(st.userByName, "reseller1")
	delete(st.groupOfCli, 5)
	delete(st.userOfCli, 5)

	snap := testSnapshot()
	snap.Folders[0]["sys_groupid"] = "3" // reseller1's legacy group

	plan, err := buildPlan(snap, st, Options{
		Selection:      Selection{Clients: true, Sites: true},
		TargetServerID: 1,
	})
	require.NoError(t, err)

	folder := itemFor(t, plan, "web_folder", "protected (domain 10)")
	require.Equal(t, ActionConflict, folder.Action)
	require.Contains(t, folder.Reason, "only being created now")
}

func TestPlanRiudCopiedVerbatim(t *testing.T) {
	snap := testSnapshot()
	snap.Domains[0]["sys_perm_group"] = "ri" // read-only for the group
	plan, err := buildPlan(snap, newLocalState(), Options{
		Selection:      Selection{Clients: true, Sites: true},
		TargetServerID: 1,
	})
	require.NoError(t, err)

	dom := itemFor(t, plan, "web_domain", "example.com (vhost)")
	require.Equal(t, "ri", dom.rec.(*model.WebDomain).SysPermGroup)
	require.Equal(t, "riud", dom.rec.(*model.WebDomain).SysPermUser)
}

func TestPlanModernAdminOwnedClientsResolveViaUserClient(t *testing.T) {
	// ISPConfig 3.3.x panels write admin-created clients' own records
	// with sys_userid=1/sys_groupid=1, so client rows alone cannot map
	// entity owner groups. The snapshot's client_get_id resolution
	// (UserClient) must close the mapping and the whole plan must be
	// conflict-free on an empty local panel.
	snap := testSnapshot()
	for _, rec := range snap.Clients {
		rec["sys_userid"], rec["sys_groupid"] = "1", "1"
	}
	snap.UserClient = map[int]int{2: 1, 3: 2}

	plan, err := buildPlan(snap, newLocalState(), Options{
		Selection:      Selection{Clients: true, Sites: true, DNS: true},
		TargetServerID: 1,
	})
	require.NoError(t, err)
	for _, it := range plan.Items {
		require.NotEqual(t, ActionConflict, it.Action, "%s %s: %s", it.Table, it.Key, it.Reason)
	}
}
