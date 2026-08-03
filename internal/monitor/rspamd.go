package monitor

// Rspamd spam statistics collector: reads the controller worker's /stat
// endpoint on localhost and stores it as monitor_data type "rspamd", which
// the panel reads through GET /api/monitor/data?type=rspamd. A host without
// Rspamd simply has no sample and the widget hides itself.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"gorm.io/gorm"
)

// RspamdStatURL is the controller worker's stat endpoint; the rspamd plugin
// binds the controller to localhost only.
const RspamdStatURL = "http://localhost:11334/stat"

// rspamdStat is the subset of the controller /stat payload the widget shows.
type rspamdStat struct {
	Scanned  uint64            `json:"scanned"`
	Learned  uint64            `json:"learned"`
	Actions  map[string]uint64 `json:"actions"`
	Uptime   uint64            `json:"uptime"`
	Version  string            `json:"version"`
	Received uint64            `json:"received"`
}

// ParseRspamdStat turns a controller /stat payload into the monitor_data
// map: the raw counters plus the derived spam ratio the widget renders.
func ParseRspamdStat(payload []byte) (map[string]any, string, error) {
	var st rspamdStat
	if err := json.Unmarshal(payload, &st); err != nil {
		return nil, "unknown", fmt.Errorf("rspamd: decode stat: %w", err)
	}
	// "reject" and "add header" are the two spam verdicts; "greylist" and
	// "soft reject" are deferrals, counted separately so the ratio does not
	// swing on temporary rejections.
	spam := st.Actions["reject"] + st.Actions["add header"] + st.Actions["rewrite subject"]
	greylisted := st.Actions["greylist"] + st.Actions["soft reject"]
	data := map[string]any{
		"scanned":      st.Scanned,
		"learned":      st.Learned,
		"spam":         spam,
		"greylisted":   greylisted,
		"clean":        st.Actions["no action"],
		"uptime":       st.Uptime,
		"version":      st.Version,
		"spam_percent": 0.0,
	}
	if st.Scanned > 0 {
		data["spam_percent"] = float64(spam) * 100 / float64(st.Scanned)
	}
	return data, "ok", nil
}

// CollectRspamdStats fetches and parses the controller stats. ok=false means
// no Rspamd controller answered (not installed, not running, not local): the
// caller stores nothing and logs nothing, which is the normal state on every
// non-mail server.
func CollectRspamdStats(ctx context.Context, url string) (data map[string]any, state string, ok bool) {
	if url == "" {
		url = RspamdStatURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", false
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", false
	}
	defer resp.Body.Close() //nolint:errcheck // stat probe
	if resp.StatusCode != http.StatusOK {
		return nil, "", false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return nil, "", false
	}
	data, state, err = ParseRspamdStat(body)
	if err != nil {
		return nil, "", false
	}
	return data, state, true
}

// RunRspamdCollector stores one rspamd sample, or nothing at all when the
// controller is unreachable.
func RunRspamdCollector(ctx context.Context, db *gorm.DB, serverID uint32, url string) error {
	data, state, ok := CollectRspamdStats(ctx, url)
	if !ok {
		return nil
	}
	return Store(ctx, db, serverID, "rspamd", data, state, 0)
}
