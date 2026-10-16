package types

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestErrorMaskingPinsOriginalAttemptAddress(t *testing.T) {
	cause := fmt.Errorf("POST https://private-first.internal/v1 failed; https://docs.example.org/retry")
	e := NewOpenAIError(cause, ErrorCodeBadResponse, 502, ErrOptionWithUpstreamAddresses("https://private-first.internal"))
	// A rejected next candidate must not change the source scope of this error.
	for _, message := range []string{e.MaskSensitiveError("https://docs.example.org"), e.ToOpenAIError("https://docs.example.org").Message, e.ToClaudeError("https://docs.example.org").Message} {
		require.NotContains(t, message, "private-first.internal")
		require.Contains(t, message, "https://docs.example.org/retry")
	}
}
func TestHiddenErrorMessageRetainsCancellationCause(t *testing.T) {
	e := NewError(fmt.Errorf("transport: %w", context.Canceled), ErrorCodeDoRequestFailed, ErrOptionWithHideErrMsg("transport failed"))
	require.ErrorIs(t, e, context.Canceled)
	require.Equal(t, "transport failed", e.Error())
}
