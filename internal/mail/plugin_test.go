package mail

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"go-ispconfig/internal/getconf"
)

func TestResolveUIDGIDAlreadySet(t *testing.T) {
	// uid/gid present: no lookup, no DB write-back (db is nil on purpose —
	// touching it would panic).
	p := NewPlugin(nil, nil, nil, 1, nil)
	uid, gid := p.resolveUIDGID(context.Background(), getconf.DefaultMailConfig(),
		row{"uid": float64(5000), "gid": float64(5000), "mailuser_id": float64(1)})
	assert.EqualValues(t, 5000, uid)
	assert.EqualValues(t, 5000, gid)
}

func TestRowHelpers(t *testing.T) {
	r := row{"a": "x", "b": float64(7), "c": nil, "d": "42"}
	assert.Equal(t, "x", r.str("a"))
	assert.Equal(t, "7", r.str("b"))
	assert.Equal(t, "", r.str("c"))
	assert.EqualValues(t, 42, r.num("d"))
	assert.EqualValues(t, 0, r.num("missing"))
}
