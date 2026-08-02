package api

import (
	"testing"

	"go-ispconfig/internal/model"
)

func TestMinRule(t *testing.T) {
	r := minRule(60, "range_error")
	for value, want := range map[string]string{
		"60": "", "3600": "", "59": "range_error", "": "range_error", "abc": "range_error",
	} {
		if got, _ := r.Validate(nil, value); got != want {
			t.Errorf("minRule(60)(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestIPListRule(t *testing.T) {
	allow := ipListRule("ip_error", true)
	deny := ipListRule("ip_error", false)
	for value, want := range map[string]string{
		"":                          "",
		"10.0.0.1":                  "",
		"10.0.0.1,2001:db8::1":      "",
		"10.0.0.1, 192.168.0.2":     "",
		"10.0.0.1,not-an-ip":        "ip_error",
		"example.com":               "ip_error",
		"10.0.0.1;192.168.0.2":      "ip_error",
		"<script>alert(1)</script>": "ip_error",
	} {
		if got, _ := allow.Validate(nil, value); got != want {
			t.Errorf("ipListRule(allowEmpty)(%q) = %q, want %q", value, got, want)
		}
	}
	if got, _ := deny.Validate(nil, ""); got != "ip_error" {
		t.Errorf("ipListRule(required)(\"\") = %q, want ip_error", got)
	}
}

func TestDNSSECAlgoRule(t *testing.T) {
	r := dnssecAlgoRule()
	for value, want := range map[string]string{
		"":                             "",
		"ECDSAP256SHA256":              "",
		"NSEC3RSASHA1":                 "",
		"NSEC3RSASHA1,ECDSAP256SHA256": "",
		"RSAMD5":                       "dnssec_algo_error",
		"ECDSAP256SHA256,RSAMD5":       "dnssec_algo_error",
		"ecdsap256sha256":              "dnssec_algo_error",
	} {
		if got, _ := r.Validate(nil, value); got != want {
			t.Errorf("dnssecAlgoRule(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestIdnLower(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Example.COM.", "example.com."},
		{"Bücher.example.", "xn--bcher-kva.example."},
		{"plain.example", "plain.example"},
	}
	for _, tt := range tests {
		body := map[string]any{"origin": tt.in}
		idnLower(body, "origin")
		if body["origin"] != tt.want {
			t.Errorf("idnLower(%q) = %q, want %q", tt.in, body["origin"], tt.want)
		}
	}
}

func TestZoneFieldStringMatchesBodyString(t *testing.T) {
	z := &model.DNSSoa{ServerID: 1, Origin: "example.com.", Refresh: 7200, Active: "Y"}
	if zoneFieldString(z, "refresh") != bodyString(float64(7200)) {
		t.Error("numeric body value must compare equal to the stored field")
	}
	if zoneFieldString(z, "active") != bodyString("Y") {
		t.Error("string body value must compare equal to the stored field")
	}
	if zoneFieldString(z, "origin") == bodyString("other.com.") {
		t.Error("different values must not compare equal")
	}
}
