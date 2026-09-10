package common

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWaitBackgroundNeverRunsAlreadyCanceledTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.False(t, WaitBackground(ctx, 0))
}
