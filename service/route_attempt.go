package service

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type routeAttempt struct {
	probe          *routeProbe
	once           sync.Once
	started        time.Time
	channelID      int
	model          string
	epoch          uint64
	bound          bool
	operation      int
	firstOutput    atomic.Int64
	lastOutput     atomic.Int64
	streamEvents   atomic.Int64
	requestEnded   atomic.Bool
	stream         bool
	breakerSettled atomic.Bool
	status         atomic.Int64
	cooldown       atomic.Int64
	estimated      atomic.Bool
	estimatedCount atomic.Int64
	usageMu        sync.Mutex
	usage          routeUsage
	info           *relaycommon.RelayInfo
	admission      string
	price          float64
	quality        int
	cacheMode      string
}

func currentRouteAttempt(c *gin.Context) *routeAttempt {
	value, _ := c.Get("route_attempt")
	attempt, _ := value.(*routeAttempt)
	return attempt
}

// RouteAttemptWasSent separates transport attempts from local validation and billing errors.
// Read it before FinishRouteAttempt clears the current attempt.
func RouteAttemptWasSent(c *gin.Context) bool {
	return currentRouteAttempt(c) != nil
}

// ObserveRouteStreamOutput 标记第一个有模型输出的事件：它既是调度延迟的终点，也是熔断
// 判定流式请求已服务的成功界。原生首字节时钟保持独立，因为重试安全还要看控制帧。
func ObserveRouteStreamOutput(c *gin.Context, data string) {
	attempt := currentRouteAttempt(c)
	if attempt == nil {
		return
	}
	attempt.streamEvents.Add(1)
	// Responses terminals can replay all previously delivered content. Only a
	// terminal-only response may use that replay as its first observed output.
	if attempt.firstOutput.Load() != 0 {
		switch gjson.Get(data, "type").String() {
		case "response.completed", "response.done", "response.incomplete", "response.output_item.done":
			return
		}
	}
	if streamEventHasOutput(data) {
		attempt.markOutput(time.Now())
		return
	}
	// The end reason only matters while the stream has produced nothing.
	if attempt.firstOutput.Load() == 0 && streamEventEndsForRequest(data) {
		attempt.requestEnded.Store(true)
	}
}

// ObserveRouteBinaryOutput 供直接写出二进制数据的流使用：调用方在非空数据写出并刷新后调用，
// 与有输出的事件一样标记首个输出。
func ObserveRouteBinaryOutput(c *gin.Context) {
	attempt := currentRouteAttempt(c)
	if attempt == nil {
		return
	}
	attempt.markOutput(time.Now())
}

func (a *routeAttempt) markOutput(now time.Time) {
	instant := now.UnixNano()
	if a.firstOutput.CompareAndSwap(0, instant) {
		a.settleStreamProbe()
	}
	a.lastOutput.Store(instant)
}

// settleStreamProbe 在流式探测的首个内容事件时按成功结算熔断：首输出是流式的成功界，
// 结算会清掉打开状态里匹配的许可，下一个请求即可领取探测。只结算熔断，渠道并发槽
// 仍到尝试结束才释放。首输出与结束可能在不同 goroutine，breakerSettled 保证只记一次。
func (a *routeAttempt) settleStreamProbe() {
	if !a.bound || !a.stream || a.probe == nil || !a.breakerSettled.CompareAndSwap(false, true) {
		return
	}
	routeObservations.recordBreaker(a.epoch, routeBreakerKey{a.channelID, a.model}, a.probe, routeBreakerSample{success: true})
}

// ObserveRouteResponse 由传输层在收到上游 429 时按 Retry-After 或缺头的短冷却期限将渠道降档。
func ObserveRouteResponse(c *gin.Context, resp *http.Response) {
	attempt := currentRouteAttempt(c)
	if attempt == nil || !attempt.bound || resp == nil {
		return
	}
	attempt.status.Store(int64(resp.StatusCode))
	if resp.StatusCode != http.StatusTooManyRequests {
		return
	}
	if cooldown, ok := retryAfterUntil(resp.Header, time.Now()); ok {
		attempt.cooldown.Store(cooldown.until)
		routeObservations.coolDown(attempt.epoch, attempt.channelID, cooldown)
	}
}

// MarkRouteAttemptSent runs immediately before transport dispatch, after local validation.
func MarkRouteAttemptSent(c *gin.Context, info *relaycommon.RelayInfo) {
	if value, ok := c.Get("route_attempt"); ok && value != nil {
		return
	}
	MarkRouteScanDispatched(c)
	if info != nil {
		info.BeginUpstreamAttempt()
		info.PerformanceOutputTokens = 0
	}
	name := ""
	if info != nil {
		name, _, _ = relaycommon.ResolveMappedModelName(info.OriginModelName, c.GetString("model_mapping"))
	}
	value, exists := c.Get("route_observation_epoch")
	epoch, bound := value.(uint64)
	// A native token count is free and carries no generation outcome, so it never
	// writes the breaker, the 429 cooldown or observation samples that order paid
	// generation requests; the attempt still releases its channel slot.
	bound = exists && bound && (info == nil || info.RelayMode != relayconstant.RelayModeNativeCount)
	c.Set("route_attempt", &routeAttempt{
		info: info, started: time.Now(), channelID: common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		admission: c.GetString("route_admission_source"), quality: c.GetInt("route_selected_quality"),
		price: c.GetFloat64("route_price_factor"), cacheMode: routeCacheContext(c).mode,
		probe: currentRouteProbe(c), model: name, epoch: epoch, bound: bound, operation: routeOperation(c),
		stream: info != nil && info.IsStream,
	})
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
		now := time.Now()
		evidence := attempt.finalEvidence(c, info, apiErr, now)
		outcome := perfmetrics.Outcome(evidence.Outcome)
		failure := routeBreakerSample{status: evidence.Status, code: routeFailureCode(info, apiErr, evidence.OutcomeReason)}
		recordAttemptBreaker(attempt, info.IsStream, outcome, failure)
		state := RequestPolicy(c)
		state.mu.Lock()
		attachRouteEvidence(state.events, state.Attempts, attempt.channelID, &evidence)
		state.mu.Unlock()
		if outcome == perfmetrics.OutcomeIgnored {
			return
		}
		good := outcome == perfmetrics.OutcomeSuccess
		latency := evidence.DurationMS
		if info.IsStream {
			latency = evidence.FirstOutputMS
		}
		if !good {
			latency = 0
		}
		key := routeObservationKey{attempt.channelID, attempt.model, attempt.operation, info.IsStream}
		tokens, generation := evidence.speedSample()
		recordRouteQuality(attempt.epoch, info.OriginModelName, key, good, latency, tokens, generation)
		recordRouteObservation(attempt.epoch, key.id, key.model, key.operation, key.stream, good, latency)
		if good {
			ctx := routeCacheContext(c)
			routeObservations.mu.Lock()
			if !routeObservations.staleLocked(attempt.epoch, attempt.channelID) {
				routeCaches.record(ctx, info.OriginModelName, key, attempt.started, now, evidence.CacheReadTokens, evidence.InputTokens, evidence.CacheKnown, attempt.admission)
			}
			routeObservations.mu.Unlock()
		}
	})
}

// recordAttemptBreaker 以第一个有内容的事件为流式请求的成功界：之后的中断、取消或
// 错误不再算作渠道失败。没有输出的流式请求和非流式请求按最终结果计。已在首输出时
// 结算的流式探测不再计入。
func recordAttemptBreaker(attempt *routeAttempt, stream bool, outcome perfmetrics.Outcome, failure routeBreakerSample) {
	if !attempt.breakerSettled.CompareAndSwap(false, true) {
		return
	}
	key := routeBreakerKey{attempt.channelID, attempt.model}
	switch {
	case stream && attempt.firstOutput.Load() != 0, outcome == perfmetrics.OutcomeSuccess:
		routeObservations.recordBreaker(attempt.epoch, key, attempt.probe, routeBreakerSample{success: true})
	case outcome == perfmetrics.OutcomeFailure:
		routeObservations.recordBreaker(attempt.epoch, key, attempt.probe, failure)
	}
}
