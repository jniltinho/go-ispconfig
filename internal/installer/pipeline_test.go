package installer

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeStep runs in order, optionally failing or skipping; done simulates
// per-step idempotency state for the re-run test.
type fakeStep struct {
	name string
	fail error
	done *bool // when already true the step skips
	ran  *[]string
}

func (f fakeStep) Name() string { return f.name }

func (f fakeStep) Run(_ context.Context, _ *State) error {
	*f.ran = append(*f.ran, f.name)
	if f.fail != nil {
		return f.fail
	}
	if f.done != nil {
		if *f.done {
			return Skip("already done")
		}
		*f.done = true
	}
	return nil
}

func TestRunOrderAndFailureStops(t *testing.T) {
	var ran []string
	out := &bytes.Buffer{}
	st := &State{Out: out}
	boom := errors.New("boom")
	err := Run(context.Background(), st, []Step{
		fakeStep{name: "one", ran: &ran},
		fakeStep{name: "two", fail: boom, ran: &ran},
		fakeStep{name: "three", ran: &ran},
	})
	require.ErrorIs(t, err, boom)
	assert.ErrorContains(t, err, "step two")
	assert.Equal(t, []string{"one", "two"}, ran, "failure stops the pipeline, order preserved")
	assert.Contains(t, out.String(), "[one] done")
	assert.Contains(t, out.String(), "[two] FAILED")
}

func TestRunRerunSkips(t *testing.T) {
	var ran []string
	done := false
	out := &bytes.Buffer{}
	st := &State{Out: out}
	steps := []Step{fakeStep{name: "conv", done: &done, ran: &ran}}

	require.NoError(t, Run(context.Background(), st, steps))
	require.NoError(t, Run(context.Background(), st, steps), "re-run must converge, not fail")
	assert.Equal(t, []string{"conv", "conv"}, ran)
	assert.Contains(t, out.String(), "[conv] skipped: already done")
}
