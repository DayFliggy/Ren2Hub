package service

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
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
	params, err := buildWssConsumptionParams(ctx, relayInfo, usage, nil)
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
	usage *dto.RealtimeUsage, extraContent []*common.Message) {
	var err error
	usage, err = NormalizeRealtimeUsage(usage)
	if err != nil {
		SettleBillingFallback(ctx, relayInfo, billingFallbackPricingFailed, err)
		return
	}
	// An incomplete session settles its observed cumulative usage as an
	// estimate; the usage snapshot keeps the incomplete observation label.
	params, err := buildWssConsumptionParams(ctx, relayInfo, usage, extraContent)
	if err != nil {
		SettleBillingFallback(ctx, relayInfo, billingFallbackPricingFailed, err)
		return
	}
	settleBillingConsumption(ctx, relayInfo, params, usage.TotalTokens > 0 || relayInfo.PriceData.UsePrice && relayInfo.HasValidOutput())
}

func buildWssConsumptionParams(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.RealtimeUsage, extraContent []*common.Message) (model.RecordConsumeLogParams, error) {
	facts := NormalizedBillingUsage(usage.CanonicalUsage(), "openai")
	if relayInfo.PriceData.UsePrice && (relayInfo.HasValidOutput() || usage.Observation != nil && usage.Observation.Complete && usage.Observation.Terminal == "completed") {
		facts.Quantities["calls"] = "1"
	}
	facts.ActualServiceTier = relayInfo.ActualServiceTier
	facts.PricingKind = "realtime"
	facts, result, err := evaluateRequestBilling(relayInfo, facts)
	relayInfo.PriceData.Usage, relayInfo.PriceData.Charge = &facts, result
	if err != nil {
		return model.RecordConsumeLogParams{}, err
	}
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

func PostAudioConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent []*common.Message) {
	PostTextConsumeQuota(ctx, relayInfo, usage, extraContent)
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
			lang := userSetting.Language
			prompt := i18n.Translate(lang, i18n.MsgNotifyQuotaLowTitle)
			content, values := quotaNotifyContent(lang, userSetting.NotifyType, prompt, logger.FormatQuota(relayInfo.UserQuota))
			err := NotifyUser(relayInfo.UserId, relayInfo.UserEmail, relayInfo.UserSetting, dto.NewNotify(dto.NotifyTypeQuotaExceed, prompt, content, values))
			if err != nil {
				common.SysError(common.LogText("failed to send quota notify to user %d: %s", relayInfo.UserId, err.Error()))
			}
		}
	})
}

// quotaNotifyContent renders a low-quota notice in the user's saved language,
// DEFAULT_LANGUAGE when none. Bark and Gotify get plain text; email and
// webhook get HTML with the top-up link. values stay in the notify payload for
// webhook receivers.
func quotaNotifyContent(lang string, notifyType string, prompt string, remaining string) (string, []any) {
	switch notifyType {
	case dto.NotifyTypeBark:
		// Bark推送使用简短文本，不支持HTML
		return i18n.Translate(lang, i18n.MsgNotifyQuotaLowBark, map[string]any{"Prompt": prompt, "Quota": remaining}), []any{prompt, remaining}
	case dto.NotifyTypeGotify:
		return i18n.Translate(lang, i18n.MsgNotifyQuotaLowGotify, map[string]any{"Prompt": prompt, "Quota": remaining}), []any{prompt, remaining}
	default:
		topUpLink := PaymentReturnURL("/wallet")
		return i18n.Translate(lang, i18n.MsgNotifyQuotaLowEmail, map[string]any{"Prompt": prompt, "Quota": remaining, "Link": topUpLink}), []any{prompt, remaining, topUpLink, topUpLink}
	}
}
