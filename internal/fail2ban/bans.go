package fail2ban

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"go-ispconfig/internal/engine"
)

// jailNameRe is the whitelist every jail name coming from HTTP must match
// before it reaches an argv slice.
var jailNameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// ValidJailName reports whether name is safe to pass to fail2ban-client.
func ValidJailName(name string) bool {
	return len(name) <= 64 && jailNameRe.MatchString(name)
}

// Status is the parsed `fail2ban-client status <jail>` output.
type Status struct {
	// Name is the jail name.
	Name string `json:"name"`
	// CurrentlyFailed is the filter's live failure count.
	CurrentlyFailed int `json:"currently_failed"`
	// TotalFailed is the filter's cumulative failure count.
	TotalFailed int `json:"total_failed"`
	// CurrentlyBanned is the number of active bans.
	CurrentlyBanned int `json:"currently_banned"`
	// TotalBanned is the cumulative ban count.
	TotalBanned int `json:"total_banned"`
	// BannedIPs are the currently banned addresses.
	BannedIPs []string `json:"banned_ips"`
}

// Client is a thin fail2ban-client wrapper. Every invocation is an argv
// slice through the foundation CommandRunner — no shell strings.
type Client struct {
	// Runner executes fail2ban-client.
	Runner engine.CommandRunner
}

// NewClient returns a Client backed by the production exec runner.
func NewClient() *Client { return &Client{Runner: engine.ExecRunner{}} }

// Jails returns the live jail names from `fail2ban-client status`.
func (c *Client) Jails(ctx context.Context) ([]string, error) {
	out, err := c.Runner.Run(ctx, "fail2ban-client", "status")
	if err != nil {
		return nil, fmt.Errorf("fail2ban-client status: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return ParseJailList(string(out)), nil
}

// Status returns the parsed detail of one jail.
func (c *Client) Status(ctx context.Context, jail string) (Status, error) {
	if !ValidJailName(jail) {
		return Status{}, fmt.Errorf("fail2ban: invalid jail name %q", jail)
	}
	out, err := c.Runner.Run(ctx, "fail2ban-client", "status", jail)
	if err != nil {
		return Status{}, fmt.Errorf("fail2ban-client status %s: %w (%s)", jail, err, strings.TrimSpace(string(out)))
	}
	st := ParseStatus(string(out))
	st.Name = jail
	return st, nil
}

// Unban releases ip from jail. Both arguments arrive from HTTP, so both are
// validated before the command is built.
func (c *Client) Unban(ctx context.Context, jail, ip string) error {
	if !ValidJailName(jail) {
		return fmt.Errorf("fail2ban: invalid jail name %q", jail)
	}
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("fail2ban: invalid ip %q", ip)
	}
	if out, err := c.Runner.Run(ctx, "fail2ban-client", "set", jail, "unbanip", ip); err != nil {
		return fmt.Errorf("fail2ban-client set %s unbanip: %w (%s)", jail, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ParseJailList extracts the jail names from the `Jail list:` line of
// `fail2ban-client status`. Unrecognised output yields no jails.
func ParseJailList(out string) []string {
	var jails []string
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := splitStatusLine(line)
		if !ok || key != "Jail list" {
			continue
		}
		for _, name := range strings.Split(value, ",") {
			if name = strings.TrimSpace(name); name != "" {
				jails = append(jails, name)
			}
		}
	}
	return jails
}

// ParseStatus extracts the counters and banned IPs from
// `fail2ban-client status <jail>`. Unknown lines are ignored, so a format
// change degrades to zero counters instead of an error.
func ParseStatus(out string) Status {
	var st Status
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := splitStatusLine(line)
		if !ok {
			continue
		}
		switch key {
		case "Currently failed":
			st.CurrentlyFailed, _ = strconv.Atoi(value)
		case "Total failed":
			st.TotalFailed, _ = strconv.Atoi(value)
		case "Currently banned":
			st.CurrentlyBanned, _ = strconv.Atoi(value)
		case "Total banned":
			st.TotalBanned, _ = strconv.Atoi(value)
		case "Banned IP list":
			st.BannedIPs = strings.Fields(value)
		}
	}
	return st
}

// splitStatusLine strips fail2ban's tree drawing ("|- ", "`- ", "   ") and
// splits the remaining "Key:\tvalue" pair.
func splitStatusLine(line string) (key, value string, ok bool) {
	line = strings.TrimLeft(line, " \t|`-")
	key, value, ok = strings.Cut(line, ":")
	return strings.TrimSpace(key), strings.TrimSpace(value), ok
}
