package powerdns

import (
	"fmt"
	"strconv"
	"strings"

	"go-ispconfig/internal/engine"
)

// row is one decoded datalog record ({old} or {new}) or a scanned map.
type row map[string]any

// str returns the value of k as a string ("" for missing/nil).
func (r row) str(k string) string {
	if r == nil {
		return ""
	}
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
	if r == nil {
		return 0
	}
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

// activeY reports whether the Active enum is 'Y' (DNS tables use uppercase).
func (r row) activeY() bool { return r.str("active") == "Y" }

// payloadServerID returns server_id from new (preferred) or old (delete).
func payloadServerID(data engine.Data) uint32 {
	if n := row(data.New).num("server_id"); n > 0 {
		return uint32(n)
	}
	if n := row(data.Old).num("server_id"); n > 0 {
		return uint32(n)
	}
	return 0
}
