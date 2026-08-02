package clients

import "testing"

func TestNewSMTPMailer(t *testing.T) {
	if m := NewSMTPMailer("", 0, "", "", ""); m != nil {
		t.Fatal("empty host must yield a nil mailer (SMTP not configured)")
	}
	m := NewSMTPMailer("mail.example.com", 0, "u", "p", "")
	sm, ok := m.(*SMTPMailer)
	if !ok || sm.Addr != "mail.example.com:25" || sm.From != "ispconfig@localhost" {
		t.Fatalf("defaults not applied: %+v", m)
	}
}
