package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateConfigSectionSSHPort guards the one Server-tab field whose bad
// value fails silently: the firewall module falls back to port 22 when
// ssh_port does not parse, so an out-of-range value would leave the operator
// convinced they had changed something.
func TestValidateConfigSectionSSHPort(t *testing.T) {
	tests := []struct {
		name    string
		section string
		body    ServerConfigSection
		wantErr bool
	}{
		{"valid port", "server", ServerConfigSection{"ssh_port": "2222"}, false},
		{"lowest port", "server", ServerConfigSection{"ssh_port": "1"}, false},
		{"highest port", "server", ServerConfigSection{"ssh_port": "65535"}, false},
		{"empty means unset", "server", ServerConfigSection{"ssh_port": ""}, false},
		{"whitespace means unset", "server", ServerConfigSection{"ssh_port": "  "}, false},
		{"absent key", "server", ServerConfigSection{"ip_address": "10.0.0.1"}, false},
		{"zero", "server", ServerConfigSection{"ssh_port": "0"}, true},
		{"above range", "server", ServerConfigSection{"ssh_port": "70000"}, true},
		{"negative", "server", ServerConfigSection{"ssh_port": "-1"}, true},
		{"not a number", "server", ServerConfigSection{"ssh_port": "ssh"}, true},
		// Other sections are not second-guessed: a compatibility value is
		// written for ISPConfig3's benefit, not ours.
		{"other section is untouched", "web", ServerConfigSection{"ssh_port": "70000"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateConfigSection(tc.section, tc.body)
			if !tc.wantErr {
				assert.NoError(t, err)
				return
			}
			var ve *ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Equal(t, []string{"srvcfg_error_ssh_port"}, ve.Fields["ssh_port"])
		})
	}
}
