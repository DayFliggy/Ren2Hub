package relay

import (
	"fmt"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

func WssHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	statusCodeMappingStr := c.GetString("status_code_mapping")
	resp, err := adaptor.DoRequest(c, info, nil)
	if err != nil {
		return types.NewError(err, types.ErrorCodeDoRequestFailed)
	}

	upstream, ok := resp.(*websocket.Conn)
	if !ok || upstream == nil {
		return types.NewError(fmt.Errorf("upstream realtime connection is missing"), types.ErrorCodeBadResponse, types.ErrOptionWithSkipRetry())
	}
	info.TargetWs = upstream
	info.UpstreamAccepted = true
	defer upstream.Close()

	usage, newAPIError := adaptor.DoResponse(c, nil, info)
	if realtimeUsage, ok := usage.(*dto.RealtimeUsage); ok && realtimeUsage != nil {
		// Input accepted by a session is not delivered output. A failed or empty
		// session refunds its reservation even if local input estimates exist.
		if !info.HasValidOutput() {
			realtimeUsage = &dto.RealtimeUsage{}
		}
		source := "estimated"
		if info.ResponseUsage != nil {
			source = info.ResponseUsage.UsageSource
		}
		info.CaptureResponseUsage(&dto.Usage{PromptTokens: realtimeUsage.InputTokens, CompletionTokens: realtimeUsage.OutputTokens,
			TotalTokens: realtimeUsage.TotalTokens, PromptTokensDetails: realtimeUsage.InputTokenDetails,
			CompletionTokenDetails: realtimeUsage.OutputTokenDetails, UsageSource: source}, "realtime", []string{"realtime usage: " + source})
	}
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}
	return nil
}
