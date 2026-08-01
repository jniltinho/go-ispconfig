package repository

import (
	"crypto/md5" //nolint:gosec // identifier only, not a security hash: PHP ISPConfig stores md5(ip) in attempts_login.ip
	"encoding/hex"
	"fmt"

	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// maxLoginAttempts failed logins within the last minute lock further
// attempts (port of interface/web/login/index.php).
const maxLoginAttempts = 5

// HashIP returns the md5 hex of a remote address, the key format PHP
// ISPConfig uses for the attempts_login.ip column.
func HashIP(remoteAddr string) string {
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

// RecordFailedLogin counts a failed login for the remote address: first
// failure inserts a row, later ones increment the newest row and refresh
// its timestamp (exact port of the PHP insert/update pair).
func RecordFailedLogin(db *gorm.DB, remoteAddr string) error {
	ip := HashIP(remoteAddr)
	row, err := recentAttempts(db, remoteAddr)
	if err != nil {
		return err
	}
	if row.Times == 0 {
		err = db.Exec("INSERT INTO `attempts_login` (`ip`, `times`, `login_time`) VALUES (?, 1, NOW())", ip).Error
	} else {
		err = db.Exec("UPDATE `attempts_login` SET `times` = `times` + 1, `login_time` = NOW() WHERE `ip` = ? ORDER BY `login_time` DESC LIMIT 1", ip).Error
	}
	if err != nil {
		return fmt.Errorf("recording failed login: %w", err)
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
