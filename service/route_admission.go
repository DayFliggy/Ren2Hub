package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
)

type routePauseError struct {
	class routeAvailability
	until int64
}

func (e *routePauseError) Error() string { return "channel_temporarily_unavailable" }

func routePreselecting(c *gin.Context) bool {
	state, ok := common.GetContextKeyType[*taskPluginPreselection](c, taskPluginPreselectionKey)
	return ok && state.selecting
}

func currentRouteProbe(c *gin.Context) *routeProbe {
	probe, _ := common.GetContextKeyType[*routeProbe](c, "route_probe")
	return probe
}

func routeAvailabilityKey(channel *model.Channel, modelName string) routeBreakerKey {
	name, _, err := relaycommon.ResolveMappedModelName(modelName, channel.GetModelMapping())
	if err != nil {
		name = ""
	}
	return routeBreakerKey{channel.Id, name}
}

// 正式占槽与领取探测许可之间复核健康状态。插件预选只读，免费计数不占生成探测名额。
func acquireHealthyRoute(c *gin.Context, channel *model.Channel, modelName string, allowCooling bool) (*model.Channel, error) {
	key := routeAvailabilityKey(channel, modelName)
	class, until := routeObservations.availability(key)
	if class != routeAvailable && !(class == routeCooling && allowCooling) {
		return nil, &routePauseError{class, until}
	}
	selected, err := acquireRouteChannel(c, channel)
	if err != nil {
		return nil, err
	}
	epoch, _ := common.GetContextKeyType[uint64](c, "route_observation_epoch")
	readOnly := routePreselecting(c) || relayconstant.Path2RelayMode(c.Request.URL.Path) == relayconstant.RelayModeNativeCount
	probe, class, until := routeObservations.claimProbe(key, epoch, allowCooling, readOnly)
	if class != routeAvailable {
		ReleaseRouteChannel(c)
		return nil, &routePauseError{class, until}
	}
	if probe != nil {
		c.Set("route_probe", probe)
	}
	return selected, nil
}

// 先扫描可用渠道；只有不存在可用者时才考虑仅冷却渠道。第二轮仍复核熔断与容量。
func (s *routeScan) acquire(c *gin.Context, rows []*model.Channel, modelName string, automatic bool) (*model.Channel, error) {
	var cooling []*model.Channel
	var earliest int64
	var blocked int
	visit := func(channel *model.Channel, fallback bool) (*model.Channel, error) {
		var selected *model.Channel
		var err error
		if automatic {
			selected, err = acquireHealthyRoute(c, channel, modelName, fallback)
		} else {
			selected, err = acquireRouteChannel(c, channel)
		}
		var pause *routePauseError
		switch {
		case errors.As(err, &pause):
			switch pause.class {
			case routeCooling:
				cooling = append(cooling, channel)
				s.Cooling++
			case routeOpen:
				blocked++
				s.Open++
				if earliest == 0 || pause.until < earliest {
					earliest = pause.until
				}
			case routeProbeBusy:
				blocked++
				s.ProbeBusy++
			}
		case errors.Is(err, ErrConcurrencyFull):
			s.FullSkips++
		case errors.Is(err, errConcurrencyResourceDeleted):
			// 在读取候选后被删除，继续候选扫描。
		default:
			return selected, err
		}
		return nil, nil
	}
	for _, channel := range rows {
		if row, err := visit(channel, false); row != nil || err != nil {
			return row, err
		}
	}
	if s.FullSkips > 0 {
		return nil, ErrConcurrencyFull
	}
	for _, channel := range cooling {
		if row, err := visit(channel, true); row != nil || err != nil {
			return row, err
		}
	}
	if s.FullSkips > 0 {
		return nil, ErrConcurrencyFull
	}
	if blocked > 0 {
		return nil, &routePauseError{routeOpen, earliest}
	}
	return nil, nil
}

func setRouteRetryAfter(c *gin.Context, err error, first bool) {
	var pause *routePauseError
	if !first || !errors.As(err, &pause) || pause.until <= 0 || c.Writer.Written() {
		return
	}
	delay := pause.until - time.Now().UnixMilli()
	if delay > 0 {
		c.Header("Retry-After", fmt.Sprint((delay+999)/1000))
	}
}
