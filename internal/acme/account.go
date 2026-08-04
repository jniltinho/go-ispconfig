package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-acme/lego/v4/registration"
)

// Account is the ACME account this node registers under. It implements lego's
// registration.User.
//
// Losing a certificate costs a re-issue; losing the account key costs the
// rate-limit history attached to it, which is why it is stored apart from the
// certificates and never in the database — a private key on sys_datalog's
// replication path would be handed to every node in a multi-server install.
type Account struct {
	Email        string                 `json:"email"`
	Registration *registration.Resource `json:"registration"`

	key crypto.PrivateKey
}

// GetEmail returns the account contact address (registration.User).
func (a *Account) GetEmail() string { return a.Email }

// GetRegistration returns the stored registration, nil before the first one.
func (a *Account) GetRegistration() *registration.Resource { return a.Registration }

// GetPrivateKey returns the account key, which is what carries the CA's
// rate-limit history for this node.
func (a *Account) GetPrivateKey() crypto.PrivateKey { return a.key }

// AccountDir is where this node's account lives. Keyed by server id because
// the panel is per-server, which certbot has no concept of — the one
// deliberate divergence from its layout.
func AccountDir(root string, serverID uint32) string {
	if root == "" {
		root = DefaultRoot
	}
	return filepath.Join(root, "accounts", fmt.Sprintf("%d", serverID))
}

// LoadOrCreateAccount reads the stored account, generating and persisting a
// key on first use. It does not register: that is the caller's job once it has
// a lego client, and the returned Registration is nil until it does.
func LoadOrCreateAccount(dir, email string) (*Account, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("acme: creating account dir: %w", err)
	}
	keyPath := filepath.Join(dir, "account.key")
	jsonPath := filepath.Join(dir, "account.json")

	acc := &Account{Email: email}
	if raw, err := os.ReadFile(jsonPath); err == nil {
		if err := json.Unmarshal(raw, acc); err != nil {
			return nil, fmt.Errorf("acme: reading %s: %w", jsonPath, err)
		}
		// The stored email wins: re-registering under a different contact
		// would orphan the rate-limit history of the existing account.
		if acc.Email == "" {
			acc.Email = email
		}
	}

	switch raw, err := os.ReadFile(keyPath); {
	case err == nil:
		block, _ := pem.Decode(raw)
		if block == nil {
			return nil, fmt.Errorf("acme: %s is not PEM", keyPath)
		}
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("acme: parsing %s: %w", keyPath, err)
		}
		acc.key = key
	case os.IsNotExist(err):
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, err
		}
		buf := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
		if err := writeFileAtomic(keyPath, buf, 0o600); err != nil {
			return nil, fmt.Errorf("acme: writing %s: %w", keyPath, err)
		}
		acc.key = key
	default:
		return nil, err
	}
	return acc, nil
}

// Save persists the registration resource next to the key, so the next start
// reuses the account instead of registering a second one.
func (a *Account) Save(dir string) error {
	raw, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "account.json"), raw, 0o600)
}
