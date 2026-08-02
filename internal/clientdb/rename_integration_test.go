//go:build integration

package clientdb

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/engine"
)

// TestRenameDatabase covers task 3.7 / design D9 against MariaDB: the
// empty-database path and the base-table path (RENAME TABLE keeps the
// data; the old schema is dropped).
func TestRenameDatabase(t *testing.T) {
	p, c, _ := startAdmin(t, "ren")
	p.runner = engine.ExecRunner{}
	ctx := context.Background()

	// Empty database path: create new + drop old.
	require.True(t, p.createDatabase(ctx, c, row{"database_name": "c1_empty"}))
	ok := p.renameDatabase(ctx, c, engine.Data{
		Old: map[string]any{"database_name": "c1_empty"},
		New: map[string]any{"database_name": "c1_renamed"},
	})
	require.True(t, ok)
	assert.False(t, c.schemaExists(ctx, "c1_empty"))
	assert.True(t, c.schemaExists(ctx, "c1_renamed"))

	// Base table path: data must survive the move.
	require.True(t, p.createDatabase(ctx, c, row{"database_name": "c1_data"}))
	_, err := c.ExecContext(ctx, "CREATE TABLE c1_data.items (id INT PRIMARY KEY, name VARCHAR(32))")
	require.NoError(t, err)
	_, err = c.ExecContext(ctx, "INSERT INTO c1_data.items VALUES (1, 'widget'), (2, 'gadget')")
	require.NoError(t, err)

	ok = p.renameDatabase(ctx, c, engine.Data{
		Old: map[string]any{"database_name": "c1_data"},
		New: map[string]any{"database_name": "c1_moved"},
	})
	require.True(t, ok)
	assert.False(t, c.schemaExists(ctx, "c1_data"))
	var n int
	require.NoError(t, c.QueryRowContext(ctx, "SELECT COUNT(*) FROM c1_moved.items").Scan(&n))
	assert.Equal(t, 2, n)

	// Denylist and same-name guards.
	assert.False(t, p.renameDatabase(ctx, c, engine.Data{
		Old: map[string]any{"database_name": "mysql"},
		New: map[string]any{"database_name": "c1_x"},
	}))
	assert.False(t, p.renameDatabase(ctx, c, engine.Data{
		Old: map[string]any{"database_name": "c1_moved"},
		New: map[string]any{"database_name": "C1_MOVED"},
	}))
}

// TestRenameDatabaseWithView exercises the mysqldump/import path when
// the host has the MariaDB client tools; skipped otherwise.
func TestRenameDatabaseWithView(t *testing.T) {
	if _, err := exec.LookPath("mysqldump"); err != nil {
		t.Skip("mysqldump not installed on test host")
	}
	if _, err := exec.LookPath("mysql"); err != nil {
		t.Skip("mysql client not installed on test host")
	}
	p, c, _ := startAdmin(t, "renv")
	p.runner = engine.ExecRunner{}
	ctx := context.Background()

	require.True(t, p.createDatabase(ctx, c, row{"database_name": "c1_vdb"}))
	_, err := c.ExecContext(ctx, "CREATE TABLE c1_vdb.items (id INT PRIMARY KEY)")
	require.NoError(t, err)
	_, err = c.ExecContext(ctx, "INSERT INTO c1_vdb.items VALUES (7)")
	require.NoError(t, err)
	_, err = c.ExecContext(ctx, "CREATE VIEW c1_vdb.v_items AS SELECT id FROM c1_vdb.items")
	require.NoError(t, err)

	ok := p.renameDatabase(ctx, c, engine.Data{
		Old: map[string]any{"database_name": "c1_vdb"},
		New: map[string]any{"database_name": "c1_vnew"},
	})
	require.True(t, ok)
	var n int
	require.NoError(t, c.QueryRowContext(ctx, "SELECT COUNT(*) FROM c1_vnew.v_items").Scan(&n))
	assert.Equal(t, 1, n)
	assert.False(t, c.schemaExists(ctx, "c1_vdb"))
}
