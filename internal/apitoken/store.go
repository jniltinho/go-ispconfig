package apitoken

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// ErrDenied is the single failure of a token verification. It is deliberately
// one error for every cause — unknown id, wrong secret, revoked, expired,
// inactive owner, disallowed IP — so a caller can never probe which token ids
// exist by reading the difference.
var ErrDenied = errors.New("apitoken: denied")

// Verified is a successfully authenticated token and the owner it acts as.
type Verified struct {
	// ID is remote_user.remote_userid.
	ID uint32
	// Label is remote_user.remote_username, the human name of the token.
	Label string
	// Owner is the sys_user the request executes as.
	Owner model.SysUser
	// Scopes are the granted scopes.
	Scopes []string
}

// lastUsedInterval bounds how often a successful authentication writes the
// last-used stamp. A busy automation client would otherwise turn every read
// into a write (spec: "at most once per minute per token").
const lastUsedInterval = time.Minute

// lastUsedWrites remembers the in-process time of the last write per token so
// the throttle costs no query. Losing it on restart only means one extra
// write, which is why a plain map is enough.
//
// ponytail: process-local map; if the panel is ever run as several serve
// processes the throttle becomes per-process, which is still bounded.
var lastUsedWrites sync.Map

// Verify resolves a presented credential to its owner, applying every gate:
// the digest, the enabled flag, the token expiry, the owner's active flag and
// the IP allow-list. callerIP is the already-resolved client address (behind
// the trusted-proxy chain), and may be empty when it cannot be determined.
func Verify(ctx context.Context, db *gorm.DB, presented, callerIP string, now time.Time) (*Verified, error) {
	id, secret, err := Parse(presented)
	if err != nil {
		return nil, ErrDenied
	}

	var row model.RemoteUser
	if err := db.WithContext(ctx).Where("remote_userid = ?", id).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Burn a digest computation so an unknown id costs the same as a
			// wrong secret.
			_ = Digest(secret)
			return nil, ErrDenied
		}
		return nil, err
	}
	if !VerifyDigest(row.RemotePassword, secret) {
		return nil, ErrDenied
	}
	if row.RemoteAccess != "y" {
		return nil, ErrDenied
	}

	meta := ParseMeta(row.RemoteFunctions)
	if len(meta.Scopes) == 0 || meta.Expired(now) {
		return nil, ErrDenied
	}
	if !IPAllowed(row.RemoteIPs, callerIP) {
		return nil, ErrDenied
	}

	var owner model.SysUser
	if err := db.WithContext(ctx).Where("userid = ?", row.SysUserID).Take(&owner).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDenied
		}
		return nil, err
	}
	if owner.Active != 1 {
		return nil, ErrDenied
	}

	touchLastUsed(ctx, db, row, meta, now)

	return &Verified{ID: row.RemoteUserID, Label: row.RemoteUsername, Owner: owner, Scopes: meta.Scopes}, nil
}

// touchLastUsed records the use, at most once per token per interval. A
// failure here is deliberately ignored: the request already authenticated,
// and losing a timestamp must never turn into a 500.
func touchLastUsed(ctx context.Context, db *gorm.DB, row model.RemoteUser, meta Meta, now time.Time) {
	if prev, ok := lastUsedWrites.Load(row.RemoteUserID); ok {
		if last, ok := prev.(time.Time); ok && now.Sub(last) < lastUsedInterval {
			return
		}
	}
	if !meta.LastUsed.IsZero() && now.Sub(meta.LastUsed) < lastUsedInterval {
		lastUsedWrites.Store(row.RemoteUserID, now)
		return
	}
	meta.LastUsed = now
	lastUsedWrites.Store(row.RemoteUserID, now)
	_ = db.WithContext(ctx).Model(&model.RemoteUser{}).
		Where("remote_userid = ?", row.RemoteUserID).
		Update("remote_functions", meta.String()).Error
}

// ValidIPEntry reports whether one allow-list entry parses as an address or a
// CIDR. An entry that parses as neither would silently lock the token out of
// every address, so the API refuses it at the boundary.
func ValidIPEntry(entry string) error {
	if strings.Contains(entry, "/") {
		_, err := netip.ParsePrefix(entry)
		return err
	}
	_, err := netip.ParseAddr(entry)
	return err
}

// IPAllowed reports whether callerIP satisfies a CSV allow-list of addresses
// and CIDRs. An empty list allows any address — the field is optional
// hardening, matching ISPConfig3's remote_ips.
func IPAllowed(list, callerIP string) bool {
	list = strings.TrimSpace(list)
	if list == "" {
		return true
	}
	addr, err := netip.ParseAddr(callerIP)
	if err != nil {
		// The list restricts to named addresses and the caller's is unknown:
		// refuse rather than fall through to "any".
		return false
	}
	for entry := range strings.SplitSeq(list, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if prefix, err := netip.ParsePrefix(entry); err == nil && prefix.Contains(addr) {
				return true
			}
			continue
		}
		if want, err := netip.ParseAddr(entry); err == nil && want == addr {
			return true
		}
	}
	return false
}
