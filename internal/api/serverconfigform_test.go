package api

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

// iniKeys returns the `ini` struct tags of a getconf config struct — the keys
// the daemon actually decodes out of that INI section.
func iniKeys(v any) map[string]bool {
	out := map[string]bool{}
	for f := range reflect.TypeOf(v).Fields() {
		if key := f.Tag.Get("ini"); key != "" {
			out[key] = true
		}
	}
	return out
}

// TestServerConfigFormMatchesGetconf is the staleness guard: the editor must
// render exactly the keys getconf decodes. A key added to a getconf struct
// without a form field would be invisible to the operator; a field with no
// getconf key would be a knob the applying plugins never read.
func TestServerConfigFormMatchesGetconf(t *testing.T) {
	want := map[string]map[string]bool{
		"web":     iniKeys(getconf.WebConfig{}),
		"dns":     iniKeys(getconf.DNSConfig{}),
		"mail":    iniKeys(getconf.MailConfig{}),
		"getmail": iniKeys(getconf.GetmailConfig{}),
		"jailkit": iniKeys(getconf.JailkitConfig{}),
	}

	form := serverConfigForm()
	require.Len(t, form.Tabs, len(want), "one tab per decoded INI section")

	for _, tab := range form.Tabs {
		keys, ok := want[tab.Name]
		require.True(t, ok, "tab %q has no matching getconf section", tab.Name)
		seen := map[string]bool{}
		for _, f := range tab.Fields {
			assert.True(t, keys[f.Name], "%s.%s is rendered but getconf never decodes it", tab.Name, f.Name)
			assert.False(t, seen[f.Name], "%s.%s rendered twice", tab.Name, f.Name)
			seen[f.Name] = true
		}
		for key := range keys {
			assert.True(t, seen[key], "%s.%s is decoded by getconf but has no form field", tab.Name, key)
		}
		delete(want, tab.Name)
	}
}

// TestServerConfigFormDefaultsAreSelectable guards against carrying over the
// upstream tform bug where a SELECT ships a default that is not one of its own
// options (maildir_format => '20'): the form would preselect a value the INI
// parser cannot decode.
func TestServerConfigFormDefaultsAreSelectable(t *testing.T) {
	for _, tab := range serverConfigForm().Tabs {
		for _, f := range tab.Fields {
			if len(f.Options) == 0 || f.Default == nil {
				continue
			}
			def, ok := f.Default.(string)
			require.True(t, ok, "%s.%s default is not a string", tab.Name, f.Name)
			found := false
			for _, o := range f.Options {
				if o.Value == def {
					found = true
					break
				}
			}
			assert.True(t, found, "%s.%s default %q is not one of its options", tab.Name, f.Name, def)
		}
	}
}

// TestServerConfigFormFieldNamesAreWritable keeps every rendered key inside
// the grammar serverConfigSaveHandler accepts — a field the form can show but
// the save endpoint rejects would be a dead-end for the operator.
func TestServerConfigFormFieldNamesAreWritable(t *testing.T) {
	for _, tab := range serverConfigForm().Tabs {
		assert.Regexp(t, iniNameRe, tab.Name)
		for _, f := range tab.Fields {
			assert.Regexp(t, iniNameRe, f.Name, "%s.%s is not a writable INI key", tab.Name, f.Name)
		}
	}
}
