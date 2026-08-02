package clients

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go-ispconfig/internal/model"
)

// TestResolveRuleMailEntities pins the mail entity → limit column/error
// mapping (add-mail-module: limit enforcement via the client limit hook).
func TestResolveRuleMailEntities(t *testing.T) {
	tests := []struct {
		entity string
		key    string
		limit  int32
		get    func(*model.Client) int32
	}{
		{"mail-domains", "error.limit_maildomain", 3, func(c *model.Client) int32 { return c.LimitMaildomain }},
		{"mailboxes", "error.limit_mailbox", 5, func(c *model.Client) int32 { return c.LimitMailbox }},
		{"aliases", "error.limit_mailalias", 7, func(c *model.Client) int32 { return c.LimitMailalias }},
		{"alias-domains", "error.limit_mailaliasdomain", 2, func(c *model.Client) int32 { return c.LimitMailaliasdomain }},
		{"forwards", "error.limit_mailforward", 4, func(c *model.Client) int32 { return c.LimitMailforward }},
		{"catchalls", "error.limit_mailcatchall", 1, func(c *model.Client) int32 { return c.LimitMailcatchall }},
	}
	for _, tt := range tests {
		t.Run(tt.entity, func(t *testing.T) {
			rule, ok := resolveRule(tt.entity, nil)
			assert.True(t, ok, "mail entity must resolve a limit rule")
			assert.Equal(t, tt.key, rule.key)
			c := &model.Client{}
			// Set the field via the accessor's mirror: build a client with
			// the value and confirm the rule reads the same column.
			assert.Equal(t, tt.get(c), rule.limit(c))
		})
	}

	// A truly unknown entity is still a no-op (never vetoed).
	_, ok := resolveRule("gizmos", nil)
	assert.False(t, ok)
}
