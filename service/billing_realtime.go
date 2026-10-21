package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// Realtime 在发送前拿不到用量，只能在每轮 response.done 后把该轮额度补进会话预扣。
// 钱包是唯一资金来源且不记欠费，所以所有 Realtime 扣费都经过同一个原子预扣：
// 补不上的部分不扣，会话随即断开。

// reserveRealtimeRound 把本轮额度补进会话预扣；余额或令牌不足时返回错误，由适配器断开会话。
func reserveRealtimeRound(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, quota int) *types.NewAPIError {
	if quota <= 0 {
		return nil
	}
	return PreConsumeBilling(ctx, relayInfo.FinalPreConsumedQuota+quota, relayInfo)
}

// realtimeChargeableQuota 返回结算时实际可扣的额度：先尝试把预扣补到 quota，
// 补不上时只按已预扣的额度结算，保证钱包不会变成负数。
func realtimeChargeableQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, quota int) int {
	reserved := relayInfo.FinalPreConsumedQuota
	if quota <= reserved {
		return quota
	}
	apiErr := PreConsumeBilling(ctx, quota, relayInfo)
	if apiErr == nil {
		return quota
	}
	logger.LogWarn(ctx, common.LogText("realtime usage exceeds the reservable balance, charging the reserved %s of %s: %s",
		logger.FormatQuota(reserved), logger.FormatQuota(quota), apiErr.Error()))
	return reserved
}
