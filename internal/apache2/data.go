package apache2

import (
	"fmt"
	"strconv"
	"strings"
)

// row is one decoded datalog record ({old} or {new} payload) or a database
// row scanned into a map. Values may be strings (PHP-era payloads, text
// columns), json float64 numbers, ints from GORM scans or nil.
//
// ponytail: duplicated from internal/nginx/data.go. The add-apache2-module
// proposal calls for extracting these into a shared internal/websites
// package; that refactor touches every nginx golden test, so it is deferred
// until a second consumer needs more than these three helpers.
type row map[string]any

// str returns the value of k as a string ("" for missing/nil).
func (r row) str(k string) string {
	switch v := r[k].(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

// num returns the value of k as an int64 (0 when missing or non-numeric).
func (r row) num(k string) int64 {
	switch v := r[k].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	case int32:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	case []byte:
		n, _ := strconv.ParseInt(strings.TrimSpace(string(v)), 10, 64)
		return n
	default:
		return 0
	}
}

// isVhostType reports whether t owns a vhost file and a site directory tree.
func isVhostType(t string) bool {
	return t == "vhost" || t == "vhostsubdomain" || t == "vhostalias"
}

// atoi64 parses a config number stored as string (0 when empty/invalid).
func atoi64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

// yn maps a bool to the 'y'/'n' spelling the master templates compare against.
func yn(b bool) string {
	if b {
		return "y"
	}
	return "n"
}

// versionGE compares dot-separated numeric versions (PHP version_compare).
func versionGE(v, min string) bool {
	if v == "" {
		return false
	}
	va, vb := strings.Split(v, "."), strings.Split(min, ".")
	for i := 0; i < len(va) || i < len(vb); i++ {
		var a, b int
		if i < len(va) {
			a, _ = strconv.Atoi(va[i]) // non-numeric parts read as 0
		}
		if i < len(vb) {
			b, _ = strconv.Atoi(vb[i])
		}
		if a != b {
			return a > b
		}
	}
	return true
}
