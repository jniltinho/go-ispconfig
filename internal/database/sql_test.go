package database

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitStatementsEmbeddedSchema(t *testing.T) {
	stmts, err := SplitStatements(schemaSQL)
	require.NoError(t, err)
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
	stmts, err := SplitStatements(`-- a comment; with semicolon
INSERT INTO t VALUES ('a;b', 'it\'s', "x;y"); /* block; comment */
SELECT 1;`)
	require.NoError(t, err)
	require.Len(t, stmts, 2)
	assert.Equal(t, `INSERT INTO t VALUES ('a;b', 'it\'s', "x;y")`, stmts[0])
	assert.Equal(t, "SELECT 1", stmts[1])
}

func TestSplitStatementsDoubledQuoteEscape(t *testing.T) {
	// '' is the SQL-standard escape for a quote inside a string; the
	// semicolon after it is still inside the literal.
	stmts, err := SplitStatements(`INSERT INTO t VALUES ('a''b;c'); SELECT 2;`)
	require.NoError(t, err)
	require.Len(t, stmts, 2)
	assert.Equal(t, `INSERT INTO t VALUES ('a''b;c')`, stmts[0])
	assert.Equal(t, "SELECT 2", stmts[1])
}

func TestSplitStatementsMultilineInsert(t *testing.T) {
	stmts, err := SplitStatements(`INSERT INTO t (a, b) VALUES
	(1, 'first;row'),
	(2, 'second
line');
SELECT 3;`)
	require.NoError(t, err)
	require.Len(t, stmts, 2)
	assert.Contains(t, stmts[0], "'first;row'")
	assert.Contains(t, stmts[0], "'second\nline'")
	assert.Equal(t, "SELECT 3", stmts[1])
}

func TestSplitStatementsUnterminated(t *testing.T) {
	for _, in := range []string{
		"SELECT 'unterminated",
		`SELECT "unterminated`,
		"SELECT `unterminated",
		"SELECT 1 /* unterminated",
		"SELECT 'ends in escape''",
	} {
		_, err := SplitStatements(in)
		assert.Errorf(t, err, "input %q must fail", in)
	}

	// A line comment without trailing newline at EOF is fine.
	stmts, err := SplitStatements("SELECT 1; -- trailing comment")
	require.NoError(t, err)
	assert.Equal(t, []string{"SELECT 1"}, stmts)
}
