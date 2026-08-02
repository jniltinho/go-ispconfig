package mail

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/mastertpl"
)

// loadSieveTemplate reads the embedded stock template.
func loadSieveTemplate(t *testing.T) string {
	t.Helper()
	src, source, err := mastertpl.Load(sieveTemplate, "")
	require.NoError(t, err)
	require.Equal(t, mastertpl.SourceEmbedded, source)
	return src
}

func TestSieveRelevantChanged(t *testing.T) {
	oldRow := row{"quota": "100", "move_junk": "y", "email": "a@b.c"}
	assert.False(t, sieveRelevantChanged(oldRow, row{"quota": "999", "move_junk": "y", "email": "a@b.c"}),
		"quota alone is not maildeliver-relevant")
	assert.True(t, sieveRelevantChanged(oldRow, row{"quota": "100", "move_junk": "n", "email": "a@b.c"}))
	assert.True(t, sieveRelevantChanged(oldRow, row{"move_junk": "y", "email": "x@b.c"}))
	assert.True(t, sieveRelevantChanged(oldRow, row{"move_junk": "y", "email": "a@b.c", "autoresponder": "y"}))
}

func TestSieveAddressStr(t *testing.T) {
	assert.Equal(t, "", sieveAddressStr(nil))
	assert.Equal(t, `:addresses ["a@x.tld"]`, sieveAddressStr([]string{"a@x.tld"}))
	assert.Equal(t, `:addresses ["a@x.tld","b@y.tld"]`, sieveAddressStr([]string{"a@x.tld", "b@y.tld"}))
}

func TestSieveVarsVector(t *testing.T) {
	vars := sieveVars(row{
		"custom_mailfilter":        "keep;\r\nstop;",
		"move_junk":                "a",
		"imap_prefix":              "INBOX.",
		"autoresponder":            "y",
		"autoresponder_start_date": "2026-08-01 08:00:00",
		"autoresponder_end_date":   "2026-08-15 18:00:00",
		"autoresponder_subject":    `Out "of" office`,
		"autoresponder_text":       `I am "away"`,
		"forward_in_lda":           "y",
		"cc":                       "copy@x.tld, second@y.tld ,",
	}, []string{"u@example.com", "alias@example.com"})

	assert.Equal(t, "keep;\nstop;", vars["custom_mailfilter"], "CRLF normalized")
	assert.Equal(t, "2026-08-01T08:00:00", vars["start_date"], "sieve iso8601 date")
	assert.Equal(t, "2026-08-15T18:00:00", vars["end_date"])
	assert.Equal(t, "Out 'of' office", vars["autoresponder_subject"], "double quotes become single")
	assert.Equal(t, "I am 'away'", vars["autoresponder_text"])
	assert.Equal(t, `:addresses ["u@example.com","alias@example.com"]`, vars["addresses"])
	loop := vars["ccloop"].([]map[string]any)
	require.Len(t, loop, 2, "empty cc entries dropped")
	assert.Equal(t, "copy@x.tld", loop[0]["address"])
	assert.Equal(t, "second@y.tld", loop[1]["address"])

	// cc only participates when forward_in_lda is y.
	vars = sieveVars(row{"forward_in_lda": "n", "cc": "copy@x.tld"}, nil)
	_, hasCC := vars["cc"]
	assert.False(t, hasCC)
}

func TestRenderSieveBeforeAndAfter(t *testing.T) {
	src := loadSieveTemplate(t)
	vars := sieveVars(row{
		"move_junk": "y", "imap_prefix": "",
		"autoresponder":            "y",
		"autoresponder_start_date": "2026-08-01 08:00:00",
		"autoresponder_end_date":   "2026-08-15 18:00:00",
		"autoresponder_subject":    "Away",
		"autoresponder_text":       "Back soon",
		"forward_in_lda":           "y", "cc": "copy@x.tld",
	}, []string{"u@example.com"})

	before, err := renderSieve(src, "before", vars)
	require.NoError(t, err)
	assert.Contains(t, before, `fileinto :create "Junk";`, "move_junk=y filters in the before script")
	assert.Contains(t, before, `redirect :copy "copy@x.tld";`)
	assert.NotContains(t, before, "vacation  :days", "autoresponder only lives in the after script")

	after, err := renderSieve(src, "after", vars)
	require.NoError(t, err)
	assert.Contains(t, after, "vacation  :days 1")
	assert.Contains(t, after, `:subject "Away"`)
	assert.Contains(t, after, `:addresses ["u@example.com"]`)
	assert.Contains(t, after, `currentdate :value "ge" "iso8601" "2026-08-01T08:00:00"`)
	assert.Contains(t, after, `currentdate :value "le" "iso8601" "2026-08-15T18:00:00"`)
	assert.NotContains(t, after, `fileinto :create`, "move_junk=y (before-mode) must not filter in after")

	// move_junk=a filters in the after script instead.
	vars["move_junk"] = "a"
	before, err = renderSieve(src, "before", vars)
	require.NoError(t, err)
	assert.NotContains(t, before, `fileinto :create`)
	after, err = renderSieve(src, "after", vars)
	require.NoError(t, err)
	assert.True(t, strings.Contains(after, `fileinto :create "Junk";`))
}
