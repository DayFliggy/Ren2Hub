package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

func TestGatewayResponsesWSHandshakeCanFailoverButAcceptedCallCannotReplay(t *testing.T) {
	base, key, _, accepted := setupResponsesWSAudit(t)
	var handshakes atomic.Int32
	rejected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handshakes.Add(1)
		http.Error(w, "handshake unavailable", 502)
	}))
	defer rejected.Close()
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Update("base_url", rejected.URL).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	configureGatewayManualQueue(t, key, 1, 4)
	client := dialResponsesWSAudit(t, base, key)
	writeResponsesWSAudit(t, client, `{"type":"response.create","stream_id":"active","model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`)
	upstream := acceptResponsesWSAudit(t, accepted)
	require.Contains(t, readResponsesWSAudit(t, upstream), `"type":"response.create"`)
	require.EqualValues(t, 1, handshakes.Load())
	var attemptCount int64
	require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Count(&attemptCount).Error)
	require.EqualValues(t, 1, attemptCount, "the rejected handshake never sent a generation")
	created := `{"type":"response.created","stream_id":"active","response":{"id":"accepted-stale","status":"in_progress"}}`
	writeResponsesWSAudit(t, upstream, created)
	require.JSONEq(t, created, readResponsesWSAudit(t, client))
	require.NoError(t, upstream.Close())
	require.Contains(t, readResponsesWSAudit(t, client), `"type":"error"`)
	assertResponsesWSAuditAccounting(t, model.BillingRecoveryStatusRefunded, 1)
	require.Empty(t, accepted, "accepted request must never reconnect and replay")
	require.EqualValues(t, 1, handshakes.Load())
	var diagnostic model.GatewayRequestDiagnostic
	require.NoError(t, model.DB.First(&diagnostic).Error)
	projected, err := service.ProjectRequestDiagnostic(diagnostic, true)
	require.NoError(t, err)
	require.Len(t, projected.Attempts, 1, "one dispatched failure must be observed once")
	require.Equal(t, service.AttemptAccepted, projected.Attempts[0].Acceptance)
	require.False(t, projected.Attempts[0].Success)
}
