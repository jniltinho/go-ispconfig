package jailkit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/system"
)

func TestMergeConfigDefaults(t *testing.T) {
	cfg := MergeConfig(getconf.DefaultJailkitConfig(), nil)
	assert.Equal(t, "/home/[username]", cfg.Home)
	assert.Contains(t, cfg.Sections, "coreutils")
	assert.Contains(t, cfg.Sections, "ssh")
	assert.Contains(t, cfg.Programs, "unzip")
	assert.Contains(t, cfg.CronPrograms, "/usr/bin/php")
	assert.Equal(t, "allow", cfg.Hardlinks)
}

func TestMergeConfigSiteOverridesAndPHPSection(t *testing.T) {
	web := system.Row{
		"jailkit_chroot_app_sections": "basicshell git",
		"jailkit_chroot_app_programs": "htop",
		"php_jk_section":              "php8_3",
	}
	cfg := MergeConfig(getconf.DefaultJailkitConfig(), web)

	// Site sections replace the server list; php section is appended,
	// unique-sorted.
	assert.Equal(t, []string{"basicshell", "git", "php8_3"}, cfg.Sections)
	assert.Equal(t, []string{"htop"}, cfg.Programs)
	// Cron programs stay server-side (no web override column).
	assert.NotEmpty(t, cfg.CronPrograms)
}

func TestMergeConfigPHPSectionDedupes(t *testing.T) {
	web := system.Row{
		"jailkit_chroot_app_sections": "php8_3 basicshell php8_3",
		"php_jk_section":              "php8_3",
	}
	cfg := MergeConfig(getconf.DefaultJailkitConfig(), web)
	assert.Equal(t, []string{"basicshell", "php8_3"}, cfg.Sections)
}

func TestHashStableAndSensitive(t *testing.T) {
	a := Config{
		Sections:     []string{"b", "a"},
		Programs:     []string{"unzip"},
		CronPrograms: []string{"/usr/bin/php"},
	}
	b := Config{
		Sections:     []string{"a", "b"},
		Programs:     []string{"unzip"},
		CronPrograms: []string{"/usr/bin/php"},
	}
	// Order of input does not matter: uniqueSorted normalises.
	assert.Equal(t, Hash(a), Hash(b))
	require.Len(t, Hash(a), 32)

	c := a
	c.Sections = append(c.Sections, "php8_3")
	assert.NotEqual(t, Hash(a), Hash(c), "adding a section must change the hash")
}

func TestHomeOf(t *testing.T) {
	assert.Equal(t, "/home/web1user", HomeOf(Config{Home: "/home/[username]"}, "web1user"))
	assert.Equal(t, "/srv/web1user", HomeOf(Config{Home: "/srv/[username]"}, "web1user"))
}

func TestHardlinkOpts(t *testing.T) {
	assert.Equal(t, []string{"hardlink"}, HardlinkOpts(Config{Hardlinks: "yes"}))
	assert.Equal(t, []string{"allow_hardlink"}, HardlinkOpts(Config{Hardlinks: ""}))
	assert.Nil(t, HardlinkOpts(Config{Hardlinks: "allow"}))
	assert.Nil(t, HardlinkOpts(Config{Hardlinks: "no"}))
}

func TestSplitTokens(t *testing.T) {
	assert.Equal(t, []string{"a", "b", "c"}, splitTokens(" a,b\tc  "))
	assert.Empty(t, splitTokens(""))
}
