package client

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Record is one legacy database record as returned by the JSON API. The
// legacy panel serializes every column value as a string; UnmarshalJSON
// normalizes whatever arrives to that shape: strings stay as-is, null
// becomes "", and any other value (number, bool, nested structure from a
// newer/older panel version) keeps its literal JSON text. Columns the
// import engine does not map are simply carried along, so unknown fields
// never break decoding.
type Record map[string]string

// UnmarshalJSON implements json.Unmarshaler with the normalization
// described on Record.
func (r *Record) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	rec := make(Record, len(raw))
	for key, val := range raw {
		var s string
		switch {
		case json.Unmarshal(val, &s) == nil:
			rec[key] = s
		case string(val) == "null":
			rec[key] = ""
		default:
			rec[key] = string(val)
		}
	}
	*r = rec
	return nil
}

// Int returns the value under key parsed as an integer, or 0 when the key
// is absent or not numeric.
func (r Record) Int(key string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(r[key]))
	return n
}

// Filter selects records by column value, matching the legacy
// remoting_lib filter-object semantics: keys are column names, values
// containing "%" are matched with SQL LIKE, everything else with
// equality. Paged getters add the #OFFSET#/#LIMIT# keys themselves.
type Filter map[string]string

// flexInt decodes a JSON integer that the legacy panel may serialize as
// either a number or a numeric string.
type flexInt int

// UnmarshalJSON implements json.Unmarshaler.
func (v *flexInt) UnmarshalJSON(data []byte) error {
	n, err := strconv.Atoi(strings.Trim(string(data), `"`))
	if err != nil {
		return err
	}
	*v = flexInt(n)
	return nil
}
