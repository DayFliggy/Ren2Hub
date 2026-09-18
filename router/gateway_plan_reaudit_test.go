package router

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayQueuedTokenIPRevocationPreventsSendAndDebit(t *testing.T) {
	var sends atomic.Int32
	base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); gatewaySDKUpstream(w, r) })
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Update("capacity_total", 1).Error)
	configureGatewayManualQueue(t, key, 1)
	holder, _, err := service.AcquireGatewayChannelPermit(context.Background(), "ip-audit-holder", 1, false, service.NewGatewayWaitBudget(0))
	require.NoError(t, err)
	defer holder.Release()
	done := startGatewayRouteRequest(t, base, key, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"fixture"}]}`)
	require.Eventually(t, func() bool { return service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(1)).Queued == 1 }, time.Second, time.Millisecond)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("key = ?", key).Update("allow_ips", "192.0.2.0/24").Error)
	holder.Release()
	result := <-done
	require.NoError(t, result.err)
	assert.NotEqual(t, http.StatusOK, result.code)
	assert.Zero(t, sends.Load())
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&count).Error)
	assert.Zero(t, count)
	assertGatewayRouteIdle(t, 1)
}

func TestGatewayNativeCitationCannotBeReplayedThroughAnotherProtocol(t *testing.T) {
	var sends atomic.Int32
	base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg-citation","type":"message","stop_reason":"end_turn","content":[{"type":"text","text":"private-answer","citations":[{"type":"web_search_result_location","url":"https://example.test","cited_text":"private-quote","encrypted_index":"opaque-citation"}]}],"usage":{"input_tokens":3,"output_tokens":5}}`)
	})
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Update("type", constant.ChannelTypeAnthropic).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	events := captureGatewayRouteDecisions(t)
	result := <-startGatewayRouteRequest(t, base, key, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"fixture"}]}`)
	require.NoError(t, result.err)
	assert.Equal(t, http.StatusBadGateway, result.code, result.body)
	assert.EqualValues(t, 1, sends.Load())
	assert.NotContains(t, result.body, "private-quote")
	assert.NotContains(t, result.body, "opaque-citation")
	decision := awaitGatewayRouteDecision(t, events, result.requestID)
	require.Len(t, decision.Attempts, 1)
	assert.Equal(t, service.AttemptOriginLocalConvert, decision.Attempts[0].Origin)
	assert.Equal(t, service.AttemptRetryForbidden, decision.Attempts[0].RetryPermission)
	var health []model.ChannelHealth
	require.NoError(t, model.DB.Find(&health).Error)
	for _, row := range health {
		assert.Zero(t, row.FailureCount)
	}
	assertGatewayRouteIdle(t, 1)
}
