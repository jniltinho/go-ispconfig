package acme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ledgerMu serialises the ledger's read-modify-write. The per-lineage locks
// deliberately let Issue run in parallel for different sites, and every one of
// them touches this one file — without this, two sites failing at the same
// moment each read the ledger, each add their own entry, and the second write
// drops the first. A lost failure is a site that retries immediately, which is
// exactly the rate-limit burn the ledger exists to prevent.
var ledgerMu sync.Mutex

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

func (c *Client) readLedger() (map[string]failure, error) {
	raw, err := os.ReadFile(c.ledgerPath())
	if os.IsNotExist(err) {
		return map[string]failure{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]failure{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
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
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	l, err := c.readLedger()
	if err != nil {
		c.log.Warn("acme: backoff ledger unreadable; refusing issuance locally", "error", err)
		return true, now.Add(backoffMin)
	}
	f, ok := l[lineage]
	if !ok || now.After(f.Until) {
		return false, time.Time{}
	}
	return true, f.Until
}

// recordFailure extends the backoff for a lineage: 15 minutes, doubling,
// capped at a day.
func (c *Client) recordFailure(lineage string, now time.Time, cause string) {
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	l, err := c.readLedger()
	if err != nil {
		c.log.Error("acme: backoff ledger unreadable; cannot record failure", "error", err)
		return
	}
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
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	l, err := c.readLedger()
	if err != nil {
		c.log.Error("acme: backoff ledger unreadable; cannot clear failure", "error", err)
		return
	}
	if _, ok := l[lineage]; !ok {
		return
	}
	delete(l, lineage)
	c.writeLedger(l)
}
