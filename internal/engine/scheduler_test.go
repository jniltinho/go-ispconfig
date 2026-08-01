package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

func schedDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)
	return db
}

func TestSchedulerRegister(t *testing.T) {
	s := NewScheduler(schedDB(t), nil)

	require.NoError(t, s.Register("nightly", "0 3 * * *", func(context.Context) error { return nil }))
	require.NoError(t, s.Register("daily", "@daily", func(context.Context) error { return nil }))

	err := s.Register("nightly", "@hourly", func(context.Context) error { return nil })
	require.ErrorContains(t, err, "already registered")

	err = s.Register("broken", "not a cron spec", func(context.Context) error { return nil })
	require.ErrorContains(t, err, "parsing cron spec")

	jobs := s.Jobs(context.Background())
	require.Len(t, jobs, 2)
	require.Equal(t, "nightly", jobs[0].Name)
	require.Equal(t, "0 3 * * *", jobs[0].Spec)
}

func TestSchedulerDatalogPruningSpec(t *testing.T) {
	s := NewScheduler(schedDB(t), nil)
	require.NoError(t, s.RegisterDatalogPruning(1, 30))
	jobs := s.Jobs(context.Background())
	require.Len(t, jobs, 1)
	require.Equal(t, "datalog_prune", jobs[0].Name)
	require.Equal(t, "0 3 * * *", jobs[0].Spec)
}
