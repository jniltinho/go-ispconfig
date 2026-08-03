package cron

import (
	"fmt"
	"strconv"
	"strings"
)

// row is one decoded datalog record ({old} or {new} payload).
type row map[string]any

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
