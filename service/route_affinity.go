package service

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// selectAffinityChannel 在首轮尝试会话亲和渠道。decided 为 false 时调用方继续正常选路。
// strict 模式下亲和渠道不可用直接拒绝；prefer 模式下渠道不合格或满载时退回正常选路，
// 存储等技术错误不当作“不合格”处理，照常返回 500。
func selectAffinityChannel(c *gin.Context, modelName string) (channel *model.Channel, selectErr *ChannelSelectError, decided bool) {
	preferred, found := GetPreferredChannelByAffinity(c, modelName)
	if !found {
		return nil, nil, false
	}
	row, err := AcquirePinnedRouteChannel(c, preferred, modelName)
	if err == nil {
		MarkChannelAffinityUsed(c, row.Id)
		return row, nil, true
	}
	full := errors.Is(err, ErrConcurrencyFull)
	if RequestPolicy(c).SessionMode == "strict" {
		if !full && !ShouldKeepChannelAffinityOnChannelDisabled() {
			ClearCurrentChannelAffinityCache(c)
		}
		c.Set(ginKeyChannelAffinitySkipRetry, true)
		status := http.StatusServiceUnavailable
		if full {
			status = http.StatusTooManyRequests
		}
		return nil, &ChannelSelectError{StatusCode: status, Code: "session_channel_unavailable", Message: "Bound session channel is unavailable"}, true
	}
	if full {
		return nil, nil, false
	}
	if !errors.Is(err, model.ErrTokenRouteForbidden) {
		return nil, routeLoadFailure(c, err), true
	}
	if !ShouldKeepChannelAffinityOnChannelDisabled() {
		ClearCurrentChannelAffinityCache(c)
	}
	return nil, nil, false
}
