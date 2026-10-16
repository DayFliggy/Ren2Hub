package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMantleReferencesBindFinalHTTPTransportAndDoNotReuseInvokeModelState(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&model.GatewayStateBinding{}, &model.GatewayCredential{}))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "fixture-key|us-east-1")
	info := &relaycommon.RelayInfo{UserId: 1, TokenId: 2, RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "fixture-key|us-east-1", ChannelType: constant.ChannelTypeAws}}
	channel := &model.Channel{Id: 3, Type: constant.ChannelTypeAws}
	require.NoError(t, PrepareGatewayStateOutput(c, info, channel))
	value, _ := c.Get("gateway_state_output")
	invokeTransport := value.(*gatewayStateOutput).owner.TransportID
	require.NotEmpty(t, invokeTransport)
	require.NoError(t, BindGatewayOutput(c, []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"invoke-signature"}}`)))
	info.ChannelSetting.AwsAPIType = dto.AwsAPITypeMantle
	require.NoError(t, PrepareGatewayStateOutput(c, info, channel))
	value, _ = c.Get("gateway_state_output")
	assert.Empty(t, value.(*gatewayStateOutput).owner.TransportID)
	request, err := http.NewRequest(http.MethodPost, "https://bedrock-mantle.us-east-1.api.aws/anthropic/v1/messages", nil)
	require.NoError(t, err)
	request.Header.Set("X-Api-Key", "fixture-key")
	request.Header.Set("X-Tenant", "selected-tenant")
	require.NoError(t, ValidateGatewayHTTPIdentity(c, info, request, request.Header.Clone(), map[string]string{"X-Tenant": "selected-tenant"}))
	assert.NotEmpty(t, value.(*gatewayStateOutput).owner.TransportID)
	assert.NotEqual(t, invokeTransport, value.(*gatewayStateOutput).owner.TransportID)
	require.Error(t, ValidateGatewayOutboundState(c, info, []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"previous","signature":"invoke-signature"}]}]}`)))
	require.NoError(t, BindGatewayOutput(c, []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"mantle-signature"}}`)))
	continuation := []byte(`{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"previous","signature":"mantle-signature"}]}]}`)
	require.NoError(t, ValidateGatewayOutboundState(c, info, continuation))
	request.Header.Set("X-Tenant", "another-tenant")
	require.NoError(t, ValidateGatewayHTTPIdentity(c, info, request, request.Header.Clone(), map[string]string{"X-Tenant": "another-tenant"}))
	require.Error(t, ValidateGatewayOutboundState(c, info, continuation))
}
