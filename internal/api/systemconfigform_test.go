package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
)

// sysIniReadPatterns are the shapes a sys_ini read takes in this codebase,
// each tied to the section it reads from. The panel-wide config is reached
// two ways — indexed directly off getconf.GetGlobalConfig, or through a small
// accessor that returns one section — so both are scanned.
//
// A new access shape would slip past this test. That is the known ceiling:
// it is a grep, and it fails loudly with the key named when it does catch
// something, which is worth more than the false confidence of no test at all.
// The same bug class left [auth] jwt_secret out of config.toml.example.
var sysIniReadPatterns = []struct {
	section string
	re      *regexp.Regexp
}{
	// sections["misc"]["min_password_length"]
	{"", regexp.MustCompile(`sections\["(\w+)"\]\["(\w+)"\]`)},
	// global := sitesGlobalConfig(db); global["dbname_prefix"]
	{"sites", regexp.MustCompile(`sitesGlobalConfig\([^)]*\)\["(\w+)"\]`)},
	{"sites", regexp.MustCompile(`\bglobal\["(\w+)"\]`)},
}

// TestSysIniFormCoversEveryRead is the staleness guard for Main Config: a
// sys_ini key this port reads must be editable from the panel, or explicitly
// listed as read elsewhere. Before this screen existed the only way to change
// any of them was SQL, which is exactly the state this test prevents from
// coming back one key at a time.
func TestSysIniFormCoversEveryRead(t *testing.T) {
	rendered := map[string]bool{}
	for _, tab := range systemConfigForm().Tabs {
		for _, f := range tab.Fields {
			rendered[tab.Name+"."+f.Name] = true
		}
	}

	root := filepath.Join("..", "..")
	found := map[string]string{} // "section.key" -> file it was read in

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable path is not this test's business
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "base", "vendor", "docs", "openspec":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// The writers of the blob are not readers of its keys.
		if strings.HasSuffix(path, "systemconfig.go") || strings.HasSuffix(path, "systemconfigform.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil //nolint:nilerr
		}
		text := string(src)
		// The [mail] accessor returns its section; attribute its reads.
		mailAccessor := strings.Contains(text, `sections["mail"], nil`)

		for _, p := range sysIniReadPatterns {
			for _, m := range p.re.FindAllStringSubmatch(text, -1) {
				var section, key string
				switch {
				case p.section == "":
					section, key = m[1], m[2]
				case mailAccessor && p.re.String() == `\bglobal\["(\w+)"\]`:
					section, key = "mail", m[1]
				default:
					section, key = p.section, m[1]
				}
				// serverconfig.go indexes the per-server INI with the same
				// shape; only the panel-wide sections are in scope here.
				switch section {
				case "sites", "mail", "misc":
					found[section+"."+key] = path
				}
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, found, "no sys_ini reads found — did the access shape change?")

	for k, file := range found {
		if rendered[k] {
			continue
		}
		if reason, ok := sysIniReadElsewhere[k]; ok {
			assert.NotEmpty(t, reason, "%s is excluded without a reason", k)
			continue
		}
		t.Errorf("sys_ini key %q is read in %s but Main Config does not render it "+
			"— add a field, or list it in sysIniReadElsewhere with the reason", k, file)
	}
}

// TestSysIniReadElsewhereIsNotRendered keeps the exclusion list honest: a key
// listed as edited somewhere else must not also appear on this form, or the
// panel would offer two answers to one question.
func TestSysIniReadElsewhereIsNotRendered(t *testing.T) {
	rendered := map[string]bool{}
	for _, tab := range systemConfigForm().Tabs {
		for _, f := range tab.Fields {
			rendered[tab.Name+"."+f.Name] = true
		}
	}
	for k := range sysIniReadElsewhere {
		assert.False(t, rendered[k], "%s is both rendered and listed as read elsewhere", k)
	}
}

// TestValidateSysIniSection covers the one setting whose bad value is felt
// immediately and everywhere: every generated credential is measured against
// the password policy.
func TestValidateSysIniSection(t *testing.T) {
	tests := []struct {
		name    string
		section string
		body    ServerConfigSection
		field   string
	}{
		{"valid length", "misc", ServerConfigSection{"min_password_length": "8"}, ""},
		{"empty length is unset", "misc", ServerConfigSection{"min_password_length": ""}, ""},
		{"absent key", "misc", ServerConfigSection{"admin_name": "x"}, ""},
		{"zero length", "misc", ServerConfigSection{"min_password_length": "0"}, "min_password_length"},
		{"unsatisfiable length", "misc", ServerConfigSection{"min_password_length": "999"}, "min_password_length"},
		{"non-numeric length", "misc", ServerConfigSection{"min_password_length": "long"}, "min_password_length"},
		{"valid strength", "misc", ServerConfigSection{"min_password_strength": "2"}, ""},
		{"empty strength is unset", "misc", ServerConfigSection{"min_password_strength": ""}, ""},
		{"strength out of range", "misc", ServerConfigSection{"min_password_strength": "9"}, "min_password_strength"},
		// Other sections carry no policy to check.
		{"sites section untouched", "sites", ServerConfigSection{"min_password_length": "999"}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSysIniSection(tc.section, tc.body)
			if tc.field == "" {
				assert.NoError(t, err)
				return
			}
			var ve *ValidationError
			require.ErrorAs(t, err, &ve)
			assert.Contains(t, ve.Fields, tc.field)
		})
	}
}

// TestSysIniSectionNameIsLowercased pins the fix for a case bug the cross
// review caught: getconf.ParseINI lowercases section headers, so a PUT to
// "Misc" would merge into a section that never matches the parsed "[misc]"
// and end up written as a second, shadowed block in the same blob. Both INI
// editors normalise the name, so this asserts the invariant they rely on.
func TestSysIniSectionNameIsLowercased(t *testing.T) {
	sections := getconf.ParseINI("[Misc]\nFoo=1\n")
	_, upper := sections["Misc"]
	_, lower := sections["misc"]
	assert.False(t, upper, "ParseINI must not keep the header's case")
	assert.True(t, lower, "ParseINI lowercases the header — the handlers match that")

	// Keys are NOT lowercased, which is why only the section name is folded.
	assert.Equal(t, "1", sections["misc"]["Foo"])
}

// sysIniLegacyDefaults are the values ISPConfig's install/tpl/system.ini.master
// carries for the keys this form renders, copied byte for byte. They are NOT
// the tform defaults: system_config.tform.php ships an empty default for every
// prefix and min_password_length=5, while a real install gets c[CLIENTID],
// [CLIENTNAME] and 8/3 from the template its installer writes.
//
// Taking the tform value would show an operator a default their PHP panel
// never had — and, because the form default is also what a fresh section save
// materialises, would write it into the database.
var sysIniLegacyDefaults = map[string]string{
	"sites.dbname_prefix":                "c[CLIENTID]",
	"sites.dbuser_prefix":                "c[CLIENTID]",
	"sites.ftpuser_prefix":               "[CLIENTNAME]",
	"sites.shelluser_prefix":             "[CLIENTNAME]",
	"sites.phpmyadmin_url":               "https://[SERVERNAME]:8081/phpmyadmin",
	"sites.ssh_authentication":           "",
	"mail.enable_welcome_mail":           "y",
	"mail.show_per_domain_relay_options": "n",
	"misc.min_password_length":           "8",
	"misc.min_password_strength":         "3",
}

// TestSysIniDefaultsMatchLegacyTemplate pins every rendered default against
// system.ini.master.
func TestSysIniDefaultsMatchLegacyTemplate(t *testing.T) {
	got := map[string]string{}
	for _, tab := range systemConfigForm().Tabs {
		for _, f := range tab.Fields {
			def, _ := f.Default.(string)
			got[tab.Name+"."+f.Name] = def
		}
	}
	for key, want := range sysIniLegacyDefaults {
		require.Contains(t, got, key, "%s is no longer rendered", key)
		assert.Equal(t, want, got[key],
			"%s must default to what install/tpl/system.ini.master writes, not to the tform value", key)
	}
}

// TestSeededSysIniMatchesFormDefaults keeps the two copies of these values in
// step: the form default an operator sees, and the value
// internal/database/system_config.ini writes into a fresh database. A form
// that shows c[CLIENTID] over a database seeded with nothing would be a lie
// that only surfaces when the first client database comes out unprefixed.
func TestSeededSysIniMatchesFormDefaults(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "database", "system_config.ini"))
	require.NoError(t, err)
	seeded := getconf.ParseINI(string(raw))

	for key, want := range sysIniLegacyDefaults {
		section, name, _ := strings.Cut(key, ".")
		require.Contains(t, seeded, section, "seed has no [%s] section", section)
		assert.Equal(t, want, seeded[section][name],
			"internal/database/system_config.ini must seed %s with the same value the form defaults to", key)
	}
}
