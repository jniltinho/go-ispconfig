package powerdns

import (
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"go-ispconfig/internal/database"
)

// DeriveDSN builds a PowerDNS DSN from the panel MariaDB DSN by replacing
// the database name with DatabaseName ("powerdns"). query parameters are
// preserved. When override is non-empty it is returned as-is.
func DeriveDSN(panelDSN, override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), nil
	}
	if strings.TrimSpace(panelDSN) == "" {
		return "", fmt.Errorf("powerdns: empty panel DSN and no [powerdns] dsn override")
	}
	cfg, err := mysql.ParseDSN(panelDSN)
	if err != nil {
		return "", fmt.Errorf("powerdns: parsing panel DSN: %w", err)
	}
	cfg.DBName = DatabaseName
	return cfg.FormatDSN(), nil
}

// Open connects to the PowerDNS database. overrideDSN, when set, is used
// verbatim; otherwise the DSN is derived from panelDSN with database
// "powerdns". The connection is pinged so unreachable backends fail at
// open time with a clear error (daemon bootstrap).
func Open(panelDSN, overrideDSN string) (*gorm.DB, error) {
	dsn, err := DeriveDSN(panelDSN, overrideDSN)
	if err != nil {
		return nil, err
	}
	db, err := database.Open(dsn)
	if err != nil {
		return nil, fmt.Errorf("powerdns: connecting to %s database: %w", DatabaseName, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("powerdns: underlying sql.DB: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("powerdns: database unreachable (dns_backend=powerdns requires a reachable %q MariaDB schema; install with --dns-backend powerdns or set [powerdns] dsn): %w", DatabaseName, err)
	}
	return db, nil
}
