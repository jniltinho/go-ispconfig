// Package database opens the MariaDB connection and creates or validates the
// ISPConfig3-identical schema from the embedded original DDL (design D9).
package database

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open connects to MariaDB/MySQL using the DSN from config.toml and returns
// a GORM handle with silent logging (queries are not application output).
func Open(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	return db, nil
}
