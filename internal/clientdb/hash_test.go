package clientdb

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNativePasswordHash covers task 4.1 with known *SHA1(SHA1()) vectors
// (SELECT PASSWORD('...') on MySQL/MariaDB).
func TestNativePasswordHash(t *testing.T) {
	assert.Equal(t, "*14E65567ABDB5135D0CFD9A70B3032C179A49EE7", NativePasswordHash("secret"))
	assert.Equal(t, "*D8DECEC305209EEFEC43008E1D420E1AA06B19E0", NativePasswordHash("newpass"))
	assert.Equal(t, "*2470C0C06DEE42FD1618BB99005ADCA2EC9D1E19", NativePasswordHash("password"))
	// empty password still yields a well-formed 41-char hash, never ""
	assert.Len(t, NativePasswordHash(""), 41)
}

var sha2Re = regexp.MustCompile(`^\$A\$005\$.{20}[./0-9A-Za-z]{43}$`)

// TestSha2PasswordHash: MySQL $A$005$<20-byte salt><43-char digest>
// format, deterministic for a fixed salt, salt alphabet constraints.
func TestSha2PasswordHash(t *testing.T) {
	h, err := Sha2PasswordHash("secret")
	require.NoError(t, err)
	assert.Regexp(t, sha2Re, h)

	h2, err := Sha2PasswordHash("secret")
	require.NoError(t, err)
	assert.NotEqual(t, h, h2, "random salt must differ")

	salt := []byte("01234567890123456789")
	fixed1 := mysqlSha256Crypt([]byte("secret"), salt, 5000)
	fixed2 := mysqlSha256Crypt([]byte("secret"), salt, 5000)
	assert.Equal(t, fixed1, fixed2)
	assert.Regexp(t, sha2Re, fixed1)
	assert.NotEqual(t, fixed1, mysqlSha256Crypt([]byte("other"), salt, 5000))

	// long passwords exercise the >32-byte branches
	long := mysqlSha256Crypt([]byte("a-very-long-password-exceeding-32-bytes-for-sure"), salt, 5000)
	assert.Regexp(t, sha2Re, long)
}

// TestGenSalt: 20 bytes from the SHA-crypt alphabet.
func TestGenSalt(t *testing.T) {
	const alphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	for range 50 {
		salt, err := genSalt(20)
		require.NoError(t, err)
		require.Len(t, salt, 20)
		for _, b := range salt {
			assert.Contains(t, alphabet, string(b))
		}
	}
}
