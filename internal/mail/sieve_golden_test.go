package mail

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSieveGoldenMatchesPHP renders every fixture with the Go vector and
// compares byte-identically against the outputs produced by ISPConfig's
// own tpl engine (internal/mail/golden/generate.php via php:8.2-cli —
// spec mail-mailbox-management golden-file fidelity).
func TestSieveGoldenMatchesPHP(t *testing.T) {
	raw, err := os.ReadFile("golden/fixtures.json")
	require.NoError(t, err)
	var fixtures map[string]map[string]any
	require.NoError(t, json.Unmarshal(raw, &fixtures))
	require.NotEmpty(t, fixtures)

	src := loadSieveTemplate(t)
	for name, f := range fixtures {
		var addresses []string
		for _, a := range f["addresses"].([]any) {
			addresses = append(addresses, a.(string))
		}
		vars := sieveVars(row(f), addresses)
		for _, script := range []string{"before", "after"} {
			t.Run(name+"/"+script, func(t *testing.T) {
				want, err := os.ReadFile("golden/" + name + "." + script + ".sieve")
				require.NoError(t, err)
				got, err := renderSieve(src, script, vars)
				require.NoError(t, err)
				assert.Equal(t, string(want), got, "byte-identical to the PHP render")
			})
		}
	}
}
