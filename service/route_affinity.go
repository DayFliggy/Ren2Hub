package service

import (
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// selectAffinityChannel 在首轮尝试会话亲和渠道。decided 为 false 时调用方继续正常选路。
// strict 模式下亲和渠道不可用直接拒绝，熔断打开或探测占用也算不可用（与上游对不可用
// 亲和渠道的处理一致：503，除 KeepOnChannelDisabled 外清除绑定），仅 429 冷却照常发送；
// prefer 模式下渠道不合格、暂停或满载时退回正常选路，存储等技术错误不当作“不合格”
// 处理，照常返回 500。
func selectAffinityChannel(c *gin.Context, modelName string) (channel *model.Channel, selectErr *ChannelSelectError, decided bool) {
	preferred, found := GetPreferredChannelByAffinity(c, modelName)
	if !found {
		return nil, nil, false
	}
	gate := affinityRouteGate(c)
	strict := gate == boundHealthyAllowCooling
	row, err := acquireBoundRouteChannel(c, preferred, modelName, gate)
	if err == nil && row.Id != preferred {
		// The binding is kept; SwitchOnSuccess moves it after a success.
		if !routePreselecting(c) {
			setRouteDecision(c, row.Id, "affinity_exit")
		}
		return row, nil, true
	}
	if err == nil {
		MarkChannelAffinityUsed(c, row.Id)
		if !routePreselecting(c) {
			source := "prefer"
			if strict {
				source = "strict"
			}
			setRouteDecision(c, row.Id, source)
		}
		return row, nil, true
	}
	full := errors.Is(err, ErrConcurrencyFull)
	if strict {
		// An unreadable limit says nothing about the bound channel; keep the binding.
		if errors.Is(err, errConcurrencyLimitUnavailable) {
			return nil, routeLoadFailure(c, err), true
		}
		if !full && !ShouldKeepChannelAffinityOnChannelDisabled() {
			clearAffinityIfStillBound(c, preferred)
		}
		setRouteRetryAfter(c, err, true)
		status := http.StatusServiceUnavailable
		if full {
			status = http.StatusTooManyRequests
		}
		return nil, &ChannelSelectError{StatusCode: status, Message: "strict_session_binding_unavailable"}, true
	}
	var pause *routePauseError
	if full || errors.As(err, &pause) {
		return nil, nil, false
	}
	if !errors.Is(err, model.ErrTokenRouteForbidden) {
		return nil, routeLoadFailure(c, err), true
	}
	if !ShouldKeepChannelAffinityOnChannelDisabled() {
		clearAffinityIfStillBound(c, preferred)
	}
	return nil, nil, false
}

// routeQualitySevereBand is the quality band at which a prefer binding yields.
const routeQualitySevereBand = 2

// preferAffinityExit serves a request bound by prefer affinity from a better
// channel while the bound one is severely downgraded: in quality band 2 on the
// request's model, operation and stream type (cache bands do not count). The
// candidates in a better band are tried in automatic ranking order through the
// normal admission, so an open breaker, a 429 cooldown or a full channel skips
// a candidate just as in automatic selection. It returns nil when the binding
// is not downgraded or no better candidate is admitted; the caller then uses
// the bound channel. Strict affinity never calls it. Exploration does not run
// here, so a yielding request spends no exploration budget.
func preferAffinityExit(c *gin.Context, rows []*model.Channel, bound *model.Channel, modelName string) (*model.Channel, error) {
	operation, stream := routeOperation(c), common.GetContextKeyBool(c, constant.ContextKeyIsStream)
	quality, now := routeQualities.snapshot(), time.Now()
	band := func(channel *model.Channel) int {
		mapped := routeAvailabilityKey(channel, modelName).model
		return routeQualityOf(quality, now, modelName, channel.Id, mapped, operation, stream).Band
	}
	limit := band(bound)
	if limit < routeQualitySevereBand {
		return nil, nil
	}
	var better []*model.Channel
	for _, channel := range rows {
		if channel.Id != bound.Id && band(channel) < limit {
			better = append(better, channel)
		}
	}
	orderAutomaticRoutes(better, c.GetString(common.RequestIdKey), modelName, operation, stream)
	for _, channel := range better {
		selected, err := acquireHealthyRoute(c, channel, modelName, false)
		var pause *routePauseError
		switch {
		case err == nil:
			return selected, nil
		case errors.As(err, &pause), errors.Is(err, ErrConcurrencyFull), errors.Is(err, errConcurrencyResourceDeleted):
			continue
		default:
			return nil, err
		}
	}
	return nil, nil
}

// clearAffinityIfStillBound clears the session binding only while it still
// points at the channel this request found unavailable: another request of the
// same session may have bound a working channel since this one read the
// binding. Reading and deleting are two cache operations, not one atomic
// compare-and-delete, so a binding written between them can still be lost; the
// window shrinks from the whole request to that gap.
func clearAffinityIfStillBound(c *gin.Context, channelID int) {
	cacheKey, _, ok := getChannelAffinityContext(c)
	if !ok {
		return
	}
	current, found, err := getChannelAffinityCache().Get(cacheKey)
	if err != nil {
		// The key holds the session value, so it is not logged.
		common.SysError(common.LogText("channel affinity cache get before clear failed: err=%v", err))
		return
	}
	if !found || current != channelID {
		// The binding this request used is gone; it no longer limits retries.
		c.Set(ginKeyChannelAffinitySkipRetry, false)
		return
	}
	ClearCurrentChannelAffinityCache(c)
}
