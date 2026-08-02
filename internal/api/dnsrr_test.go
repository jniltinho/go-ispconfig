package api

import (
	"testing"

	"go-ispconfig/internal/model"
)

func TestMergeRecordBody(t *testing.T) {
	t.Run("SPF helper stores as TXT", func(t *testing.T) {
		rr := &model.DNSRr{Active: "Y"}
		rt, err := mergeRecordBody(rr, map[string]any{
			"type": "SPF", "name": "", "data": "v=spf1 mx a ~all"})
		if err != nil {
			t.Fatal(err)
		}
		if rt.Type != "SPF" || rr.Type != "TXT" {
			t.Errorf("got descriptor %s stored %s, want SPF/TXT", rt.Type, rr.Type)
		}
	})

	t.Run("unknown type is a validation error", func(t *testing.T) {
		rr := &model.DNSRr{}
		if _, err := mergeRecordBody(rr, map[string]any{"type": "BOGUS"}); err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("name is IDN-normalized and lowercased", func(t *testing.T) {
		rr := &model.DNSRr{}
		if _, err := mergeRecordBody(rr, map[string]any{
			"type": "A", "name": "WWW.Bücher", "data": "10.0.0.1"}); err != nil {
			t.Fatal(err)
		}
		if rr.Name != "www.xn--bcher-kva" {
			t.Errorf("name = %q", rr.Name)
		}
	})

	t.Run("active normalizes to Y unless N", func(t *testing.T) {
		rr := &model.DNSRr{}
		if _, err := mergeRecordBody(rr, map[string]any{"type": "A", "active": "whatever"}); err != nil {
			t.Fatal(err)
		}
		if rr.Active != "Y" {
			t.Errorf("active = %q, want Y", rr.Active)
		}
		if _, err := mergeRecordBody(rr, map[string]any{"type": "A", "active": "N"}); err != nil {
			t.Fatal(err)
		}
		if rr.Active != "N" {
			t.Errorf("active = %q, want N", rr.Active)
		}
	})

	t.Run("absent fields keep stored values", func(t *testing.T) {
		rr := &model.DNSRr{Type: "MX", Name: "old", Data: "mail.example.com.", Aux: 10, TTL: 3600, Active: "Y"}
		rt, err := mergeRecordBody(rr, map[string]any{"data": "mail2.example.com."})
		if err != nil {
			t.Fatal(err)
		}
		if rt.Type != "MX" || rr.Name != "old" || rr.Aux != 10 || rr.Data != "mail2.example.com." {
			t.Errorf("merge lost stored values: %+v", rr)
		}
	})
}
