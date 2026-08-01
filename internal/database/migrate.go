package database

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// MinDBVersion is the server.dbversion value of a fresh ISPConfig 3.3.1p1
// install (the highest install/sql/incremental/upd_NNNN.sql number shipped
// with the embedded schema). Older databases (3.1/3.2) must be upgraded with
// the PHP ISPConfig updater first (design D9).
const MinDBVersion = 104

// Migrate creates the ISPConfig3-identical schema on an empty database by
// executing the embedded ispconfig3.sql statement by statement, or adopts an
// existing ISPConfig3 database after validating its server.dbversion.
//
// It returns needSeed=true when the caller should run Seed: either the DDL
// was just executed (fresh install) or a complete schema with an empty
// server table was found (a previous install was interrupted between DDL
// and seed). needSeed=false means an existing schema was adopted.
func Migrate(db *gorm.DB) (needSeed bool, err error) {
	hasServer, err := tableExists(db, "server")
	if err != nil {
		return false, err
	}

	if hasServer {
		return validateExisting(db)
	}

	var tables int64
	err = db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()").
		Scan(&tables).Error
	if err != nil {
		return false, fmt.Errorf("inspecting database: %w", err)
	}
	if tables > 0 {
		return false, fmt.Errorf("database is not empty (%d tables) and has no ISPConfig `server` table; refusing to run DDL — point the DSN at an empty database or an existing ISPConfig 3.3.x database", tables)
	}

	stmts, err := SplitStatements(schemaSQL)
	if err != nil {
		return false, fmt.Errorf("parsing embedded schema: %w", err)
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			return false, fmt.Errorf("executing schema DDL: %w", err)
		}
	}
	return true, nil
}

// validateExisting checks that an already-present ISPConfig schema is at
// least the 3.3.x dbversion supported by go-ispconfig. No DDL is executed.
// A complete schema whose server table is empty (install interrupted after
// the DDL, before the seed) returns needSeed=true so the caller resumes the
// seed instead of aborting.
func validateExisting(db *gorm.DB) (needSeed bool, err error) {
	hasCol, err := columnExists(db, "server", "dbversion")
	if err != nil {
		return false, err
	}
	if !hasCol {
		return false, fmt.Errorf("existing `server` table has no `dbversion` column: this is not a supported ISPConfig database")
	}

	var version *int64
	if err := db.Raw("SELECT MIN(dbversion) FROM server").Scan(&version).Error; err != nil {
		return false, fmt.Errorf("reading server.dbversion: %w", err)
	}
	if version == nil {
		complete, err := schemaComplete(db)
		if err != nil {
			return false, err
		}
		if complete {
			return true, nil
		}
		return false, fmt.Errorf("partial ISPConfig schema present and the `server` table is empty (interrupted DDL?): drop the database and run migrate again")
	}
	if *version < MinDBVersion {
		return false, fmt.Errorf("existing ISPConfig schema is dbversion %d, minimum supported is %d (ISPConfig 3.3.x): update the PHP ISPConfig install to 3.3.x first (its updater applies install/sql/incremental/), then run migrate again", *version, MinDBVersion)
	}
	return false, nil
}

// schemaComplete reports whether the database has as many tables as the
// embedded dump creates, i.e. the DDL ran to completion.
func schemaComplete(db *gorm.DB) (bool, error) {
	var tables int64
	err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()").
		Scan(&tables).Error
	if err != nil {
		return false, fmt.Errorf("inspecting database: %w", err)
	}
	return tables >= int64(strings.Count(schemaSQL, "CREATE TABLE")), nil
}

func tableExists(db *gorm.DB, name string) (bool, error) {
	var n int64
	err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?", name).
		Scan(&n).Error
	if err != nil {
		return false, fmt.Errorf("checking table %s: %w", name, err)
	}
	return n > 0, nil
}

func columnExists(db *gorm.DB, table, column string) (bool, error) {
	var n int64
	err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?", table, column).
		Scan(&n).Error
	if err != nil {
		return false, fmt.Errorf("checking column %s.%s: %w", table, column, err)
	}
	return n > 0, nil
}
