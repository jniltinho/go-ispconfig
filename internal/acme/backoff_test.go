package acme

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Issue runs in parallel for different sites by design, and every one of them
// writes this one file. Without serialisation the second write drops the
// first's entry, and a lost failure is a site that retries immediately —
// the rate-limit burn the ledger exists to prevent.
func TestLedgerSurvivesConcurrentFailures(t *testing.T) {
	c := New(Config{Root: t.TempDir()})
	now := time.Now()

	const n = 16
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.recordFailure(lineageName(i), now, "boom")
		}(i)
	}
	wg.Wait()

	l, err := c.readLedger()
	require.NoError(t, err)
	assert.Len(t, l, n, "every concurrent failure is recorded, none clobbered")
	for i := 0; i < n; i++ {
		blocked, _ := c.blocked(lineageName(i), now)
		assert.True(t, blocked, "%s must be backing off", lineageName(i))
	}
}

func lineageName(i int) string {
	return "site" + string(rune('a'+i)) + ".example.com"
}
