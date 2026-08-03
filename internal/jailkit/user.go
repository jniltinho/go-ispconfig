package jailkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-ispconfig/internal/system"
)

// myCNF is the minimal .my.cnf a jailed MySQL client gets (port of
// user_my.cnf.master: defaults that keep the client talking to localhost).
const myCNF = `[client]
# Managed by go-ispconfig jailkit plugin — do not edit by hand.
`

// addUser relocates the shell account into the jail and creates its home
// (port of _add_jailkit_user + system::create_jailkit_user).
func (p *Plugin) addUser(ctx context.Context, u, old system.Row, cfg Config) error {
	username, puser, pgroup := u.Str("username"), u.Str("puser"), u.Str("pgroup")
	dir := u.Str("dir")
	userHome := HomeOf(cfg, username)
	puserHome := HomeOf(cfg, puser)
	userHomeOld := ""
	if old != nil {
		userHomeOld = HomeOf(cfg, old.Str("username"))
	}

	// ALWAYS re-jail the user (PHP comment: a no-shell → jailkit update would
	// otherwise leave the account with full root access).
	dotHome := filepath.Join(dir, "."+strings.TrimPrefix(userHome, "/"))
	if err := os.MkdirAll(filepath.Join(dir, "etc"), 0o755); err != nil {
		return fmt.Errorf("jailkit: mkdir %s/etc: %w", dir, err)
	}
	passwd := filepath.Join(dir, "etc", "passwd")
	if _, err := os.Stat(passwd); os.IsNotExist(err) {
		if err := os.WriteFile(passwd, nil, 0o644); err != nil {
			return fmt.Errorf("jailkit: touch %s: %w", passwd, err)
		}
	}

	// Shadow home path the OS usermod --home points at: dir/.<userhome>.
	if err := system.MkdirPath(ctx, p.runner, dotHome, 0o755, username, ""); err != nil {
		// Ownership may fail in tests with a fake runner that does not chown
		// for real; mkdir itself is what matters.
		if _, statErr := os.Stat(dotHome); statErr != nil {
			return err
		}
	}
	if out, err := p.runner.Run(ctx, "usermod", "--home="+dotHome, username); err != nil {
		p.log.Debug("jailkit: usermod --home", "username", username, "err", err, "output", string(out))
	}
	shell := u.Str("shell")
	if shell == "" {
		shell = "/bin/bash"
	}
	if out, err := p.runner.Run(ctx, "jk_jailuser", "-n", "-s", shell, "-j", dir, username); err != nil {
		return fmt.Errorf("jailkit: jk_jailuser %s: %w: %s", username, err, out)
	}
	if puser != "" {
		pDot := filepath.Join(dir, "."+strings.TrimPrefix(puserHome, "/"))
		if out, err := p.runner.Run(ctx, "usermod", "--home="+pDot, puser); err != nil {
			p.log.Debug("jailkit: usermod --home parent", "puser", puser, "err", err, "output", string(out))
		}
	}

	// Effective shell for the OS account: inactive → /bin/false.
	loginShell := jkChrootShell
	if u.Str("active") != "y" {
		loginShell = "/bin/false"
	}
	jailedHome := dir + "/." + strings.TrimPrefix(userHome, "/")
	if out, err := p.runner.Run(ctx, "usermod", "-d", jailedHome, "-s", loginShell, username); err != nil {
		return fmt.Errorf("jailkit: usermod %s: %w: %s", username, err, out)
	}
	if puser != "" {
		pJailed := dir + "/." + strings.TrimPrefix(puserHome, "/")
		if out, err := p.runner.Run(ctx, "usermod", "-d", pJailed, "-s", jkChrootShell, puser); err != nil {
			p.log.Debug("jailkit: usermod parent shell", "puser", puser, "err", err, "output", string(out))
		}
	}

	// Jailed home directory (inside the chroot tree).
	homePath := filepath.Join(dir, strings.TrimPrefix(userHome, "/"))
	if _, err := os.Stat(homePath); os.IsNotExist(err) {
		if userHomeOld != "" {
			oldPath := filepath.Join(u.Str("dir"), strings.TrimPrefix(userHomeOld, "/"))
			if old != nil {
				oldPath = filepath.Join(old.Str("dir"), strings.TrimPrefix(userHomeOld, "/"))
			}
			if info, err := os.Stat(oldPath); err == nil && info.IsDir() {
				if err := os.Rename(oldPath, homePath); err != nil {
					return fmt.Errorf("jailkit: renaming home %s → %s: %w", oldPath, homePath, err)
				}
			} else if err := system.MkdirPath(ctx, p.runner, homePath, 0o750, "", ""); err != nil {
				return err
			}
		} else if err := system.MkdirPath(ctx, p.runner, homePath, 0o750, "", ""); err != nil {
			return err
		}
	}
	if err := system.Chown(ctx, p.runner, homePath, username, pgroup, false); err != nil {
		return err
	}

	// Parent user home inside the jail.
	pHomePath := filepath.Join(dir, strings.TrimPrefix(puserHome, "/"))
	if err := os.MkdirAll(pHomePath, 0o750); err != nil {
		return fmt.Errorf("jailkit: mkdir %s: %w", pHomePath, err)
	}
	if err := system.Chown(ctx, p.runner, pHomePath, puser, pgroup, false); err != nil {
		return err
	}

	// .my.cnf so a jailed mysql client has a config file.
	mycnf := filepath.Join(homePath, ".my.cnf")
	if _, err := os.Stat(mycnf); os.IsNotExist(err) {
		if err := os.WriteFile(mycnf, []byte(myCNF), 0o600); err != nil {
			return fmt.Errorf("jailkit: writing %s: %w", mycnf, err)
		}
		if err := system.Chown(ctx, p.runner, mycnf, username, pgroup, false); err != nil {
			return err
		}
	}
	p.log.Debug("jailkit: added jailkit user home", "home", homePath)
	return nil
}

// setupSSHRSA writes authorized_keys under the jailed home (port of the
// jailkit plugin's private _setup_ssh_rsa). Layout: <dir>/<home>/.ssh.
func (p *Plugin) setupSSHRSA(ctx context.Context, u, old system.Row, cfg Config) error {
	home := filepath.Join(u.Str("dir"), strings.TrimPrefix(HomeOf(cfg, u.Str("username")), "/"))
	sshDir := filepath.Join(home, ".ssh")
	keyFile := filepath.Join(sshDir, "authorized_keys")

	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		if err := system.MkdirPath(ctx, p.runner, sshDir, 0o700, "", ""); err != nil {
			return err
		}
		_ = os.Chmod(sshDir, 0o700)
		// Seed from the template (usually /root/.ssh/authorized_keys).
		tmpl := cfg.AuthorizedKeysTmpl
		if tmpl == "" {
			tmpl = p.RootAuthorizedKeys
		}
		if data, err := os.ReadFile(tmpl); err == nil {
			if err := os.WriteFile(keyFile, data, 0o600); err != nil {
				return fmt.Errorf("jailkit: seeding authorized_keys: %w", err)
			}
		} else if err := os.WriteFile(keyFile, nil, 0o600); err != nil {
			return fmt.Errorf("jailkit: creating authorized_keys: %w", err)
		}
	}

	existing, _ := readKeyLines(keyFile)
	// Drop keys that the previous row had and are no longer present.
	existing = withoutKeys(existing, splitKeyLines(old.Str("ssh_rsa")))
	// Merge the new row's keys.
	keys := dedupeKeys(append(existing, splitKeyLines(u.Str("ssh_rsa"))...))
	content := ""
	if len(keys) > 0 {
		content = joinKeys(keys)
	}
	if err := os.WriteFile(keyFile, []byte(content), 0o600); err != nil {
		return fmt.Errorf("jailkit: writing authorized_keys: %w", err)
	}
	_ = os.Chmod(keyFile, 0o600)
	return system.Chown(ctx, p.runner, sshDir, u.Str("puser"), u.Str("pgroup"), true)
}

// setupShellPHP writes a minimal .bashrc and a PHP CLI symlink under the
// non-jailed home layout PHP still uses for the bashrc path
// (<docroot>/home/<user>/.bashrc). Full alternatives handling is simplified
// to a symlink of php_cli_binary when present.
func (p *Plugin) setupShellPHP(ctx context.Context, u, web system.Row, cfg Config) error {
	docroot := web.Str("document_root")
	username, pgroup := u.Str("username"), u.Str("pgroup")
	// PHP writes bashrc under document_root/home/<user> even for jailkit.
	bashrcDir := filepath.Join(docroot, "home", username)
	if err := os.MkdirAll(bashrcDir, 0o750); err != nil {
		return fmt.Errorf("jailkit: mkdir %s: %w", bashrcDir, err)
	}
	bashrc := filepath.Join(bashrcDir, ".bashrc")
	_ = os.Remove(bashrc)
	body := fmt.Sprintf("# go-ispconfig jailkit bashrc for %s\nexport PATH=\"$HOME/.local/bin:$PATH\"\n",
		web.Str("domain"))
	if err := os.WriteFile(bashrc, []byte(body), 0o644); err != nil {
		return fmt.Errorf("jailkit: writing %s: %w", bashrc, err)
	}
	if err := system.Chown(ctx, p.runner, bashrc, username, pgroup, false); err != nil {
		return err
	}

	phpCLI := web.Str("php_cli_binary")
	if phpCLI == "" {
		return nil
	}
	// Symlink under the jailed home's .local/bin when the binary is in the jail.
	home := filepath.Join(u.Str("dir"), strings.TrimPrefix(HomeOf(cfg, username), "/"))
	localBin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o750); err != nil {
		return fmt.Errorf("jailkit: mkdir %s: %w", localBin, err)
	}
	link := filepath.Join(localBin, "php")
	_ = os.Remove(link)
	// Prefer the path as it appears inside the jail (php_cli_binary is absolute
	// on the host and identical inside after jk_cp).
	if err := os.Symlink(phpCLI, link); err != nil {
		p.log.Debug("jailkit: php symlink", "link", link, "target", phpCLI, "err", err)
	}
	return system.Chown(ctx, p.runner, filepath.Join(home, ".local"), username, pgroup, true)
}

func readKeyLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return splitKeyLines(string(data)), nil
}

func splitKeyLines(blob string) []string {
	var keys []string
	start := 0
	for i := 0; i <= len(blob); i++ {
		if i == len(blob) || blob[i] == '\n' {
			line := blob[start:i]
			// trim \r and spaces
			for len(line) > 0 && (line[0] == ' ' || line[0] == '\t' || line[0] == '\r') {
				line = line[1:]
			}
			for len(line) > 0 && (line[len(line)-1] == ' ' || line[len(line)-1] == '\t' || line[len(line)-1] == '\r') {
				line = line[:len(line)-1]
			}
			if line != "" {
				keys = append(keys, line)
			}
			start = i + 1
		}
	}
	return keys
}

func withoutKeys(keys, drop []string) []string {
	if len(drop) == 0 {
		return keys
	}
	gone := make(map[string]struct{}, len(drop))
	for _, k := range drop {
		gone[k] = struct{}{}
	}
	out := keys[:0:0]
	for _, k := range keys {
		if _, ok := gone[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}

func dedupeKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	out := keys[:0:0]
	for _, k := range keys {
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

func joinKeys(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	n := 0
	for _, k := range keys {
		n += len(k) + 1
	}
	b := make([]byte, 0, n)
	for i, k := range keys {
		if i > 0 {
			b = append(b, '\n')
		}
		b = append(b, k...)
	}
	b = append(b, '\n')
	return string(b)
}
