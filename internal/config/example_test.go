package config

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setDefaultRe finds the keys registered in setDefaults().
var setDefaultRe = regexp.MustCompile(`viper\.SetDefault\("([^"]+)"`)

// TestExampleConfigDocumentsEverySetting is the staleness guard for
// config.toml.example. `go-ispconfig init` writes that file verbatim, so a
// setting missing from it is a setting an operator cannot discover without
// reading the Go source — which is how [auth] jwt_secret and the whole [mail]
// SMTP section went undocumented.
//
// A key may be commented out in the example (optional sections are shown that
// way on purpose); it only has to appear.
func TestExampleConfigDocumentsEverySetting(t *testing.T) {
	source, err := os.ReadFile("config.go")
	require.NoError(t, err)
	example, err := os.ReadFile("../../config.toml.example")
	require.NoError(t, err)

	matches := setDefaultRe.FindAllStringSubmatch(string(source), -1)
	require.NotEmpty(t, matches, "no viper defaults found — did setDefaults move?")

	text := string(example)
	for _, m := range matches {
		full := m[1]
		// The example uses TOML sections, so only the leaf name appears.
		leaf := full[strings.LastIndex(full, ".")+1:]
		assert.Contains(t, text, leaf,
			"config.toml.example does not document %q — `go-ispconfig init` would hide it", full)
	}
}
