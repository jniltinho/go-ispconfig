package clientdb

import (
	"fmt"
	"strconv"
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
