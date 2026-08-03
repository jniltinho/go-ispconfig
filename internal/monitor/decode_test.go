package monitor

import (
	"strings"
	"testing"

	"github.com/elliotchance/phpserialize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecodePayload_JSON(t *testing.T) {
	res := DecodePayload(`{"load_1":0.5,"MemTotal":1024}`)
	require.Empty(t, res.DecodeError)
	assert.Equal(t, "json", res.Format)
	m, ok := res.Value.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 0.5, m["load_1"])
	assert.EqualValues(t, 1024, m["MemTotal"])
}

func TestDecodePayload_JSONArray(t *testing.T) {
	res := DecodePayload(`[1,2,3]`)
	require.Empty(t, res.DecodeError)
	assert.Equal(t, "json", res.Format)
	arr, ok := res.Value.([]any)
	require.True(t, ok)
	assert.Len(t, arr, 3)
}

func TestDecodePayload_PHPSerialize(t *testing.T) {
	// a:2:{s:6:"load_1";d:1.5;s:8:"MemTotal";i:2048;}
	raw, err := phpserialize.Marshal(map[any]any{
		"load_1":   1.5,
		"MemTotal": 2048,
	}, nil)
	require.NoError(t, err)

	res := DecodePayload(string(raw))
	require.Empty(t, res.DecodeError, "decode error: %s", res.DecodeError)
	assert.Equal(t, "php", res.Format)
	m, ok := res.Value.(map[string]any)
	require.True(t, ok, "got %T", res.Value)
	assert.Equal(t, 1.5, m["load_1"])
	assert.EqualValues(t, 2048, m["MemTotal"])
}

func TestDecodePayload_PHPSerialize_literalFixture(t *testing.T) {
	// Hand-written PHP serialize matching ISPConfig-style monitor payload.
	raw := `a:2:{s:5:"model";s:5:"Intel";s:5:"cores";i:4;}`
	res := DecodePayload(raw)
	require.Empty(t, res.DecodeError)
	assert.Equal(t, "php", res.Format)
	m := res.Value.(map[string]any)
	assert.Equal(t, "Intel", m["model"])
	assert.EqualValues(t, 4, m["cores"])
}

func TestDecodePayload_prefersJSON(t *testing.T) {
	// Valid JSON that is not valid PHP serialize for associative arrays.
	res := DecodePayload(`{"a":1}`)
	assert.Equal(t, "json", res.Format)
}

func TestDecodePayload_empty(t *testing.T) {
	res := DecodePayload("")
	require.Empty(t, res.DecodeError)
	assert.Equal(t, "json", res.Format)
	assert.Nil(t, res.Value)
}

func TestDecodePayload_garbage(t *testing.T) {
	res := DecodePayload("not-json-and-not-php")
	assert.NotEmpty(t, res.DecodeError)
	assert.Equal(t, "not-json-and-not-php", res.Raw)
	assert.Nil(t, res.Value)
}

func TestDecodePayload_sizeCap(t *testing.T) {
	huge := strings.Repeat("x", MaxPayloadBytes+10)
	res := DecodePayload(huge)
	assert.NotEmpty(t, res.DecodeError)
	assert.Contains(t, res.DecodeError, "exceeds")
	assert.True(t, strings.HasSuffix(res.Raw, "…"))
	assert.LessOrEqual(t, len(res.Raw), MaxPayloadBytes+len("…"))
}

func TestDecodeDatalogDiff_JSON(t *testing.T) {
	raw := `{"old":{"domain":"a.com"},"new":{"domain":"b.com"}}`
	diff, res := DecodeDatalogDiff(raw)
	require.Empty(t, res.DecodeError)
	assert.Equal(t, "json", res.Format)
	assert.Equal(t, "a.com", diff.Old["domain"])
	assert.Equal(t, "b.com", diff.New["domain"])
}

func TestDecodeDatalogDiff_PHPSerialize(t *testing.T) {
	// a:2:{s:3:"old";a:1:{s:6:"domain";s:5:"a.com";}s:3:"new";a:1:{s:6:"domain";s:5:"b.com";}}
	raw, err := phpserialize.Marshal(map[any]any{
		"old": map[any]any{"domain": "a.com"},
		"new": map[any]any{"domain": "b.com"},
	}, nil)
	require.NoError(t, err)

	diff, res := DecodeDatalogDiff(string(raw))
	require.Empty(t, res.DecodeError, res.DecodeError)
	assert.Equal(t, "php", res.Format)
	assert.Equal(t, "a.com", diff.Old["domain"])
	assert.Equal(t, "b.com", diff.New["domain"])
}

func TestDecodeDatalogDiff_insertOnlyNew(t *testing.T) {
	raw := `{"old":{},"new":{"id":1}}`
	diff, res := DecodeDatalogDiff(raw)
	require.Empty(t, res.DecodeError)
	assert.Empty(t, diff.Old)
	assert.EqualValues(t, 1, diff.New["id"])
}

func TestDecodeDatalogDiff_notObject(t *testing.T) {
	diff, res := DecodeDatalogDiff(`[1,2]`)
	assert.NotEmpty(t, res.DecodeError)
	assert.Empty(t, diff.Old)
	assert.Empty(t, diff.New)
}
