package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/spf13/viper"
	"gorm.io/gorm"

	"go-ispconfig/internal/database"
)

// mariadbStep creates the ISPConfig database and user as MariaDB root
// (unix socket by default, design D5), then delegates schema creation to
// the foundation Migrate/Seed code path (design D4) — the installer never
// carries its own DDL. On a fresh install Seed also creates the local
// server row (web+dns) and the admin user; the generated admin password is
// kept in State for the final summary.
type mariadbStep struct{}

// Name identifies the step in the pipeline log.
func (mariadbStep) Name() string { return "mariadb" }

// Run converges database, user, grants and schema; safe to re-run.
func (mariadbStep) Run(_ context.Context, st *State) error {
	// Reuse the DB password of an existing config.toml so re-runs never
	// rotate working credentials (spec: credentials survive re-run).
	if st.DBPassword == "" {
		st.DBPassword = existingDBPassword(filepath.Join(st.ConfigDir, "config.toml"), st.Answers.DBUser)
	}
	if st.DBPassword == "" {
		pw, err := database.RandomPassword(20)
		if err != nil {
			return err
		}
		st.DBPassword = pw
	}

	root, err := connectRoot(st)
	if err != nil {
		return err
	}
	defer closeDB(root)

	a := st.Answers
	// Answers validation restricts DBName/DBUser to identifier chars; the
	// password (generated or from our own config.toml) is quote-escaped.
	pw := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(st.DBPassword)
	stmts := []string{
		fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci", a.DBName),
	}
	for _, host := range st.DBUserHosts {
		stmts = append(stmts,
			fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY '%s'", a.DBUser, host, pw),
			// ALTER keeps an existing user in sync with the (possibly
			// reused) password in State.
			fmt.Sprintf("ALTER USER '%s'@'%s' IDENTIFIED BY '%s'", a.DBUser, host, pw),
			fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'%s'", a.DBName, a.DBUser, host),
		)
	}
	for _, s := range stmts {
		if err := root.Exec(s).Error; err != nil {
			return fmt.Errorf("mariadb setup: %w", err)
		}
	}

	if err := clientdbAdmin(st, root); err != nil {
		return err
	}

	// Schema through the foundation migrate path (embedded ispconfig3.sql,
	// existing-schema detection included).
	db, err := database.Open(st.ispconfigDSN())
	if err != nil {
		return fmt.Errorf("connecting as %s: %w", a.DBUser, err)
	}
	st.DB = db
	needSeed, err := database.Migrate(db)
	if err != nil {
		return err
	}
	if !needSeed {
		st.logf("  existing ISPConfig schema adopted; no DDL, no seed")
		return nil
	}
	adminPassword, err := database.Seed(db, a.Hostname, "")
	if err != nil {
		return err
	}
	st.AdminPassword = adminPassword
	return nil
}

// clientdbAdmin creates the dedicated client-DB admin account and writes its
// credentials file. The mysql_clientdb plugin refuses to provision any client
// database without it, so skipping this leaves every site's database created
// in the panel but absent from MariaDB. It needs GRANT OPTION because it hands
// privileges to the per-site database users it creates.
func clientdbAdmin(st *State, root *gorm.DB) error {
	const user = "goisp_clientdb"
	path := filepath.Join(st.ConfigDir, "mysql_clientdb.conf")

	// Reuse the password of an existing file so re-runs never break a
	// working install.
	pw := existingClientDBPassword(path)
	if pw == "" {
		generated, err := database.RandomPassword(20)
		if err != nil {
			return err
		}
		pw = generated
	}
	esc := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(pw)

	for _, host := range st.DBUserHosts {
		for _, s := range []string{
			fmt.Sprintf("CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY '%s'", user, host, esc),
			fmt.Sprintf("ALTER USER '%s'@'%s' IDENTIFIED BY '%s'", user, host, esc),
			fmt.Sprintf("GRANT ALL PRIVILEGES ON *.* TO '%s'@'%s' WITH GRANT OPTION", user, host),
		} {
			if err := root.Exec(s).Error; err != nil {
				return fmt.Errorf("client-DB admin setup: %w", err)
			}
		}
	}

	content := fmt.Sprintf("clientdb_host = %q\nclientdb_port = 3306\nclientdb_user = %q\nclientdb_password = %q\n",
		"localhost", user, pw)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// existingClientDBPassword returns the password already recorded in the
// client-DB credentials file, or "" when the file is missing or unreadable.
func existingClientDBPassword(path string) string {
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("toml")
	if err := v.ReadInConfig(); err != nil {
		return ""
	}
	return v.GetString("clientdb_password")
}

// rootDSN builds a root DSN through mysql.Config so an operator-supplied
// password with DSN metacharacters (@ : / ...) survives the round trip.
func rootDSN(passwd, net, addr string) string {
	cfg := mysql.NewConfig()
	cfg.User = "root"
	cfg.Passwd = passwd
	cfg.Net = net
	cfg.Addr = addr
	cfg.Params = map[string]string{"charset": "utf8mb4"}
	return cfg.FormatDSN()
}

// connectRoot opens a root MariaDB session: unix socket without password
// first (Debian/Ubuntu default auth), then the --db-root-password answer
// over socket and TCP.
func connectRoot(st *State) (*gorm.DB, error) {
	dsns := []string{rootDSN("", "unix", st.MySQLSocket)}
	if pw := st.Answers.DBRootPassword; pw != "" {
		dsns = append(dsns,
			rootDSN(pw, "unix", st.MySQLSocket),
			rootDSN(pw, "tcp", st.DBAddr),
		)
	}
	var lastErr error
	for _, dsn := range dsns {
		db, err := database.Open(dsn)
		if err == nil {
			return db, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("connecting to MariaDB as root (socket %s): %w — is mariadb running? use --db-root-password if socket auth is disabled",
		st.MySQLSocket, lastErr)
}

// ispconfigDSN is the application DSN written to config.toml and used for
// the schema/seed steps.
func (st *State) ispconfigDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		st.Answers.DBUser, st.DBPassword, st.DBAddr, st.Answers.DBName)
}

// existingDBPassword extracts the password of user from the DSN of an
// existing config.toml; "" when absent or unparsable.
func existingDBPassword(configPath, user string) string {
	if _, err := os.Stat(configPath); err != nil {
		return ""
	}
	v := viper.New()
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		return ""
	}
	cfg, err := mysql.ParseDSN(v.GetString("database.dsn"))
	if err != nil || cfg.User != user {
		return ""
	}
	return cfg.Passwd
}

// closeDB closes the underlying sql.DB of a gorm handle.
func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
