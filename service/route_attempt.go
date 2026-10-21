package service

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

type routeAttempt struct {
	once        sync.Once
	started     time.Time
	channelID   int
	model       string
	epoch       uint64
	bound       bool
	operation   int
	firstOutput atomic.Int64
}

func currentRouteAttempt(c *gin.Context) *routeAttempt {
	value, _ := c.Get("route_attempt")
	attempt, _ := value.(*routeAttempt)
	return attempt
}

// ObserveRouteStreamOutput 标记第一个有模型输出的事件：它既是调度延迟的终点，也是熔断
// 判定流式请求已服务的成功界。原生首字节时钟保持独立，因为重试安全还要看控制帧。
func ObserveRouteStreamOutput(c *gin.Context, data string) {
	attempt := currentRouteAttempt(c)
	if attempt == nil || attempt.firstOutput.Load() != 0 {
		return
	}
	if streamEventHasOutput(data) {
		attempt.firstOutput.CompareAndSwap(0, time.Now().UnixNano())
	}
}

// ObserveRouteResponse 由传输层在收到上游响应后调用。上游 429 带 Retry-After 时，
// 在这段时间内把渠道降到最差档；只降档、不剔除。
func ObserveRouteResponse(c *gin.Context, resp *http.Response) {
	attempt := currentRouteAttempt(c)
	if attempt == nil || !attempt.bound || resp == nil || resp.StatusCode != http.StatusTooManyRequests {
		return
	}
	if until, ok := retryAfterUntil(resp.Header, time.Now()); ok {
		routeObservations.coolDown(attempt.epoch, attempt.channelID, until)
	}
}

// MarkRouteAttemptSent runs immediately before transport dispatch, after local validation.
func MarkRouteAttemptSent(c *gin.Context, info *relaycommon.RelayInfo) {
	if value, ok := c.Get("route_attempt"); ok && value != nil {
		return
	}
	if info != nil {
		info.BeginUpstreamAttempt()
	}
	name := ""
	if info != nil {
		name, _, _ = relaycommon.ResolveMappedModelName(info.OriginModelName, c.GetString("model_mapping"))
	}
	value, exists := c.Get("route_observation_epoch")
	epoch, bound := value.(uint64)
	c.Set("route_attempt", &routeAttempt{started: time.Now(), channelID: common.GetContextKeyInt(c, constant.ContextKeyChannelId), model: name, epoch: epoch, bound: exists && bound, operation: routeOperation(c)})
}

// FinishRouteAttempt records at most one sample and releases the attempt's channel slot.
func FinishRouteAttempt(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) {
	defer ReleaseRouteChannel(c)
	value, ok := c.Get("route_attempt")
	if !ok {
		return
	}
	c.Set("route_attempt", nil)
	attempt, ok := value.(*routeAttempt)
	if !ok || attempt == nil || info == nil {
		return
	}
	attempt.once.Do(func() {
		if !attempt.bound {
			return
		}
		outcome := perfmetrics.OutcomeIgnored
		if c.Request.Context().Err() != context.DeadlineExceeded {
			outcome = perfmetrics.ClassifyRelayOutcome(c.Request.Context(), info, apiErr)
		}
		recordAttemptBreaker(attempt, info.IsStream, outcome)
		if outcome == perfmetrics.OutcomeIgnored {
			return
		}
		latency := int64(0)
		if outcome == perfmetrics.OutcomeSuccess {
			if !info.IsStream {
				latency = time.Since(attempt.started).Milliseconds()
			} else if first := attempt.firstOutput.Load(); first > attempt.started.UnixNano() {
				latency = max(1, time.Unix(0, first).Sub(attempt.started).Milliseconds())
			}
		}
		recordRouteObservation(attempt.epoch, attempt.channelID, attempt.model, attempt.operation, info.IsStream, outcome == perfmetrics.OutcomeSuccess, latency)
	})
}

// recordAttemptBreaker 以第一个有内容的事件为流式请求的成功界：之后的中断、取消或
// 错误不再算作渠道失败。没有输出的流式请求和非流式请求按最终结果计。
func recordAttemptBreaker(attempt *routeAttempt, stream bool, outcome perfmetrics.Outcome) {
	switch {
	case stream && attempt.firstOutput.Load() != 0:
		recordRouteBreaker(attempt.epoch, attempt.channelID, attempt.model, true)
	case outcome == perfmetrics.OutcomeSuccess:
		recordRouteBreaker(attempt.epoch, attempt.channelID, attempt.model, true)
	case outcome == perfmetrics.OutcomeFailure:
		recordRouteBreaker(attempt.epoch, attempt.channelID, attempt.model, false)
	}
}
