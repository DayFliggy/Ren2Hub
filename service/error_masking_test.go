package service

import (
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestTaskAndResponsesErrorsKeepUsefulFieldsAndMaskPrivateAddresses(t *testing.T) {
	rawString, stringErr := MaskResponsesErrorPayload([]byte(`{"type":"response.failed","response":{"error":"private-task.internal failed"}}`), "https://private-task.internal")
	require.NoError(t, stringErr)
	require.NotContains(t, string(rawString), "private-task")
	upstream := "https://private-task.internal"
	err := TaskErrorWrapper(errors.New("private-task.internal failed; https://docs.example.org/errors?key=secret"), "provider", 502, upstream)
	require.NotContains(t, err.Message, "private-task")
	require.Contains(t, err.Message, "https://docs.example.org/errors?key=***")
	raw := []byte(`{"type":"response.failed","sequence_number":4,"response":{"id":"r","error":{"code":"503","type":"provider_error","param":"model","message":"private-task.internal failed"},"usage":{"input_tokens":2},"custom":"keep"},"vendor":"keep"}`)
	masked, maskErr := MaskResponsesErrorPayload(raw, upstream)
	require.NoError(t, maskErr)
	require.NotContains(t, string(masked), "private-task")
	require.Contains(t, string(masked), `"usage":{"input_tokens":2}`)
	require.Contains(t, string(masked), `"vendor":"keep"`)
	require.Contains(t, string(masked), `"param":"model"`)
}
