package mail

import (
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"go-ispconfig/internal/engine"
	"go-ispconfig/internal/getconf"
)

// transportUpdate queues a delayed postfix reload so transport map
// changes are picked up (PHP reloads immediately; the delayed registry
// batches within one datalog run).
func (p *Plugin) transportUpdate(_ context.Context, _ engine.Data) error {
	p.services.RestartServiceDelayed(PostfixService, engine.ActionReload)
	return nil
}

// welcomeTemplate is ISPConfig's stock welcome mail (conf/mail/
// welcome_email_en.txt): line 1 From:, line 2 Subject:, rest body.
// conf-custom overrides are not ported (translations are a non-goal).
//
//go:embed welcome_email_en.txt
var welcomeTemplate string

// emailRe extracts the sender address for the envelope from (PHP parity).
var emailRe = regexp.MustCompile(`(?i)\b([A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,63})\b`)

// sendWelcomeMail renders and submits the welcome message for a new
// mailbox when globally enabled. Best effort: failures are logged and
// never fail the mailbox creation. The daemon only runs on master
// servers (mirror_server_id = 0 is enforced at startup), so the PHP
// mirror gate is implicit.
func (p *Plugin) sendWelcomeMail(ctx context.Context, email string) {
	global, err := p.globalMailConfig(ctx)
	if err != nil {
		p.log.Warn("mail: could not load global mail config, welcome mail skipped", "error", err)
		return
	}
	if global["enable_welcome_mail"] != "y" {
		return
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return
	}
	adminMail := global["admin_mail"]
	if adminMail == "" {
		adminMail = "root"
	}
	repl := strings.NewReplacer(
		"{domain}", email[at+1:],
		"{email}", email,
		"{admin_mail}", adminMail,
		"{admin_name}", global["admin_name"],
	)
	lines := strings.Split(repl.Replace(welcomeTemplate), "\n")
	if len(lines) < 2 {
		return
	}
	from := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[0]), "From:"))
	subject := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[1]), "Subject:"))
	body := strings.TrimSpace(strings.Join(lines[2:], "\n"))

	msg := "MIME-Version: 1.0\n" +
		"Content-Type: text/plain; charset=utf-8\n" +
		"Content-Transfer-Encoding: 8bit\n" +
		"From: " + from + "\n" +
		"Reply-To: " + from + "\n" +
		"To: " + email + "\n" +
		"Subject: =?utf-8?B?" + base64.StdEncoding.EncodeToString([]byte(subject)) + "?=\n\n" +
		body + "\n"

	envelopeFrom := ""
	if m := emailRe.FindStringSubmatch(from); m != nil {
		envelopeFrom = m[1]
	}
	send := p.Sendmail
	if send == nil {
		send = p.execSendmail
	}
	if err := send(ctx, envelopeFrom, email, []byte(msg)); err != nil {
		p.log.Warn("mail: welcome mail not sent", "email", email, "error", err)
		return
	}
	p.log.Debug("mail: welcome mail submitted", "email", email)
}

// globalMailConfig loads the [mail] section of the panel-wide sys_ini.
func (p *Plugin) globalMailConfig(_ context.Context) (map[string]string, error) {
	if p.LoadGlobalConfig != nil {
		return p.LoadGlobalConfig()
	}
	if p.db == nil {
		return nil, fmt.Errorf("mail: no database handle for global config")
	}
	sections, err := getconf.GetGlobalConfig(p.db)
	if err != nil {
		return nil, err
	}
	return sections["mail"], nil
}

// execSendmail pipes the message into the configured sendmail binary.
func (p *Plugin) execSendmail(ctx context.Context, envelopeFrom, to string, msg []byte) error {
	cfg, err := p.config(ctx)
	if err != nil {
		return err
	}
	path := cfg.SendmailPath
	if path == "" {
		path = "/usr/sbin/sendmail"
	}
	args := []string{"-i", to}
	if envelopeFrom != "" {
		args = append([]string{"-f", envelopeFrom}, args...)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = strings.NewReader(string(msg))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mail: sendmail: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
