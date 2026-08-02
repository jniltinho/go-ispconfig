package mail

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// DKIM key handling (design D6, API side): pure crypto/rsa replaces the
// PHP openssl shell-outs. Key material is never logged.

// selectorRe ports validate_selector: lowercase alphanumeric, 1-63.
var selectorRe = regexp.MustCompile(`^[a-z0-9]{1,63}$`)

// ValidDKIMSelector reports whether the selector is acceptable.
func ValidDKIMSelector(s string) bool { return selectorRe.MatchString(s) }

// ErrInvalidDKIMKey is returned for unparsable or non-RSA private keys.
var ErrInvalidDKIMKey = errors.New("mail: invalid DKIM private key")

// GenerateDKIMKey creates an RSA key pair of the given strength and
// returns both sides PEM-encoded (private PKCS#1, public PKIX — the
// formats openssl genrsa/rsa -pubout emit).
func GenerateDKIMKey(bits int) (privPEM, pubPEM string, err error) {
	if bits < 1024 {
		bits = 2048
	}
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return "", "", fmt.Errorf("mail: generating DKIM key: %w", err)
	}
	privPEM = string(pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
	pubPEM, err = encodePublicKey(&key.PublicKey)
	return privPEM, pubPEM, err
}

// ParseDKIMPrivate validates a PEM private key (PKCS#1 or PKCS#8 RSA —
// openssl rsa -check parity).
func ParseDKIMPrivate(privPEM string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(privPEM))
	if block == nil {
		return nil, ErrInvalidDKIMKey
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, ErrInvalidDKIMKey
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, ErrInvalidDKIMKey
	}
	return key, nil
}

// DeriveDKIMPublic returns the PEM public key of a private key.
func DeriveDKIMPublic(privPEM string) (string, error) {
	key, err := ParseDKIMPrivate(privPEM)
	if err != nil {
		return "", err
	}
	return encodePublicKey(&key.PublicKey)
}

// encodePublicKey PEM-encodes an RSA public key in PKIX form.
func encodePublicKey(pub *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", fmt.Errorf("mail: encoding DKIM public key: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// DKIMTXTValue builds the TXT record data from the PEM public key
// (mail_domain_edit parity: PEM armor and line breaks stripped).
func DKIMTXTValue(pubPEM string) string {
	p := strings.NewReplacer(
		"-----BEGIN PUBLIC KEY-----", "",
		"-----END PUBLIC KEY-----", "",
		"\r", "", "\n", "",
	).Replace(pubPEM)
	return "v=DKIM1; t=s; p=" + p
}

// DKIMRecordName is the owner name of the DKIM TXT record.
func DKIMRecordName(selector, domain string) string {
	return selector + "._domainkey." + domain + "."
}
