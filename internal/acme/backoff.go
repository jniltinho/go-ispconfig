package acme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Let's Encrypt's limits are account-wide and punitive — 5 failed validations
// per hour per account/hostname, 50 certificates per domain per week — and an
// operator feels a lockout days later as "the panel stopped issuing
// certificates". So a failure is recorded and the next attempt for that
// lineage is refused *locally* until a backoff has passed. The CA is never the
// thing that tells us to slow down.
const (
	backoffMin = 15 * time.Minute
	backoffMax = 24 * time.Hour
)

// failure is one lineage's recorded refusal.
type failure struct {
	Count int       `json:"count"`
	Until time.Time `json:"until"`
	Last  string    `json:"last_error"`
}

// ledgerPath is where the failures live: beside the account, not in the
// database, because it is per-node state about a per-node account.
func (c *Client) ledgerPath() string {
	return filepath.Join(AccountDir(c.cfg.Root, c.cfg.ServerID), "failures.json")
}

func (c *Client) readLedger() map[string]failure {
	raw, err := os.ReadFile(c.ledgerPath())
	if err != nil {
		return map[string]failure{}
	}
	out := map[string]failure{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]failure{}
	}
	return out
}

func (c *Client) writeLedger(l map[string]failure) {
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.ledgerPath()), 0o700); err != nil {
		return
	}
	// Best effort: failing to record a failure must not turn into a second
	// failure the caller has to handle.
	_ = writeFileAtomic(c.ledgerPath(), raw, 0o600)
}

// blocked reports whether this lineage is still inside its backoff, and until
// when.
func (c *Client) blocked(lineage string, now time.Time) (bool, time.Time) {
	f, ok := c.readLedger()[lineage]
	if !ok || now.After(f.Until) {
		return false, time.Time{}
	}
	return true, f.Until
}

// recordFailure extends the backoff for a lineage: 15 minutes, doubling,
// capped at a day.
func (c *Client) recordFailure(lineage string, now time.Time, cause string) {
	l := c.readLedger()
	f := l[lineage]
	f.Count++
	d := backoffMin << (f.Count - 1)
	if d > backoffMax || d <= 0 {
		d = backoffMax
	}
	f.Until = now.Add(d)
	f.Last = cause
	l[lineage] = f
	c.writeLedger(l)
}

// clearFailure forgets a lineage's history after a success, so one bad day
// does not slow down the next month.
func (c *Client) clearFailure(lineage string) {
	l := c.readLedger()
	if _, ok := l[lineage]; !ok {
		return
	}
	delete(l, lineage)
	c.writeLedger(l)
}
