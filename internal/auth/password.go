// Package auth implements panel authentication: password verification with
// ISPConfig3 legacy hash support (design D10), sys_session-backed sessions
// with CSRF protection (design D4) and security policy flags.
package auth

import (
	"strings"

	"github.com/GehirnInc/crypt"
	// Register the legacy ISPConfig3 crypt schemes ($1$ MD5-crypt and
	// $6$ SHA-512 crypt) with the crypt registry.
	_ "github.com/GehirnInc/crypt/md5_crypt"
	_ "github.com/GehirnInc/crypt/sha512_crypt"
	"golang.org/x/crypto/bcrypt"
)

// legacyPrefixes are the crypt(3) scheme prefixes ISPConfig3 writes into
// sys_user.passwort: SHA-512 crypt and the older MD5-crypt.
var legacyPrefixes = []string{"$6$", "$1$"}

// dummyHash is a valid bcrypt hash of a throwaway value. When the stored
// hash is empty (unknown username), a comparison is burned against it so a
// login attempt costs the same whether or not the user exists — otherwise
// response timing would reveal valid usernames.
var dummyHash = []byte("$2a$10$a4yt9izRIwPQ6OFD7gKkCuk2/EXn9oCKRQzfKNejH4d20.3EXNSaG")

// IsLegacyHash reports whether hash uses an ISPConfig3 crypt scheme
// ($6$ SHA-512 crypt or $1$ MD5-crypt) rather than bcrypt.
func IsLegacyHash(hash string) bool {
	for _, p := range legacyPrefixes {
		if strings.HasPrefix(hash, p) {
			return true
		}
	}
	return false
}

// VerifyPassword checks a cleartext password against a stored hash, which
// may be bcrypt ($2a$/$2b$/$2y$) or a legacy ISPConfig3 crypt hash
// ($6$ SHA-512 crypt, $1$ MD5-crypt). It returns false for empty, unknown
// or malformed hashes; it never returns an error to avoid leaking which
// part of the verification failed.
func VerifyPassword(password, hash string) bool {
	switch {
	case password == "":
		return false
	case hash == "":
		// Timing oracle defense: burn a bcrypt comparison so a
		// nonexistent user costs the same as a wrong password.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return false
	case IsLegacyHash(hash):
		c := crypt.NewFromHash(hash)
		if c == nil {
			return false
		}
		return c.Verify(hash, []byte(password)) == nil
	case strings.HasPrefix(hash, "$2"):
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	default:
		return false
	}
}

// HashPassword hashes a cleartext password with bcrypt at the default cost.
// This is the only hash scheme go-ispconfig writes for new passwords.
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(h), err
}

// CryptPassword hashes a cleartext password with SHA-512 crypt ($6$), the
// scheme ISPConfig3 stores for web folder users and stats passwords. These
// hashes are copied verbatim into .htpasswd files by the daemon and must
// stay verifiable by nginx's crypt(3)-based auth_basic, so bcrypt is not an
// option here.
func CryptPassword(password string) (string, error) {
	return crypt.SHA512.New().Generate([]byte(password), nil)
}

// VerifyAndMaybeRehash verifies password against hash and, when the hash is
// a legacy crypt hash and rehashLegacy is true (config auth.rehash_legacy),
// returns a fresh bcrypt hash the caller must persist. newHash is empty when
// no rehash is wanted. Rehash is opt-in because PHP ISPConfig cannot verify
// bcrypt: an eager rewrite would break the cutover rollback path (design D10).
func VerifyAndMaybeRehash(password, hash string, rehashLegacy bool) (ok bool, newHash string, err error) {
	if !VerifyPassword(password, hash) {
		return false, "", nil
	}
	if rehashLegacy && IsLegacyHash(hash) {
		newHash, err = HashPassword(password)
		if err != nil {
			return true, "", err
		}
	}
	return true, newHash, nil
}
