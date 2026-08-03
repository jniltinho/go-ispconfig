package cron

import "testing"

func TestValidateCommand(t *testing.T) {
	tests := []struct {
		name    string
		command string
		domain  string
		valid   bool
	}{
		{name: "plain command", command: "/usr/bin/php script.php", valid: true},
		{name: "https URL", command: "https://example.com/cron", valid: true},
		{name: "http URL with domain placeholder", command: "http://{DOMAIN}/cron", domain: "example.com", valid: true},
		{name: "unsupported URL scheme", command: "ftp://example.com/file", valid: false},
		{name: "bad URL hostname", command: "https://not a host/path", valid: false},
		{name: "unexpanded placeholder", command: "https://{DOMAIN}/cron", valid: false},
		{name: "backslash URL", command: "https://example.com/cron\\x", valid: false},
		{name: "newline", command: "echo ok\necho bad", valid: false},
		{name: "carriage return", command: "echo ok\recho bad", valid: false},
		{name: "nul", command: "echo\x00bad", valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidateCommand(test.command, test.domain) == nil; got != test.valid {
				t.Fatalf("ValidateCommand(%q, %q) valid = %v, want %v", test.command, test.domain, got, test.valid)
			}
		})
	}
}
