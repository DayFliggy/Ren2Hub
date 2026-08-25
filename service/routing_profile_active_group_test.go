package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveActiveGroupIDAcceptsPrivateKeysForNewGroups(t *testing.T) {
	active, err := resolveActiveGroupID(intPointer(-2), map[int]int{-1: 101, -2: 102})
	require.NoError(t, err)
	require.NotNil(t, active)
	require.Equal(t, 102, *active)
}

func intPointer(value int) *int {
	return &value
}
