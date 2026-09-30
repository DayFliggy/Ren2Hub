package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func realtimeSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	upgradeErrors := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			upgradeErrors <- err
			return
		}
		accepted <- conn
	}))
	t.Cleanup(server.Close)
	peer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	var conn *websocket.Conn
	select {
	case conn = <-accepted:
	case err := <-upgradeErrors:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("websocket upgrade did not finish")
	}
	t.Cleanup(func() { _ = peer.Close(); _ = conn.Close() })
	return conn, peer
}

type realtimeTestResult struct {
	err   *types.NewAPIError
	usage *dto.RealtimeUsage
}

func runRealtimeHandler(t *testing.T, quota int) (*websocket.Conn, *websocket.Conn, context.CancelFunc, <-chan realtimeTestResult) {
	t.Setenv("CRYPTO_SECRET", "relay-realtime-test-secret")
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.BillingRecovery{}, &model.BillingEvent{}, &model.BillingAttempt{}, &model.Log{}, &model.QuotaData{}, &model.Channel{}, &model.BillingPriceVersion{}))
	previousDB := model.DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousBatch := common.RedisEnabled, common.BatchUpdateEnabled
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled, common.BatchUpdateEnabled = previousRedis, previousBatch
		_ = sqlDB.Close()
	})
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "realtime-test", Quota: common.Quota(quota)}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 2, UserId: 1, Key: "realtime-test-token", RemainQuota: common.Quota(quota)}).Error)
	client, clientPeer := realtimeSocketPair(t)
	target, targetPeer := realtimeSocketPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/realtime", nil).WithContext(ctx)
	info := &relaycommon.RelayInfo{
		ClientWs: client, TargetWs: target, RelayFormat: types.RelayFormatOpenAIRealtime,
		RequestId: "realtime-test-session", UserId: 1, TokenId: 2, TokenKey: "realtime-test-token",
		OriginModelName: "realtime-test-model", PriceData: hosttypes.PriceData{Quote: &billing.Quote{ChannelRatio: "1"}},
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1, UpstreamModelName: "realtime-test-model"},
	}
	definition, err := billing.NewModelPriceVersion("realtime-test-model", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": "p * 2 + c * 2 + ai * 2 + ao * 2"}, false)
	require.NoError(t, err)
	version, err := model.StoreBillingPriceEvidence(t.Context(), definition)
	require.NoError(t, err)
	quote, err := billing.NewQuote(*version, "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	info.PriceData.Quote = quote
	info.BillingRequestInput = &billingexpr.RequestInput{}
	info.BillingAttemptID = info.RequestId + ":attempt"
	estimate, charge, err := model.EvaluateBilling(quote, billing.Usage{InputTokens: 5, ContextTokens: 5, Estimated: true, Source: "request_estimate"}, *info.BillingRequestInput, info.RequestId, info.BillingAttemptID, "estimate")
	require.NoError(t, err)
	info.PriceData.Usage, info.PriceData.Charge = &estimate, charge
	require.Nil(t, service.PreConsumeBilling(c, 5, info))
	info.BillingAttemptID = info.RequestId + ":attempt"
	require.NoError(t, model.PrepareBillingAttempt(context.Background(), info.RequestId, info.BillingAttemptID, info.ChannelMeta.ChannelId, quote))
	require.NoError(t, model.PersistBillingEstimate(info.BillingAttemptID, estimate, charge))
	done := make(chan realtimeTestResult, 1)
	go func() {
		err, usage := OpenaiRealtimeHandler(c, info)
		done <- realtimeTestResult{err: err, usage: usage}
	}()
	return clientPeer, targetPeer, cancel, done
}

func readRealtimeMessage(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, message, err := conn.ReadMessage()
	require.NoError(t, err)
	return message
}

func awaitRealtimeResult(t *testing.T, done <-chan realtimeTestResult) realtimeTestResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("realtime handler did not stop its readers")
		return realtimeTestResult{}
	}
}

func TestRealtimeProviderUsageReplacesEstimatesAndDeduplicatesResponses(t *testing.T) {
	client, target, _, done := runRealtimeHandler(t, 100)
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.update","session":{"instructions":"be concise"}}`)))
	readRealtimeMessage(t, target)
	require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.text.delta","delta":"hello"}`)))
	readRealtimeMessage(t, client)
	for _, event := range []string{
		`{"event_id":"event-1","type":"response.done","response":{"id":"response-1","usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}}`,
		`{"event_id":"event-2","type":"response.done","response":{"id":"response-1","usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}}`,
	} {
		require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(event)))
		readRealtimeMessage(t, client)
	}
	require.NoError(t, target.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
	result := awaitRealtimeResult(t, done)
	require.Nil(t, result.err)
	require.NotNil(t, result.usage)
	assert.Equal(t, 30, result.usage.TotalTokens)
	assert.Equal(t, 10, result.usage.InputTokens)
	assert.Equal(t, 20, result.usage.OutputTokens)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.EqualValues(t, 70, user.Quota)
}

func TestRealtimeCancellationReturnsPartialUsageAndClosesBothPeers(t *testing.T) {
	client, target, cancel, done := runRealtimeHandler(t, 100)
	require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.text.delta","delta":"hello"}`)))
	readRealtimeMessage(t, client)
	cancel()
	result := awaitRealtimeResult(t, done)
	require.NotNil(t, result.err)
	require.NotNil(t, result.usage)
	assert.Positive(t, result.usage.OutputTokens)
	for _, peer := range []*websocket.Conn{client, target} {
		require.NoError(t, peer.SetReadDeadline(time.Now().Add(time.Second)))
		_, _, err := peer.ReadMessage()
		assert.Error(t, err)
	}
}

func TestRealtimeReservationFailureReturnsIncurredUsage(t *testing.T) {
	client, target, _, done := runRealtimeHandler(t, 20)
	require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.text.delta","delta":"hello"}`)))
	readRealtimeMessage(t, client)
	require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.done","response":{"id":"response-1","usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}}`)))
	result := awaitRealtimeResult(t, done)
	require.NotNil(t, result.err)
	assert.Contains(t, result.err.Error(), "funding quota insufficient")
	require.NotNil(t, result.usage)
	assert.Equal(t, 30, result.usage.TotalTokens)
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", "realtime-test-session").First(&recovery).Error)
	assert.Equal(t, model.BillingRecoveryStateUsageCollecting, recovery.ConsumptionState)
	assert.NotEmpty(t, recovery.ConsumptionSnapshot)
}

func TestRealtimeInterleavedResponsesKeepIndependentUsageAndTermination(t *testing.T) {
	for _, finishSecond := range []bool{false, true} {
		t.Run(fmt.Sprint("finish_second=", finishSecond), func(t *testing.T) {
			client, target, _, done := runRealtimeHandler(t, 1000)
			events := []string{
				`{"type":"response.created","response":{"id":"first","status":"in_progress"}}`,
				`{"type":"response.created","response":{"id":"second","status":"in_progress"}}`,
				`{"type":"response.output_text.delta","response_id":"first","delta":"hello"}`,
				`{"type":"response.output_text.delta","response_id":"second","delta":"independent output remains billable"}`,
				`{"type":"response.done","response":{"id":"first","status":"completed","usage":{"input_tokens":10,"output_tokens":20}}}`,
				`{"type":"response.done","response":{"id":"first","status":"completed","usage":{"input_tokens":10,"output_tokens":20}}}`,
			}
			if finishSecond {
				events = append(events, `{"type":"response.done","response":{"id":"second","status":"completed"}}`)
			}
			for _, event := range events {
				require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(event)))
				readRealtimeMessage(t, client)
			}
			require.NoError(t, target.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
			result := awaitRealtimeResult(t, done)
			if finishSecond {
				assert.Nil(t, result.err)
			} else {
				require.NotNil(t, result.err, "one completed response must not hide another unfinished response")
				assert.ErrorIs(t, result.err, io.ErrUnexpectedEOF)
			}
			require.NotNil(t, result.usage)
			assert.Equal(t, 10, result.usage.InputTokens)
			assert.Greater(t, result.usage.OutputTokens, 20, "first response usage must not erase another response's delivered estimates")
		})
	}
}

func TestRealtimeTerminalOnlyOutputIsEstimated(t *testing.T) {
	for _, output := range []string{
		`[{"type":"message","content":[{"type":"output_text","text":"complete final response"}]}]`,
		`[{"type":"function_call","name":"square","call_id":"call-one","arguments":"{\"value\":7}"}]`,
	} {
		t.Run(output, func(t *testing.T) {
			client, target, _, done := runRealtimeHandler(t, 100)
			require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.done","response":{"id":"final-only","status":"completed","output":`+output+`}}`)))
			readRealtimeMessage(t, client)
			require.NoError(t, target.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
			result := awaitRealtimeResult(t, done)
			require.Nil(t, result.err)
			require.NotNil(t, result.usage)
			assert.Positive(t, result.usage.OutputTokens)
			require.NoError(t, client.SetReadDeadline(time.Now().Add(time.Second)))
			_, _, err := client.ReadMessage()
			assert.True(t, websocket.IsCloseError(err, websocket.CloseNormalClosure), "completed session must close normally: %v", err)
		})
	}
}

func TestRealtimeDeliveredItemsRemainBillableBeforeResponseDone(t *testing.T) {
	client, target, _, done := runRealtimeHandler(t, 100)
	for _, event := range []string{
		`{"type":"response.function_call_arguments.delta","response_id":"items","item_id":"tool-one","delta":"{\"value\":7}"}`,
		`{"type":"response.output_item.done","response_id":"items","item":{"id":"tool-one","type":"function_call","name":"square","call_id":"call-one","arguments":"{\"value\":7}"}}`,
		`{"type":"response.output_item.done","response_id":"items","item":{"id":"tool-one","type":"function_call","name":"square","call_id":"call-one","arguments":"{\"value\":7}"}}`,
		`{"type":"response.output_text.delta","response_id":"items","item_id":"message-two","delta":"independent output"}`,
	} {
		require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(event)))
		readRealtimeMessage(t, client)
	}
	require.NoError(t, target.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
	result := awaitRealtimeResult(t, done)
	require.NotNil(t, result.err)
	expected := service.CountTextToken(`square{"value":7}`, "realtime-test-model") + service.CountTextToken("independent output", "realtime-test-model")
	assert.Equal(t, expected, result.usage.OutputTokens, "count both delivered items without counting duplicate item.done")
}

func TestRealtimeConfirmedUsageDoesNotRequireOutputDeltas(t *testing.T) {
	client, target, _, done := runRealtimeHandler(t, 1000)
	for _, event := range []string{
		`{"type":"response.output_text.delta","response_id":"first","delta":"hello"}`,
		`{"type":"response.done","response":{"id":"first","status":"completed","usage":{"input_tokens":10,"output_tokens":20}}}`,
		`{"type":"response.created","response":{"id":"empty"}}`,
		`{"type":"response.done","response":{"id":"empty","status":"completed","usage":{"input_tokens":40,"output_tokens":50}}}`,
	} {
		require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(event)))
		readRealtimeMessage(t, client)
	}
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.update","session":{"instructions":"unused instructions after all output finished"}}`)))
	readRealtimeMessage(t, target)
	require.NoError(t, target.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
	result := awaitRealtimeResult(t, done)
	require.Nil(t, result.err)
	assert.Equal(t, 120, result.usage.TotalTokens)
	require.True(t, result.usage.Observation.Complete)
	require.Equal(t, []string{"empty", "first"}, result.usage.Observation.ResponseIDs)
}

func TestRealtimeConcurrentResponseRequiresUnambiguousIdentity(t *testing.T) {
	client, target, _, done := runRealtimeHandler(t, 100)
	for _, event := range []string{
		`{"type":"response.created","response":{"id":"first"}}`,
		`{"type":"response.created","response":{"id":"second"}}`,
	} {
		require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(event)))
		readRealtimeMessage(t, client)
	}
	require.NoError(t, target.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.output_text.delta","delta":"ambiguous"}`)))
	result := awaitRealtimeResult(t, done)
	require.NotNil(t, result.err)
	assert.Contains(t, result.err.Error(), "missing response identity")
	assert.Zero(t, result.usage.TotalTokens)
}
