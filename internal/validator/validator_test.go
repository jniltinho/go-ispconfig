package validator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

// check runs a rule against a value with an empty context and returns the
// error key; infrastructure errors fail the test.
func check(t *testing.T, r Rule, value string) string {
	t.Helper()
	key, err := r.Validate(&Context{Ctx: context.Background()}, value)
	require.NoError(t, err)
	return key
}

func TestRegex(t *testing.T) {
	r := Rule{Type: "REGEX", Regex: `^([0-9]{1,5},?){1,}$`, ErrKey: "error_port_syntax"}
	require.Empty(t, check(t, r, "80,443"))
	require.Empty(t, check(t, r, "8080"))
	require.Equal(t, "error_port_syntax", check(t, r, "80;443"))
	require.Equal(t, "error_port_syntax", check(t, r, "abc"))

	broken := Rule{Type: "REGEX", Regex: `([`, ErrKey: "bad"}
	require.Equal(t, "bad", check(t, broken, "anything"), "uncompilable pattern must fail closed")
}

func TestNotEmpty(t *testing.T) {
	r := Rule{Type: "NOTEMPTY", ErrKey: "required"}
	require.Empty(t, check(t, r, "x"))
	require.Equal(t, "required", check(t, r, ""))
}

func TestIsEmail(t *testing.T) {
	r := Rule{Type: "ISEMAIL", ErrKey: "email_error"}
	require.Empty(t, check(t, r, "user@example.com"))
	require.Empty(t, check(t, r, "first.last@sub.example.org"))
	require.Equal(t, "email_error", check(t, r, "not-an-email"))
	require.Equal(t, "email_error", check(t, r, "user+tag@example.com"), "tform disallows + in the local part")
	require.Equal(t, "email_error", check(t, r, ""))

	allowEmpty := Rule{Type: "ISEMAIL", AllowEmpty: true, ErrKey: "email_error"}
	require.Empty(t, check(t, allowEmpty, ""))
}

func TestIsInt(t *testing.T) {
	r := Rule{Type: "ISINT", ErrKey: "int_error"}
	require.Empty(t, check(t, r, "42"))
	require.Empty(t, check(t, r, "-7"))
	require.Equal(t, "int_error", check(t, r, ""), "empty must fail ISINT without AllowEmpty")
	require.Equal(t, "int_error", check(t, r, "3.14"))
	require.Equal(t, "int_error", check(t, r, "abc"))

	allowEmpty := Rule{Type: "ISINT", AllowEmpty: true, ErrKey: "int_error"}
	require.Empty(t, check(t, allowEmpty, ""))
}

func TestIsPositive(t *testing.T) {
	r := Rule{Type: "ISPOSITIVE", ErrKey: "positive_error"}
	require.Empty(t, check(t, r, "1"))
	require.Empty(t, check(t, r, "9999"))
	require.Equal(t, "positive_error", check(t, r, ""), "empty must fail ISPOSITIVE without AllowEmpty")
	require.Equal(t, "positive_error", check(t, r, "0"))
	require.Equal(t, "positive_error", check(t, r, "-3"))
	require.Equal(t, "positive_error", check(t, r, "abc"))

	allowEmpty := Rule{Type: "ISPOSITIVE", AllowEmpty: true, ErrKey: "positive_error"}
	require.Empty(t, check(t, allowEmpty, ""))
}

func TestIsIPv4(t *testing.T) {
	r := Rule{Type: "ISIPV4", ErrKey: "ipv4_error"}
	require.Empty(t, check(t, r, "192.168.1.1"))
	require.Equal(t, "ipv4_error", check(t, r, "999.1.1.1"))
	require.Equal(t, "ipv4_error", check(t, r, "2001:db8::1"))
	require.Equal(t, "ipv4_error", check(t, r, ""))
}

func TestIsIPv6(t *testing.T) {
	r := Rule{Type: "ISIPV6", ErrKey: "ipv6_error"}
	require.Empty(t, check(t, r, "2001:db8::1"))
	require.Empty(t, check(t, r, "::1"))
	require.Equal(t, "ipv6_error", check(t, r, "192.168.1.1"))
	require.Equal(t, "ipv6_error", check(t, r, "not-an-ip"))
}

func TestIsIP(t *testing.T) {
	r := Rule{Type: "ISIP", ErrKey: "ip_error"}
	require.Empty(t, check(t, r, "10.0.0.1"))
	require.Empty(t, check(t, r, "fe80::1"))
	require.Equal(t, "ip_error", check(t, r, "999.1.1.1"))
	require.Equal(t, "ip_error", check(t, r, ""))

	allowEmpty := Rule{Type: "ISIP", AllowEmpty: true, ErrKey: "ip_error"}
	require.Empty(t, check(t, allowEmpty, ""))
}

func TestCustom(t *testing.T) {
	r := Rule{Type: "CUSTOM", Fn: func(_ *Context, value string) string {
		if value != "ok" {
			return "custom_error"
		}
		return ""
	}}
	require.Empty(t, check(t, r, "ok"))
	require.Equal(t, "custom_error", check(t, r, "nope"))

	noFn := Rule{Type: "CUSTOM", ErrKey: "misconfigured"}
	require.Equal(t, "misconfigured", check(t, noFn, "anything"), "CUSTOM without Fn must fail closed")
}

func TestUnknownTypeFailsClosed(t *testing.T) {
	r := Rule{Type: "NOPE"}
	require.Equal(t, "error.validation.nope", check(t, r, "x"))
}

func TestErrKeyFallback(t *testing.T) {
	r := Rule{Type: "NOTEMPTY"}
	require.Equal(t, "error.validation.notempty", check(t, r, ""))
}

// TestUniqueQuery asserts the SQL shape of the UNIQUE duplicate check on a
// connection-less DryRun handle (project convention); real duplicate
// behavior runs in the API integration suite against MariaDB.
func TestUniqueQuery(t *testing.T) {
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{DryRun: true})
	require.NoError(t, err)

	t.Run("create scopes by column only", func(t *testing.T) {
		vc := &Context{Ctx: context.Background(), DB: db, Table: "server_ip", Column: "ip_address", PKColumn: "server_ip_id"}
		var n int64
		stmt := uniqueQuery(vc, "10.0.0.1").Session(&gorm.Session{DryRun: true}).Count(&n).Statement
		require.Contains(t, stmt.SQL.String(), "`ip_address` = ?")
		require.NotContains(t, stmt.SQL.String(), "server_ip_id")
		require.Equal(t, []interface{}{"10.0.0.1"}, stmt.Vars)
	})

	t.Run("update excludes the record itself", func(t *testing.T) {
		vc := &Context{Ctx: context.Background(), DB: db, Table: "server_ip", Column: "ip_address", PKColumn: "server_ip_id", PK: uint32(7)}
		var n int64
		stmt := uniqueQuery(vc, "10.0.0.1").Session(&gorm.Session{DryRun: true}).Count(&n).Statement
		require.Contains(t, stmt.SQL.String(), "`ip_address` = ?")
		require.Contains(t, stmt.SQL.String(), "`server_ip_id` != ?")
		require.Equal(t, []interface{}{"10.0.0.1", uint32(7)}, stmt.Vars)
	})
}
