package service

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayErrorLogAndPolicyEventsRedactCodeWithoutChangingRetryCode(t *testing.T) {
	truncate(t)
	oldLog, oldEnabled := model.LOG_DB, constant.ErrorLogEnabled
	model.LOG_DB, constant.ErrorLogEnabled = model.DB, true
	t.Cleanup(func() { model.LOG_DB, constant.ErrorLogEnabled = oldLog, oldEnabled })
	require.NoError(t, model.DB.AutoMigrate(&model.Log{}))
	require.NoError(t, model.DB.Create(&model.User{Id: 991, Username: "error-boundary", AffCode: "error-boundary"}).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("id", 991)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "BARE_CHANNEL_KEY")
	apiErr := types.WithOpenAIError(types.OpenAIError{Message: "BARE_CHANNEL_KEY", Code: "BARE_CHANNEL_KEY"}, 502)
	BindRelayErrorContext(c, apiErr)
	RecordPolicyFailure(c, 8, apiErr, PolicyDecision{Action: "retry"})
	RecordRequestPolicyTermination(c, apiErr)
	for _, event := range RequestPolicy(c).Events() {
		assert.Equal(t, "***", event.ErrorCode)
	}
	RecordRelayErrorLog(c, 8, apiErr, nil)
	var log model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ? AND type = ?", 991, model.LogTypeError).Last(&log).Error)
	assert.NotContains(t, log.Other, "BARE_CHANNEL_KEY")
	assert.NotContains(t, log.Content, "BARE_CHANNEL_KEY")
	assert.Equal(t, types.ErrorCode("BARE_CHANNEL_KEY"), apiErr.GetErrorCode())
}

func TestAutomaticDisableKeywordsUseOriginalErrorAfterProjectionBinding(t *testing.T) {
	oldEnabled, oldKeywords := common.AutomaticDisableChannelEnabled, operation_setting.AutomaticDisableKeywords
	common.AutomaticDisableChannelEnabled = true
	operation_setting.AutomaticDisableKeywords = []string{"specific.private.invalid"}
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = oldEnabled
		operation_setting.AutomaticDisableKeywords = oldKeywords
	})
	apiErr := types.NewOpenAIError(errors.New("specific.private.invalid rejected credential"), types.ErrorCodeBadResponseStatusCode, 418)
	apiErr.BindSensitiveContext([]string{"https://specific.private.invalid"})
	assert.NotContains(t, apiErr.Error(), "specific.private.invalid")
	assert.True(t, ShouldDisableChannel(apiErr), "masking must not change existing internal keyword matching")
}

func TestViolationNormalizationKeepsRawClassificationAndSensitiveContext(t *testing.T) {
	apiErr := types.WithOpenAIError(types.OpenAIError{Message: "BARE_CHANNEL_KEY " + CSAMViolationMarker, Code: "upstream_error"}, 400)
	apiErr.BindSensitiveContext(nil, "BARE_CHANNEL_KEY", CSAMViolationMarker)
	normalized := NormalizeViolationFeeError(apiErr)
	assert.Equal(t, types.ErrorCodeViolationFeeGrokCSAM, normalized.GetErrorCode(), "internal marker must use the original message")
	assert.NotContains(t, normalized.Error(), "BARE_CHANNEL_KEY")
	assert.NotContains(t, normalized.ToOpenAIError().Message, CSAMViolationMarker)
	assert.True(t, types.IsSkipRetryError(normalized))
}
