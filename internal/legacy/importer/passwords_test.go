package importer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/auth"
	"go-ispconfig/internal/model"
)

func TestResetRequired(t *testing.T) {
	t.Run("created panel users are flagged", func(t *testing.T) {
		plan, err := buildPlan(testSnapshot(), newLocalState(), Options{
			Selection:      Selection{Clients: true},
			TargetServerID: 1,
		})
		require.NoError(t, err)
		require.Equal(t, []string{"reseller1", "client2"}, plan.ResetRequired())

		// Every flagged user is planned with the unusable placeholder.
		for _, it := range plan.Items {
			if it.Table == "sys_user" && it.Action == ActionCreate {
				require.Equal(t, PlaceholderHash, it.rec.(*model.SysUser).Passwort)
			}
		}
	})

	t.Run("existing panel users are never flagged or touched", func(t *testing.T) {
		plan, err := buildPlan(testSnapshot(), localWithClient2(), Options{
			Selection:      Selection{Clients: true},
			TargetServerID: 1,
		})
		require.NoError(t, err)
		require.Empty(t, plan.ResetRequired(), "existing sys_users keep their passwords")
		for _, it := range plan.Items {
			if it.Table == "sys_user" {
				require.Equal(t, ActionSkip, it.Action)
			}
		}
	})
}

func TestPlaceholderIsUnusable(t *testing.T) {
	for _, pw := range []string{"", PlaceholderHash, "password", "admin"} {
		require.False(t, auth.VerifyPassword(pw, PlaceholderHash),
			"placeholder must reject %q", pw)
	}
}

func TestHashResetToken(t *testing.T) {
	h := HashResetToken("deadbeef")
	require.Len(t, h, 43, "must fit the legacy lost_password_hash varchar(50)")
	require.NotContains(t, h, "deadbeef", "stored value must not contain the cleartext token")
	require.Equal(t, h, HashResetToken("deadbeef"), "deterministic for later verification")
	require.NotEqual(t, h, HashResetToken("deadbeee"))
}
