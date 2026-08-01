package database

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"go-ispconfig/internal/model"
)

// defaultServerConfig is the minimal server.config INI seeded for a fresh
// local server (nginx + Bind, the go-ispconfig target stack). It uses the
// same key names as ISPConfig's server.ini.master so getconf and the
// nginx/bind modules read it unchanged.
const defaultServerConfig = `[web]
server_type=nginx
website_basedir=/var/www
website_path=/var/www/clients/client[client_id]/web[website_id]
user=www-data
group=www-data
nginx_user=www-data
nginx_group=www-data
nginx_vhost_conf_dir=/etc/nginx/sites-available
nginx_vhost_conf_enabled_dir=/etc/nginx/sites-enabled
security_level=20
php_fpm_start_port=9010

[dns]
bind_user=root
bind_group=bind
bind_zonefiles_dir=/etc/bind
bind_keyfiles_dir=/etc/bind
bind_zonefiles_masterprefix=pri.
bind_zonefiles_slaveprefix=slave/sec.
named_conf_path=/etc/bind/named.conf
named_conf_local_path=/etc/bind/named.conf.local
disable_bind_log=n
`

// Seed finishes a fresh install after Migrate executed the DDL: it sets a
// randomly generated bcrypt password on the admin sys_user (id 1, inserted
// by the dump with an unusable placeholder) and creates the local server row
// (web + dns roles) with a minimal config INI. The admin group and essential
// sys_config defaults (db.db_version, interface.session_timeout) already
// come from the embedded dump. It returns the generated plaintext password,
// which the caller must print exactly once.
func Seed(db *gorm.DB, hostname string) (adminPassword string, err error) {
	adminPassword, err = randomPassword(20)
	if err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(adminPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing admin password: %w", err)
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.SysUser{}).Where("userid = 1").Update("passwort", string(hash))
		if res.Error != nil {
			return fmt.Errorf("setting admin password: %w", res.Error)
		}
		if res.RowsAffected != 1 {
			return fmt.Errorf("admin sys_user (id 1) not found; schema seed data is incomplete")
		}

		server := model.Server{
			SysUserID:    1,
			SysGroupID:   1,
			SysPermUser:  "riud",
			SysPermGroup: "riud",
			SysPermOther: "r",
			ServerName:   hostname,
			WebServer:    1,
			DNSServer:    1,
			Config:       defaultServerConfig,
			DBVersion:    MinDBVersion,
			Active:       1,
		}
		if err := tx.Create(&server).Error; err != nil {
			return fmt.Errorf("creating local server row: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return adminPassword, nil
}

// randomPassword returns n characters from an unambiguous alphanumeric
// alphabet using crypto/rand.
func randomPassword(n int) (string, error) {
	const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	out := make([]byte, n)
	for i := range out {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", fmt.Errorf("generating password: %w", err)
		}
		out[i] = alphabet[idx.Int64()]
	}
	return string(out), nil
}
