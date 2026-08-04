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
		"server":  iniKeys(getconf.ServerSection{}),
		"web":     iniKeys(getconf.WebConfig{}),
		"dns":     iniKeys(getconf.DNSConfig{}),
		"mail":    iniKeys(getconf.MailConfig{}),
		"getmail": iniKeys(getconf.GetmailConfig{}),
		"jailkit": iniKeys(getconf.JailkitConfig{}),
	}
	// The [server] tab is the one place a rendered field may have no getconf
	// key: ISPConfig3 writes network, backup and monit/munin settings there
	// that this port does not act on, and hiding them is what made an adopted
	// database look like it had lost them. They are allowed — but only by
	// being named in serverCompatKeys, so nothing lands on the tab by
	// accident (spec server-config-server-tab).
	compat := map[string]map[string]bool{"server": {}}
	for _, k := range serverCompatKeys {
		compat["server"][k] = true
	}

	form := serverConfigForm()
	require.Len(t, form.Tabs, len(want), "one tab per decoded INI section")

	for _, tab := range form.Tabs {
		keys, ok := want[tab.Name]
		require.True(t, ok, "tab %q has no matching getconf section", tab.Name)
		seen := map[string]bool{}
		for _, f := range tab.Fields {
			if f.Type == "legend" {
				continue // structural, not a config key
			}
			assert.True(t, keys[f.Name] || compat[tab.Name][f.Name],
				"%s.%s is rendered but getconf never decodes it and it is not in serverCompatKeys",
				tab.Name, f.Name)
			assert.False(t, seen[f.Name], "%s.%s rendered twice", tab.Name, f.Name)
			seen[f.Name] = true
		}
		for key := range keys {
			assert.True(t, seen[key], "%s.%s is decoded by getconf but has no form field", tab.Name, key)
		}
		for key := range compat[tab.Name] {
			assert.True(t, seen[key], "%s.%s is listed as a compatibility field but is not rendered", tab.Name, key)
		}
		delete(want, tab.Name)
	}
}

// TestServerCompatKeysAreNotDecoded pins the meaning of the two groups on the
// Server tab: a key listed as compatibility-only must genuinely have no
// consumer, or the tab would tell the operator it does nothing while the
// daemon quietly reads it.
func TestServerCompatKeysAreNotDecoded(t *testing.T) {
	decoded := iniKeys(getconf.ServerSection{})
	for _, k := range serverCompatKeys {
		assert.False(t, decoded[k],
			"%q is in serverCompatKeys but getconf decodes it — move it above the legend", k)
	}
	assert.NotEmpty(t, serverCompatKeys)
}

// TestServerConfigFormOptionValuesAreDistinct catches the class of bug the
// name-only staleness tests cannot see: a SELECT whose options collapse onto
// fewer stored values than it shows. It happened twice while generating this
// table from the legacy tform — `backup_time` rendered 96 times and stored 4
// distinct values, and `loglevel` stored its labels instead of its keys —
// because PHP writes a list and a map identically once the key quoting is
// dropped. Either way the symptom is silent: pick one option, reload, get a
// different one, and an adopted database's value matches nothing.
func TestServerConfigFormOptionValuesAreDistinct(t *testing.T) {
	for _, tab := range serverConfigForm().Tabs {
		for _, f := range tab.Fields {
			if len(f.Options) == 0 {
				continue
			}
			seen := map[string]bool{}
			for _, o := range f.Options {
				assert.False(t, seen[o.Value],
					"%s.%s offers %d options but repeats the value %q — a map was parsed as a list, or vice versa",
					tab.Name, f.Name, len(f.Options), o.Value)
				seen[o.Value] = true
			}
		}
	}
}

// TestServerConfigFormKnownSelectValues pins the two selects that were wrong,
// against the legacy tform they are generated from.
func TestServerConfigFormKnownSelectValues(t *testing.T) {
	fields := map[string]FormFieldMeta{}
	for _, tab := range serverConfigForm().Tabs {
		for _, f := range tab.Fields {
			fields[tab.Name+"."+f.Name] = f
		}
	}

	loglevel := fields["server.loglevel"]
	require.Len(t, loglevel.Options, 3)
	assert.Equal(t, []Option{
		{Value: "0", Label: "Debug"},
		{Value: "1", Label: "Warnings"},
		{Value: "2", Label: "Errors"},
	}, loglevel.Options, "loglevel is INTEGER 0/1/2 in the legacy tform, not its labels")

	backup := fields["server.backup_time"]
	require.Len(t, backup.Options, 96, "every quarter hour of the day")
	assert.Equal(t, "0:00", backup.Options[0].Value)
	assert.Equal(t, "23:45", backup.Options[len(backup.Options)-1].Value)
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
