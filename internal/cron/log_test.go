package cron

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatRunMessage(t *testing.T) {
	start := time.Unix(1700000000, 0).UTC()
	end := start.Add(2 * time.Second)
	msg := FormatRunMessage(RunLogInput{
		CronID:         12,
		ParentDomainID: 3,
		Type:           "url",
		Result: RunResult{
			Status:   StatusOK,
			ExitCode: 200,
			Output:   "hello\nworld",
			Start:    start,
			End:      end,
		},
	})
	assert.Equal(t, "cron_run id=12 parent_domain_id=3 type=url status=ok exit=200 start=1700000000 end=1700000002 output=hello world", msg)
	assert.True(t, strings.HasPrefix(msg, RunMessagePrefix(12)))
}

func TestShouldLogRun(t *testing.T) {
	assert.True(t, ShouldLogRun("y", StatusOK, false))
	assert.False(t, ShouldLogRun("n", StatusOK, false))
	assert.True(t, ShouldLogRun("n", StatusError, true), "security abort always logs")
	assert.False(t, ShouldLogRun("n", StatusExit, false))
	assert.True(t, ShouldLogRun("y", StatusTimeout, false))
}

func TestSanitizeOutputBounds(t *testing.T) {
	long := strings.Repeat("a", maxLogOutput+100)
	got := sanitizeOutput(long, maxLogOutput)
	require.Len(t, got, maxLogOutput)
	assert.Equal(t, "-", sanitizeOutput("", maxLogOutput))
	assert.Equal(t, "a b", sanitizeOutput("a\nb", maxLogOutput))
}

func TestWriteRunLogSkipsWhenUnloggedOK(t *testing.T) {
	// No DB required: log='n' + ok must be a no-op.
	err := WriteRunLog(t.Context(), nil, RunLogInput{
		Log:    "n",
		Result: RunResult{Status: StatusOK},
	})
	require.NoError(t, err)
}

func TestWriteRunLogRequiresDBWhenLogging(t *testing.T) {
	err := WriteRunLog(t.Context(), nil, RunLogInput{
		Log:    "y",
		Result: RunResult{Status: StatusOK, Start: time.Now(), End: time.Now()},
	})
	require.Error(t, err)
}
