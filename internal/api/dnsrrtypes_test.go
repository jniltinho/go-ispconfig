package api

import (
	"regexp"
	"testing"
)

// TestDNSRecordTypeTable sanity-checks the descriptor table: unique API
// types, valid stored enum values, compilable regexes.
func TestDNSRecordTypeTable(t *testing.T) {
	stored := map[string]bool{"A": true, "AAAA": true, "ALIAS": true, "CNAME": true,
		"DNAME": true, "CAA": true, "DS": true, "HINFO": true, "LOC": true, "MX": true,
		"NAPTR": true, "NS": true, "PTR": true, "RP": true, "SRV": true, "SSHFP": true,
		"TXT": true, "TLSA": true}
	seen := map[string]bool{}
	for _, rt := range dnsRecordTypes {
		if seen[rt.Type] {
			t.Errorf("duplicate type %s", rt.Type)
		}
		seen[rt.Type] = true
		if !stored[rt.StoredType] {
			t.Errorf("%s: stored type %s not in the dns_rr enum", rt.Type, rt.StoredType)
		}
		if _, err := regexp.Compile(rt.NameRegex); err != nil {
			t.Errorf("%s: bad name regex: %v", rt.Type, err)
		}
		if rt.DataRegex != "" {
			if _, err := regexp.Compile(rt.DataRegex); err != nil {
				t.Errorf("%s: bad data regex: %v", rt.Type, err)
			}
		}
		if rt.TTLDefault < 60 {
			t.Errorf("%s: ttl default below 60", rt.Type)
		}
	}
	if len(dnsRecordTypes) != 21 {
		t.Errorf("expected 21 record types (18 + SPF/DKIM/DMARC), got %d", len(dnsRecordTypes))
	}
}

func TestValidateDNSRecord(t *testing.T) {
	rt := func(name string) *DNSRecordType {
		r, ok := dnsRecordTypeByName(name)
		if !ok {
			t.Fatalf("unknown type %s", name)
		}
		return r
	}
	tests := []struct {
		name  string
		typ   string
		rec   [2]string // name, data
		aux   int64
		ttl   int64
		field string // expected error field, "" = valid
		key   string
	}{
		{"valid A", "A", [2]string{"www", "10.0.0.1"}, 0, 3600, "", ""},
		{"A with invalid ip", "A", [2]string{"www", "not-an-ip"}, 0, 3600, "data", "ip_error_wrong"},
		{"A with ipv6 data", "A", [2]string{"www", "2001:db8::1"}, 0, 3600, "data", "ip_error_wrong"},
		{"valid AAAA", "AAAA", [2]string{"www", "2001:db8::1"}, 0, 3600, "", ""},
		{"AAAA with ipv4 data", "AAAA", [2]string{"www", "10.0.0.1"}, 0, 3600, "data", "ip_error_wrong"},
		{"valid MX with priority", "MX", [2]string{"", "mail.example.com."}, 10, 3600, "", ""},
		{"MX bad hostname", "MX", [2]string{"", "mail server"}, 10, 3600, "data", "data_error_regex"},
		{"valid CNAME", "CNAME", [2]string{"www", "example.com."}, 0, 3600, "", ""},
		{"empty data", "CNAME", [2]string{"www", ""}, 0, 3600, "data", "data_error_empty"},
		{"valid TXT", "TXT", [2]string{"", "hello world"}, 0, 3600, "", ""},
		{"spf payload in plain TXT", "TXT", [2]string{"", "v=spf1 mx a ~all"}, 0, 3600, "data", "data_error_use_dedicated_form"},
		{"dkim payload in plain TXT", "TXT", [2]string{"", "v=DKIM1; p=abc"}, 0, 3600, "data", "data_error_use_dedicated_form"},
		{"dmarc payload in plain TXT", "TXT", [2]string{"", "v=DMARC1; p=none"}, 0, 3600, "data", "data_error_use_dedicated_form"},
		{"valid SPF helper", "SPF", [2]string{"", "v=spf1 mx a ~all"}, 0, 3600, "", ""},
		{"SPF without prefix", "SPF", [2]string{"", "mx a ~all"}, 0, 3600, "data", "data_error_regex"},
		{"valid DMARC helper", "DMARC", [2]string{"_dmarc", "v=DMARC1; p=none;"}, 0, 3600, "", ""},
		{"valid SRV", "SRV", [2]string{"_sip._tcp", "10 5060 sip.example.com."}, 5, 3600, "", ""},
		{"SRV data missing fields", "SRV", [2]string{"_sip._tcp", "5060"}, 5, 3600, "data", "data_error_regex"},
		{"valid CAA", "CAA", [2]string{"", `0 issue "letsencrypt.org"`}, 0, 3600, "", ""},
		{"CAA bad tag", "CAA", [2]string{"", `0 grant "x"`}, 0, 3600, "data", "data_error_regex"},
		{"valid TLSA", "TLSA", [2]string{"_443._tcp.example.com", "3 1 1 abc123"}, 0, 7200, "", ""},
		{"TLSA bad name", "TLSA", [2]string{"www", "3 1 1 abc"}, 0, 7200, "name", "name_error_regex"},
		{"PTR requires name", "PTR", [2]string{"", "host.example.com."}, 0, 3600, "name", "name_error_empty"},
		{"ttl below 60", "A", [2]string{"www", "10.0.0.1"}, 0, 59, "ttl", "ttl_range_error"},
		{"aux above range", "MX", [2]string{"", "mail.example.com."}, 70000, 3600, "aux", "aux_error_range"},
		{"wildcard A name", "A", [2]string{"*", "10.0.0.1"}, 0, 3600, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := validateDNSRecord(rt(tt.typ), tt.rec[0], tt.rec[1], tt.aux, tt.ttl)
			if tt.field == "" {
				if len(fields) != 0 {
					t.Fatalf("expected valid, got %v", fields)
				}
				return
			}
			keys, ok := fields[tt.field]
			if !ok {
				t.Fatalf("expected error on %s, got %v", tt.field, fields)
			}
			found := false
			for _, k := range keys {
				if k == tt.key {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected key %s on %s, got %v", tt.key, tt.field, keys)
			}
		})
	}
}

func TestDNSRecordTypeByName(t *testing.T) {
	if _, ok := dnsRecordTypeByName("mx"); !ok {
		t.Error("lookup must be case-insensitive")
	}
	if _, ok := dnsRecordTypeByName("BOGUS"); ok {
		t.Error("unknown types must not resolve")
	}
	if rt, _ := dnsRecordTypeByName("SPF"); rt.StoredType != "TXT" {
		t.Error("SPF must store as TXT")
	}
}
