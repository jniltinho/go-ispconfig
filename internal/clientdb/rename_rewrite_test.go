package clientdb

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRewriteViewCreate covers both SHOW CREATE VIEW shapes: MariaDB
// qualifies the view name with the schema, MySQL may return it bare. The
// rewrite must target newSchema.view, drop DEFINER and repoint qualified
// body references.
func TestRewriteViewCreate(t *testing.T) {
	qualified := "CREATE ALGORITHM=UNDEFINED DEFINER=`root`@`localhost` SQL SECURITY DEFINER" +
		" VIEW `c1_vdb`.`v_items` AS select `c1_vdb`.`items`.`id` AS `id` from `c1_vdb`.`items`"
	bare := "CREATE ALGORITHM=UNDEFINED DEFINER=`root`@`localhost` SQL SECURITY DEFINER" +
		" VIEW `v_items` AS select `c1_vdb`.`items`.`id` AS `id` from `c1_vdb`.`items`"

	for name, in := range map[string]string{"qualified": qualified, "bare": bare} {
		t.Run(name, func(t *testing.T) {
			got := rewriteViewCreate(in, "c1_vdb", "c1_vnew", "v_items")
			require.NotEmpty(t, got)
			assert.True(t, strings.HasPrefix(got, "CREATE VIEW `c1_vnew`.`v_items` AS "), got)
			assert.Contains(t, got, "`c1_vnew`.`items`")
			assert.NotContains(t, got, "DEFINER=")
			assert.NotContains(t, got, "c1_vdb")
		})
	}

	// Malformed statements return "" so the caller aborts the rename.
	assert.Empty(t, rewriteViewCreate("SELECT 1", "a", "b", "v"))
	assert.Empty(t, rewriteViewCreate("CREATE VIEW `v` broken", "a", "b", "v"))
}
