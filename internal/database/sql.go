package database

import (
	_ "embed"
	"strings"
)

// schemaSQL is the original, unmodified ISPConfig 3.3.1p1 DDL
// (install/sql/ispconfig3.sql). It is executed verbatim on an empty database
// so the resulting schema is byte-identical to a PHP ISPConfig3 install.
//
//go:embed ispconfig3.sql
var schemaSQL string

// SplitStatements splits a standard MySQL dump into individual statements on
// top-level semicolons. It understands single/double-quoted and backtick
// strings (including backslash escapes), `-- `/`#` line comments and
// `/* ... */` block comments, which is all ispconfig3.sql uses (the dump has
// no DELIMITER blocks or stored procedures). Comment-only fragments and empty
// statements are dropped.
func SplitStatements(sql string) []string {
	var stmts []string
	var b strings.Builder

	const (
		code = iota
		lineComment
		blockComment
		singleQuote
		doubleQuote
		backtick
	)
	state := code

	flush := func() {
		s := strings.TrimSpace(b.String())
		b.Reset()
		if s != "" {
			stmts = append(stmts, s)
		}
	}

	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch state {
		case lineComment:
			if c == '\n' {
				state = code
				b.WriteByte(c)
			}
		case blockComment:
			if c == '*' && i+1 < len(sql) && sql[i+1] == '/' {
				state = code
				i++
			}
		case singleQuote, doubleQuote:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(sql) {
				b.WriteByte(sql[i+1])
				i++
			} else if (state == singleQuote && c == '\'') || (state == doubleQuote && c == '"') {
				state = code
			}
		case backtick:
			b.WriteByte(c)
			if c == '`' {
				state = code
			}
		default: // code
			switch {
			case c == ';':
				flush()
			case c == '\'':
				state = singleQuote
				b.WriteByte(c)
			case c == '"':
				state = doubleQuote
				b.WriteByte(c)
			case c == '`':
				state = backtick
				b.WriteByte(c)
			case c == '#':
				state = lineComment
			case c == '-' && i+1 < len(sql) && sql[i+1] == '-' &&
				(i+2 >= len(sql) || sql[i+2] == ' ' || sql[i+2] == '\n' || sql[i+2] == '\r' || sql[i+2] == '\t'):
				state = lineComment
			case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
				state = blockComment
				i++
			default:
				b.WriteByte(c)
			}
		}
	}
	flush()
	return stmts
}
