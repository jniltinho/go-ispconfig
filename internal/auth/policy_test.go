package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPolicyAllows(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		userID uint32
		want   bool
	}{
		{"yes allows anyone", "yes", 42, true},
		{"yes allows superadmin", "yes", 1, true},
		{"no denies everyone", "no", 1, false},
		{"superadmin allows only user id 1", "superadmin", 1, true},
		{"superadmin denies other admins", "superadmin", 2, false},
		{"none denies", "none", 1, false},
		{"empty (unknown flag) denies", "", 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, PolicyAllows(tt.value, tt.userID))
		})
	}
}

func TestPolicyDefaultsPortIniFaithfully(t *testing.T) {
	// Spot-check the values ported from security_settings.ini.master.
	require.Equal(t, "yes", policyDefaults["allow_shell_user"])
	require.Equal(t, "superadmin", policyDefaults["admin_allow_server_config"])
	require.Equal(t, "yes", policyDefaults["remote_api_allowed"])
	require.Len(t, policyDefaults, 17)
}
