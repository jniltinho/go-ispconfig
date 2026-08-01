package database

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitStatementsEmbeddedSchema(t *testing.T) {
	stmts := SplitStatements(schemaSQL)
	require.NotEmpty(t, stmts)

	assert.Equal(t, "SET FOREIGN_KEY_CHECKS = 0", stmts[0])
	assert.Equal(t, "SET FOREIGN_KEY_CHECKS = 1", stmts[len(stmts)-1])

	var creates int
	for _, s := range stmts {
		switch {
		case strings.HasPrefix(s, "CREATE TABLE"):
			creates++
		case strings.HasPrefix(s, "INSERT INTO"), strings.HasPrefix(s, "SET "),
			strings.HasPrefix(s, "ALTER TABLE"), strings.HasPrefix(s, "DROP TABLE"):
		default:
			t.Errorf("unexpected statement kind: %.80q", s)
		}
	}
	// One per CREATE TABLE in the raw dump: proves no statement was split
	// in the middle or merged with a neighbor.
	assert.Equal(t, strings.Count(schemaSQL, "CREATE TABLE"), creates)
}

func TestSplitStatementsQuoting(t *testing.T) {
	stmts := SplitStatements(`-- a comment; with semicolon
INSERT INTO t VALUES ('a;b', 'it\'s', "x;y"); /* block; comment */
SELECT 1;`)
	require.Len(t, stmts, 2)
	assert.Equal(t, `INSERT INTO t VALUES ('a;b', 'it\'s', "x;y")`, stmts[0])
	assert.Equal(t, "SELECT 1", stmts[1])
}
