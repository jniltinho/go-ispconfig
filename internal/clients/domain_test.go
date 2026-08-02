package clients

import (
	"testing"

	"github.com/stretchr/testify/require"

	"go-ispconfig/internal/model"
)

func TestIsReseller(t *testing.T) {
	tests := []struct {
		name  string
		limit int32
		want  bool
	}{
		{"plain client", 0, false},
		{"unlimited reseller", -1, true},
		{"quota reseller", 5, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsReseller(&model.Client{LimitClient: tt.limit}))
		})
	}
}

func TestValidUsername(t *testing.T) {
	tests := []struct {
		username string
		want     bool
	}{
		{"acme", true},
		{"acme.corp-2_x", true},
		{"", false},
		{"has space", false},
		{"ütf8", false},
		{"a@b", false},
		{string(make([]byte, 65)), false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, ValidUsername(tt.username), "username %q", tt.username)
	}
}

func TestCheckParent(t *testing.T) {
	reseller := &model.Client{ClientID: 2, LimitClient: -1}
	plain := &model.Client{ClientID: 3, LimitClient: 0}

	tests := []struct {
		name            string
		parent          *model.Client
		childIsReseller bool
		wantErr         error
	}{
		{"admin-owned client", nil, false, nil},
		{"admin-owned reseller", nil, true, nil},
		{"client under reseller", reseller, false, nil},
		{"client under plain client", plain, false, ErrParentNotReseller},
		{"reseller under reseller", reseller, true, ErrNestedReseller},
		{"reseller under plain client", plain, true, ErrParentNotReseller},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckParent(tt.parent, tt.childIsReseller)
			if tt.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}
