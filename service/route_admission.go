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

// routeScanRankedLimit bounds the visits a scan keeps for administrators.
const routeScanRankedLimit = 8

// routeScanCandidate is one visit of an automatic scan in visiting order, with
// the ranking inputs of the visited channel and the reason it was skipped.
type routeScanCandidate struct {
	ChannelID   int     `json:"channel_id"`
	Health      int     `json:"health"`
	Band        int     `json:"band"`
	CacheBand   *int    `json:"cache_band,omitempty"`
	Priority    int64   `json:"priority"`
	Weight      int     `json:"weight"`
	Load        int     `json:"load"`
	PriceFactor float64 `json:"price_factor"`
	Explore     bool    `json:"explore,omitempty"`
	Skip        string  `json:"skip,omitempty"`
}

// routeScanAbortReason names the technical error that ended a scan at a
// candidate; it is empty for the selected candidate.
func routeScanAbortReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errConcurrencyLimitUnavailable):
		return "limit_unavailable"
	}
	return "error"
}

// noteVisit records an automatic visit until the limit is reached.
func (s *routeScan) noteVisit(c *gin.Context, ranks map[int]routeRank, channel *model.Channel, skip string) {
	if len(s.Ranked) >= routeScanRankedLimit {
		return
	}
	r := ranks[channel.Id]
	entry := routeScanCandidate{ChannelID: channel.Id, Health: r.health, Band: r.band, Priority: channel.GetPriority(),
		Weight: channel.GetWeight(), Load: r.load, PriceFactor: r.price, Explore: channel.Id == c.GetInt("route_explore_channel"), Skip: skip}
	if r.cacheKnown {
		entry.CacheBand = &r.cacheBand
	}
	s.Ranked = append(s.Ranked, entry)
}

func routeRanksByChannel(c *gin.Context) map[int]routeRank {
	ranks, _ := common.GetContextKeyType[[]routeRank](c, "route_rank_candidates")
	byID := make(map[int]routeRank, len(ranks))
	for _, r := range ranks {
		byID[r.channel.Id] = r
	}
	return byID
}

// 先扫描可用渠道；只有不存在可用者时才考虑仅冷却渠道。第二轮仍复核熔断与容量。
func (s *routeScan) acquire(c *gin.Context, rows []*model.Channel, modelName string, automatic bool) (*model.Channel, error) {
	var cooling []*model.Channel
	var earliest int64
	var blocked int
	var ranks map[int]routeRank
	if automatic {
		ranks = routeRanksByChannel(c)
	}
	visit := func(channel *model.Channel, fallback bool) (*model.Channel, error) {
		var selected *model.Channel
		var err error
		if automatic {
			selected, err = acquireHealthyRoute(c, channel, modelName, fallback)
		} else {
			selected, err = acquireRouteChannel(c, channel)
		}
		skip := ""
		var pause *routePauseError
		switch {
		case errors.As(err, &pause):
			switch pause.class {
			case routeCooling:
				cooling = append(cooling, channel)
				s.Cooling++
				skip = "cooling"
			case routeOpen:
				blocked++
				s.Open++
				if earliest == 0 || pause.until < earliest {
					earliest = pause.until
				}
				skip = "open"
			case routeProbeBusy:
				blocked++
				s.ProbeBusy++
				skip = "probe_busy"
			}
		case errors.Is(err, ErrConcurrencyFull):
			s.FullSkips++
			skip = "full"
		case errors.Is(err, errConcurrencyResourceDeleted):
			// 在读取候选后被删除，继续候选扫描。
			skip = "deleted"
		default:
			// The selected channel, or a technical error that ends the scan.
			if automatic {
				s.noteVisit(c, ranks, channel, routeScanAbortReason(err))
			}
			return selected, err
		}
		if automatic {
			s.noteVisit(c, ranks, channel, skip)
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
