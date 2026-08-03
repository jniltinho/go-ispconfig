package powerdns

import (
	"fmt"

	"gorm.io/gorm"

	"go-ispconfig/internal/database"
)

// ApplySchema executes the embedded powerdns.sql against db (expected to be
// connected to the powerdns database). Safe to re-run: DDL uses IF NOT EXISTS
// and the trailing ALTER guards are idempotent.
func ApplySchema(db *gorm.DB) error {
	stmts, err := database.SplitStatements(SchemaSQL)
	if err != nil {
		return fmt.Errorf("parsing powerdns schema: %w", err)
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("executing powerdns DDL: %w", err)
		}
	}
	return nil
}
