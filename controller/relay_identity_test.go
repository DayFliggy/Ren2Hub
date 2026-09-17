package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalSendRechecksCredentialAfterQualification(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	channel := model.Channel{Id: 981, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "selected-fixture"}
	require.NoError(t, db.Create(&channel).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	service.CaptureGatewayChannelIdentity(c, &channel)
	common.SetContextKey(c, constant.ContextKeyChannelKey, channel.Key)
	require.NoError(t, service.ValidateGatewayChannelIdentity(c, channel.Id))
	channel.Key = "replacement-fixture"
	require.NoError(t, channel.Update())
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channel.Id}}
	err := prepareLiveRouteSend(c, info, &routeAttemptHealthState{}, "identity-before-send", nil)
	require.ErrorIs(t, err, service.ErrAdmissionRuntime)
	assert.False(t, info.UpstreamSent)
	assert.Zero(t, liveRouteRetryCounters(c).TotalAttempts)
	assert.Nil(t, info.Billing)
}
