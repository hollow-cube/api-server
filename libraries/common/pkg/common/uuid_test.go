package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsUUID(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"123e4567-e89b-12d3-a456-426655440000", true},
		{"c73bcdcc-2669-4bf6-81d3-e4ae73fb11fd", true},
		{"C73BCDCC-2669-4Bf6-81d3-E4AE73FB11FD", true},
		{"c73bcdcc-2669-4bf6-81d3-e4an73fb11fd", false},
		{"c73bcdcc26694bf681d3e4ae73fb11fd", false},
		{"definitely-not-a-uuid", false},
	}

	for _, test := range tests {
		require.Equal(t, test.valid, IsUUID(test.input))
	}
}
