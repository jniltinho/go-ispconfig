package cron

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

func TestRegisterRowsFiltersViaCaller(t *testing.T) {
	// LoadActiveJobs itself filters in SQL; registerRows just maps whatever
	// rows it is given — here we verify schedule fields and presence.
	r := NewClientJobRunner(nil)
	t.Cleanup(r.Stop)

	rows := []model.Cron{
		{
			ID: 10, ServerID: 1, Active: "y",
			RunMin: "*/5", RunHour: "*", RunMday: "*", RunMonth: "*", RunWday: "*",
			Command: "https://a.example/cron", Type: model.CronTypeURL,
		},
		{
			ID: 11, ServerID: 1, Active: "y",
			RunMin: "*", RunHour: "*", RunMday: "*", RunMonth: "@reboot", RunWday: "*",
			Command: "/usr/bin/php job.php", Type: model.CronTypeFull,
		},
	}
	seen := map[uint32]string{}
	n, err := registerRows(r, rows, func(job model.Cron) JobFunc {
		seen[job.ID] = job.Command
		return func(context.Context) {}
	})
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.True(t, r.Has(10))
	assert.True(t, r.Has(11))
	assert.Equal(t, "https://a.example/cron", seen[10])
	assert.Equal(t, "/usr/bin/php job.php", seen[11])
}

func TestRegisterRowsInvalidSchedule(t *testing.T) {
	r := NewClientJobRunner(nil)
	t.Cleanup(r.Stop)
	_, err := registerRows(r, []model.Cron{{
		ID: 1, RunMin: "bad", RunHour: "*", RunMday: "*", RunMonth: "*", RunWday: "*",
	}}, func(model.Cron) JobFunc { return func(context.Context) {} })
	require.Error(t, err)
	assert.False(t, r.Has(1))
}

func TestLoadActiveJobsNilArgs(t *testing.T) {
	r := NewClientJobRunner(nil)
	t.Cleanup(r.Stop)
	_, err := LoadActiveJobs(context.Background(), nil, 1, r, func(model.Cron) JobFunc {
		return func(context.Context) {}
	})
	require.Error(t, err)
	_, err = LoadActiveJobs(context.Background(), nil, 1, nil, nil)
	require.Error(t, err)
}
