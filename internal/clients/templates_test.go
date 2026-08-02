package clients

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAdditionalList(t *testing.T) {
	tests := []struct {
		raw  string
		want []int32
	}{
		{"", nil},
		{"2/5", []int32{2, 5}},
		{"2/5/2", []int32{2, 5, 2}}, // duplicates are meaningful
		{"/2//5/", []int32{2, 5}},
		{"2/x/-3/5", []int32{2, 5}},
		{" 2 / 5 ", []int32{2, 5}},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, ParseAdditionalList(tt.raw), "raw %q", tt.raw)
	}
}
