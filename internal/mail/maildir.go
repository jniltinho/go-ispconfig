package mail

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// dovecotFolders are the standard IMAP folders provisioned on create
// (mail_plugin parity: Sent/Drafts/Trash/Junk plus the INBOX maildir).
var dovecotFolders = []string{"Sent", "Drafts", "Trash", "Junk"}

// userInsert provisions the mailbox on disk (port of
// mail_plugin::user_insert; the welcome mail is task 3.5).
func (p *Plugin) userInsert(ctx context.Context, data engine.Data) error {
	return p.provisionMailbox(ctx, row(data.New))
}

// provisionMailbox is the shared create/repair path of user_insert and
// user_update: resolve uid/gid, ensure the domain base dir, build the
// maildir layout (or mdbox via doveadm), quarantine corrupt trees and
// chown recursively.
func (p *Plugin) provisionMailbox(ctx context.Context, newRow row) error {
	cfg, err := p.config(ctx)
	if err != nil {
		p.log.Warn("mail: using default [mail] config", "error", err)
	}
	maildir := newRow.str("maildir")
	if maildir == "" {
		return nil
	}
	uid, gid := p.resolveUIDGID(ctx, cfg, newRow)
	owner := strconv.FormatInt(uid, 10) + ":" + strconv.FormatInt(gid, 10)

	// Domain base dir: 0770 vmail:vmail (group access — subfolder users
	// may differ from vmail).
	basePath := filepath.Dir(maildir)
	if basePath != "" && !isDir(basePath) {
		if err := p.mkdirOwned(ctx, basePath, 0o770, cfg.MailuserName+":"+cfg.MailuserGroup); err != nil {
			return err
		}
	}

	if newRow.str("maildir_format") == "mdbox" {
		return p.mdboxCreate(ctx, newRow.str("email"))
	}

	// Dovecot keeps the mailbox in a separate Maildir/ subdirectory.
	mailboxPath := maildir
	if cfg.POP3IMAPDaemon == "dovecot" {
		if err := p.mkdirOwned(ctx, maildir, 0o700, owner); err != nil {
			return err
		}
		mailboxPath = maildir + "/Maildir"
	}

	// Existing dir that is not a valid maildir → quarantine it.
	if isDir(mailboxPath) && !isDir(mailboxPath+"/new") && !isDir(mailboxPath+"/cur") {
		if err := p.quarantineMaildir(ctx, cfg, maildir, newRow.num("mailuser_id")); err != nil {
			return err
		}
	}

	if !isDir(mailboxPath + "/new") {
		if err := p.maildirmake(ctx, cfg, mailboxPath, "", owner); err != nil {
			return err
		}
	}
	for _, folder := range dovecotFolders {
		if !isDir(mailboxPath + "/." + folder) {
			if err := p.maildirmake(ctx, cfg, mailboxPath, folder, owner); err != nil {
				return err
			}
		}
	}

	// Recursive ownership over the whole mailbox tree (PHP chown -R).
	if _, err := p.runner.Run(ctx, "chown", "-R", owner, maildir); err != nil {
		p.log.Error("mail: chown -R failed", "path", maildir, "error", err)
	}
	p.log.Debug("mail: provisioned maildir", "path", maildir, "owner", owner)
	return nil
}

// maildirmake creates a maildir (or a .subfolder) with cur/new/tmp and
// registers the subfolder in Dovecot's subscriptions file (pure-Go port
// of system.inc.php maildirmake; layout identical).
func (p *Plugin) maildirmake(ctx context.Context, cfg getconf.MailConfig, maildirPath, subfolder, owner string) error {
	dir := maildirPath
	if subfolder != "" {
		dir = maildirPath + "/." + subfolder
	}
	for _, d := range []string{dir, dir + "/cur", dir + "/new", dir + "/tmp"} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("mail: creating %s: %w", d, err)
		}
		if err := os.Chmod(d, 0o700); err != nil {
			return err
		}
		if _, err := p.runner.Run(ctx, "chown", owner, d); err != nil {
			p.log.Error("mail: chown failed", "path", d, "error", err)
		}
	}
	if subfolder != "" && cfg.POP3IMAPDaemon == "dovecot" {
		if err := p.subscribeFolder(ctx, cfg, maildirPath, subfolder); err != nil {
			return err
		}
	}
	return nil
}

// subscribeFolder appends the folder to <maildir>/subscriptions once
// (0744 vmail:vmail, one folder name per line — Dovecot format).
func (p *Plugin) subscribeFolder(ctx context.Context, cfg getconf.MailConfig, maildirPath, subfolder string) error {
	file := maildirPath + "/subscriptions"
	raw, err := os.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if line == subfolder {
			return nil
		}
	}
	content := string(raw)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += subfolder + "\n"
	if err := os.WriteFile(file, []byte(content), 0o744); err != nil {
		return err
	}
	if _, err := p.runner.Run(ctx, "chown", cfg.MailuserName+":"+cfg.MailuserGroup, file); err != nil {
		p.log.Error("mail: chown failed", "path", file, "error", err)
	}
	return nil
}

// quarantineMaildir moves an invalid maildir tree under
// homedir_path/corrupted/<mailuser_id> before a clean one is created.
func (p *Plugin) quarantineMaildir(ctx context.Context, cfg getconf.MailConfig, maildir string, mailuserID int64) error {
	corrupted := fmt.Sprintf("%s/corrupted/%d", cfg.HomedirPath, mailuserID)
	if !isDir(corrupted) {
		if err := p.mkdirOwned(ctx, corrupted, 0o700, cfg.MailuserName+":"+cfg.MailuserGroup); err != nil {
			return err
		}
	}
	if _, err := p.runner.Run(ctx, "mv", "-f", maildir, corrupted); err != nil {
		return fmt.Errorf("mail: quarantining %s: %w", maildir, err)
	}
	p.log.Warn("mail: moved invalid maildir to corrupted folder", "path", maildir, "quarantine", corrupted)
	return nil
}

// mdboxCreate provisions an mdbox mailbox through doveadm (PHP runs the
// same commands via su; the daemon is root, doveadm -u suffices).
func (p *Plugin) mdboxCreate(ctx context.Context, email string) error {
	for _, action := range []string{"create", "subscribe"} {
		for _, box := range []string{"INBOX", "Sent", "Trash", "Junk", "Drafts"} {
			if _, err := p.runner.Run(ctx, "doveadm", "mailbox", action, "-u", email, box); err != nil {
				p.log.Error("mail: doveadm failed", "action", action, "mailbox", box, "user", email, "error", err)
			}
		}
	}
	return nil
}

// mkdirOwned creates the path (all parents) with the mode and chowns it.
func (p *Plugin) mkdirOwned(ctx context.Context, path string, mode os.FileMode, owner string) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("mail: creating %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	if _, err := p.runner.Run(ctx, "chown", owner, path); err != nil {
		p.log.Error("mail: chown failed", "path", path, "error", err)
	}
	return nil
}

// isDir reports whether path exists and is a directory.
func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
