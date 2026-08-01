package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// Real crypt(3) fixtures generated with openssl passwd -6 / -1 for the
// password "secret123" (and one UTF-8 password), including the
// $6$rounds=5000$ salt format that ISPConfig3's crypt_password() writes.
const (
	sha512Hash       = "$6$nnGUnBKWLFoUKUw3$f/3QI/na.v7iVt9tDvrh.BI4t6n8mrK6QR.A54tbZDMlvmhYW5LAM5/R58pkmRjoqoS1mDdnvu.LQ5cxuTfPm."
	sha512RoundsHash = "$6$rounds=5000$0123456789abcdef$Kxy.mOcHGoObTjq8dN5ieAd3z/8lD5MMihKbH3r0XBTx.u.iBlZIfB8Wt8YLTrCYQIMFkr/AGoXRL5umwugd.."
	md5Hash          = "$1$kvkDCAHN$kks2DWA8nFL/FdREQ2gjc0"
	sha512UTF8Hash   = "$6$UENZ398b4HVJ7q1P$v631cuA.DKIYYR8sFSb4lWXMZ8v/FNvdc9RZdlgTkMn3N82lLZtOR43xOk2olKeOiRT5JfOQs/culdt42h0Wp0"
)

func TestVerifyPassword(t *testing.T) {
	bcryptHash, err := HashPassword("secret123")
	require.NoError(t, err)

	tests := []struct {
		name     string
		password string
		hash     string
		want     bool
	}{
		{"sha512 ok", "secret123", sha512Hash, true},
		{"sha512 wrong password", "wrong", sha512Hash, false},
		{"sha512 rounds=5000 ok", "secret123", sha512RoundsHash, true},
		{"sha512 rounds=5000 wrong", "Secret123", sha512RoundsHash, false},
		{"md5-crypt ok", "secret123", md5Hash, true},
		{"md5-crypt wrong", "secret1234", md5Hash, false},
		{"sha512 utf-8 password ok", "pä§ßword", sha512UTF8Hash, true},
		{"bcrypt ok", "secret123", bcryptHash, true},
		{"bcrypt wrong", "wrong", bcryptHash, false},
		{"empty hash", "secret123", "", false},
		{"empty password", "", sha512Hash, false},
		{"plaintext stored value", "secret123", "secret123", false},
		{"unknown scheme", "secret123", "$9$foo$bar", false},
		{"malformed legacy hash", "secret123", "$6$", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, VerifyPassword(tt.password, tt.hash))
		})
	}
}

func TestIsLegacyHash(t *testing.T) {
	require.True(t, IsLegacyHash(sha512Hash))
	require.True(t, IsLegacyHash(md5Hash))
	require.False(t, IsLegacyHash("$2a$10$abcdefghijklmnopqrstuv"))
	require.False(t, IsLegacyHash(""))
}

func TestVerifyAndMaybeRehash(t *testing.T) {
	t.Run("legacy hash, rehash disabled (default)", func(t *testing.T) {
		ok, newHash, err := VerifyAndMaybeRehash("secret123", sha512Hash, false)
		require.NoError(t, err)
		require.True(t, ok)
		require.Empty(t, newHash, "must not rehash while auth.rehash_legacy is off")
	})

	t.Run("legacy hash, rehash enabled", func(t *testing.T) {
		ok, newHash, err := VerifyAndMaybeRehash("secret123", md5Hash, true)
		require.NoError(t, err)
		require.True(t, ok)
		require.NotEmpty(t, newHash)
		require.NoError(t, bcrypt.CompareHashAndPassword([]byte(newHash), []byte("secret123")))
	})

	t.Run("bcrypt hash never rehashed", func(t *testing.T) {
		bcryptHash, err := HashPassword("secret123")
		require.NoError(t, err)
		ok, newHash, err := VerifyAndMaybeRehash("secret123", bcryptHash, true)
		require.NoError(t, err)
		require.True(t, ok)
		require.Empty(t, newHash)
	})

	t.Run("wrong password never rehashed", func(t *testing.T) {
		ok, newHash, err := VerifyAndMaybeRehash("wrong", sha512Hash, true)
		require.NoError(t, err)
		require.False(t, ok)
		require.Empty(t, newHash)
	})
}
