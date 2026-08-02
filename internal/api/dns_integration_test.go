//go:build integration

package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/api"
	"go-ispconfig/internal/model"
)

// todaySerial returns <today>NN.
func todaySerial(nn int) float64 {
	now := time.Now()
	return float64((now.Year()*10000+int(now.Month())*100+now.Day())*100 + nn)
}

// TestDNSZoneAPI covers /api/dns/zones and the extra zone routes (tasks
// 4.2/4.4/5.1): CRUD with serial management, IDN normalization, update_acl
// admin gating, cross-client denial, id-by-origin, status and DNSSEC
// toggles.
func TestDNSZoneAPI(t *testing.T) {
	env := newSitesTestEnv(t, "dnszones")
	db, srv := env.db, env.srv

	var zoneID float64

	t.Run("client create sets initial serial and journals datalog", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/dns/zones", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "origin": "Bücher.example.", "ns": "ns1.example.net",
				"mbox": "hostmaster.example.com.", "update_acl": "10.0.0.9"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		zoneID = rec["id"].(float64)

		require.Equal(t, "xn--bcher-kva.example.", rec["origin"], "IDN + lowercase applied")
		require.Equal(t, todaySerial(1), rec["serial"], "initial serial <today>01")
		require.Equal(t, "", rec["update_acl"], "update_acl ignored for non-admins")
		require.EqualValues(t, 7200, rec["refresh"], "defaults applied")
		require.Equal(t, "Y", rec["active"])

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'dns_soa' AND action = 'i'").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("id:%d", int(zoneID)), dl.DBIdx)
	})

	t.Run("validation failures are 422 without rows", func(t *testing.T) {
		var before int64
		require.NoError(t, db.Model(&model.SysDatalog{}).Count(&before).Error)

		// Duplicate origin.
		status, data := call(t, srv, http.MethodPost, "/api/dns/zones", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "origin": "xn--bcher-kva.example.",
				"ns": "ns1.example.net", "mbox": "hostmaster.example.com."})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["origin"], "origin_error_unique")

		// Bad xfer IP list.
		status, data = call(t, srv, http.MethodPost, "/api/dns/zones", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "origin": "valid.example.", "ns": "ns1.example.net",
				"mbox": "hostmaster.example.com.", "xfer": "10.0.0.1,nope"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["xfer"], "xfer_error_regex")

		// TTL below 60.
		status, data = call(t, srv, http.MethodPost, "/api/dns/zones", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "origin": "valid.example.", "ns": "ns1.example.net",
				"mbox": "hostmaster.example.com.", "ttl": 30})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["ttl"], "ttl_range_error")

		var after int64
		require.NoError(t, db.Model(&model.SysDatalog{}).Count(&after).Error)
		require.Equal(t, before, after, "no datalog rows for rejected writes")
	})

	t.Run("update bumps the serial once per change", func(t *testing.T) {
		path := fmt.Sprintf("/api/dns/zones/%d", int(zoneID))
		status, data := call(t, srv, http.MethodPut, path, env.aCookie, env.aCSRF,
			map[string]any{"refresh": 7300})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, todaySerial(2), rec["serial"], "serial bumped on change")

		// A no-op update keeps the serial and writes no datalog row.
		var before int64
		require.NoError(t, db.Model(&model.SysDatalog{}).Count(&before).Error)
		status, data = call(t, srv, http.MethodPut, path, env.aCookie, env.aCSRF,
			map[string]any{"refresh": 7300})
		require.Equal(t, http.StatusOK, status)
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, todaySerial(2), rec["serial"], "no bump without changes")
		var after int64
		require.NoError(t, db.Model(&model.SysDatalog{}).Count(&after).Error)
		require.Equal(t, before, after)
	})

	t.Run("id by origin with and without trailing dot", func(t *testing.T) {
		for _, origin := range []string{"xn--bcher-kva.example", "xn--bcher-kva.example."} {
			status, data := call(t, srv, http.MethodGet, "/api/dns/zones/origin/"+origin,
				env.aCookie, "", nil)
			require.Equal(t, http.StatusOK, status, "%s", data)
			var resp api.DNSZoneIDResponse
			require.NoError(t, json.Unmarshal(data, &resp))
			require.EqualValues(t, zoneID, resp.ID)
		}
		// Not accessible for another client.
		status, _ := call(t, srv, http.MethodGet, "/api/dns/zones/origin/xn--bcher-kva.example",
			env.bCookie, "", nil)
		require.Equal(t, http.StatusNotFound, status)
	})

	t.Run("cross-client zone access denied", func(t *testing.T) {
		path := fmt.Sprintf("/api/dns/zones/%d", int(zoneID))
		status, _ := call(t, srv, http.MethodGet, path, env.bCookie, "", nil)
		require.Equal(t, http.StatusForbidden, status)
		status, _ = call(t, srv, http.MethodPut, path, env.bCookie, env.bCSRF,
			map[string]any{"refresh": 9999})
		require.Equal(t, http.StatusForbidden, status)
	})

	t.Run("status toggle journals and bumps", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost,
			fmt.Sprintf("/api/dns/zones/%d/status", int(zoneID)), env.aCookie, env.aCSRF,
			map[string]any{"status": "inactive"})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "N", rec["active"])
		require.Equal(t, todaySerial(3), rec["serial"])

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'dns_soa' AND action = 'u'").
			Order("datalog_id DESC").First(&dl).Error)
		require.Contains(t, dl.Data, `"active"`)
	})

	t.Run("dnssec toggle validates the algorithm", func(t *testing.T) {
		path := fmt.Sprintf("/api/dns/zones/%d/dnssec", int(zoneID))
		status, data := call(t, srv, http.MethodPost, path, env.aCookie, env.aCSRF,
			map[string]any{"dnssec_wanted": "Y", "dnssec_algo": "RSAMD5"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)

		status, data = call(t, srv, http.MethodPost, path, env.aCookie, env.aCSRF,
			map[string]any{"dnssec_wanted": "Y", "dnssec_algo": "ECDSAP256SHA256"})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "Y", rec["dnssec_wanted"])
	})

	t.Run("admin sets update_acl, metadata hides it from clients", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPut,
			fmt.Sprintf("/api/dns/zones/%d", int(zoneID)), env.adminCookie, env.adminCSRF,
			map[string]any{"update_acl": "10.0.0.9"})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "10.0.0.9", rec["update_acl"])

		fieldNames := func(cookie string) []string {
			status, data := call(t, srv, http.MethodGet, "/api/meta/forms/zones", cookie, "", nil)
			require.Equal(t, http.StatusOK, status)
			var meta api.FormMeta
			require.NoError(t, json.Unmarshal(data, &meta))
			var names []string
			for _, tab := range meta.Tabs {
				for _, f := range tab.Fields {
					names = append(names, f.Name)
				}
			}
			return names
		}
		require.Contains(t, fieldNames(env.adminCookie), "update_acl")
		require.NotContains(t, fieldNames(env.aCookie), "update_acl")
	})
}

// TestDNSRecordAPI covers /api/dns/zones/{id}/records and /api/dns/records
// (tasks 4.3/4.4/5.2): typed validation, serial bump per mutation,
// update_serial flag, ordering and cross-client denial.
func TestDNSRecordAPI(t *testing.T) {
	env := newSitesTestEnv(t, "dnsrr")
	db, srv := env.db, env.srv

	// clienta's zone.
	status, data := call(t, srv, http.MethodPost, "/api/dns/zones", env.aCookie, env.aCSRF,
		map[string]any{"server_id": 1, "origin": "records.example.", "ns": "ns1.example.net",
			"mbox": "hostmaster.example.com."})
	require.Equal(t, http.StatusCreated, status, "%s", data)
	var zone map[string]any
	require.NoError(t, json.Unmarshal(data, &zone))
	zoneID := int(zone["id"].(float64))
	recordsPath := fmt.Sprintf("/api/dns/zones/%d/records", zoneID)

	var mxID float64

	t.Run("A record inherits server and bumps serial", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, recordsPath, env.aCookie, env.aCSRF,
			map[string]any{"type": "A", "name": "www", "data": "10.0.0.1"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.EqualValues(t, 1, rec["server_id"], "server inherited from zone")
		require.EqualValues(t, 3600, rec["ttl"], "ttl default applied")
		require.Equal(t, "Y", rec["active"])

		var soa model.DNSSoa
		require.NoError(t, db.Where("id = ?", zoneID).First(&soa).Error)
		require.EqualValues(t, todaySerial(2), soa.Serial, "SOA serial bumped")

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'dns_rr' AND action = 'i'").
			Order("datalog_id DESC").First(&dl).Error)
		require.Contains(t, dl.Data, "10.0.0.1")
	})

	t.Run("invalid records are 422 without datalog", func(t *testing.T) {
		var before int64
		require.NoError(t, db.Model(&model.SysDatalog{}).Count(&before).Error)

		status, data := call(t, srv, http.MethodPost, recordsPath, env.aCookie, env.aCSRF,
			map[string]any{"type": "A", "name": "bad", "data": "not-an-ip"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["data"], "ip_error_wrong")

		status, data = call(t, srv, http.MethodPost, recordsPath, env.aCookie, env.aCSRF,
			map[string]any{"type": "TXT", "name": "", "data": "v=spf1 mx a ~all"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["data"], "data_error_use_dedicated_form")

		status, data = call(t, srv, http.MethodPost, recordsPath, env.aCookie, env.aCSRF,
			map[string]any{"type": "BOGUS", "name": "x", "data": "y"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["type"], "type_error_unknown")

		var after int64
		require.NoError(t, db.Model(&model.SysDatalog{}).Count(&after).Error)
		require.Equal(t, before, after)
	})

	t.Run("MX stores priority in aux", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, recordsPath, env.aCookie, env.aCSRF,
			map[string]any{"type": "MX", "name": "", "data": "mail.records.example.", "aux": 10})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		mxID = rec["id"].(float64)
		require.EqualValues(t, 10, rec["aux"])
	})

	t.Run("SPF helper stores as TXT and skips serial with flag", func(t *testing.T) {
		var soaBefore model.DNSSoa
		require.NoError(t, db.Where("id = ?", zoneID).First(&soaBefore).Error)

		status, data := call(t, srv, http.MethodPost, recordsPath, env.aCookie, env.aCSRF,
			map[string]any{"type": "SPF", "name": "", "data": "v=spf1 mx a ~all",
				"update_serial": false})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "TXT", rec["type"])

		var soaAfter model.DNSSoa
		require.NoError(t, db.Where("id = ?", zoneID).First(&soaAfter).Error)
		require.Equal(t, soaBefore.Serial, soaAfter.Serial, "update_serial=false skips bump")
	})

	t.Run("list is ordered by type then name", func(t *testing.T) {
		status, data := call(t, srv, http.MethodGet, recordsPath, env.aCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var items []map[string]any
		require.NoError(t, json.Unmarshal(data, &items))
		require.Len(t, items, 3)
		require.Equal(t, "A", items[0]["type"])
		require.Equal(t, "MX", items[1]["type"])
		require.Equal(t, "TXT", items[2]["type"])
	})

	t.Run("record update revalidates and bumps", func(t *testing.T) {
		var soaBefore model.DNSSoa
		require.NoError(t, db.Where("id = ?", zoneID).First(&soaBefore).Error)

		path := fmt.Sprintf("/api/dns/records/%d", int(mxID))
		status, data := call(t, srv, http.MethodPut, path, env.aCookie, env.aCSRF,
			map[string]any{"data": "mail space"})
		require.Equal(t, http.StatusUnprocessableEntity, status, "%s", data)

		status, data = call(t, srv, http.MethodPut, path, env.aCookie, env.aCSRF,
			map[string]any{"data": "mail2.records.example.", "aux": 20})
		require.Equal(t, http.StatusOK, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.EqualValues(t, 20, rec["aux"])

		var soaAfter model.DNSSoa
		require.NoError(t, db.Where("id = ?", zoneID).First(&soaAfter).Error)
		require.Greater(t, soaAfter.Serial, soaBefore.Serial)
	})

	t.Run("foreign zone and record are 403", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodPost, recordsPath, env.bCookie, env.bCSRF,
			map[string]any{"type": "A", "name": "evil", "data": "10.0.0.66"})
		require.Equal(t, http.StatusForbidden, status)

		path := fmt.Sprintf("/api/dns/records/%d", int(mxID))
		status, _ = call(t, srv, http.MethodPut, path, env.bCookie, env.bCSRF,
			map[string]any{"data": "evil.example."})
		require.Equal(t, http.StatusForbidden, status)
		status, _ = call(t, srv, http.MethodDelete, path, env.bCookie, env.bCSRF, nil)
		require.Equal(t, http.StatusForbidden, status)

		status, _ = call(t, srv, http.MethodGet, recordsPath, env.bCookie, "", nil)
		require.Equal(t, http.StatusForbidden, status, "record list of a foreign zone")
	})

	t.Run("delete journals and bumps", func(t *testing.T) {
		var soaBefore model.DNSSoa
		require.NoError(t, db.Where("id = ?", zoneID).First(&soaBefore).Error)

		status, _ := call(t, srv, http.MethodDelete,
			fmt.Sprintf("/api/dns/records/%d", int(mxID)), env.aCookie, env.aCSRF, nil)
		require.Equal(t, http.StatusNoContent, status)

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'dns_rr' AND action = 'd'").
			Order("datalog_id DESC").First(&dl).Error)
		require.Equal(t, fmt.Sprintf("id:%d", int(mxID)), dl.DBIdx)

		var soaAfter model.DNSSoa
		require.NoError(t, db.Where("id = ?", zoneID).First(&soaAfter).Error)
		require.Greater(t, soaAfter.Serial, soaBefore.Serial)
	})
}

// TestDNSWizardAndSlaveAPI covers the wizard, template listing/CRUD and
// slave zones (tasks 4.5/5.3).
func TestDNSWizardAndSlaveAPI(t *testing.T) {
	env := newSitesTestEnv(t, "dnswiz")
	db, srv := env.db, env.srv

	t.Run("default template creates a full zone atomically", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/dns/zones/wizard", env.aCookie, env.aCSRF,
			map[string]any{"template_id": 1, "domain": "wizard.example", "ip": "10.0.0.5",
				"ns1": "ns1.example.net", "ns2": "ns2.example.net", "email": "admin@example.com"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		zoneID := int(rec["id"].(float64))
		require.Equal(t, "wizard.example.", rec["origin"])
		require.Equal(t, "admin.example.com.", rec["mbox"], "@ replaced with dot")
		require.Equal(t, todaySerial(1), rec["serial"])
		require.Equal(t, "Y", rec["active"])

		var n int64
		require.NoError(t, db.Model(&model.DNSRr{}).Where("zone = ?", zoneID).Count(&n).Error)
		require.EqualValues(t, 7, n, "default template creates 7 records")
		require.NoError(t, db.Model(&model.SysDatalog{}).
			Where("dbtable = 'dns_rr' AND dbidx LIKE 'id:%'").Count(&n).Error)
		require.GreaterOrEqual(t, n, int64(7), "datalog row per record")

		// The zone list shows it for the owner.
		status, data = call(t, srv, http.MethodGet, "/api/dns/zones?origin=wizard", env.aCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var list api.ListResponse
		require.NoError(t, json.Unmarshal(data, &list))
		require.EqualValues(t, 1, list.Total)
		require.Equal(t, "pending", list.Items[0]["_datalog_state"], "datalog badge present")
	})

	t.Run("missing wizard field creates nothing", func(t *testing.T) {
		var zonesBefore, dlBefore int64
		require.NoError(t, db.Model(&model.DNSSoa{}).Count(&zonesBefore).Error)
		require.NoError(t, db.Model(&model.SysDatalog{}).Count(&dlBefore).Error)

		status, data := call(t, srv, http.MethodPost, "/api/dns/zones/wizard", env.aCookie, env.aCSRF,
			map[string]any{"template_id": 1, "domain": "half.example", "ip": "10.0.0.5",
				"ns1": "ns1.example.net", "ns2": "ns2.example.net"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["email"], "error_email_empty")

		var zonesAfter, dlAfter int64
		require.NoError(t, db.Model(&model.DNSSoa{}).Count(&zonesAfter).Error)
		require.NoError(t, db.Model(&model.SysDatalog{}).Count(&dlAfter).Error)
		require.Equal(t, zonesBefore, zonesAfter)
		require.Equal(t, dlBefore, dlAfter)
	})

	t.Run("hidden templates are not listed", func(t *testing.T) {
		require.NoError(t, db.Create(&model.DNSTemplate{
			SysUserID: 1, SysGroupID: 1, SysPermUser: "riud", SysPermGroup: "riud",
			Name: "Hidden", Fields: "DOMAIN", Template: "[ZONE]\norigin={DOMAIN}.", Visible: "n",
		}).Error)

		status, data := call(t, srv, http.MethodGet, "/api/dns/templates", env.aCookie, "", nil)
		require.Equal(t, http.StatusOK, status)
		var tpls []api.DNSTemplateInfo
		require.NoError(t, json.Unmarshal(data, &tpls))
		for _, tpl := range tpls {
			require.NotEqual(t, "Hidden", tpl.Name)
		}
		require.NotEmpty(t, tpls, "the seeded Default template is visible")
	})

	t.Run("template CRUD is admin only", func(t *testing.T) {
		status, _ := call(t, srv, http.MethodGet, "/api/dns/zone-templates", env.aCookie, "", nil)
		require.Equal(t, http.StatusForbidden, status)
		status, _ = call(t, srv, http.MethodPost, "/api/dns/zone-templates", env.aCookie, env.aCSRF,
			map[string]any{"name": "Client template"})
		require.Equal(t, http.StatusForbidden, status)

		status, data := call(t, srv, http.MethodPost, "/api/dns/zone-templates", env.adminCookie, env.adminCSRF,
			map[string]any{"name": "Admin template", "fields": "DOMAIN,IP",
				"template": "[ZONE]\norigin={DOMAIN}.", "visible": "y"})
		require.Equal(t, http.StatusCreated, status, "%s", data)

		status, data = call(t, srv, http.MethodPost, "/api/dns/zone-templates", env.adminCookie, env.adminCSRF,
			map[string]any{"name": "Bad fields", "fields": "DOMAIN,BOGUS"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["fields"], "fields_error")
	})

	t.Run("secondary zone reaches the datalog", func(t *testing.T) {
		status, data := call(t, srv, http.MethodPost, "/api/dns/slave-zones", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "origin": "slave.example.", "ns": "10.0.0.53"})
		require.Equal(t, http.StatusCreated, status, "%s", data)
		var rec map[string]any
		require.NoError(t, json.Unmarshal(data, &rec))
		require.Equal(t, "Y", rec["active"])

		var dl model.SysDatalog
		require.NoError(t, db.Where("dbtable = 'dns_slave' AND action = 'i'").
			Order("datalog_id DESC").First(&dl).Error)
		require.EqualValues(t, 1, dl.ServerID)

		// Master ns must be an IP list.
		status, data = call(t, srv, http.MethodPost, "/api/dns/slave-zones", env.aCookie, env.aCSRF,
			map[string]any{"server_id": 1, "origin": "slave2.example.", "ns": "master.example.com"})
		require.Equal(t, http.StatusUnprocessableEntity, status)
		require.Contains(t, errKeyOf(t, data).Fields["ns"], "ns_error_regex")
	})
}
