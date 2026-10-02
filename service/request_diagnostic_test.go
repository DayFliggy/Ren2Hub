package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestDiagnosticCorrelationValidation(t *testing.T) {
	for _, value := range []string{"trace:client-1", "a_b.c", strings.Repeat("a", 128)} {
		assert.True(t, ValidRequestCorrelationID(value))
	}
	for _, value := range []string{"", "with space", "secret\nheader", "中文", strings.Repeat("x", 129)} {
		assert.False(t, ValidRequestCorrelationID(value))
	}
}

func TestRequestDiagnosticProjectionRetainsUnknownTimingAndOmitsPrivateFields(t *testing.T) {
	row := model.GatewayRequestDiagnostic{RequestID: "request", TokenID: 1, AttemptsJSON: `[{"sequence":1,"attempt_id":"id","credential_id":"private-id","channel_id":7,"success":false,"origin":"provider","latency_ms":12,"queue_ms":0,"send_started_at":100,"finished_at":112,"acceptance":"unknown","retry_permission":"policy","response_committed":false,"response_diagnostics":[{"code":"unknown","path":"private-input"}],"body":"never project"}]`}
	value, err := ProjectRequestDiagnostic(row, false)
	require.NoError(t, err)
	require.Len(t, value.Attempts, 1)
	assert.Nil(t, value.Attempts[0].TTFTMS)
	assert.Nil(t, value.Attempts[0].FirstOutputAt)
	data, err := common.Marshal(value)
	require.NoError(t, err)
	for _, secret := range []string{"private-id", "channel_id", "private-input", "never project"} {
		assert.NotContains(t, string(data), secret)
	}
	admin, err := ProjectRequestDiagnostic(row, true)
	require.NoError(t, err)
	assert.Equal(t, "private-id", admin.Attempts[0].CredentialID)
	row.AttemptsJSON = "invalid"
	_, err = ProjectRequestDiagnostic(row, false)
	assert.Error(t, err)
}
