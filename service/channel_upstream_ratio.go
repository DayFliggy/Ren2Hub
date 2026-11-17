package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/shopspring/decimal"
)

// 渠道上游倍率只用于展示，不写 channel_ratio，也不参与路由或计费。
const (
	UpstreamRatioKindSub2API     = model.UpstreamRatioSourceSub2API
	UpstreamRatioKindNewAPI      = model.UpstreamRatioSourceNewAPI
	UpstreamRatioKindDetect      = "detect"
	UpstreamRatioKindUnsupported = "unsupported"
)

var (
	ErrUpstreamRatioChannelChanged = errors.New("channel configuration changed during upstream ratio probe")
	ErrUpstreamRatioInvalid        = errors.New("invalid upstream ratio update")
)

// 主流协议渠道可能挂在 Sub2API/NewAPI 中转后面，需要管理员手动识别。
// upstreamRatioDetectTypes 只含正常转发时把保存的 Key 全部内容作为凭据发给渠道地址的类型
// （火山引擎的语音合成把 appid|token 拆开发送，两段都发给渠道地址），识别请求把它作为 Bearer
// 发送不会扩大泄露面。智谱 v3（JWT）、讯飞（签名地址）、腾讯（复合 Key 走 TC3 签名）和
// 百度 v2（只发送第一段）由复合 Key 派生凭据，不在其中。
var upstreamRatioDetectTypes = map[int]struct{}{
	constant.ChannelTypeOpenAI: {}, constant.ChannelTypeAnthropic: {}, constant.ChannelTypeGemini: {},
	constant.ChannelTypeXai: {}, constant.ChannelTypeOpenRouter: {},
	constant.ChannelTypeBaidu: {}, constant.ChannelTypeAli: {}, constant.ChannelTypeMoonshot: {},
	constant.ChannelTypeZhipu_v4: {}, constant.ChannelTypeLingYiWanWu: {}, constant.ChannelTypeMiniMax: {},
	constant.ChannelTypeSiliconFlow: {}, constant.ChannelTypeDeepSeek: {}, constant.ChannelTypeVolcEngine: {},
}

// UpstreamRatioKind 决定渠道走哪条探测路径。多 Key 渠道无法确定用哪把 Key 代表渠道，只能手动填写。
func UpstreamRatioKind(channel *model.Channel) string {
	if channel.ChannelInfo.IsMultiKey {
		return UpstreamRatioKindUnsupported
	}
	switch channel.Type {
	case constant.ChannelTypeSub2API:
		return UpstreamRatioKindSub2API
	case constant.ChannelTypeNewAPI:
		return UpstreamRatioKindNewAPI
	}
	if _, ok := upstreamRatioDetectTypes[channel.Type]; ok {
		return UpstreamRatioKindDetect
	}
	return UpstreamRatioKindUnsupported
}

type upstreamProbeResult struct {
	Source    string
	Outcome   probeOutcome
	Billing   *Sub2APIBilling
	TokenName string
	NewAPI    *NewAPIDetail
}

func failedProbe(status, reason string) upstreamProbeResult {
	return upstreamProbeResult{Outcome: probeOutcome{Status: status, Reason: reason}}
}

// runUpstreamRatioProbe 发起实际请求。fullDetect 只用于管理员手动识别；
// 调度只重探已识别出的来源，不对主流协议渠道反复试探两个路径。
func runUpstreamRatioProbe(ctx context.Context, channel *model.Channel, kind, knownSource string, fullDetect bool, now time.Time) upstreamProbeResult {
	if kind == UpstreamRatioKindUnsupported {
		return failedProbe(model.UpstreamRatioStatusUnsupported, "unsupported_channel")
	}
	if kind == UpstreamRatioKindDetect && !fullDetect && knownSource == "" {
		return failedProbe(model.UpstreamRatioStatusUnsupported, "not_detected")
	}
	target, err := newUpstreamRatioTarget(channel)
	if errors.Is(err, errUpstreamRatioSetting) {
		return failedProbe(model.UpstreamRatioStatusFailed, "invalid_proxy")
	}
	if err != nil {
		return failedProbe(model.UpstreamRatioStatusFailed, "invalid_base_url")
	}
	if target.Key == "" {
		return failedProbe(model.UpstreamRatioStatusFailed, "missing_key")
	}
	if parsed, err := url.Parse(target.BaseURL); err != nil || isOfficialUpstreamAPI(parsed.Hostname()) {
		return failedProbe(model.UpstreamRatioStatusUnsupported, "official_api")
	}
	client, err := newUpstreamRatioHTTPClient(target.Proxy)
	if err != nil {
		return failedProbe(model.UpstreamRatioStatusFailed, "invalid_proxy")
	}
	defer client.CloseIdleConnections()

	probeSub2API := func() upstreamProbeResult {
		resp, err := upstreamRatioGet(ctx, client, target.BaseURL+upstreamRatioSub2APIBillingPath, target.Key, upstreamRatioSmallBodyLimit)
		result := upstreamProbeResult{Source: UpstreamRatioKindSub2API, Outcome: classifyUpstreamRatioResponse(resp, err, now)}
		if !result.Outcome.ok() {
			return result
		}
		if result.Billing, err = parseSub2APIBillingResponse(resp.Body); err != nil {
			result.Outcome.Status, result.Outcome.Reason = model.UpstreamRatioStatusFailed, "invalid_response"
		}
		return result
	}
	probeNewAPI := func() upstreamProbeResult {
		return probeNewAPIUpstream(ctx, client, target, now)
	}

	switch {
	case kind == UpstreamRatioKindSub2API, kind == UpstreamRatioKindDetect && !fullDetect && knownSource == UpstreamRatioKindSub2API:
		return probeSub2API()
	case kind == UpstreamRatioKindNewAPI, kind == UpstreamRatioKindDetect && !fullDetect:
		return probeNewAPI()
	}
	sub2api := probeSub2API()
	if sub2api.Outcome.ok() {
		return sub2api
	}
	newapi := probeNewAPI()
	if newapi.Outcome.ok() {
		return newapi
	}
	return detectFailure(sub2api, newapi)
}

func probeNewAPIUpstream(ctx context.Context, client *http.Client, target upstreamRatioTarget, now time.Time) upstreamProbeResult {
	resp, err := upstreamRatioGet(ctx, client, target.BaseURL+upstreamRatioNewAPIUsagePath, target.Key, upstreamRatioSmallBodyLimit)
	result := upstreamProbeResult{Source: UpstreamRatioKindNewAPI, Outcome: classifyUpstreamRatioResponse(resp, err, now)}
	if !result.Outcome.ok() {
		return result
	}
	if result.TokenName, err = parseNewAPIUsage(resp.Body); err != nil {
		result.Outcome.Status, result.Outcome.Reason = model.UpstreamRatioStatusFailed, "invalid_response"
		return result
	}
	// 定价页匿名访问；上游开启“定价页需登录”时只能手动填写。
	resp, err = upstreamRatioGet(ctx, client, target.BaseURL+upstreamRatioNewAPIPricingPath, "", upstreamRatioPricingBodyLimit)
	pricing := classifyUpstreamRatioResponse(resp, err, now)
	if !pricing.ok() {
		pricing.Status = model.UpstreamRatioStatusFailed
		if pricing.Reason == "auth_failed" {
			pricing.Reason = "pricing_auth_required"
		} else if pricing.HTTPStatus != 0 {
			pricing.Reason = "pricing_" + pricing.Reason
		}
		result.Outcome = pricing
		return result
	}
	if result.NewAPI, err = parseNewAPIPricing(resp.Body); err != nil {
		result.Outcome = probeOutcome{Status: model.UpstreamRatioStatusFailed, Reason: "invalid_pricing", HTTPStatus: pricing.HTTPStatus}
	}
	return result
}

// detectFailure 合并两条识别路径的失败。路径不存在或返回的不是对应格式都算“不是这种上游”；
// 两边都是这种情况才判为不支持，否则报告更有信息量的那个失败。
func detectFailure(results ...upstreamProbeResult) upstreamProbeResult {
	for _, result := range results {
		if result.Outcome.Reason == "invalid_response" {
			continue
		}
		if result.Outcome.Status != model.UpstreamRatioStatusUnsupported {
			return result
		}
	}
	return failedProbe(model.UpstreamRatioStatusUnsupported, "unsupported")
}

func ratioString(value float64) string {
	return decimal.NewFromFloat(value).String()
}

func upstreamRatioInterval() time.Duration {
	return upstreamRatioIntervalMinutes * time.Minute
}
