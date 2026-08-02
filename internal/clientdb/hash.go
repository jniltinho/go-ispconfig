package clientdb

import (
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // MySQL mysql_native_password is defined over SHA1
	"crypto/sha256"
	"fmt"
	"strings"
)

// NativePasswordHash returns the mysql_native_password hash of a plaintext
// password: '*' + uppercase hex of SHA1(SHA1(password)) (port of
// db_mysql getPasswordHash, encryption MYSQL).
func NativePasswordHash(password string) string {
	inner := sha1.Sum([]byte(password)) //nolint:gosec // protocol-defined
	outer := sha1.Sum(inner[:])         //nolint:gosec // protocol-defined
	return fmt.Sprintf("*%X", outer[:])
}

// Sha2PasswordHash returns a caching_sha2_password-compatible hash of a
// plaintext password in MySQL's $A$005$<salt><digest> authentication
// string format, with a random 20-byte salt and the MySQL default 5000
// rounds (port of db_mysql getPasswordHash, encryption MYSQLSHA2).
func Sha2PasswordHash(password string) (string, error) {
	salt, err := genSalt(20)
	if err != nil {
		return "", err
	}
	return mysqlSha256Crypt([]byte(password), salt, 5000), nil
}

// genSalt returns size random bytes constrained to MySQL's salt alphabet:
// 7-bit, no control chars, never '$' (port of db_mysql genSalt).
func genSalt(size int) ([]byte, error) {
	salt := make([]byte, size)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("clientdb: generating salt: %w", err)
	}
	for i := range salt {
		b := salt[i] & 0x7f
		if b < 32 {
			b += 32
		}
		if b == '$' {
			b++
		}
		salt[i] = b
	}
	return salt, nil
}

// mysqlSha256Crypt is the SHA256-crypt variant MySQL uses for
// caching_sha2_password authentication strings: standard SHA-crypt
// (akkadia.org/drepper/SHA-crypt.txt) except the salt is 20 bytes and not
// truncated to 16 (port of db_mysql mysqlSha256Crypt /
// mysys/crypt_genhash_impl.cc).
func mysqlSha256Crypt(plaintext, salt []byte, rounds int) string {
	ctxA := sha256.New()
	ctxA.Write(plaintext)
	ctxA.Write(salt)

	ctxB := sha256.New()
	ctxB.Write(plaintext)
	ctxB.Write(salt)
	ctxB.Write(plaintext)
	sumB := ctxB.Sum(nil)

	i := len(plaintext)
	for ; i > 32; i -= 32 {
		ctxA.Write(sumB)
	}
	ctxA.Write(sumB[:i])
	for i = len(plaintext); i > 0; i >>= 1 {
		if i&1 != 0 {
			ctxA.Write(sumB)
		} else {
			ctxA.Write(plaintext)
		}
	}
	sumA := ctxA.Sum(nil)

	ctxDP := sha256.New()
	for range plaintext {
		ctxDP.Write(plaintext)
	}
	dp := ctxDP.Sum(nil)
	var p []byte
	for i = len(plaintext); i > 32; i -= 32 {
		p = append(p, dp...)
	}
	p = append(p, dp[:i]...)

	ctxDS := sha256.New()
	for range 16 + int(sumA[0]) {
		ctxDS.Write(salt)
	}
	ds := ctxDS.Sum(nil)
	var s []byte
	for i = len(salt); i >= 32; i -= 32 {
		s = append(s, ds...)
	}
	s = append(s, ds[:i]...)

	c := sumA
	for i := range rounds {
		ctxC := sha256.New()
		if i&1 != 0 {
			ctxC.Write(p)
		} else {
			ctxC.Write(c)
		}
		if i%3 != 0 {
			ctxC.Write(s)
		}
		if i%7 != 0 {
			ctxC.Write(p)
		}
		if i&1 != 0 {
			ctxC.Write(c)
		} else {
			ctxC.Write(p)
		}
		c = ctxC.Sum(nil)
	}

	const alphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	var b64 strings.Builder
	b64From24bit := func(b2, b1, b0 byte, n int) {
		w := uint32(b2)<<16 | uint32(b1)<<8 | uint32(b0)
		for ; n > 0; n-- {
			b64.WriteByte(alphabet[w&0x3f])
			w >>= 6
		}
	}
	b64From24bit(c[0], c[10], c[20], 4)
	b64From24bit(c[21], c[1], c[11], 4)
	b64From24bit(c[12], c[22], c[2], 4)
	b64From24bit(c[3], c[13], c[23], 4)
	b64From24bit(c[24], c[4], c[14], 4)
	b64From24bit(c[15], c[25], c[5], 4)
	b64From24bit(c[6], c[16], c[26], 4)
	b64From24bit(c[27], c[7], c[17], 4)
	b64From24bit(c[18], c[28], c[8], 4)
	b64From24bit(c[9], c[19], c[29], 4)
	b64From24bit(0, c[31], c[30], 3)

	return fmt.Sprintf("$A$%03x$%s%s", rounds/1000, salt, b64.String())
}
