package helper

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hostreasoning "github.com/QuantumNous/new-api/setting/reasoning"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func modelPriceNotConfiguredError(c *gin.Context, modelName string, userId int) error {
	key := i18n.MsgRelayModelPriceNotConfigured
	if model.IsAdmin(userId) {
		key = i18n.MsgRelayModelPriceNotConfiguredAdmin
	}
	return errors.New(i18n.T(c, key, map[string]any{"Model": modelName}))
}

// HandleChannelRatio uses the selected channel's validated decimal multiplier.
// Every pricing entry must stop on the error: an invalid ratio never bills as 0.
// It is the only place the usage discount factor is applied; the decision is
// kept on info so retries and repricing of the same request reuse it.
func HandleChannelRatio(ctx *gin.Context, info *relaycommon.RelayInfo) (hosttypes.ChannelRatioInfo, error) {
	value, err := model.NormalizeChannelRatio(ctx.GetString("channel_ratio"))
	if err != nil {
		return hosttypes.ChannelRatioInfo{}, fmt.Errorf("selected channel ratio is invalid: %w", err)
	}
	base, _ := decimal.NewFromString(value)
	if info.UsageDiscount == nil {
		info.UsageDiscount = service.ResolveUsageDiscount(info.UserId, info.IsChannelTest)
	}
	return hosttypes.ChannelRatioInfo{
		ChannelRatio:     base.Mul(info.UsageDiscount.Factor).InexactFloat64(),
		BaseChannelRatio: base.InexactFloat64(),
		UsageDiscount:    info.UsageDiscount,
	}, nil
}

// isFreeModel is the single free-model rule. Channel ratios are always
// positive, so only a zero model price or model ratio can make a model free,
// and only when the operator disabled free-model pre-consumption.
func isFreeModel(usePrice bool, modelPrice, modelRatio float64) bool {
	if operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		return false
	}
	if usePrice {
		return modelPrice == 0
	}
	return modelRatio == 0
}

func ModelPriceHelper(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta) (hosttypes.PriceData, error) {
	if info != nil {
		if matched := resolveBillingModelName(info.GetOriginModelName()); matched != "" && matched != info.OriginModelName {
			info.BillingModelName = matched
		}
	}
	billingModelName := info.GetBillingModelName()
	modelPrice, usePrice := ratio_setting.GetModelPrice(billingModelName, false)

	channelRatioInfo, err := HandleChannelRatio(c, info)
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	// Check if this model uses tiered_expr billing
	if billing_setting.GetBillingMode(billingModelName) == billing_setting.BillingModeTieredExpr {
		return modelPriceHelperTiered(c, info, billingModelName, promptTokens, channelRatioInfo)
	}

	var preConsumedQuota int
	var modelRatio float64
	var completionRatio float64
	var cacheRatio float64
	var imageRatio float64
	var cacheCreationRatio float64
	var cacheCreationRatio5m float64
	var cacheCreationRatio1h float64
	var cacheCreationRatio12h float64
	var audioRatio float64
	var audioCompletionRatio float64
	var freeModel bool
	if !usePrice {
		preConsumeMultiplier, err := operation_setting.InputPreConsumeMultiplier()
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		preConsumedTokens := float64(promptTokens) * preConsumeMultiplier
		var success bool
		var matchName string
		modelRatio, success, matchName = ratio_setting.GetModelRatio(billingModelName)
		if !success {
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !acceptUnsetRatio {
				return hosttypes.PriceData{}, modelPriceNotConfiguredError(c, matchName, info.UserId)
			}
		}
		completionRatio = ratio_setting.GetCompletionRatio(billingModelName)
		cacheRatio, _ = ratio_setting.GetCacheRatio(billingModelName)
		cacheCreationRatio, _ = ratio_setting.GetCreateCacheRatio(billingModelName)
		cacheCreationRatio5m = cacheCreationRatio
		// Long-lived cache writes have fixed multipliers over the short-write price.
		cacheCreationRatio1h = cacheCreationRatio * hosttypes.ClaudeCacheCreation1hMultiplier
		cacheCreationRatio12h = cacheCreationRatio * hosttypes.OpenAICacheCreation12hMultiplier
		imageRatio, _ = ratio_setting.GetImageRatio(billingModelName)
		audioRatio = ratio_setting.GetAudioRatio(billingModelName)
		audioCompletionRatio = ratio_setting.GetAudioCompletionRatio(billingModelName)
		ratio := modelRatio * channelRatioInfo.ChannelRatio
		quota, err := common.QuotaFromFloatStrict(preConsumedTokens * ratio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		preConsumedQuota = quota
		if _, image := info.Request.(*dto.ImageRequest); image {
			info.ImageQuotaBeforeChannel = preConsumedTokens * modelRatio
		}
	} else {
		if meta.ImagePriceRatio != 0 {
			modelPrice = modelPrice * meta.ImagePriceRatio
		}
		if _, image := info.Request.(*dto.ImageRequest); image {
			info.ImageQuotaBeforeChannel = modelPrice * common.QuotaPerUnit
		}
	}

	// if model price or ratio is 0 and free model pre-consume is disabled, do not pre-consume quota
	freeModel = isFreeModel(usePrice, modelPrice, modelRatio)
	if freeModel {
		preConsumedQuota = 0
	}

	priceData := hosttypes.PriceData{
		FreeModel:             freeModel,
		ModelPrice:            modelPrice,
		ModelRatio:            modelRatio,
		CompletionRatio:       completionRatio,
		ChannelRatioInfo:      channelRatioInfo,
		UsePrice:              usePrice,
		CacheRatio:            cacheRatio,
		ImageRatio:            imageRatio,
		AudioRatio:            audioRatio,
		AudioCompletionRatio:  audioCompletionRatio,
		CacheCreationRatio:    cacheCreationRatio,
		CacheCreation5mRatio:  cacheCreationRatio5m,
		CacheCreation1hRatio:  cacheCreationRatio1h,
		CacheCreation12hRatio: cacheCreationRatio12h,
		QuotaToPreConsume:     preConsumedQuota,
	}
	if usePrice {
		for name, ratio := range meta.BillingRatios {
			priceData.AddOtherRatio(name, ratio)
		}
	}
	if request, image := info.Request.(*dto.ImageRequest); image {
		count, err := request.ImageCount(false)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		if usePrice {
			priceData.AddOtherRatio("n", float64(count))
		}
		if !usePrice {
			quota, err := common.QuotaFromFloatStrict(priceData.ApplyOtherRatiosToFloat(info.ImageQuotaBeforeChannel * channelRatioInfo.ChannelRatio))
			if err != nil {
				return hosttypes.PriceData{}, err
			}
			priceData.QuotaToPreConsume = quota
		}
	}
	if usePrice {
		quotaToPreConsume := priceData.ApplyOtherRatiosToFloat(modelPrice * common.QuotaPerUnit * channelRatioInfo.ChannelRatio)
		quota, err := common.QuotaFromFloatStrict(quotaToPreConsume)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		priceData.QuotaToPreConsume = quota
	}

	if common.DebugEnabled {
		logger.LogDebug(c, "model_price_helper result: %s", priceData.ToSetting())
	}
	info.PriceData = priceData
	return priceData, nil
}

// ModelPriceHelperPerCall 按次/按量计费的 PriceHelper (MJ、Task)
func ModelPriceHelperPerCall(c *gin.Context, info *relaycommon.RelayInfo) (hosttypes.PriceData, error) {
	channelRatioInfo, err := HandleChannelRatio(c, info)
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	modelPrice, success := ratio_setting.GetModelPrice(info.OriginModelName, true)
	usePrice := success
	var modelRatio float64

	if !success {
		var ratioSuccess bool
		var matchName string
		modelRatio, ratioSuccess, matchName = ratio_setting.GetModelRatio(info.OriginModelName)
		acceptUnsetRatio := false
		if info.UserSetting.AcceptUnsetRatioModel {
			acceptUnsetRatio = true
		}
		if !ratioSuccess && !acceptUnsetRatio {
			return hosttypes.PriceData{}, modelPriceNotConfiguredError(c, matchName, info.UserId)
		}
	}

	var quota int

	if usePrice {
		quota, err = common.QuotaFromFloatStrict(modelPrice * common.QuotaPerUnit * channelRatioInfo.ChannelRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
	} else {
		// 按量计费：以模型倍率的一半作为预扣额度
		quota, err = common.QuotaFromFloatStrict(modelRatio / 2 * common.QuotaPerUnit * channelRatioInfo.ChannelRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		modelPrice = -1
	}
	freeModel := isFreeModel(usePrice, modelPrice, modelRatio)
	if freeModel {
		quota = 0
	}

	priceData := hosttypes.PriceData{
		FreeModel:        freeModel,
		ModelPrice:       modelPrice,
		ModelRatio:       modelRatio,
		UsePrice:         usePrice,
		Quota:            quota,
		ChannelRatioInfo: channelRatioInfo,
	}
	return priceData, nil
}

func HasModelBillingConfig(modelName string) bool {
	if _, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return true
	}
	if _, ok, _ := ratio_setting.GetModelRatio(modelName); ok {
		return true
	}
	if billing_setting.GetBillingMode(modelName) != billing_setting.BillingModeTieredExpr {
		return false
	}
	expr, ok := billing_setting.GetBillingExpr(modelName)
	return ok && strings.TrimSpace(expr) != ""
}

// HasPriceOrRatioEntry reports whether name has a configured price, ratio, or
// tiered billing-mode entry after a single wildcard normalization. Self-use
// fallback does not count as a configured ratio.
func HasPriceOrRatioEntry(name string) bool {
	formatted := ratio_setting.FormatMatchingModelName(name)
	if _, ok := ratio_setting.GetModelPrice(formatted, false); ok {
		return true
	}
	if ratio_setting.HasConfiguredModelRatio(formatted) {
		return true
	}
	return billing_setting.GetBillingMode(formatted) == billing_setting.BillingModeTieredExpr
}

func resolveBillingModelName(origin string) string {
	var candidates []string
	if !reasoning.ParseModelModifiers(origin).HasModifiers() {
		candidates = append(candidates, origin)
	}
	candidates = append(candidates, hostreasoning.CanonicalBillingModelNames(origin)...)
	base := hostreasoning.BaseModelName(origin)
	// DeepSeek thinking tails (-none/-low/-max) bill at the base model's price,
	// with or without @ modifiers after them.
	if deepSeekBase, _, _, ok := reasoning.ParseDeepSeekV4ThinkingSuffix(base); ok && !model_setting.ShouldPreserveThinkingSuffix(base) {
		candidates = append(candidates, deepSeekBase)
	}
	candidates = append(candidates, base)

	seen := make(map[string]struct{}, len(candidates))
	matched := ""
	for _, name := range candidates {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if HasPriceOrRatioEntry(name) {
			matched = name
			break
		}
	}
	if matched == "" {
		matched = base
	}
	return matched
}

func modelPriceHelperTiered(c *gin.Context, info *relaycommon.RelayInfo, billingModelName string, promptTokens int, channelRatioInfo hosttypes.ChannelRatioInfo) (hosttypes.PriceData, error) {
	exprStr, ok := billing_setting.GetBillingExpr(billingModelName)
	if !ok {
		return hosttypes.PriceData{}, fmt.Errorf("model %s is configured as tiered_expr but has no billing expression", billingModelName)
	}
	exprHash := billingexpr.ExprHashString(exprStr)
	usedVars := billingexpr.UsedVarsByHash(exprStr, exprHash)
	if usedVars["u"] {
		return hosttypes.PriceData{}, errors.New(i18n.T(c, i18n.MsgRelayTaskUsagePriceUnsupported, map[string]any{"Model": billingModelName}))
	}
	fixedPricing := billingexpr.UsesFixedPricingByHash(exprStr, exprHash)
	value, _ := c.Get(jsplugin.ContextKeyRelayEndpoint)
	if endpoint, ok := value.(jsplugin.PinnedEndpoint); ok && !fixedPricing {
		for _, candidate := range endpoint.Candidates {
			pluginKey := candidate.Plugin.Meta.Key
			if _, overridden := billing_setting.GetPluginBillingExpr(pluginKey, endpoint.Model); overridden {
				continue
			}
			if _, overridden := billing_setting.GetPluginBillingExpr(pluginKey, endpoint.MappedModel); endpoint.MappedModel != "" && overridden {
				continue
			}
			taskBillingModel := endpoint.Model
			if billing_setting.GetBillingMode(taskBillingModel) != billing_setting.BillingModeTieredExpr {
				taskBillingModel = endpoint.MappedModel
			}
			// Task expressions return USD; ordinary expressions return USD *
			// 1M. Even a constant without u() cannot safely serve both contracts.
			if taskBillingModel == billingModelName {
				return hosttypes.PriceData{}, errors.New(i18n.T(c, i18n.MsgRelaySharedTaskPriceNeedsOverride, map[string]any{"Model": billingModelName, "Plugin": pluginKey}))
			}
		}
	}
	if info.RelayFormat == types.RelayFormatOpenAIRealtime && fixedPricing {
		return hosttypes.PriceData{}, fmt.Errorf("fixed pricing is not supported for Realtime requests")
	}

	preConsumeMultiplier, err := operation_setting.InputPreConsumeMultiplier()
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	requestInput, err := ResolveIncomingBillingExprRequestInput(c, info)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	if usedVars["image_count"] {
		requestInput, err = ResolveImageBillingRequestInput(c, info, requestInput)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
	}

	rawCost, trace, err := billingexpr.RunExprByHashWithRequest(exprStr, exprHash, billingexpr.TokenParams{
		P:   float64(promptTokens),
		C:   0,
		Len: float64(promptTokens),
	}, requestInput)
	if err != nil {
		return hosttypes.PriceData{}, fmt.Errorf("model %s tiered expr run failed: %w", billingModelName, err)
	}

	// Expression coefficients are $/1M tokens prices; convert to quota the same way per-call billing does.
	quotaBeforeChannel := rawCost / 1_000_000 * common.QuotaPerUnit
	// Scale the reservation, preserving the expression's context-length tier
	// and leaving actual settlement and fixed request prices unchanged.
	if trace.BillingUnit != billingexpr.BillingUnitRequest {
		quotaBeforeChannel *= preConsumeMultiplier
	}
	preConsumedQuota, err := billingexpr.QuotaRoundStrict(quotaBeforeChannel * channelRatioInfo.ChannelRatio)
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	snapshot := &billingexpr.BillingSnapshot{
		EstimatedImageCount:         trace.ImageCount,
		BillingMode:                 billing_setting.BillingModeTieredExpr,
		ModelName:                   billingModelName,
		ExprString:                  exprStr,
		ExprHash:                    exprHash,
		ChannelRatio:                channelRatioInfo.ChannelRatio,
		EstimatedPromptTokens:       promptTokens,
		EstimatedCompletionTokens:   0,
		PreConsumeMultiplier:        preConsumeMultiplier,
		EstimatedQuotaBeforeChannel: quotaBeforeChannel,
		EstimatedQuotaAfterChannel:  preConsumedQuota,
		EstimatedTier:               trace.MatchedTier,
		EstimatedBillingUnit:        trace.BillingUnit,
		EstimatedFixedPrice:         trace.FixedPrice,
		QuotaPerUnit:                common.QuotaPerUnit,
		ExprVersion:                 billingexpr.ExprVersion(exprStr),
	}
	info.TieredBillingSnapshot = snapshot
	info.BillingRequestInput = &requestInput

	priceData := hosttypes.PriceData{
		ChannelRatioInfo:  channelRatioInfo,
		QuotaToPreConsume: preConsumedQuota,
	}

	logger.LogDebug(c, "model_price_helper_tiered result: model=%s preConsume=%d quotaBeforeChannel=%.2f channelRatio=%.2f tier=%s", billingModelName, preConsumedQuota, quotaBeforeChannel, channelRatioInfo.ChannelRatio, trace.MatchedTier)

	info.PriceData = priceData
	return priceData, nil
}
