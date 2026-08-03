package cron

import (
	"testing"

	"go-ispconfig/internal/model"
)

func TestDeriveType(t *testing.T) {
	tests := []struct {
		name       string
		command    string
		limitType  string
		adminOwned bool
		want       string
	}{
		{name: "https forces url", command: "https://example.com/job", limitType: "full", want: model.CronTypeURL},
		{name: "http forces url case insensitive", command: "HTTP://example.com/job", limitType: "chrooted", want: model.CronTypeURL},
		{name: "full limit", command: "/usr/bin/php job.php", limitType: "full", want: model.CronTypeFull},
		{name: "chrooted limit", command: "/usr/bin/php job.php", limitType: "chrooted", want: model.CronTypeChrooted},
		{name: "admin owned", command: "/usr/bin/php job.php", adminOwned: true, want: model.CronTypeFull},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := DeriveType(test.command, test.limitType, test.adminOwned); got != test.want {
				t.Fatalf("DeriveType = %q, want %q", got, test.want)
			}
		})
	}
}
