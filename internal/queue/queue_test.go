package queue

import (
	"context"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/config"
)

func TestName(t *testing.T) {
	require.Equal(t, "server:1", Name(1))
	require.Equal(t, "server:42", Name(42))
}

func TestWorkerMuxDispatch(t *testing.T) {
	w := NewWorker(config.QueueConfig{Addr: "localhost:6379"}, 1, nil)

	var woke bool
	w.Handle(TypeDatalogReady, func(context.Context, []byte) error {
		woke = true
		return nil
	})
	var jobs []string
	w.HandleSchedulerJobs(func(_ context.Context, name string) error {
		jobs = append(jobs, name)
		return nil
	})

	ctx := context.Background()
	require.NoError(t, w.mux.ProcessTask(ctx, asynq.NewTask(TypeDatalogReady, nil)))
	require.True(t, woke)
	require.NoError(t, w.mux.ProcessTask(ctx, asynq.NewTask(TypeSchedulerJob, []byte("datalog_prune"))))
	require.Equal(t, []string{"datalog_prune"}, jobs)
}
