package service

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGatewayStateOutputAndContinuationUseSameOwnership(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.GatewayStateBinding{}, &model.GatewayCredential{}))
	assertGatewayStateOutputOwnership(t, db)
}

func assertGatewayStateOutputOwnership(t *testing.T, db *gorm.DB) {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set("id", 1)
	c.Set("token_id", 2)
	c.Set("gateway_state_output", &gatewayStateOutput{owner: model.GatewayStateBinding{UserID: 1, TokenID: 2, ChannelID: 3, CredentialID: "identity", EndpointID: "endpoint", TransportID: "transport"}, signatures: make(map[int]string)})
	require.NoError(t, BindGatewayOutput(c, []byte(`{"type":"response.created","response":{"object":"response","id":"resp_bound","output":[]}}`)))
	require.NoError(t, BindGatewayOutput(c, []byte(`{"type":"content_block_delta","index":1,"delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","encrypted_index":"citation-bound"}}}`)))
	_, err := model.FindGatewayState(context.Background(), 1, 2, "encrypted_index", "citation-bound")
	require.NoError(t, err)
	_, err = model.FindGatewayState(context.Background(), 1, 4, "encrypted_index", "citation-bound")
	require.ErrorIs(t, err, model.ErrGatewayStateUnknown)
	for _, tokenID := range []int{2, 4} {
		citationContext, _ := gin.CreateTestContext(httptest.NewRecorder())
		citationContext.Set("id", 1)
		citationContext.Set("token_id", tokenID)
		citationContext.Request = httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"messages":[{"role":"assistant","content":[{"type":"text","text":"answer","citations":[{"type":"web_search_result_location","encrypted_index":"citation-bound"}]}]}]}`))
		citationContext.Request.Header.Set("Content-Type", "application/json")
		owner, err := ResolveGatewayRequestState(citationContext)
		common.CleanupBodyStorage(citationContext)
		if tokenID == 2 {
			require.NoError(t, err)
			require.NotNil(t, owner)
			assert.Equal(t, 3, owner.ChannelID)
		} else {
			require.ErrorIs(t, err, model.ErrGatewayStateUnknown)
		}
	}
	require.NoError(t, BindGatewayOutput(c, []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"first"}}`)))
	require.NoError(t, BindGatewayOutput(c, []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"second"}}`)))
	_, err = model.FindGatewayState(context.Background(), 1, 2, "signature", "firstsecond")
	require.NoError(t, err)
	c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(`{"previous_response_id":"resp_bound"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	defer common.CleanupBodyStorage(c)
	owner, err := ResolveGatewayRequestState(c)
	require.NoError(t, err)
	require.NotNil(t, owner)
	assert.Equal(t, 3, owner.ChannelID)
	c.Set("token_id", 4)
	_, err = ResolveGatewayRequestState(c)
	require.ErrorIs(t, err, model.ErrGatewayStateUnknown)
	var rows []model.GatewayStateBinding
	require.NoError(t, db.Find(&rows).Error)
	encoded, err := common.Marshal(rows)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "resp_bound")
	assert.NotContains(t, string(encoded), "firstsecond")
	assert.NotContains(t, string(encoded), "citation-bound")

	shortExpiry := time.Now().Add(time.Hour).Unix()
	require.NoError(t, BindGatewayOutput(c, []byte(fmt.Sprintf(`{"type":"response.completed","response":{"object":"response","id":"resp_bound","status":"completed","expires_at":%d}}`, shortExpiry))))
	shortened, err := model.FindGatewayState(context.Background(), 1, 2, "response", "resp_bound")
	require.NoError(t, err)
	assert.Equal(t, shortExpiry, shortened.ExpiresAt)
	require.NoError(t, BindGatewayOutput(c, []byte(`{"type":"response.failed","response":{"object":"response","id":"resp_failed","status":"failed"}}`)))
	_, err = model.FindGatewayState(context.Background(), 1, 2, "response", "resp_failed")
	require.ErrorIs(t, err, model.ErrGatewayStateUnknown)
	require.Error(t, BindGatewayOutput(c, []byte(`{"object":"response","id":"resp_invalid","expires_at":"not-a-timestamp"}`)))
	_, err = model.FindGatewayState(context.Background(), 1, 2, "response", "resp_invalid")
	require.ErrorIs(t, err, model.ErrGatewayStateUnknown)
}

func TestGatewayStateReferenceExtractionDoesNotInspectToolArguments(t *testing.T) {
	var payload map[string]any
	require.NoError(t, common.Unmarshal([]byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","signature":"opaque"},{"type":"tool_use","input":{"signature":"application-value"}}]}]}`), &payload))
	assert.Equal(t, []gatewayStateReference{{"signature", "opaque"}}, stateReferences(payload, false))
}

// The converter attaches a public bypass value to cross-protocol Gemini
// history; it is not provider state and needs no binding, while a real
// thought signature in the same request still does.
func TestGatewayStateReferencesSkipGeminiThoughtSignatureBypass(t *testing.T) {
	var payload map[string]any
	require.NoError(t, common.Unmarshal([]byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{}},"thoughtSignature":"context_engineering_is_the_way_to_go"},{"text":"done","thoughtSignature":"provider-issued"}]}]}`), &payload))
	assert.Equal(t, []gatewayStateReference{{"thoughtSignature", "provider-issued"}}, stateReferences(payload, false))
}

func TestGatewayOpaqueStateReferenceKinds(t *testing.T) {
	for _, test := range []struct {
		body, kind, value string
		output            bool
	}{
		{`{"input":[{"type":"item_reference","id":"item_one"}]}`, "response_item", "item_one", false},
		{`{"output":[{"type":"reasoning","id":"item_one"}]}`, "response_item", "item_one", true},
		{`{"messages":[{"content":[{"type":"redacted_thinking","data":"opaque"}]}]}`, "redacted_thinking", "opaque", false},
		{`{"content":[{"type":"redacted_thinking","data":"opaque"}]}`, "redacted_thinking", "opaque", true},
		{`{"cachedContent":"cachedContents/one"}`, "cached_content", "cachedContents/one", false},
		{`{"tools":[{"type":"code_interpreter","container":"container_one"}]}`, "container", "container_one", false},
		{`{"output":[{"type":"code_interpreter_call","container_id":"container_one"}]}`, "container", "container_one", true},
		{`{"input":[{"content":[{"type":"input_file","file_id":"file_one"}]}]}`, "file", "file_one", false},
		{`{"messages":[{"role":"user","content":[{"type":"file","file":{"file_id":"file_one"}}]}]}`, "file", "file_one", false},
		{`{"messages":[{"content":[{"type":"document","source":{"type":"file","file_id":"file_one"}}]}]}`, "file", "file_one", false},
		{`{"contents":[{"parts":[{"fileData":{"mimeType":"image/png","fileUri":"https://generativelanguage.googleapis.com/v1beta/files/one"}}]}]}`, "file_uri", "https://generativelanguage.googleapis.com/v1beta/files/one", false},
		{`{"contents":[{"parts":[{"file_data":{"mime_type":"image/png","file_uri":"gs://bucket/one"}}]}]}`, "file_uri", "gs://bucket/one", false},
	} {
		var payload map[string]any
		require.NoError(t, common.Unmarshal([]byte(test.body), &payload))
		assert.Equal(t, []gatewayStateReference{{test.kind, test.value}}, stateReferences(payload, test.output))
	}
}

func TestGatewaySignatureFragmentsAreAttemptScoped(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.GatewayStateBinding{}, &model.GatewayCredential{}))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "test-key")
	info := &relaycommon.RelayInfo{UserId: 1, TokenId: 2, RelayFormat: types.RelayFormatClaude}
	channel := &model.Channel{Id: 3, Type: constant.ChannelTypeAnthropic}
	require.NoError(t, PrepareGatewayStateOutput(c, info, channel))
	value, _ := c.Get("gateway_state_output")
	value.(*gatewayStateOutput).owner.TransportID = "first-transport"
	require.NoError(t, BindGatewayOutput(c, []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"abandoned"}}`)))
	require.NoError(t, PrepareGatewayStateOutput(c, info, channel))
	value, _ = c.Get("gateway_state_output")
	value.(*gatewayStateOutput).owner.TransportID = "second-transport"
	require.NoError(t, BindGatewayOutput(c, []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"current"}}`)))
	_, err = model.FindGatewayState(context.Background(), 1, 2, "signature", "current")
	require.NoError(t, err)
	_, err = model.FindGatewayState(context.Background(), 1, 2, "signature", "abandonedcurrent")
	require.ErrorIs(t, err, model.ErrGatewayStateUnknown)
}

func TestGatewayStateRecheckReadsCurrentCredentialAndEndpoint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.GatewayCredential{}))
	endpoint := "https://state.example.invalid"
	channel := model.Channel{Type: constant.ChannelTypeOpenAI, Key: "first\nsecond", BaseURL: &endpoint, Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{IsMultiKey: true},
	}
	require.NoError(t, db.Create(&channel).Error)
	credential, err := model.EnsureGatewayCredential(context.Background(), channel.Id, "second")
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set(GatewayStateContextKey, &model.GatewayStateBinding{ChannelID: channel.Id, CredentialID: credential.ID, EndpointID: channel.GatewayEndpointID(), TransportID: "transport", ExpiresAt: time.Now().Add(time.Hour).Unix()})
	key, index, err := ValidateGatewayStateOwner(c, channel.Id)
	require.NoError(t, err)
	assert.Equal(t, "second", key)
	assert.Equal(t, 1, index)
	require.NoError(t, db.Model(&channel).Updates(map[string]any{"key": "second\nfirst"}).Error)
	key, index, err = ValidateGatewayStateOwner(c, channel.Id)
	require.NoError(t, err)
	assert.Equal(t, "second", key)
	assert.Equal(t, 0, index)
	require.NoError(t, db.Model(&channel).Update("base_url", "https://replacement.example.invalid").Error)
	_, _, err = ValidateGatewayStateOwner(c, channel.Id)
	require.ErrorIs(t, err, model.ErrGatewayStateUnavailable)
	value, _ := c.Get(GatewayStateContextKey)
	value.(*model.GatewayStateBinding).ExpiresAt = time.Now().Add(-time.Second).Unix()
	_, _, err = ValidateGatewayStateOwner(c, channel.Id)
	require.ErrorIs(t, err, model.ErrGatewayStateUnknown)
}
