package service

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

type routeAttempt struct {
	once      sync.Once
	started   time.Time
	channelID int
	model     string
	epoch     uint64
	bound     bool
	operation int
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
		if c.Request.Context().Err() == context.DeadlineExceeded {
			return
		}
		outcome := perfmetrics.ClassifyRelayOutcome(c.Request.Context(), info, apiErr)
		if outcome == perfmetrics.OutcomeIgnored {
			return
		}
		latency := int64(0)
		if outcome == perfmetrics.OutcomeSuccess {
			if !info.IsStream {
				latency = time.Since(attempt.started).Milliseconds()
			} else if info.FirstResponseTime.After(attempt.started) {
				latency = info.FirstResponseTime.Sub(attempt.started).Milliseconds()
			}
		}
		recordRouteObservation(attempt.epoch, attempt.channelID, attempt.model, attempt.operation, info.IsStream, outcome == perfmetrics.OutcomeSuccess, latency)
	})
}
