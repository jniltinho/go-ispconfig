package clientdb

import (
	"fmt"
	"strconv"
	"strings"
)

// row is one decoded datalog record ({old} or {new} payload). Values may
// be strings (PHP-era payloads, text columns), json float64 numbers or
// nil (same shape as the dns/mail/firewall plugin row helpers).
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
