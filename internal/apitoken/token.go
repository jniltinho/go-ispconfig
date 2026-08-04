// Package apitoken implements the machine credential of the REST API
// (spec add-api-tokens): a long-lived token an admin mints in the panel,
// presented as `Authorization: Bearer goisp_<id>_<secret>` on the same
// endpoints the SPA uses, plus the scope model that attenuates what the
// token's owner may do.
//
// Tokens live in the ISPConfig3 `remote_user` table, which ships in the
// schema and was never read by this port (design D1): the owner is
// sys_userid, the SHA-256 digest of the secret is remote_password, the
// enabled flag is remote_access, the IP allow-list is remote_ips and the
// scopes plus expiry and last-used ride in remote_functions (design D2).
package apitoken

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

// Prefix marks a go-ispconfig API token. It makes a leaked credential
// greppable in a log and recognisable to secret-scanning tooling.
const Prefix = "goisp_"

// secretBytes is the entropy of the secret half. 256 bits of uniform
// randomness has no dictionary to attack, which is why the digest is a plain
// SHA-256 and not bcrypt: a slow KDF would add cost to every API call and buy
// nothing (design D3).
const secretBytes = 32

// ErrMalformed is returned by Parse for anything that is not shaped like a
// token. Callers must not distinguish it from an unknown id in a response —
// both are simply "unauthenticated".
var ErrMalformed = errors.New("apitoken: malformed token")

// Mint returns the plaintext credential for a stored token id and the digest
// to persist. The plaintext is the only copy: it is returned to the creator
// once and never stored.
func Mint(id uint32) (plaintext, digest string, err error) {
	buf := make([]byte, secretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	secret := base64.RawURLEncoding.EncodeToString(buf)
	return Prefix + strconv.FormatUint(uint64(id), 10) + "_" + secret, Digest(secret), nil
}

// Digest is the stored form of a secret.
func Digest(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// Parse splits a presented credential into its token id and secret. The id
// travels in the credential so verification is one primary-key lookup plus
// one constant-time compare, instead of a digest compare against every row.
func Parse(presented string) (id uint32, secret string, err error) {
	rest, ok := strings.CutPrefix(presented, Prefix)
	if !ok {
		return 0, "", ErrMalformed
	}
	idPart, secretPart, ok := strings.Cut(rest, "_")
	if !ok || idPart == "" || secretPart == "" {
		return 0, "", ErrMalformed
	}
	n, convErr := strconv.ParseUint(idPart, 10, 32)
	if convErr != nil || n == 0 {
		return 0, "", ErrMalformed
	}
	return uint32(n), secretPart, nil
}

// Looks reports whether a bearer value is shaped like an API token. The
// middleware uses it to decide which credential resolver to try, never to
// decide whether a request is authorised.
func Looks(bearer string) bool {
	return strings.HasPrefix(bearer, Prefix)
}

// VerifyDigest compares a presented secret against a stored digest in
// constant time.
func VerifyDigest(stored, secret string) bool {
	return subtle.ConstantTimeCompare([]byte(stored), []byte(Digest(secret))) == 1
}
