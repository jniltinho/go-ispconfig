package repository

import (
	"crypto/md5" //nolint:gosec // identifier only, not a security hash: PHP ISPConfig stores md5(ip) in attempts_login.ip
	"encoding/hex"
	"fmt"
	"net"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// maxLoginAttempts failed logins within the last minute lock further
// attempts (port of interface/web/login/index.php).
const maxLoginAttempts = 5

// HashIP returns the md5 hex of a remote address's host part, the key
// format PHP ISPConfig uses for the attempts_login.ip column. An "ip:port"
// RemoteAddr is reduced to the ip first so ephemeral ports of the same
// client accumulate in one counter; a bare ip is hashed as-is.
func HashIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		remoteAddr = host
	}
	sum := md5.Sum([]byte(remoteAddr)) //nolint:gosec // see import comment
	return hex.EncodeToString(sum[:])
}

// TooManyLoginAttempts reports whether the remote address exceeded
// maxLoginAttempts failed logins within the last minute and must be
// blocked (spec scenario "Brute force lockout").
func TooManyLoginAttempts(db *gorm.DB, remoteAddr string) (bool, error) {
	row, err := recentAttempts(db, remoteAddr)
	if err != nil {
		return false, err
	}
	return row.Times > maxLoginAttempts, nil
}

// RecordFailedLogin counts a failed login for the remote address. The
// increment is one atomic UPDATE of the newest counter row in the window;
// only when no row matched is a fresh row inserted. attempts_login carries
// no unique key (PHP-verbatim schema, design D9), so INSERT ... ON
// DUPLICATE KEY UPDATE is not available — the UPDATE-first form removes the
// read-then-write race of the old SELECT+UPDATE pair; two concurrent first
// failures may still insert two rows, which only ever undercounts.
func RecordFailedLogin(db *gorm.DB, remoteAddr string) error {
	ip := HashIP(remoteAddr)
	res := db.Exec("UPDATE `attempts_login` SET `times` = `times` + 1, `login_time` = NOW() WHERE `ip` = ? AND `login_time` > (NOW() - INTERVAL 1 MINUTE) ORDER BY `login_time` DESC LIMIT 1", ip)
	if res.Error != nil {
		return fmt.Errorf("recording failed login: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		if err := db.Exec("INSERT INTO `attempts_login` (`ip`, `times`, `login_time`) VALUES (?, 1, NOW())", ip).Error; err != nil {
			return fmt.Errorf("recording failed login: %w", err)
		}
	}
	return nil
}

// ClearLoginAttempts removes the failure counters for a remote address
// after a successful login.
func ClearLoginAttempts(db *gorm.DB, remoteAddr string) error {
	return db.Exec("DELETE FROM `attempts_login` WHERE `ip` = ?", HashIP(remoteAddr)).Error
}

// recentAttempts loads the failure row of the last minute for the address
// (zero-valued row when none), mirroring the PHP lookup window.
func recentAttempts(db *gorm.DB, remoteAddr string) (model.AttemptsLogin, error) {
	var row model.AttemptsLogin
	err := db.Where("ip = ? AND login_time > (NOW() - INTERVAL 1 MINUTE)", HashIP(remoteAddr)).
		Limit(1).Find(&row).Error
	if err != nil {
		return row, fmt.Errorf("checking login attempts: %w", err)
	}
	return row, nil
}
