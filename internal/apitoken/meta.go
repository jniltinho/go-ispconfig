package apitoken

import (
	"strings"
	"time"
)

// Meta is what remote_user.remote_functions carries for a go-ispconfig token.
//
// ISPConfig3 stores a bare CSV of allowed RPC function names there. Since the
// schema may not diverge (design D1), the same column carries the scope list
// plus the two attributes that have no legacy column of their own — expiry
// and last-used — as a small `key=value;` document (design D2):
//
//	scopes=sites:read,mail:*;expires=2027-01-01T00:00:00Z;last_used=2026-08-04T09:41:12Z
//
// A value with none of those keys parses as a bare scope CSV with no expiry,
// so a row written by the PHP panel still reads, and a row written here
// degrades to "a list of names" if the PHP panel ever reads it back.
type Meta struct {
	// Scopes are the granted `<resource>:<action>` strings.
	Scopes []string
	// Expires is the token's own expiry; zero means it never expires.
	Expires time.Time
	// LastUsed is the last successful authentication; zero means never.
	LastUsed time.Time
}

// ParseMeta decodes remote_functions. It never fails: an unparseable value
// yields an empty scope list, and an empty scope list denies every request
// (a token that grants nothing is safer than a token that grants everything).
func ParseMeta(raw string) Meta {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Meta{}
	}
	if !strings.Contains(raw, "=") {
		return Meta{Scopes: splitCSV(raw)}
	}
	var m Meta
	for part := range strings.SplitSeq(raw, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch key {
		case "scopes":
			m.Scopes = splitCSV(value)
		case "expires":
			m.Expires = parseTime(value)
		case "last_used":
			m.LastUsed = parseTime(value)
		}
	}
	return m
}

// String encodes the metadata back into remote_functions. Empty attributes
// are omitted so a token with neither expiry nor use history round trips as a
// value the legacy parser would still accept.
func (m Meta) String() string {
	parts := []string{"scopes=" + strings.Join(m.Scopes, ",")}
	if !m.Expires.IsZero() {
		parts = append(parts, "expires="+m.Expires.UTC().Format(time.RFC3339))
	}
	if !m.LastUsed.IsZero() {
		parts = append(parts, "last_used="+m.LastUsed.UTC().Format(time.RFC3339))
	}
	return strings.Join(parts, ";")
}

// Expired reports whether the token's own expiry has passed.
func (m Meta) Expired(now time.Time) bool {
	return !m.Expires.IsZero() && now.After(m.Expires)
}

// splitCSV trims and drops empty entries.
func splitCSV(s string) []string {
	out := []string{}
	for p := range strings.SplitSeq(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseTime accepts RFC3339 and returns the zero time for anything else, so a
// corrupted timestamp reads as "no expiry recorded" rather than failing the
// whole token — the enabled flag and the digest remain the real gates.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}
