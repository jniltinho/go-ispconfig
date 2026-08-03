// Package jailkit implements the ISPConfig3 jailkit chroot plugin for shell users.
package jailkit

import (
	"crypto/md5"
	"encoding/hex"
	"sort"
	"strings"

	"go-ispconfig/internal/getconf"
	"go-ispconfig/internal/system"
)

// Config is the effective jailkit configuration for one shell_user event
// after server defaults, site overrides and the PHP jk section have been
// merged. Sections/Programs/Cron are whitespace-split tokens ready for
// jk_init / jk_cp / the hash.
type Config struct {
	Home               string   // jailkit_chroot_home with [username] still in place
	Sections           []string // jk_init section names
	Programs           []string // extra programs for jk_cp
	CronPrograms       []string // programs the cron jail also needs
	Hardlinks          string   // "yes" | "allow" | anything else
	AuthorizedKeysTmpl string   // path seeded into a fresh authorized_keys
	DoNotRemovePaths   []string // paths preserved on unused-jail teardown
}

// MergeConfig builds the effective jail config for a website. Non-empty
// web_domain.jailkit_chroot_app_sections / jailkit_chroot_app_programs
// replace the server defaults; php_jk_section (when set) is appended to
// the sections list and the result is deduplicated and sorted, matching
// shelluser_jailkit_plugin insert/update.
func MergeConfig(server getconf.JailkitConfig, web system.Row) Config {
	cfg := Config{
		Home:               server.ChrootHome,
		Hardlinks:          server.Hardlinks,
		AuthorizedKeysTmpl: server.ChrootAuthorizedKeysTmpl,
		CronPrograms:       splitTokens(server.ChrootCronPrograms),
	}
	if cfg.Home == "" {
		cfg.Home = getconf.DefaultJailkitConfig().ChrootHome
	}

	sections := server.ChrootAppSections
	if s := web.Str("jailkit_chroot_app_sections"); s != "" {
		sections = s
	}
	programs := server.ChrootAppPrograms
	if p := web.Str("jailkit_chroot_app_programs"); p != "" {
		programs = p
	}
	if s := web.Str("jailkit_do_not_remove_paths"); s != "" {
		cfg.DoNotRemovePaths = splitTokens(s)
	}

	cfg.Sections = splitTokens(sections)
	cfg.Programs = splitTokens(programs)

	// PHP only unique+sorts when a php_jk_section is present; the hash path
	// always unique+sorts the combined list, so we unique+sort sections here
	// whenever the PHP section is added and leave plain overrides alone.
	if php := strings.TrimSpace(web.Str("php_jk_section")); php != "" {
		cfg.Sections = uniqueSorted(append(cfg.Sections, php))
	}
	return cfg
}

// Hash returns the md5 of the sorted unique set of sections + programs +
// cron programs (port of _setup_jailkit_chroot $update_hash). Equal hashes
// skip a force-update of an existing jail.
func Hash(cfg Config) string {
	all := uniqueSorted(append(append(append([]string{}, cfg.Sections...), cfg.Programs...), cfg.CronPrograms...))
	sum := md5.Sum([]byte(strings.Join(all, " ")))
	return hex.EncodeToString(sum[:])
}

// HomeOf substitutes [username] in the configured chroot home template.
func HomeOf(cfg Config, username string) string {
	return strings.ReplaceAll(cfg.Home, "[username]", username)
}

// HardlinkOpts returns the jk_init / jk_cp option tokens derived from
// jailkit_hardlinks. PHP only sets "hardlink" when the value is exactly
// "yes"; an unset key gets "allow_hardlink", a configured non-yes value
// (the default "allow") leaves the options empty.
func HardlinkOpts(cfg Config) []string {
	switch cfg.Hardlinks {
	case "yes":
		return []string{"hardlink"}
	case "":
		return []string{"allow_hardlink"}
	default:
		return nil
	}
}

// splitTokens splits a space/comma-separated config string and drops empties.
func splitTokens(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ','
	})
	out := fields[:0]
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// uniqueSorted returns the sorted unique set of tokens (PHP array_unique +
// sort SORT_STRING).
func uniqueSorted(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
