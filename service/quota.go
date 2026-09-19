package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
)

func PreWssConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.RealtimeUsage) error {
	usage, err := NormalizeRealtimeUsage(usage)
	if err != nil {
		return err
	}
	if usage.TotalTokens == 0 && !(relayInfo.PriceData.UsePrice && relayInfo.HasValidOutput()) {
		return nil
	}
	params, err := buildWssConsumptionParams(ctx, relayInfo, usage, "")
	if err != nil {
		return err
	}
	quota, err := billing.RequestQuota(params.Quota)
	if err != nil {
		return err
	}
	if relayInfo.QuotaClamp != nil {
		return relayInfo.QuotaClamp
	}
	consumption, snapshotErr := billingConsumptionSnapshot(ctx, relayInfo, params, true)
	if snapshotErr == nil {
		snapshotErr = model.RecordRealtimeBillingConsumption(relayInfo.RequestId, consumption)
	}
	if snapshotErr != nil {
		return snapshotErr
	}
	if err := relayInfo.Billing.Reserve(quota); err != nil {
		return err
	}
	relayInfo.FinalPreConsumedQuota = relayInfo.Billing.GetPreConsumedQuota()
	return nil
}

func PostWssConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, modelName string,
	usage *dto.RealtimeUsage, extraContent string) {
	var err error
	usage, err = NormalizeRealtimeUsage(usage)
	if err != nil {
		relayInfo.BillingSettlementError = err
		logger.LogError(ctx, "invalid realtime billing usage: "+err.Error())
		return
	}
	params, err := buildWssConsumptionParams(ctx, relayInfo, usage, extraContent)
	if err != nil {
		return
	}
	settleBillingConsumption(ctx, relayInfo, params, usage.TotalTokens > 0 || relayInfo.PriceData.UsePrice && relayInfo.HasValidOutput())
}

func buildWssConsumptionParams(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.RealtimeUsage, extraContent string) (model.RecordConsumeLogParams, error) {
	facts := billing.Usage{InputTokens: int64(usage.InputTokens), OutputTokens: int64(usage.OutputTokens), ContextTokens: int64(usage.InputTokens),
		CacheReadTokens: int64(usage.InputTokenDetails.CachedTokens), CacheWriteTokens: int64(usage.InputTokenDetails.CacheCreationTokensTotal()),
		ImageInputTokens: int64(usage.InputTokenDetails.ImageTokens), ImageOutputTokens: int64(usage.OutputTokenDetails.ImageTokens),
		AudioInputTokens: int64(usage.InputTokenDetails.AudioTokens), AudioOutputTokens: int64(usage.OutputTokenDetails.AudioTokens),
		InputIncludesCache: true, Source: "realtime_cumulative", Complete: true, Quantities: map[string]string{}}
	if relayInfo.PriceData.UsePrice && relayInfo.HasValidOutput() {
		facts.Quantities["calls"] = "1"
	}
	relayInfo.PriceData.Usage = &facts
	result, err := billing.Calculate(relayInfo.PriceData.Quote, facts)
	if err != nil {
		holdFailedBillingPrice(ctx, relayInfo, billingexpr.TokenParams{}, err)
		return model.RecordConsumeLogParams{}, relayInfo.BillingSettlementError
	}
	relayInfo.PriceData.Charge = result
	quota, err := billing.RequestQuota(result.FinalQuota)
	if err != nil {
		return model.RecordConsumeLogParams{}, err
	}
	other := map[string]interface{}{}
	InjectBillingPriceInfo(other, relayInfo)
	return model.RecordConsumeLogParams{ChannelId: relayInfo.ChannelId, PromptTokens: usage.InputTokens, CompletionTokens: usage.OutputTokens,
		ModelName: relayInfo.BillingModelName(), TokenName: ctx.GetString("token_name"), Quota: common.Quota(quota), Content: extraContent,
		TokenId: relayInfo.TokenId, UseTimeSeconds: int(time.Since(relayInfo.StartTime).Seconds()), IsStream: relayInfo.IsStream, Group: relayInfo.UsingGroup, Other: other}, nil
}

func PostAudioConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent string) {
	PostTextConsumeQuota(ctx, relayInfo, usage, []string{extraContent})
}

func checkAndSendQuotaNotify(relayInfo *relaycommon.RelayInfo, quota int, preConsumedQuota int) {
	gopool.Go(func() {
		userSetting := relayInfo.UserSetting
		threshold := common.Quota(common.QuotaRemindThreshold)
		if userSetting.QuotaWarningThreshold != 0 {
			threshold = common.Quota(userSetting.QuotaWarningThreshold)
		}

		//noMoreQuota := userCache.Quota-(quota+preConsumedQuota) <= 0
		quotaTooLow := false
		consumeQuota := quota + preConsumedQuota
		remaining, amountErr := common.SubtractQuota(relayInfo.UserQuota, common.Quota(consumeQuota))
		if amountErr != nil {
			common.SysError("quota notification calculation overflow")
			return
		}
		if remaining < threshold {
			quotaTooLow = true
		}
		if quotaTooLow {
			prompt := "您的额度即将用尽"
			topUpLink := PaymentReturnURL("/wallet")

			// 根据通知方式生成不同的内容格式
			var content string
			var values []interface{}

			notifyType := userSetting.NotifyType
			if notifyType == "" {
				notifyType = dto.NotifyTypeEmail
			}

			if notifyType == dto.NotifyTypeBark {
				// Bark推送使用简短文本，不支持HTML
				content = "{{value}}，剩余额度：{{value}}，请及时充值"
				values = []interface{}{prompt, logger.FormatQuota(relayInfo.UserQuota)}
			} else if notifyType == dto.NotifyTypeGotify {
				content = "{{value}}，当前剩余额度为 {{value}}，请及时充值。"
				values = []interface{}{prompt, logger.FormatQuota(relayInfo.UserQuota)}
			} else {
				// 默认内容格式，适用于Email和Webhook（支持HTML）
				content = "{{value}}，当前剩余额度为 {{value}}，为了不影响您的使用，请及时充值。<br/>充值链接：<a href='{{value}}'>{{value}}</a>"
				values = []interface{}{prompt, logger.FormatQuota(relayInfo.UserQuota), topUpLink, topUpLink}
			}

			err := NotifyUser(relayInfo.UserId, relayInfo.UserEmail, relayInfo.UserSetting, dto.NewNotify(dto.NotifyTypeQuotaExceed, prompt, content, values))
			if err != nil {
				common.SysError(fmt.Sprintf("failed to send quota notify to user %d: %s", relayInfo.UserId, err.Error()))
			}
		}
	})
}
