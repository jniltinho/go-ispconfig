package clients

import (
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// Mailer submits one plain-text email. The API uses it for the client
// messaging endpoints and the welcome-on-create hook; tests inject a
// fake. A nil Mailer means "SMTP not configured".
type Mailer interface {
	Send(to, subject, body string) error
}

// SMTPMailer is the stdlib net/smtp transport (STARTTLS when offered,
// PLAIN auth when a user is configured).
type SMTPMailer struct {
	Addr string // host:port
	User string
	Pass string
	From string
}

// NewSMTPMailer builds the transport from config values; nil when no
// host is configured (messaging endpoints then refuse to send).
func NewSMTPMailer(host string, port int, user, pass, from string) Mailer {
	if host == "" {
		return nil
	}
	if port == 0 {
		port = 25
	}
	if from == "" {
		from = "ispconfig@localhost"
	}
	return &SMTPMailer{Addr: fmt.Sprintf("%s:%d", host, port), User: user, Pass: pass, From: from}
}

// sanitizeHeader strips CR/LF so template- or API-supplied values can
// never inject additional SMTP headers.
func sanitizeHeader(v string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(v)
}

// Send submits one message via SMTP.
func (m *SMTPMailer) Send(to, subject, body string) error {
	to = sanitizeHeader(to)
	msg := strings.NewReplacer("\n", "\r\n").Replace(
		"From: " + sanitizeHeader(m.From) + "\nTo: " + to + "\nSubject: " + sanitizeHeader(subject) +
			"\nMIME-Version: 1.0\nContent-Type: text/plain; charset=utf-8\n\n" + body)
	var auth smtp.Auth
	if m.User != "" {
		host, _, err := net.SplitHostPort(m.Addr)
		if err != nil {
			host = m.Addr
		}
		auth = smtp.PlainAuth("", m.User, m.Pass, host)
	}
	if err := smtp.SendMail(m.Addr, auth, m.From, []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("clients: sending mail to %s: %w", to, err)
	}
	return nil
}
