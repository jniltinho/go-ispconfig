// Package legacytest provides an httptest-based mock of a legacy PHP
// ISPConfig3 panel's JSON remote API (/remote/json.php) with canned
// fixtures, for reuse by the legacy client, import engine, CLI and
// wizard tests.
//
// The mock mirrors the reference implementation's semantics: POST
// <url>/remote/json.php?<method> with a JSON body of named params,
// responses in the {"code","message","response"} envelope, faults
// surfaced as code "remote_fault" with the legacy message text, login
// attempt limiting, and remoting_lib filter-object reads including
// #OFFSET#/#LIMIT# pagination. One deliberate simplification: permission
// checks compare the called method name against Functions, while the
// real panel maps a few methods onto sibling grant names (for example
// dns_rr_get_all_by_zone is gated by the dns_zone_get grant).
package legacytest

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Rec is one canned fixture record; values are strings, exactly like the
// legacy panel serializes database rows.
type Rec = map[string]string

// Call is one recorded request against the mock: the invoked method and
// its decoded body params.
type Call struct {
	// Method is the legacy method name from the query string.
	Method string
	// Params is the decoded JSON body.
	Params map[string]any
}

// Server is a mock legacy ISPConfig3 panel. All fixture fields may be
// replaced or extended before (or between) client calls; the handler
// itself is safe for concurrent use.
type Server struct {
	*httptest.Server

	mu sync.Mutex

	// Username and Password are the accepted remote_user credentials.
	Username string
	// Password is checked verbatim on login.
	Password string
	// Functions is the list served by get_function_list and used for
	// permission checks (see the package comment for the simplification).
	Functions []string
	// LoginAttempts counts failed logins; at 10 or more the panel answers
	// with the login-failure-limit fault, mirroring attempts_login.
	LoginAttempts int
	// Sessions holds the currently valid remote_session ids.
	Sessions map[string]bool
	// Calls records every request in order, for assertions on call counts
	// and pagination parameters.
	Calls []Call

	// Clients maps client_id to the client record (client_get); the id
	// list is served by client_get_all.
	Clients map[int]Rec
	// Domains are the web_domain records (sites_web_domain_get).
	Domains []Rec
	// Folders are the web_folder records (sites_web_folder_get).
	Folders []Rec
	// FolderUsers are the web_folder_user records
	// (sites_web_folder_user_get), passwords as $6$ crypt hashes.
	FolderUsers []Rec
	// Zones are the dns_soa records (dns_zone_get).
	Zones []Rec
	// RRs maps zone id to its dns_rr records (dns_rr_get_all_by_zone).
	RRs map[int][]Rec
	// Slaves are the dns_slave records (dns_slave_get).
	Slaves []Rec
	// Templates are the dns_template records (dns_templatezone_get_all).
	Templates []Rec
	// Servers are the server rows served by server_get_all.
	Servers []Rec
	// ServerConfig maps section name to the config served by server_get.
	ServerConfig map[string]Rec
}

// DefaultFunctions is the grant list of the default fixture remote_user:
// every read function the import engine needs.
var DefaultFunctions = []string{
	"get_function_list",
	"client_get",
	"client_get_all",
	"sites_web_domain_get",
	"sites_web_folder_get",
	"sites_web_folder_user_get",
	"dns_zone_get",
	"dns_rr_get_all_by_zone",
	"dns_slave_get",
	"dns_templatezone_get_all",
	"server_get",
	"server_get_all",
}

// Hash6 is a crypt SHA-512 style hash used in fixtures with password
// fields; migrations must carry it verbatim.
const Hash6 = "$6$abcdefgh$0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRS"

// New starts a plain-HTTP mock panel with the default canned fixtures:
// remote/secret credentials, all read grants, 3 clients (one reseller
// hierarchy), 1200 vhost web domains (3 pages at the default page size
// of 500) plus one vhostsubdomain, web folders and folder users with $6$
// hashes, 2 DNS zones with records, a slave zone, a zone template and a
// single legacy server.
func New() *Server {
	s := defaultFixtures()
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// NewTLS is New on an httptest TLS server with a self-signed (untrusted)
// certificate, for TLS-verification tests.
func NewTLS() *Server {
	s := defaultFixtures()
	s.Server = httptest.NewTLSServer(http.HandlerFunc(s.handle))
	return s
}

// defaultFixtures builds the canned data set.
func defaultFixtures() *Server {
	s := &Server{
		Username:  "remote",
		Password:  "secret",
		Functions: append([]string{}, DefaultFunctions...),
		Sessions:  map[string]bool{},
		Clients: map[int]Rec{
			1: clientRec(Rec{
				"client_id": "1", "username": "reseller1", "contact_name": "Reseller One",
				"company_name": "Reseller One Ltd", "email": "reseller1@example.com",
				"parent_client_id": "0", "limit_client": "10",
				"sys_userid": "2", "sys_groupid": "3",
			}),
			2: clientRec(Rec{
				"client_id": "2", "username": "client2", "contact_name": "Client Two",
				"email": "client2@example.com", "parent_client_id": "1",
				"sys_userid": "3", "sys_groupid": "4",
			}),
			3: clientRec(Rec{
				"client_id": "3", "username": "client3", "contact_name": "Client Three",
				"email": "client3@example.com", "parent_client_id": "0",
				"sys_userid": "4", "sys_groupid": "5",
			}),
		},
		Folders: []Rec{
			{"web_folder_id": "1", "server_id": "1", "parent_domain_id": "1", "path": "protected", "active": "y",
				"sys_userid": "3", "sys_groupid": "4", "sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
		},
		FolderUsers: []Rec{
			{"web_folder_user_id": "1", "server_id": "1", "web_folder_id": "1", "username": "folderuser1",
				"password": Hash6, "active": "y",
				"sys_userid": "3", "sys_groupid": "4", "sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
		},
		Zones: []Rec{
			{"id": "1", "server_id": "1", "origin": "example.com.", "ns": "ns1.example.com.",
				"mbox": "hostmaster.example.com.", "serial": "2024010101", "refresh": "7200", "retry": "540",
				"expire": "604800", "minimum": "3600", "ttl": "3600", "active": "Y",
				"dnssec_wanted": "N", "dnssec_algo": "ECDSAP256SHA256",
				"sys_userid": "3", "sys_groupid": "4", "sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
			{"id": "2", "server_id": "1", "origin": "example.org.", "ns": "ns1.example.com.",
				"mbox": "hostmaster.example.org.", "serial": "2024010102", "refresh": "7200", "retry": "540",
				"expire": "604800", "minimum": "3600", "ttl": "3600", "active": "Y",
				"dnssec_wanted": "N", "dnssec_algo": "ECDSAP256SHA256",
				"sys_userid": "4", "sys_groupid": "5", "sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
		},
		RRs: map[int][]Rec{
			1: {
				{"id": "1", "server_id": "1", "zone": "1", "name": "www", "type": "A", "data": "192.0.2.10", "aux": "0", "ttl": "3600", "active": "Y",
					"sys_userid": "3", "sys_groupid": "4", "sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
				{"id": "2", "server_id": "1", "zone": "1", "name": "", "type": "MX", "data": "mail.example.com.", "aux": "10", "ttl": "3600", "active": "Y",
					"sys_userid": "3", "sys_groupid": "4", "sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
				{"id": "3", "server_id": "1", "zone": "1", "name": "", "type": "TXT", "data": "v=spf1 mx -all", "aux": "0", "ttl": "3600", "active": "Y",
					"sys_userid": "3", "sys_groupid": "4", "sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
			},
			2: {
				{"id": "4", "server_id": "1", "zone": "2", "name": "www", "type": "A", "data": "192.0.2.20", "aux": "0", "ttl": "3600", "active": "Y",
					"sys_userid": "4", "sys_groupid": "5", "sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
			},
		},
		Slaves: []Rec{
			{"id": "1", "server_id": "1", "origin": "slave.example.net.", "ns": "192.0.2.53", "active": "Y",
				"sys_userid": "1", "sys_groupid": "1", "sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": ""},
		},
		// Named distinctly from the "Default" template the local schema
		// dump seeds, so a fresh import plans it as create.
		Templates: []Rec{
			{"template_id": "1", "name": "Legacy Custom", "visible": "Y",
				"fields": "DOMAIN,IP,NS1,NS2,EMAIL", "template": "[ZONE]\norigin={DOMAIN}."},
		},
		Servers: []Rec{
			{"server_id": "1", "server_name": "legacy1"},
		},
		ServerConfig: map[string]Rec{
			"server": {"hostname": "legacy1.example.com", "ip_address": "192.0.2.2"},
		},
	}

	owners := []struct{ userid, groupid string }{{"2", "3"}, {"3", "4"}, {"4", "5"}}
	for i := 1; i <= 1200; i++ {
		owner := owners[i%len(owners)]
		domain := fmt.Sprintf("site%d.example.com", i)
		rec := domainRec(Rec{
			"domain_id": strconv.Itoa(i), "domain": domain, "type": "vhost",
			"document_root": "/var/www/clients/client" + owner.groupid + "/web" + strconv.Itoa(i),
			"system_user":   "web" + strconv.Itoa(i), "system_group": "client" + owner.groupid,
			"sys_userid": owner.userid, "sys_groupid": owner.groupid,
		})
		if i == 1 {
			rec["ssl"] = "y"
			rec["ssl_letsencrypt"] = "y"
		}
		s.Domains = append(s.Domains, rec)
	}
	s.Domains = append(s.Domains, domainRec(Rec{
		"domain_id": "1201", "parent_domain_id": "1",
		"domain": "sub.site1.example.com", "type": "vhostsubdomain",
		"document_root": "/var/www/clients/client3/web1",
		"system_user":   "web1", "system_group": "client3",
		"sys_userid": "2", "sys_groupid": "3",
	}))
	return s
}

// clientRec builds a full client record like the real json.php does
// (SELECT * serializes every column): enum columns carry their schema
// defaults, overridden by the given fields.
func clientRec(overrides Rec) Rec {
	rec := Rec{
		"company_name": "", "gender": "", "language": "en", "country": "US",
		"limit_cgi": "n", "limit_ssi": "n", "limit_perl": "n", "limit_ruby": "n",
		"limit_python": "n", "force_suexec": "y", "limit_hterror": "n",
		"limit_wildcard": "n", "limit_ssl": "y", "limit_ssl_letsencrypt": "y",
		"limit_cron_type": "url", "locked": "n", "canceled": "n",
		"can_use_api": "n", "validation_status": "accept",
		"limit_mail_backup": "y", "limit_relayhost": "n", "limit_backup": "y",
		"limit_directive_snippets": "n", "limit_xmpp_muc": "n", "limit_xmpp_anon": "n",
		"limit_xmpp_vjud": "n", "limit_xmpp_proxy": "n", "limit_xmpp_status": "n",
		"limit_xmpp_pastebin": "n", "limit_xmpp_httparchive": "n",
		"limit_client": "0", "limit_web_domain": "-1", "limit_dns_zone": "-1",
		"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": "",
	}
	for k, v := range overrides {
		rec[k] = v
	}
	return rec
}

// domainRec builds a full web_domain record with every enum column at its
// schema default, overridden by the given fields.
func domainRec(overrides Rec) Rec {
	rec := Rec{
		"server_id": "1", "parent_domain_id": "0", "active": "y",
		"cgi": "n", "ssi": "n", "suexec": "y", "subdomain": "www",
		"ruby": "n", "python": "n", "perl": "n", "php": "php-fpm",
		"ssl": "n", "ssl_letsencrypt": "n", "ssl_letsencrypt_exclude": "n",
		"rewrite_to_https": "n", "php_fpm_use_socket": "y", "enable_pagespeed": "n",
		"php_fpm_chroot": "n", "pm": "ondemand", "backup_encrypt": "n",
		"traffic_quota_lock": "n", "proxy_protocol": "n",
		"delete_unused_jailkit": "n", "disable_symlinknotowner": "n",
		"hd_quota": "-1", "traffic_quota": "-1",
		"vhost_type": "name", "ip_address": "*", "ipv6_address": "",
		"allow_override": "All", "http_port": "80", "https_port": "443",
		"sys_perm_user": "riud", "sys_perm_group": "riud", "sys_perm_other": "",
	}
	for k, v := range overrides {
		rec[k] = v
	}
	return rec
}

// respond writes one JSON envelope.
func respond(w http.ResponseWriter, code, message string, data any) {
	w.Header().Set("Content-Type", `application/json; charset="utf-8"`)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code": code, "message": message, "response": data,
	})
}

// fault writes a legacy fault envelope: code "remote_fault" carrying the
// legacy message text, exactly like the reference JSON handler wraps
// SoapFaults.
func fault(w http.ResponseWriter, message string) {
	respond(w, "remote_fault", message, false)
}

// handle is the /remote/json.php handler.
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	method := r.URL.RawQuery
	if i := strings.IndexAny(method, "=&"); i >= 0 {
		method = method[:i]
	}
	if r.Method != http.MethodPost || method == "" {
		respond(w, "invalid_method", "Method not provided in json call", false)
		return
	}

	var params map[string]any
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil || params == nil {
		respond(w, "invalid_data", "The JSON data sent to the api is invalid", false)
		return
	}
	s.Calls = append(s.Calls, Call{Method: method, Params: params})

	switch method {
	case "login":
		s.login(w, params)
		return
	case "logout":
		sid, _ := params["session_id"].(string)
		if sid == "" {
			fault(w, "The SessionID is empty.")
			return
		}
		delete(s.Sessions, sid)
		respond(w, "ok", "", true)
		return
	}

	// Every other method needs a valid session and a granted function.
	sid, _ := params["session_id"].(string)
	if sid == "" {
		fault(w, "The SessionID is empty.")
		return
	}
	if !s.Sessions[sid] {
		fault(w, "The Session is expired or does not exist.")
		return
	}
	granted := false
	for _, fn := range s.Functions {
		if fn == method {
			granted = true
			break
		}
	}
	if !granted {
		fault(w, "You do not have the permissions to access this function.")
		return
	}

	switch method {
	case "get_function_list":
		respond(w, "ok", "", s.Functions)
	case "client_get_all":
		ids := make([]int, 0, len(s.Clients))
		for id := range s.Clients {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = strconv.Itoa(id)
		}
		respond(w, "ok", "", out)
	case "client_get":
		rec, ok := s.Clients[paramInt(params["client_id"])]
		if !ok {
			fault(w, "There is no user account for this customer number.")
			return
		}
		respond(w, "ok", "", rec)
	case "sites_web_domain_get":
		s.getData(w, s.Domains, "domain_id", params["primary_id"])
	case "sites_web_folder_get":
		s.getData(w, s.Folders, "web_folder_id", params["primary_id"])
	case "sites_web_folder_user_get":
		s.getData(w, s.FolderUsers, "web_folder_user_id", params["primary_id"])
	case "dns_zone_get":
		s.getData(w, s.Zones, "id", params["primary_id"])
	case "dns_slave_get":
		s.getData(w, s.Slaves, "id", params["primary_id"])
	case "dns_templatezone_get_all":
		respond(w, "ok", "", s.Templates)
	case "dns_rr_get_all_by_zone":
		respond(w, "ok", "", s.RRs[paramInt(params["zone_id"])])
	case "server_get_all":
		respond(w, "ok", "", s.Servers)
	case "server_get":
		section, _ := params["section"].(string)
		if section == "" {
			respond(w, "ok", "", s.ServerConfig)
			return
		}
		respond(w, "ok", "", s.ServerConfig[section])
	default:
		respond(w, "invalid_method", "Method "+method+" does not exist", false)
	}
}

// login mirrors the reference remoting::login flow: failure limit first,
// then credential check with attempt counting, then session creation.
func (s *Server) login(w http.ResponseWriter, params map[string]any) {
	if s.LoginAttempts >= 10 {
		fault(w, "The login failure limit has been reached.")
		return
	}
	username, _ := params["username"].(string)
	password, _ := params["password"].(string)
	if username == "" {
		fault(w, "The login username is empty.")
		return
	}
	if password == "" {
		fault(w, "The login password is empty.")
		return
	}
	if username != s.Username || password != s.Password {
		s.LoginAttempts++
		fault(w, "The login failed. Username or password wrong.")
		return
	}
	s.LoginAttempts = 0

	// Like the panel: a letter followed by random hex.
	buf := make([]byte, 20)
	_, _ = rand.Read(buf)
	sid := "s" + hex.EncodeToString(buf)
	s.Sessions[sid] = true
	respond(w, "ok", "", sid)
}

// getData implements remoting_lib::getDataRecord: a numeric primary id
// > 0 returns the single record matching pk, -1 returns all records, and
// a filter object selects by column (LIKE when the value contains "%")
// with #OFFSET#/#LIMIT# pagination.
func (s *Server) getData(w http.ResponseWriter, records []Rec, pk string, primaryID any) {
	switch id := primaryID.(type) {
	case float64, string:
		n := paramInt(id)
		switch {
		case n > 0:
			want := strconv.Itoa(n)
			for _, rec := range records {
				if rec[pk] == want {
					respond(w, "ok", "", rec)
					return
				}
			}
			respond(w, "ok", "", false)
		case n == -1:
			respond(w, "ok", "", records)
		default:
			fault(w, "The ID has to be > 0 or -1.")
		}
	case map[string]any:
		offset, limit := 0, 0
		var matched []Rec
		for _, rec := range records {
			match := true
			for key, val := range id {
				str := fmt.Sprint(val)
				switch {
				case key == "#OFFSET#":
					offset = paramInt(val)
				case key == "#LIMIT#":
					limit = paramInt(val)
				case strings.Contains(str, "%"):
					// ponytail: LIKE approximated as substring match;
					// real wildcard semantics if a test ever needs them.
					if !strings.Contains(rec[key], strings.Trim(str, "%")) {
						match = false
					}
				default:
					if rec[key] != str {
						match = false
					}
				}
			}
			if match {
				matched = append(matched, rec)
			}
		}
		if offset >= 0 && limit > 0 {
			if offset > len(matched) {
				offset = len(matched)
			}
			end := offset + limit
			if end > len(matched) {
				end = len(matched)
			}
			matched = matched[offset:end]
		}
		respond(w, "ok", "", matched)
	default:
		fault(w, "The ID must be either an integer or an array.")
	}
}

// paramInt converts a decoded JSON param (number or numeric string) to
// an int, 0 when unparseable.
func paramInt(val any) int {
	switch v := val.(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	default:
		return 0
	}
}
