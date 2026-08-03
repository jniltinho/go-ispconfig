package shell

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBlacklisted covers the ported shelluser_blacklist: every reserved
// system account is denied (case-insensitively, trimmed, as the PHP loop
// compares strtolower(trim(...))), while ordinary customer names pass.
func TestBlacklisted(t *testing.T) {
	denied := []string{
		"root", "daemon", "bin", "sys", "www-data", "wwwrun", "apache",
		"mysql", "postgres", "postfix", "clamav", "amavis", "vmail",
		"getmail", "ispconfig", "courier", "dovecot", "mongodb",
		"nobody", "sshd", "backup", "mail",
	}
	for _, name := range denied {
		assert.Truef(t, Blacklisted(name), "%q must be blacklisted", name)
	}

	assert.True(t, Blacklisted("ROOT"), "comparison is case-insensitive")
	assert.True(t, Blacklisted("Debian-exim"), "mixed-case entry matches as stored")
	assert.True(t, Blacklisted("debian-exim"))
	assert.True(t, Blacklisted("  root \t"), "value is trimmed before matching")

	for _, name := range []string{"alice", "web1", "c1_bob", "rootie", "roo", ""} {
		assert.Falsef(t, Blacklisted(name), "%q must be allowed", name)
	}
}

// TestBlacklistLoaded guards against an empty or truncated embed: the
// ISPConfig3 file ships 35 names.
func TestBlacklistLoaded(t *testing.T) {
	assert.Len(t, blacklist, 35)
}
