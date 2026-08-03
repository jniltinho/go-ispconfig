package monitor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStateRank_order(t *testing.T) {
	order := []string{
		"no_state", "ok", "unknown", "info", "warning", "critical", "error",
	}
	for i := 1; i < len(order); i++ {
		assert.Less(t, StateRank(order[i-1]), StateRank(order[i]),
			"%s should rank below %s", order[i-1], order[i])
	}
}

func TestSetState_promoteOnly(t *testing.T) {
	tests := []struct {
		name      string
		current   string
		candidate string
		want      string
	}{
		{"ok over no_state", "no_state", "ok", "ok"},
		{"warning over ok", "ok", "warning", "warning"},
		{"error over critical", "critical", "error", "error"},
		{"no demote warning to ok", "warning", "ok", "warning"},
		{"no demote error to info", "error", "info", "error"},
		{"equal stays", "warning", "warning", "warning"},
		{"empty current as no_state", "", "ok", "ok"},
		{"empty candidate keeps current", "ok", "", "ok"},
		{"unknown label ranks unknown", "ok", "bogus", "unknown"},
		{"critical over warning", "warning", "critical", "critical"},
		{"info over ok", "ok", "info", "info"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SetState(tc.current, tc.candidate))
		})
	}
}

func TestFoldStates(t *testing.T) {
	assert.Equal(t, "no_state", FoldStates())
	assert.Equal(t, "error", FoldStates("ok", "warning", "error", "info"))
	assert.Equal(t, "ok", FoldStates("no_state", "ok"))
}
