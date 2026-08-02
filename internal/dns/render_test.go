package dns

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rr(name, typ, data string, aux, ttl int) map[string]any {
	return map[string]any{"name": name, "type": typ, "data": data, "aux": aux, "ttl": ttl}
}

func TestPreprocessTTLAndName(t *testing.T) {
	got := preprocessRecords([]map[string]any{rr("", "A", "192.0.2.1", 0, 0)}, true)
	require.Len(t, got, 1)
	assert.Equal(t, "@", got[0]["name"])
	assert.Equal(t, "", got[0]["ttl"])
}

func TestPreprocessDoesNotMutateInput(t *testing.T) {
	in := rr("", "A", "192.0.2.1", 0, 0)
	preprocessRecords([]map[string]any{in}, true)
	assert.Equal(t, "", in["name"])
	assert.Equal(t, 0, in["ttl"])
}

func TestPreprocessTXTSplit(t *testing.T) {
	long := strings.Repeat("a", 255) + strings.Repeat("b", 255) + strings.Repeat("c", 90)
	got := preprocessRecords([]map[string]any{rr("x", "TXT", long, 0, 3600)}, true)
	want := strings.Repeat("a", 255) + `" "` + strings.Repeat("b", 255) + `" "` + strings.Repeat("c", 90)
	assert.Equal(t, want, got[0]["data"])

	short := preprocessRecords([]map[string]any{rr("x", "TXT", "v=spf1 mx -all", 0, 3600)}, true)
	assert.Equal(t, "v=spf1 mx -all", short[0]["data"], "255 bytes or less stays unsplit")
}

func TestPreprocessCAAModernBindPassthrough(t *testing.T) {
	got := preprocessRecords([]map[string]any{rr("x", "CAA", `0 issue "letsencrypt.org"`, 0, 3600)}, true)
	require.Len(t, got, 1)
	assert.Equal(t, "CAA", got[0]["type"])
	assert.Equal(t, `0 issue "letsencrypt.org"`, got[0]["data"])
}

func TestPreprocessCAALegacyIssue(t *testing.T) {
	got := preprocessRecords([]map[string]any{rr("x", "CAA", `0 issue "ca.example.net"`, 0, 3600)}, false)
	require.Len(t, got, 1, "no synthetic record when a real issue record exists")
	assert.Equal(t, "TYPE257", got[0]["type"])
	// 0005 + hex("issueca.example.net") = 19 payload bytes + 2 prefix bytes.
	assert.Equal(t, `\# 21 0005697373756563612E6578616D706C652E6E6574`, got[0]["data"])
}

func TestPreprocessCAALegacyIssuewildOnlyAddsSynthetic(t *testing.T) {
	got := preprocessRecords([]map[string]any{
		rr("x", "CAA", `0 issuewild "ca.example.net"`, 0, 3600),
		rr("y", "A", "192.0.2.1", 0, 3600),
	}, false)
	require.Len(t, got, 3, "synthetic issue \";\" appended after all records")
	assert.Equal(t, "TYPE257", got[0]["type"])
	assert.Equal(t, `\# 25 0009697373756577696C6463612E6578616D706C652E6E6574`, got[0]["data"])
	assert.Equal(t, "A", got[1]["type"])
	syn := got[2]
	assert.Equal(t, "TYPE257", syn["type"])
	assert.Equal(t, "x", syn["name"], "synthetic copies the issuewild record's name")
	assert.Equal(t, `\# 8 000569737375653B`, syn["data"])
}

func TestPreprocessCAALegacyIssueCancelsSynthetic(t *testing.T) {
	got := preprocessRecords([]map[string]any{
		rr("x", "CAA", `0 issuewild "wild.example"`, 0, 3600),
		rr("x", "CAA", `0 issue "ca.example"`, 0, 3600),
	}, false)
	require.Len(t, got, 2, "real issue record removes the synthetic one")
	for _, r := range got {
		assert.NotEqual(t, `\# 8 000569737375653B`, r["data"])
	}
}

func TestRenderZone(t *testing.T) {
	soa := map[string]any{
		"ttl": 3600, "ns": "ns1.example.com.", "mbox": "admin.example.com.",
		"serial": 2026080101, "refresh": 7200, "retry": 540,
		"expire": 604800, "minimum": 3600,
	}
	records := []map[string]any{
		rr("", "A", "192.0.2.1", 0, 0),
		rr("example.com.", "MX", "mail.example.com.", 10, 3600),
	}
	out, err := RenderZone(soa, records, true, "")
	require.NoError(t, err)
	assert.Contains(t, out, "$TTL        3600")
	assert.Contains(t, out, "2026080101       ; serial")
	assert.Contains(t, out, "@       A          192.0.2.1")
	assert.Contains(t, out, "MX     10  mail.example.com.")
}
