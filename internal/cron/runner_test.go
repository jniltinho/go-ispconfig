package cron

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	robfig "github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeExpression(t *testing.T) {
	tests := []struct {
		name                         string
		min, hour, mday, month, wday string
		want                         string
	}{
		{
			name:  "five field every five minutes",
			min:   "*/5",
			hour:  "*",
			mday:  "*",
			month: "*",
			wday:  "*",
			want:  "*/5 * * * *",
		},
		{
			name:  "strips spaces per field",
			min:   "0, 30",
			hour:  "1, 2",
			mday:  "*",
			month: "1 - 6",
			wday:  "0, 1",
			want:  "0,30 1,2 * 1-6 0,1",
		},
		{
			name:  "reboot",
			min:   "*",
			hour:  "*",
			mday:  "*",
			month: "@reboot",
			wday:  "*",
			want:  "@reboot",
		},
		{
			name:  "reboot with spaces in month token",
			min:   "*",
			hour:  "*",
			mday:  "*",
			month: " @reboot ",
			wday:  "*",
			want:  "@reboot",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComposeExpression(tt.min, tt.hour, tt.mday, tt.month, tt.wday)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestClientJobRunnerAddReplaceRemove(t *testing.T) {
	r := NewClientJobRunner(nil)
	t.Cleanup(r.Stop)

	var hits atomic.Int32
	job := Job{
		ID:       42,
		RunMin:   "*/5",
		RunHour:  "*",
		RunMday:  "*",
		RunMonth: "*",
		RunWday:  "*",
		Run:      func(context.Context) { hits.Add(1) },
	}
	require.NoError(t, r.Add(job))
	assert.True(t, r.Has(42))
	assert.Equal(t, 1, r.Len())
	assert.Equal(t, "*/5 * * * *", ExpressionOf(job))

	// Replace keeps a single entry for the id.
	require.NoError(t, r.Replace(Job{
		ID:       42,
		RunMin:   "0",
		RunHour:  "*",
		RunMday:  "*",
		RunMonth: "*",
		RunWday:  "*",
		Run:      func(context.Context) { hits.Add(1) },
	}))
	assert.True(t, r.Has(42))
	assert.Equal(t, 1, r.Len())

	r.Remove(42)
	assert.False(t, r.Has(42))
	assert.Equal(t, 0, r.Len())
}

func TestClientJobRunnerRejectsInvalidExpression(t *testing.T) {
	r := NewClientJobRunner(nil)
	t.Cleanup(r.Stop)
	err := r.Add(Job{
		ID:       1,
		RunMin:   "not-a-cron",
		RunHour:  "*",
		RunMday:  "*",
		RunMonth: "*",
		RunWday:  "*",
		Run:      func(context.Context) {},
	})
	require.Error(t, err)
	assert.False(t, r.Has(1))
}

func TestClientJobRunnerRejectsMissingIDOrRun(t *testing.T) {
	r := NewClientJobRunner(nil)
	t.Cleanup(r.Stop)
	require.Error(t, r.Add(Job{ID: 0, RunMin: "*", RunHour: "*", RunMday: "*", RunMonth: "*", RunWday: "*", Run: func(context.Context) {}}))
	require.Error(t, r.Add(Job{ID: 1, RunMin: "*", RunHour: "*", RunMday: "*", RunMonth: "*", RunWday: "*"}))
}

func TestClientJobRunnerRebootRunsOnceOnStart(t *testing.T) {
	r := NewClientJobRunner(nil)
	t.Cleanup(r.Stop)

	var hits atomic.Int32
	done := make(chan struct{})
	require.NoError(t, r.Add(Job{
		ID:       7,
		RunMin:   "*",
		RunHour:  "*",
		RunMday:  "*",
		RunMonth: "@reboot",
		RunWday:  "*",
		Run: func(context.Context) {
			hits.Add(1)
			close(done)
		},
	}))
	assert.True(t, r.Has(7))
	assert.Equal(t, 0, int(hits.Load()), "must not fire before Start")

	r.Start()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected @reboot job to run on Start")
	}
	assert.Equal(t, 1, int(hits.Load()))
	// Still registered for the next daemon start; not a recurring entry.
	assert.True(t, r.Has(7))
}

func TestClientJobRunnerRebootRemove(t *testing.T) {
	r := NewClientJobRunner(nil)
	t.Cleanup(r.Stop)
	require.NoError(t, r.Add(Job{
		ID:       9,
		RunMonth: "@reboot",
		RunMin:   "*", RunHour: "*", RunMday: "*", RunWday: "*",
		Run: func(context.Context) {},
	}))
	r.Remove(9)
	assert.False(t, r.Has(9))
}

func TestClientJobRunnerReplaceRebootWithRecurring(t *testing.T) {
	r := NewClientJobRunner(nil)
	t.Cleanup(r.Stop)
	require.NoError(t, r.Add(Job{
		ID: 3, RunMonth: "@reboot",
		RunMin: "*", RunHour: "*", RunMday: "*", RunWday: "*",
		Run: func(context.Context) {},
	}))
	require.NoError(t, r.Replace(Job{
		ID: 3, RunMin: "0", RunHour: "1", RunMday: "*", RunMonth: "*", RunWday: "*",
		Run: func(context.Context) {},
	}))
	assert.True(t, r.Has(3))
	assert.Equal(t, 1, r.Len())
}

// TestNormalizeWday covers the vixie day-7 → robfig 0-6 mapping: every
// composed expression must be accepted by the parser that actually schedules.
func TestNormalizeWday(t *testing.T) {
	tests := []struct{ in, want string }{
		{"*", "*"},
		{"1", "1"},
		{"7", "0"},
		{"1,7", "1,0"},
		{"0-7", "0-6,0"},
		{"6-7", "6-6,0"},
		{"1-7/2", "1-6/2,0"},
		{"*/7", "*/7"},
		{"7/2", "0"},
	}
	for _, test := range tests {
		if got := normalizeWday(test.in); got != test.want {
			t.Errorf("normalizeWday(%q) = %q, want %q", test.in, got, test.want)
		}
		expr := ComposeExpression("0", "0", "*", "*", test.in)
		if _, err := robfig.ParseStandard(expr); err != nil {
			t.Errorf("ParseStandard(%q) from run_wday=%q: %v", expr, test.in, err)
		}
	}
}
