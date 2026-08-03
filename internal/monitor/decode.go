// Package monitor implements ISPConfig-style metric collection, dual-format
// payload decoding (JSON + PHP serialize), state aggregation, and write
// helpers for the monitor_data table. Collection jobs run only in the daemon.
package monitor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/elliotchance/phpserialize"
)

// MaxPayloadBytes caps decoded monitor_data / sys_datalog payloads so hostile
// or oversized serialize blobs cannot exhaust memory (mediumtext upper bound
// is ~16 MiB; we stay well below that for in-process decode).
const MaxPayloadBytes = 1 << 20 // 1 MiB

// DecodeResult is the outcome of dual-format payload decoding.
type DecodeResult struct {
	// Value is the decoded JSON-compatible value (map, slice, or primitive).
	// Nil when decoding failed.
	Value any `json:"value,omitempty"`
	// Raw is the original payload string (always set for failed decode).
	Raw string `json:"data_raw,omitempty"`
	// DecodeError is non-empty when neither JSON nor PHP serialize succeeded.
	DecodeError string `json:"decode_error,omitempty"`
	// Format is "json", "php", or empty on failure.
	Format string `json:"format,omitempty"`
}

// DecodePayload tries JSON first, then PHP serialize. Payloads larger than
// MaxPayloadBytes are rejected without attempting decode. On failure returns
// Raw + DecodeError so list/detail UIs still show something.
func DecodePayload(raw string) DecodeResult {
	if len(raw) > MaxPayloadBytes {
		return DecodeResult{
			Raw:         raw[:MaxPayloadBytes] + "…",
			DecodeError: fmt.Sprintf("payload exceeds %d bytes", MaxPayloadBytes),
		}
	}
	if strings.TrimSpace(raw) == "" {
		return DecodeResult{Value: nil, Format: "json"}
	}

	var jsonVal any
	if err := json.Unmarshal([]byte(raw), &jsonVal); err == nil {
		return DecodeResult{Value: jsonVal, Format: "json"}
	}

	phpVal, err := phpserialize.UnmarshalAssociativeArray([]byte(raw))
	if err == nil {
		return DecodeResult{Value: normalizePHP(phpVal), Format: "php"}
	}
	// Scalar / list PHP payloads.
	var anyVal any
	if err2 := phpserialize.Unmarshal([]byte(raw), &anyVal); err2 == nil {
		return DecodeResult{Value: normalizePHP(anyVal), Format: "php"}
	}

	return DecodeResult{
		Raw:         raw,
		DecodeError: "neither JSON nor PHP serialize: " + err.Error(),
	}
}

// DatalogDiff is the decoded {old,new} shape of sys_datalog.data.
type DatalogDiff struct {
	Old map[string]any `json:"old"`
	New map[string]any `json:"new"`
}

// DecodeDatalogDiff dual-decodes sys_datalog.data into {old,new} maps.
// On total failure, Diff is empty and DecodeError is set; Raw is always
// the original payload (truncated when over cap).
func DecodeDatalogDiff(raw string) (diff DatalogDiff, res DecodeResult) {
	res = DecodePayload(raw)
	if res.DecodeError != "" {
		return DatalogDiff{}, res
	}
	m, ok := asStringMap(res.Value)
	if !ok {
		res.DecodeError = "decoded value is not an object"
		res.Raw = raw
		res.Value = nil
		return DatalogDiff{}, res
	}
	diff.Old = map[string]any{}
	diff.New = map[string]any{}
	if old, ok := asStringMap(m["old"]); ok {
		diff.Old = old
	}
	if neu, ok := asStringMap(m["new"]); ok {
		diff.New = neu
	}
	return diff, res
}

// asStringMap coerces map[string]any or map[any]any into map[string]any.
func asStringMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			out[fmt.Sprint(k)] = normalizePHP(val)
		}
		return out, true
	default:
		return nil, false
	}
}

// normalizePHP walks PHP-decoded values so maps use string keys and nested
// structures are JSON-friendly.
func normalizePHP(v any) any {
	switch x := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = normalizePHP(val)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = normalizePHP(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalizePHP(val)
		}
		return out
	default:
		return v
	}
}
