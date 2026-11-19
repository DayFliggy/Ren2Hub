package service

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
)

// Local admission failures reach automatic disabling through the scheduled
// channel test, which wraps them as relay errors. With an operator range that
// disables on 5xx and a keyword list, neither the 503 nor client text echoed
// in a local error may disable the channel; an upstream 401 still does.
func TestLocalErrorsNeverDisableChannel(t *testing.T) {
	previousAuto := common.AutomaticDisableChannelEnabled
	previousRanges := operation_setting.AutomaticDisableStatusCodeRanges
	previousKeywords := operation_setting.AutomaticDisableKeywords
	common.AutomaticDisableChannelEnabled = true
	operation_setting.AutomaticDisableStatusCodeRanges = []operation_setting.StatusCodeRange{{Start: 401, End: 401}, {Start: 500, End: 599}}
	operation_setting.AutomaticDisableKeywords = []string{"permission denied"}
	t.Cleanup(func() {
		common.AutomaticDisableChannelEnabled = previousAuto
		operation_setting.AutomaticDisableStatusCodeRanges = previousRanges
		operation_setting.AutomaticDisableKeywords = previousKeywords
	})

	limitErr := fmt.Errorf("read channel limit: %w", errConcurrencyLimitUnavailable)
	status, code := ConcurrencyAdmissionError("channel", limitErr)
	unavailable := types.NewErrorWithStatusCode(limitErr, code, status)
	echoed := types.NewError(errors.New("fetch https://x.invalid/Permission Denied failed"), types.ErrorCodeConvertRequestFailed)
	upstream401 := types.WithOpenAIError(types.OpenAIError{Message: "Incorrect API key provided", Type: "invalid_request_error", Code: "invalid_api_key"}, http.StatusUnauthorized)

	assert.Equal(t, http.StatusServiceUnavailable, unavailable.StatusCode)
	assert.False(t, ShouldDisableChannel(unavailable), "a local concurrency rejection says nothing about the channel")
	assert.False(t, ShouldDisableChannel(echoed), "a disable keyword echoed from client input stays local")
	assert.True(t, ShouldDisableChannel(upstream401), "an upstream credential failure still disables")
}
