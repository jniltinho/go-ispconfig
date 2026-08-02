package importer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// ResetRequired lists the panel usernames the plan (re)creates with the
// unusable placeholder password: every sys_user planned as create. The
// remote API never exposes panel hashes, so all of them need the
// password-reset flow (design D5). Service-entity crypt hashes
// ($1$/$5$/$6$ — web folder users, client.password) are imported
// verbatim and are not listed here.
func (p *Plan) ResetRequired() []string {
	var users []string
	for _, it := range p.Items {
		if it.Table == "sys_user" && it.Action == ActionCreate {
			users = append(users, it.Key)
		}
	}
	return users
}

// ResetToken is one generated one-time password-reset token. Token is
// the cleartext handed to the operator (printed or e-mailed once); only
// its SHA-256 is stored in sys_user.lost_password_hash.
type ResetToken struct {
	// Username is the panel login the token belongs to.
	Username string `json:"username"`
	// Token is the one-time reset token (cleartext, never stored).
	Token string `json:"token"`
}

// HashResetToken returns the base64url SHA-256 stored (and later
// compared) for a reset token — the cleartext token never touches the
// database. Base64url keeps the digest inside the legacy
// lost_password_hash varchar(50).
func HashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// GenerateResetTokens creates one one-time reset token per username in
// bulk: a random 128-bit token whose SHA-256 lands in
// sys_user.lost_password_hash with lost_password_reqtime set and the
// lost-password function enabled. The cleartext tokens are only
// returned to the caller (CLI/wizard print or mail them); no plaintext
// password is ever generated or stored.
func GenerateResetTokens(ctx context.Context, db *gorm.DB, usernames []string) ([]ResetToken, error) {
	tokens := make([]ResetToken, 0, len(usernames))
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, username := range usernames {
			buf := make([]byte, 16)
			if _, err := rand.Read(buf); err != nil {
				return fmt.Errorf("generating reset token: %w", err)
			}
			token := hex.EncodeToString(buf)

			res := tx.Model(&model.SysUser{}).Where("username = ?", username).
				Updates(map[string]any{
					"lost_password_function": 1,
					"lost_password_hash":     HashResetToken(token),
					"lost_password_reqtime":  time.Now(),
				})
			if res.Error != nil {
				return fmt.Errorf("storing reset token for %s: %w", username, res.Error)
			}
			if res.RowsAffected == 0 {
				return fmt.Errorf("storing reset token: sys_user %q not found", username)
			}
			tokens = append(tokens, ResetToken{Username: username, Token: token})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tokens, nil
}
