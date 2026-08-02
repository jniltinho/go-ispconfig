package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// getterCall is one recorded request against the fake getter server.
type getterCall struct {
	method string
	params map[string]any
}

// getterServer serves canned responses per method and records every call.
// The sites_web_domain_get handler implements #OFFSET#/#LIMIT# paging
// over the given number of generated vhost records.
func getterServer(t *testing.T, domainCount int, responses map[string]string) (*Client, *[]getterCall) {
	t.Helper()
	var calls []getterCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var params map[string]any
		require.NoError(t, json.Unmarshal(raw, &params))
		method := r.URL.RawQuery
		calls = append(calls, getterCall{method: method, params: params})

		w.Header().Set("Content-Type", "application/json")
		if method == "sites_web_domain_get" {
			pid, ok := params["primary_id"].(map[string]any)
			require.True(t, ok, "sites_web_domain_get must send a filter object")
			offset := int(pid["#OFFSET#"].(float64))
			limit := int(pid["#LIMIT#"].(float64))
			var page []map[string]string
			for i := offset; i < offset+limit && i < domainCount; i++ {
				page = append(page, map[string]string{
					"domain_id": fmt.Sprint(i + 1),
					"domain":    fmt.Sprintf("site%d.example.com", i+1),
					"type":      "vhost",
				})
			}
			body, err := json.Marshal(page)
			require.NoError(t, err)
			_, _ = fmt.Fprintf(w, `{"code":"ok","message":"","response":%s}`, body)
			return
		}
		body, ok := responses[method]
		require.True(t, ok, "unexpected method %s", method)
		_, _ = fmt.Fprintf(w, `{"code":"ok","message":"","response":%s}`, body)
	}))
	t.Cleanup(srv.Close)

	c, err := New(Options{URL: srv.URL})
	require.NoError(t, err)
	c.sessionID = testSession
	return c, &calls
}

// callsFor filters the recorded calls by method.
func callsFor(calls []getterCall, method string) []getterCall {
	var out []getterCall
	for _, call := range calls {
		if call.method == method {
			out = append(out, call)
		}
	}
	return out
}

func TestSitesWebDomainGetPagination(t *testing.T) {
	t.Run("1200 domains at page size 500 need three pages", func(t *testing.T) {
		c, calls := getterServer(t, 1200, nil)
		got, err := c.SitesWebDomainGet(context.Background(), Filter{"type": "vhost"})
		require.NoError(t, err)
		require.Len(t, got, 1200)
		require.Equal(t, "site1.example.com", got[0]["domain"])
		require.Equal(t, "site1200.example.com", got[1199]["domain"])

		paged := callsFor(*calls, "sites_web_domain_get")
		require.Len(t, paged, 3)
		for i, wantOffset := range []float64{0, 500, 1000} {
			pid := paged[i].params["primary_id"].(map[string]any)
			require.Equal(t, wantOffset, pid["#OFFSET#"])
			require.Equal(t, float64(500), pid["#LIMIT#"])
			require.Equal(t, "vhost", pid["type"], "filter must be preserved on every page")
			require.Equal(t, testSession, paged[i].params["session_id"])
		}
	})

	t.Run("short first page stops after one call", func(t *testing.T) {
		c, calls := getterServer(t, 12, nil)
		got, err := c.SitesWebDomainGet(context.Background(), Filter{"type": "vhost"})
		require.NoError(t, err)
		require.Len(t, got, 12)
		require.Len(t, callsFor(*calls, "sites_web_domain_get"), 1)
	})

	t.Run("configurable page size", func(t *testing.T) {
		c, calls := getterServer(t, 12, nil)
		c.pageSize = 5
		got, err := c.SitesWebDomainGet(context.Background(), nil)
		require.NoError(t, err)
		require.Len(t, got, 12)
		require.Len(t, callsFor(*calls, "sites_web_domain_get"), 3) // 5+5+2
	})

	t.Run("panel ignoring #OFFSET# aborts instead of looping forever", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Always a full page, regardless of offset.
			_, _ = w.Write([]byte(`{"code":"ok","message":"","response":[{"domain_id":"1"},{"domain_id":"2"}]}`))
		}))
		t.Cleanup(srv.Close)
		c, err := New(Options{URL: srv.URL, PageSize: 2})
		require.NoError(t, err)
		c.sessionID = testSession
		_, err = c.SitesWebDomainGet(context.Background(), nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "pagination did not terminate")
	})

	t.Run("exact multiple issues one trailing empty page", func(t *testing.T) {
		c, calls := getterServer(t, 10, nil)
		c.pageSize = 5
		got, err := c.SitesWebDomainGet(context.Background(), nil)
		require.NoError(t, err)
		require.Len(t, got, 10)
		require.Len(t, callsFor(*calls, "sites_web_domain_get"), 3) // 5+5+0
	})
}

func TestGetters(t *testing.T) {
	responses := map[string]string{
		"client_get_all": `["1","2","7"]`,
		"client_get": `{"client_id":"7","username":"client7","parent_client_id":"1",
			"sys_userid":"8","sys_groupid":"9","sys_perm_user":"riud","unknown_new_column":"whatever"}`,
		"dns_zone_get": `[{"id":"1","origin":"example.com.","serial":2024010101},
			{"id":"2","origin":"example.org.","serial":2024010102}]`,
		"dns_rr_get_all_by_zone":   `[{"id":"10","zone":"1","name":"www","type":"A","data":"192.0.2.1","ttl":null}]`,
		"dns_slave_get":            `[{"id":"1","origin":"slave.example.net."}]`,
		"dns_templatezone_get_all": `[{"template_id":"1","name":"Default"}]`,
		"server_get_all":           `[{"server_id":"1","server_name":"web1"},{"server_id":"2","server_name":"dns1"}]`,
		"server_get":               `{"ip_address":"192.0.2.10","hostname":"web1.example.com"}`,
		"get_function_list":        `["login","logout","client_get","dns_zone_get"]`,
		"sites_web_folder_get":     `[{"web_folder_id":"1","parent_domain_id":"3","path":"protected"}]`,
		"sites_web_folder_user_get": `[{"web_folder_user_id":"1","web_folder_id":"1","username":"folderuser",
			"password":"$6$abcdefgh$0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRS"}]`,
	}

	c, calls := getterServer(t, 0, responses)
	ctx := context.Background()

	t.Run("ClientGetAll converts numeric strings to ints", func(t *testing.T) {
		ids, err := c.ClientGetAll(ctx)
		require.NoError(t, err)
		require.Equal(t, []int{1, 2, 7}, ids)
	})

	t.Run("ClientGet decodes a record and keeps unknown fields", func(t *testing.T) {
		rec, err := c.ClientGet(ctx, 7)
		require.NoError(t, err)
		require.Equal(t, "client7", rec["username"])
		require.Equal(t, 1, rec.Int("parent_client_id"))
		require.Equal(t, "riud", rec["sys_perm_user"])
		require.Equal(t, "whatever", rec["unknown_new_column"])
		require.Equal(t, float64(7), callsFor(*calls, "client_get")[0].params["client_id"])
	})

	t.Run("DNSZoneGetAll sends primary_id -1 and tolerates numeric fields", func(t *testing.T) {
		zones, err := c.DNSZoneGetAll(ctx)
		require.NoError(t, err)
		require.Len(t, zones, 2)
		require.Equal(t, "example.com.", zones[0]["origin"])
		require.Equal(t, "2024010101", zones[0]["serial"], "JSON numbers normalize to strings")
		require.Equal(t, float64(-1), callsFor(*calls, "dns_zone_get")[0].params["primary_id"])
	})

	t.Run("DNSRRGetAllByZone sends zone_id and normalizes null", func(t *testing.T) {
		rrs, err := c.DNSRRGetAllByZone(ctx, 1)
		require.NoError(t, err)
		require.Len(t, rrs, 1)
		require.Equal(t, "A", rrs[0]["type"])
		require.Equal(t, "", rrs[0]["ttl"], "null normalizes to empty string")
		require.Equal(t, float64(1), callsFor(*calls, "dns_rr_get_all_by_zone")[0].params["zone_id"])
	})

	t.Run("DNSSlaveGetAll sends primary_id -1", func(t *testing.T) {
		slaves, err := c.DNSSlaveGetAll(ctx)
		require.NoError(t, err)
		require.Len(t, slaves, 1)
		require.Equal(t, float64(-1), callsFor(*calls, "dns_slave_get")[0].params["primary_id"])
	})

	t.Run("DNSTemplateZoneGetAll", func(t *testing.T) {
		templates, err := c.DNSTemplateZoneGetAll(ctx)
		require.NoError(t, err)
		require.Len(t, templates, 1)
		require.Equal(t, "Default", templates[0]["name"])
	})

	t.Run("ServerGetAll", func(t *testing.T) {
		servers, err := c.ServerGetAll(ctx)
		require.NoError(t, err)
		require.Len(t, servers, 2)
		require.Equal(t, 1, servers[0].Int("server_id"))
		require.Equal(t, "dns1", servers[1]["server_name"])
	})

	t.Run("ServerGet sends server_id and section", func(t *testing.T) {
		conf, err := c.ServerGet(ctx, 1, "server")
		require.NoError(t, err)
		require.Equal(t, "web1.example.com", conf["hostname"])
		call := callsFor(*calls, "server_get")[0]
		require.Equal(t, float64(1), call.params["server_id"])
		require.Equal(t, "server", call.params["section"])
	})

	t.Run("GetFunctionList", func(t *testing.T) {
		functions, err := c.GetFunctionList(ctx)
		require.NoError(t, err)
		require.Contains(t, functions, "dns_zone_get")
	})

	t.Run("folder getters page and keep crypt hashes verbatim", func(t *testing.T) {
		folders, err := c.SitesWebFolderGet(ctx, nil)
		require.NoError(t, err)
		require.Len(t, folders, 1)

		users, err := c.SitesWebFolderUserGet(ctx, nil)
		require.NoError(t, err)
		require.Len(t, users, 1)
		require.Equal(t,
			"$6$abcdefgh$0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRS",
			users[0]["password"])

		pid := callsFor(*calls, "sites_web_folder_get")[0].params["primary_id"].(map[string]any)
		require.Equal(t, float64(0), pid["#OFFSET#"])
		require.Equal(t, float64(DefaultPageSize), pid["#LIMIT#"])
	})
}

func TestRecordNormalization(t *testing.T) {
	var rec Record
	require.NoError(t, json.Unmarshal([]byte(
		`{"s":"text","n":42,"neg":-1,"f":1.5,"b":true,"nul":null,"nested":{"a":1},"arr":[1,2]}`), &rec))
	require.Equal(t, "text", rec["s"])
	require.Equal(t, "42", rec["n"])
	require.Equal(t, "-1", rec["neg"])
	require.Equal(t, "1.5", rec["f"])
	require.Equal(t, "true", rec["b"])
	require.Equal(t, "", rec["nul"])
	require.JSONEq(t, `{"a":1}`, rec["nested"])
	require.JSONEq(t, `[1,2]`, rec["arr"])

	require.Equal(t, 42, rec.Int("n"))
	require.Equal(t, -1, rec.Int("neg"))
	require.Equal(t, 0, rec.Int("s"))
	require.Equal(t, 0, rec.Int("missing"))
}

// TestPagedFilterKeyOrder pins the JSON key order of paged filters:
// filter keys first, #OFFSET#/#LIMIT# last. The legacy remoting_lib is
// order-sensitive (see pagedFilter) — a regression here silently empties
// every filtered fetch against real panels.
func TestPagedFilterKeyOrder(t *testing.T) {
	b, err := json.Marshal(pagedFilter{filter: Filter{"type": "vhost", "active": "y"}, offset: 40, limit: 20})
	require.NoError(t, err)
	require.Equal(t, `{"active":"y","type":"vhost","#OFFSET#":40,"#LIMIT#":20}`, string(b))
}

// TestClientGetID covers the mapping, the tolerated not-found fault and
// the propagation of every other fault (e.g. permission_denied).
func TestClientGetID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery == "login" {
			fmt.Fprint(w, `{"code":"ok","message":"","response":"sess1"}`)
			return
		}
		var body struct {
			SysUserID int `json:"sys_userid"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		switch body.SysUserID {
		case 3:
			fmt.Fprint(w, `{"code":"ok","message":"","response":2}`)
		case 99:
			fmt.Fprint(w, `{"code":"remote_fault","message":"There is no sys_user account with this userid.","response":false}`)
		default:
			fmt.Fprint(w, `{"code":"remote_fault","message":"You do not have the permissions to access this function.","response":false}`)
		}
	}))
	defer srv.Close()
	c, err := New(Options{URL: srv.URL, Username: "u", Password: "p"})
	require.NoError(t, err)
	require.NoError(t, c.Login(context.Background()))

	id, err := c.ClientGetID(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, 2, id)

	id, err = c.ClientGetID(context.Background(), 99)
	require.NoError(t, err)
	require.Zero(t, id)

	_, err = c.ClientGetID(context.Background(), 7)
	require.Error(t, err)
	var f *Fault
	require.ErrorAs(t, err, &f)
}
