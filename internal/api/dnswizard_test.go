package api

import (
	"testing"
)

// stockDefaultTemplate is the dns_template "Default" row seeded by
// ispconfig3.sql (fields DOMAIN,IP,NS1,NS2,EMAIL,DKIM,DNSSEC).
const stockDefaultTemplate = "[ZONE]\norigin={DOMAIN}.\nns={NS1}.\nmbox={EMAIL}.\nrefresh=7200\nretry=540\nexpire=604800\nminimum=3600\nttl=3600\nxfer=\nalso_notify=\ndnssec_wanted=N\ndnssec_algo=ECDSAP256SHA256\n\n[DNS_RECORDS]\nA|{DOMAIN}.|{IP}|0|3600\nA|www|{IP}|0|3600\nA|mail|{IP}|0|3600\nNS|{DOMAIN}.|{NS1}.|0|3600\nNS|{DOMAIN}.|{NS2}.|0|3600\nMX|{DOMAIN}.|mail.{DOMAIN}.|10|3600\nTXT|{DOMAIN}.|v=spf1 mx a ~all|0|3600"

func defaultWizardRequest() *DNSWizardRequest {
	return &DNSWizardRequest{
		Domain: "example.com", IP: "10.0.0.1",
		NS1: "ns1.example.net", NS2: "ns2.example.net", Email: "hostmaster@example.com",
	}
}

func TestExpandDNSTemplateDefault(t *testing.T) {
	req := defaultWizardRequest()
	vars, records, err := expandDNSTemplate(stockDefaultTemplate, req)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"origin": "example.com.", "ns": "ns1.example.net.", "mbox": "hostmaster@example.com.",
		"refresh": "7200", "retry": "540", "expire": "604800", "minimum": "3600",
		"ttl": "3600", "xfer": "", "dnssec_wanted": "N", "dnssec_algo": "ECDSAP256SHA256",
	} {
		if vars[key] != want {
			t.Errorf("vars[%s] = %q, want %q", key, vars[key], want)
		}
	}
	if len(records) != 7 {
		t.Fatalf("expected 7 records, got %d", len(records))
	}
	mx := records[5]
	if mx.Type != "MX" || mx.Name != "example.com." || mx.Data != "mail.example.com." ||
		mx.Aux != 10 || mx.TTL != 3600 {
		t.Errorf("MX record wrong: %+v", mx)
	}
	if records[6].Data != "v=spf1 mx a ~all" {
		t.Errorf("TXT record wrong: %+v", records[6])
	}
}

func TestExpandDNSTemplateDNSSECInjection(t *testing.T) {
	req := defaultWizardRequest()
	req.DNSSEC = true
	vars, _, err := expandDNSTemplate(stockDefaultTemplate, req)
	if err != nil {
		t.Fatal(err)
	}
	// The DNSSEC option wins even over a template that pins
	// dnssec_wanted=N (fixes the dead-checkbox quirk of the PHP wizard).
	if vars["dnssec_wanted"] != "Y" {
		t.Errorf("dnssec_wanted = %q, want Y", vars["dnssec_wanted"])
	}
	stripped := "[ZONE]\norigin={DOMAIN}.\nns={NS1}.\nmbox={EMAIL}.\nrefresh=7200\nretry=540\nexpire=604800\nminimum=3600\nttl=3600"
	vars, _, err = expandDNSTemplate(stripped, req)
	if err != nil {
		t.Fatal(err)
	}
	if vars["dnssec_wanted"] != "Y" {
		t.Errorf("dnssec_wanted = %q, want Y after injection", vars["dnssec_wanted"])
	}
}

func TestExpandDNSTemplateErrors(t *testing.T) {
	if _, _, err := expandDNSTemplate("[BOGUS]\nx=y", defaultWizardRequest()); err == nil {
		t.Error("unknown section must fail")
	}
	if _, _, err := expandDNSTemplate("[DNS_RECORDS]\nA|www", defaultWizardRequest()); err == nil {
		t.Error("malformed record line must fail")
	}
}

func TestSOAFromTemplateVars(t *testing.T) {
	req := defaultWizardRequest()
	vars, _, err := expandDNSTemplate(stockDefaultTemplate, req)
	if err != nil {
		t.Fatal(err)
	}
	soa, errs := soaFromTemplateVars(vars)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if soa.Mbox != "hostmaster.example.com." {
		t.Errorf("mbox @ not replaced: %q", soa.Mbox)
	}
	if soa.Serial%100 != 1 {
		t.Errorf("initial serial must be <today>01, got %d", soa.Serial)
	}
	if soa.Active != "Y" || soa.Refresh != 7200 || soa.Expire != 604800 {
		t.Errorf("soa fields wrong: %+v", soa)
	}

	// Missing origin reports the wizard error.
	delete(vars, "origin")
	if _, errs := soaFromTemplateVars(vars); len(errs["origin"]) == 0 {
		t.Error("missing origin must fail")
	}
}

func TestValidateWizardInputs(t *testing.T) {
	fields := "DOMAIN,IP,NS1,NS2,EMAIL,DKIM,DNSSEC"
	if errs := validateWizardInputs(fields, defaultWizardRequest()); len(errs) > 0 {
		t.Fatalf("valid inputs rejected: %v", errs)
	}
	empty := &DNSWizardRequest{}
	errs := validateWizardInputs(fields, empty)
	for _, f := range []string{"domain", "ip", "ns1", "ns2", "email"} {
		if len(errs[f]) == 0 {
			t.Errorf("missing %s not reported", f)
		}
	}
	bad := defaultWizardRequest()
	bad.Domain = "not a domain"
	bad.Email = "not-an-email"
	errs = validateWizardInputs(fields, bad)
	if len(errs["domain"]) == 0 || len(errs["email"]) == 0 {
		t.Errorf("invalid domain/email not reported: %v", errs)
	}
}
