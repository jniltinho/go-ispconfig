package jailkit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-ispconfig/internal/system"
)

// setupChroot creates or force-updates the jail under u.dir and stamps
// last_jailkit_hash when the config set actually changed (port of
// _setup_jailkit_chroot).
func (p *Plugin) setupChroot(ctx context.Context, u, web system.Row, cfg Config, serverID uint32) error {
	dir := u.Str("dir")
	if dir == "" || dir == "/" {
		return fmt.Errorf("jailkit: refusing chroot on empty or root path")
	}
	hash := Hash(cfg)
	opts := HardlinkOpts(cfg)

	if _, err := os.Stat(jailEtcPath(dir)); os.IsNotExist(err) {
		if err := p.createChroot(ctx, dir, cfg.Sections, opts); err != nil {
			return err
		}
		if err := p.addPrograms(ctx, dir, cfg.Programs, opts); err != nil {
			return err
		}
		if err := p.writeMOTD(dir, web.Str("domain")); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("jailkit: stat %s: %w", jailEtcPath(dir), err)
	} else {
		// Existing jail: skip rebuild when the hash matches.
		if hash == web.Str("last_jailkit_hash") {
			p.log.Debug("jailkit: hash unchanged, skipping chroot rebuild",
				"dir", dir, "hash", hash)
			return nil
		}
		force := append(append([]string{}, opts...), "force")
		folders, err := p.ListWebFolders(u.Num("parent_domain_id"), dir, serverID)
		if err != nil {
			return err
		}
		for _, f := range folders {
			force = append(force, "skip="+strings.TrimPrefix(f, "/"))
		}
		if err := p.updateChroot(ctx, dir, cfg, force, web.Str("php_cli_binary")); err != nil {
			return err
		}
	}
	return p.StampHash(dir, hash)
}

// createChroot runs jk_init for the configured sections and prepares the
// standard tmp / var/run / var/tmp layout (port of create_jailkit_chroot).
func (p *Plugin) createChroot(ctx context.Context, dir string, sections, opts []string) error {
	if err := system.Chown(ctx, p.runner, dir, "root", "root", false); err != nil {
		return err
	}
	args := jkInitArgs(dir, sections, opts)
	if out, err := p.runner.Run(ctx, "jk_init", args...); err != nil {
		return fmt.Errorf("jailkit: jk_init %s: %w: %s", dir, err, out)
	}
	p.log.Info("jailkit: added jailkit chroot", "dir", dir)
	return ensureJailLayout(dir)
}

// addPrograms copies each configured program into the jail with jk_cp
// (port of _add_jailkit_programs / create_jailkit_programs). Missing paths
// on the host are skipped — the config may list tools that are not installed.
func (p *Plugin) addPrograms(ctx context.Context, dir string, programs, opts []string) error {
	for _, prog := range programs {
		if prog == "" {
			continue
		}
		if info, err := os.Stat(prog); err != nil || !(info.Mode().IsRegular() || info.IsDir()) {
			// Relative names (lesspipe, unzip, …) are looked up later by the
			// operator's PATH at jail build time; still pass them to jk_cp so
			// the command argv matches PHP (which also checks is_file/is_dir
			// and only copies when present).
			if err != nil {
				p.log.Debug("jailkit: program not present on host, skipping", "program", prog)
				continue
			}
		}
		args := jkCPArgs(dir, prog, opts)
		if out, err := p.runner.Run(ctx, "jk_cp", args...); err != nil {
			return fmt.Errorf("jailkit: jk_cp %s into %s: %w: %s", prog, dir, err, out)
		}
		p.log.Debug("jailkit: added program to chroot", "program", prog, "dir", dir)
	}
	return nil
}

// updateChroot force-refreshes an existing jail: jk_update, then re-init
// sections and re-copy programs (simplified port of update_jailkit_chroot
// that keeps the command sequence without the hardlink-hunting side quests).
func (p *Plugin) updateChroot(ctx context.Context, dir string, cfg Config, opts []string, phpCLI string) error {
	if err := system.Chown(ctx, p.runner, dir, "root", "root", false); err != nil {
		return err
	}
	args := []string{"--jail=" + dir}
	for _, o := range opts {
		switch {
		case o == "hardlink" || o == "-k":
			args = append(args, "-k")
		case strings.HasPrefix(o, "skip="):
			args = append(args, "--skip=/"+strings.TrimPrefix(o, "skip="))
		}
	}
	if out, err := p.runner.Run(ctx, "jk_update", args...); err != nil {
		// jk_update often exits non-zero on missing packages; PHP logs and
		// continues, so we do the same.
		p.log.Warn("jailkit: jk_update reported an error", "dir", dir,
			"err", err, "output", string(out))
	}
	// Reinstall sections + programs (create_jailkit_chroot is idempotent
	// under force).
	forceOpts := withoutSkips(opts)
	if err := p.createChroot(ctx, dir, cfg.Sections, append(forceOpts, "force")); err != nil {
		return err
	}
	programs := append(append([]string{}, cfg.Programs...), cfg.CronPrograms...)
	if phpCLI != "" {
		programs = append(programs, phpCLI)
	}
	if err := p.addPrograms(ctx, dir, programs, forceOpts); err != nil {
		return err
	}
	return ensureJailLayout(dir)
}

// writeMOTD drops a short login banner under <dir>/var/run/motd.
func (p *Plugin) writeMOTD(dir, domain string) error {
	motd := filepath.Join(dir, "var", "run", "motd")
	if err := os.MkdirAll(filepath.Dir(motd), 0o755); err != nil {
		return fmt.Errorf("jailkit: mkdir for motd: %w", err)
	}
	_ = os.Remove(motd) // drop a previous file or symlink
	content := fmt.Sprintf("Welcome to %s\n", domain)
	if err := os.WriteFile(motd, []byte(content), 0o644); err != nil {
		return fmt.Errorf("jailkit: writing motd: %w", err)
	}
	return nil
}

// ensureJailLayout creates the tmp / var/run / var/tmp directories every jail
// needs and locks down /bin permissions.
func ensureJailLayout(dir string) error {
	for _, d := range []struct {
		path string
		mode os.FileMode
	}{
		{filepath.Join(dir, "tmp"), 0o770},
		{filepath.Join(dir, "var", "run"), 0o755},
		{filepath.Join(dir, "var", "tmp"), 0o770},
	} {
		if err := os.MkdirAll(d.path, d.mode); err != nil {
			return fmt.Errorf("jailkit: mkdir %s: %w", d.path, err)
		}
		if err := os.Chmod(d.path, d.mode); err != nil {
			return fmt.Errorf("jailkit: chmod %s: %w", d.path, err)
		}
	}
	bin := filepath.Join(dir, "bin")
	if info, err := os.Stat(bin); err == nil && info.IsDir() {
		if err := os.Chmod(bin, 0o755); err != nil {
			return fmt.Errorf("jailkit: chmod %s: %w", bin, err)
		}
	}
	return nil
}

// jkInitArgs builds the argv of `jk_init -c … -j <dir> <sections…>`.
func jkInitArgs(dir string, sections, opts []string) []string {
	args := []string{}
	for _, o := range opts {
		switch o {
		case "hardlink", "-k":
			args = append(args, "-k")
		case "force", "-f":
			args = append(args, "-f")
		}
	}
	args = append(args, "-c", "/etc/jailkit/jk_init.ini", "-j", dir)
	for _, s := range sections {
		if s != "" {
			args = append(args, s)
		}
	}
	return args
}

// jkCPArgs builds the argv of `jk_cp -j <dir> <program>`.
func jkCPArgs(dir, program string, opts []string) []string {
	args := []string{}
	for _, o := range opts {
		switch o {
		case "hardlink", "-k":
			args = append(args, "-k")
		case "force", "-f":
			args = append(args, "-f")
		}
	}
	return append(args, "-j", dir, program)
}

// withoutSkips drops skip= tokens so re-init does not inherit them.
func withoutSkips(opts []string) []string {
	out := opts[:0:0]
	for _, o := range opts {
		if strings.HasPrefix(o, "skip=") {
			continue
		}
		out = append(out, o)
	}
	return out
}
