package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-ispconfig/internal/system"
)

// rootAuthorizedKeys is seeded into a brand-new authorized_keys file so the
// server admin keeps SSH access to the account (ISPConfig behaviour: it is
// copied once, at creation, and never re-applied afterwards).
const rootAuthorizedKeys = "/root/.ssh/authorized_keys"

// setupSSHRSA writes the account's ~/.ssh/authorized_keys: every ssh_rsa key
// of the parent website plus, on a fresh file, the client key and root's own
// key. Port of shelluser_base_plugin::_setup_ssh_rsa.
//
// The keys of the row that is being replaced are dropped first, so editing
// a key in the panel does not leave the previous one behind.
func (p *Plugin) setupSSHRSA(ctx context.Context, u, old system.Row) error {
	homedir := homeOf(u)
	sshDir := filepath.Join(homedir, ".ssh")
	keyFile := filepath.Join(sshDir, "authorized_keys")

	siteKeys, err := p.LoadSiteSSHKeys(u.Num("parent_domain_id"))
	if err != nil {
		return err
	}

	existing, err := readKeys(keyFile)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := system.MkdirPath(ctx, p.runner, sshDir, 0o700, "", ""); err != nil {
			return err
		}
		if err := os.Chmod(sshDir, 0o700); err != nil {
			return fmt.Errorf("shell: chmod %s: %w", sshDir, err)
		}
		clientKey, err := p.LoadClientSSHKey(u.Num("parent_domain_id"))
		if err != nil {
			return err
		}
		adminKeys, _ := readKeys(p.RootAuthorizedKeys)
		existing = append(adminKeys, splitKeys(clientKey)...)
	case err != nil:
		return fmt.Errorf("shell: reading %s: %w", keyFile, err)
	}

	keys := dedupe(append(without(existing, splitKeys(old.Str("ssh_rsa"))), siteKeys...))
	content := ""
	if len(keys) > 0 {
		content = strings.Join(keys, "\n") + "\n"
	}
	if err := os.WriteFile(keyFile, []byte(content), 0o600); err != nil {
		return fmt.Errorf("shell: writing %s: %w", keyFile, err)
	}
	if err := os.Chmod(keyFile, 0o600); err != nil {
		return fmt.Errorf("shell: chmod %s: %w", keyFile, err)
	}
	p.log.Debug("shell: ssh-rsa keys updated", "file", keyFile, "keys", len(keys))
	return system.Chown(ctx, p.runner, sshDir, u.Str("puser"), u.Str("pgroup"), true)
}

// readKeys returns the non-empty, trimmed lines of an authorized_keys file.
func readKeys(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return splitKeys(string(data)), nil
}

// splitKeys turns a blob of newline-separated keys into trimmed lines,
// dropping the blanks (port of file::unix_nl + remove_blank_lines).
func splitKeys(blob string) []string {
	var keys []string
	for line := range strings.SplitSeq(strings.ReplaceAll(blob, "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			keys = append(keys, line)
		}
	}
	return keys
}

// without returns keys with every entry of drop removed.
func without(keys, drop []string) []string {
	if len(drop) == 0 {
		return keys
	}
	gone := make(map[string]struct{}, len(drop))
	for _, k := range drop {
		gone[k] = struct{}{}
	}
	out := keys[:0:0]
	for _, k := range keys {
		if _, found := gone[k]; !found {
			out = append(out, k)
		}
	}
	return out
}

// dedupe drops repeated keys, keeping the first occurrence. PHP's
// array_flip(array_flip()) keeps the last one instead; for a set of
// authorized keys the difference is only line order, which ssh ignores.
func dedupe(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	out := keys[:0:0]
	for _, k := range keys {
		if _, found := seen[k]; found {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

// loadSiteSSHKeys returns the ssh_rsa keys of every shell user of a website.
func (p *Plugin) loadSiteSSHKeys(parentDomainID int64) ([]string, error) {
	if p.db == nil || parentDomainID == 0 {
		return nil, nil
	}
	var blobs []string
	err := p.db.Table("shell_user").Where("parent_domain_id = ?", parentDomainID).
		Pluck("ssh_rsa", &blobs).Error
	if err != nil {
		return nil, fmt.Errorf("shell: loading ssh keys of domain %d: %w", parentDomainID, err)
	}
	var keys []string
	for _, blob := range blobs {
		keys = append(keys, splitKeys(blob)...)
	}
	return keys, nil
}

// loadClientSSHKey returns the ssh_rsa key of the client owning a website
// (web_domain.sys_groupid -> sys_group.client_id -> client.ssh_rsa). PHP
// also generates a keypair for a client that has none; that writes back to
// the database from the daemon and is left to the panel here.
func (p *Plugin) loadClientSSHKey(parentDomainID int64) (string, error) {
	if p.db == nil || parentDomainID == 0 {
		return "", nil
	}
	var key string
	err := p.db.Table("web_domain").
		Joins("JOIN sys_group ON sys_group.groupid = web_domain.sys_groupid").
		Joins("JOIN client ON client.client_id = sys_group.client_id").
		Where("web_domain.domain_id = ?", parentDomainID).
		Pluck("client.ssh_rsa", &key).Error
	if err != nil {
		return "", fmt.Errorf("shell: loading client ssh key of domain %d: %w", parentDomainID, err)
	}
	return key, nil
}
