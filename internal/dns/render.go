package dns

import (
	"encoding/hex"
	"fmt"
	"maps"
	"strings"

	"go-ispconfig/internal/mastertpl"
)

// zoneTemplate is the master template name for Bind zone files.
const zoneTemplate = "bind_pri.domain.master"

// RenderZone renders a Bind zone file from a dns_soa row and its active
// dns_rr rows (pure function, design D3): record pre-processing per
// bind_plugin.inc.php soa_update — TTL 0 rendered empty, empty name
// rendered as "@", TXT data longer than 255 bytes split into 255-byte
// chunks joined with `" "`, and on BIND without native CAA support
// (< 9.9.6) CAA records converted to TYPE257 generic encoding including
// the synthetic `issue ";"` record when only issuewild records exist —
// followed by the bind_pri.domain.master render (custom template override
// first). The inputs are not mutated.
func RenderZone(soa map[string]any, records []map[string]any, caaSupported bool, customTplDir string) (string, error) {
	src, _, err := mastertpl.Load(zoneTemplate, customTplDir)
	if err != nil {
		return "", err
	}
	tpl := mastertpl.New(src)
	for k, v := range soa {
		tpl.SetVar(k, v)
	}
	tpl.SetLoop("zones", preprocessRecords(records, caaSupported))
	out, err := tpl.Render()
	if err != nil {
		return "", fmt.Errorf("dns: rendering %s: %w", zoneTemplate, err)
	}
	return out, nil
}

// preprocessRecords applies the per-record transformations of the PHP
// soa_update loop on copies of the input rows.
func preprocessRecords(records []map[string]any, caaSupported bool) []map[string]any {
	out := make([]map[string]any, 0, len(records))
	// synthetic is the pending `issue ";"` record appended when a zone has
	// only issuewild CAA records (PHP $caa_add_rec state machine); a real
	// issue record cancels it for good (issueSeen).
	var synthetic map[string]any
	issueSeen := false
	for _, in := range records {
		rec := maps.Clone(in)
		r := row(rec)
		if r.num("ttl") == 0 {
			rec["ttl"] = ""
		}
		if r.str("name") == "" {
			rec["name"] = "@"
		}
		if r.str("type") == "TXT" && len(r.str("data")) > 255 {
			rec["data"] = strings.Join(chunkBytes(r.str("data"), 255), `" "`)
		}
		if r.str("type") == "CAA" && !caaSupported {
			rec["type"] = "TYPE257"
			// Drop the flag field: "0 issue \"ca.example\"" -> tag + value.
			parts := strings.Split(r.str("data"), " ")
			tag, rest := "", ""
			if len(parts) > 1 {
				tag = parts[1]
				rest = strings.Join(parts[1:], " ")
			}
			// Wire format hex: tag-length prefix + hex(tag+value without
			// quotes and spaces), uppercased.
			payload := strings.NewReplacer(`"`, "", " ", "").Replace(rest)
			hexData := strings.ToUpper(hex.EncodeToString([]byte(payload)))
			if tag == "issuewild" {
				hexData = "0009" + hexData
				if synthetic == nil && !issueSeen {
					// Only issuewild records so far: queue a synthetic
					// TYPE257 record encoding `issue ";"` (PHP parity;
					// PHP array_push places it after all data records).
					synthetic = maps.Clone(rec)
					synthetic["data"] = `\# 8 000569737375653B`
				}
			} else {
				hexData = "0005" + hexData
				synthetic = nil // a real issue record exists
				issueSeen = true
			}
			rec["data"] = fmt.Sprintf(`\# %d %s`, len(hexData)/2, hexData)
		}
		out = append(out, rec)
	}
	if synthetic != nil {
		out = append(out, synthetic)
	}
	return out
}

// chunkBytes splits s into byte chunks of at most size (PHP str_split).
func chunkBytes(s string, size int) []string {
	var chunks []string
	for len(s) > size {
		chunks = append(chunks, s[:size])
		s = s[size:]
	}
	return append(chunks, s)
}
