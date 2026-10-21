package service

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// RouteChannel 是普通用户看到的渠道视图，模型目录和 Key 路由编辑器共用这一份定义。
// 保留计价和手动选路需要的 channel_ratio、max_concurrency；不含实时 in_flight，
// 其他用户的占用情况只在管理端（渠道管理、/api/performance/routing）可见。
type RouteChannel struct {
	ID                 int                                `json:"id"`
	Name               string                             `json:"name"`
	Type               int                                `json:"type"`
	Status             int                                `json:"status"`
	Models             []string                           `json:"models"`
	SupportedEndpoints map[string][]constant.EndpointType `json:"supported_endpoints"`
	ChannelRatio       string                             `json:"channel_ratio"`
	MaxConcurrency     int                                `json:"max_concurrency"`
	Eligible           bool                               `json:"eligible"`
	FilterReason       string                             `json:"filter_reason,omitempty"`
}

func routeChannelReason(channel *model.Channel, route model.TokenRouting, manual bool) string {
	if channel.Status != common.ChannelStatusEnabled || channel.Type == constant.ChannelTypeCodex {
		return "channel_unavailable"
	}
	ratio, err := model.NormalizeChannelRatio(channel.ChannelRatio)
	if err != nil {
		return "invalid_channel_ratio"
	}
	if route.MaxChannelRatio != nil {
		ceiling, err := decimal.NewFromString(*route.MaxChannelRatio)
		if err != nil {
			return "invalid_ratio_limit"
		}
		value, _ := decimal.NewFromString(ratio)
		if value.GreaterThan(ceiling) {
			return "channel_ratio_exceeded"
		}
	}
	if manual && route.Mode == model.TokenRouteManual {
		for _, entry := range route.Entries {
			if entry.ChannelID == channel.Id {
				if entry.Enabled {
					return ""
				}
				return "entry_disabled"
			}
		}
		return "not_in_manual_route"
	}
	return ""
}

func RouteCandidates(ctx context.Context, tokenID int, modelName string, filters []dto.ChannelFilter) ([]*model.Channel, error) {
	route := model.TokenRouting{TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteAuto}}
	var token *model.Token
	if tokenID > 0 {
		var err error
		route, err = model.ReadTokenRouting(ctx, tokenID)
		if err != nil {
			return nil, err
		}
		token, err = model.GetTokenById(tokenID)
		if err != nil {
			return nil, err
		}
		if token.ModelLimitsEnabled && !TokenModelLimitAllows(token.GetModelLimitsMap(), modelName) {
			return []*model.Channel{}, nil
		}
	}
	rows, err := model.RouteChannelRows(ctx, modelName, nil)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	abilities, err := model.EnabledRouteModels(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]*model.Channel, 0, len(rows))
	for _, row := range rows {
		if routeCandidateReason(row, route, token, modelName, abilities[row.Id], filters) == "" {
			result = append(result, row)
		}
	}
	if route.Mode == model.TokenRouteManual {
		positions := make(map[int]int, len(route.Entries))
		for i, e := range route.Entries {
			positions[e.ChannelID] = i
		}
		sort.SliceStable(result, func(i, j int) bool { return positions[result[i].Id] < positions[result[j].Id] })
	}
	return result, nil
}

func CatalogRouteChannels(ctx context.Context, userID, tokenID int) ([]RouteChannel, error) {
	return catalogRouteChannels(ctx, userID, tokenID, true)
}

func catalogRouteChannels(ctx context.Context, userID, tokenID int, manual bool) ([]RouteChannel, error) {
	route := model.TokenRouting{TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteAuto}}
	var token *model.Token
	if tokenID > 0 {
		var err error
		token, err = model.GetTokenByIds(tokenID, userID)
		if err != nil {
			return nil, err
		}
		route, err = model.ReadTokenRouting(ctx, tokenID)
		if err != nil {
			return nil, err
		}
	}
	rows, err := model.RouteChannelRows(ctx, "", nil)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	advertised, err := model.EnabledRouteModels(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]RouteChannel, 0, len(rows))
	for _, row := range rows {
		item := RouteChannel{ID: row.Id, Name: row.Name, Type: row.Type, Status: row.Status, Models: []string{}, SupportedEndpoints: map[string][]constant.EndpointType{}, ChannelRatio: row.ChannelRatio, MaxConcurrency: row.MaxConcurrency}
		names := advertised[row.Id]
		if token != nil && token.ModelLimitsEnabled {
			abilities := make(map[string]bool, len(names))
			for _, ability := range names {
				abilities[ability] = true
			}
			requests := token.GetModelLimitsMap()
			for _, ability := range names {
				if TokenModelLimitAllows(requests, ability) {
					requests[ability] = true
				}
			}
			names = []string{}
			for requestName := range requests {
				if abilities[requestName] || abilities[ratio_setting.RoutingMatchModelName(requestName)] {
					names = append(names, requestName)
				}
			}
			sort.Strings(names)
		}
		for _, name := range names {
			item.Models = append(item.Models, name)
			endpoints := common.GetEndpointTypesByChannelType(row.Type, name)
			if cfg := row.GetOtherSettings().AdvancedCustom; row.Type == constant.ChannelTypeAdvancedCustom && cfg != nil {
				endpoints = cfg.SupportedEndpointTypesForModel(name)
			}
			item.SupportedEndpoints[name] = endpoints
		}
		if len(item.Models) == 0 {
			continue
		}
		item.FilterReason = routeChannelReason(row, route, manual)
		item.Eligible = item.FilterReason == ""
		result = append(result, item)
	}
	return result, nil
}

func observationBand(value, warning, critical float64) int {
	if value >= critical {
		return 2
	}
	if value >= warning {
		return 1
	}
	return 0
}
func routeLoadBucket(active int) int {
	if active <= 0 {
		return 0
	}
	return max(1, int(math.Log2(float64(active))))
}

func orderAutomaticRoutes(rows []*model.Channel, requestID, modelName string, operation int, stream bool) {
	type ranking struct {
		health, load, latency int
		rank                  float64
	}
	ranks := make(map[int]ranking, len(rows))
	for _, c := range rows {
		r := ranking{load: routeLoadBucket(ChannelInFlight(c.Id))}
		upstreamModel, _, mappingErr := relaycommon.ResolveMappedModelName(modelName, c.GetModelMapping())
		ob := RouteObservationSnapshot{}
		if mappingErr == nil {
			ob = routeObservations.get(c.Id, upstreamModel, operation, stream)
		}
		if ob.Known {
			r.health = observationBand(ob.ErrorRate, .05, .20)
			if ob.LatencyKnown {
				r.latency = observationBand(ob.LatencyMS, 3000, 10000)
			}
		}
		hash := sha256.Sum256([]byte(requestID + ":" + strconv.Itoa(c.Id)))
		uniform := (float64(binary.BigEndian.Uint64(hash[:8])>>11) + 1) / (float64(uint64(1)<<53) + 1)
		r.rank = -math.Log(uniform) / float64(max(c.GetWeight(), 1))
		ranks[c.Id] = r
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		x, y := ranks[a.Id], ranks[b.Id]
		if a.GetPriority() != b.GetPriority() {
			return a.GetPriority() > b.GetPriority()
		}
		if x.health != y.health {
			return x.health < y.health
		}
		if (a.GetWeight() > 0) != (b.GetWeight() > 0) {
			return a.GetWeight() > 0
		}
		if x.load != y.load {
			return x.load < y.load
		}
		if x.latency != y.latency {
			return x.latency < y.latency
		}
		if x.rank != y.rank {
			return x.rank < y.rank
		}
		return a.Id < b.Id
	})
}

func ReleaseRouteChannel(c *gin.Context) {
	if value, ok := c.Get("route_channel_permit"); ok {
		if permit, ok := value.(*ConcurrencyPermit); ok {
			permit.Release()
		}
		c.Set("route_channel_permit", (*ConcurrencyPermit)(nil))
	}
}

func selectQualifiedChannel(c *gin.Context, modelName string) (*model.Channel, error) {
	ReleaseRouteChannel(c)
	BeginRouteObservation(c)
	if err := LoadTokenRouteConstraints(c); err != nil {
		return nil, err
	}
	tokenID := common.GetContextKeyInt(c, constant.ContextKeyTokenId)
	rows, err := RouteCandidates(c.Request.Context(), tokenID, modelName, GetChannelConstraints(c).Filters)
	if err != nil {
		return nil, err
	}
	if !HasPrivateTokenRoute(c) {
		orderAutomaticRoutes(rows, c.GetString(common.RequestIdKey), modelName, routeOperation(c), common.GetContextKeyBool(c, constant.ContextKeyIsStream))
	}
	used := make(map[int]bool)
	for _, value := range c.GetStringSlice("use_channel") {
		id, _ := strconv.Atoi(value)
		used[id] = true
	}
	pin, pinned, _ := GetChannelConstraints(c).ResolvedPin()
	busy := false
	for _, channel := range rows {
		if used[channel.Id] && !(pinned && pin.RetryMode == dto.PinRetrySameChannel) || pinned && channel.Id != pin.ChannelId {
			continue
		}
		permit, err := AcquireChannelConcurrency(c.Request.Context(), channel)
		if errors.Is(err, ErrConcurrencyFull) {
			busy = true
			continue
		}
		if err != nil {
			return nil, err
		}
		c.Set("route_channel_permit", permit)
		c.Set("route_selected_channel", channel)
		c.Set("channel_ratio", channel.ChannelRatio)
		return channel, nil
	}
	if busy {
		return nil, ErrConcurrencyFull
	}
	return nil, nil
}

// AcquirePinnedRouteChannel revalidates a continuation without selecting another channel.
func AcquirePinnedRouteChannel(c *gin.Context, channelID int, modelName string) (*model.Channel, error) {
	ReleaseRouteChannel(c)
	BeginRouteObservation(c)
	rows, err := RouteCandidates(c.Request.Context(), c.GetInt("token_id"), modelName, GetChannelConstraints(c).Filters)
	if err != nil {
		return nil, err
	}
	for _, channel := range rows {
		if channel.Id != channelID {
			continue
		}
		return acquireRouteChannel(c, channel)
	}
	return nil, model.ErrTokenRouteForbidden
}

// AcquireOriginTaskRouteChannel is only used after the task resolver has
// verified ownership and pinned its original channel. Current Key preferences
// do not move an accepted task to another supplier, but capacity still applies.
func AcquireOriginTaskRouteChannel(c *gin.Context, channelID int) (*model.Channel, error) {
	ReleaseRouteChannel(c)
	BeginRouteObservation(c)
	if err := c.Request.Context().Err(); err != nil {
		return nil, err
	}
	channel, err := model.GetChannelById(channelID, true)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, model.ErrTokenRouteForbidden
	}
	if err != nil {
		return nil, err
	}
	if channel.Status != common.ChannelStatusEnabled || channel.Type == constant.ChannelTypeCodex {
		return nil, model.ErrTokenRouteForbidden
	}
	if _, err := model.NormalizeChannelRatio(channel.ChannelRatio); err != nil {
		return nil, err
	}
	return acquireRouteChannel(c, channel)
}

func acquireRouteChannel(c *gin.Context, channel *model.Channel) (*model.Channel, error) {
	permit, err := AcquireChannelConcurrency(c.Request.Context(), channel)
	if err != nil {
		return nil, err
	}
	c.Set("route_channel_permit", permit)
	c.Set("route_selected_channel", channel)
	c.Set("channel_ratio", channel.ChannelRatio)
	return channel, nil
}

func routeCandidateReason(row *model.Channel, route model.TokenRouting, token *model.Token, modelName string, abilities []string, filters []dto.ChannelFilter) string {
	if row == nil {
		return "channel_unavailable"
	}
	if token != nil && token.ModelLimitsEnabled && !TokenModelLimitAllows(token.GetModelLimitsMap(), modelName) {
		return "model_not_allowed"
	}
	if reason := routeChannelReason(row, route, true); reason != "" {
		return reason
	}
	matched := false
	for _, ability := range abilities {
		if ability == modelName || ability == ratio_setting.RoutingMatchModelName(modelName) {
			matched = true
			break
		}
	}
	if !matched {
		return "model_unavailable"
	}
	if ok, kind := model.ChannelSatisfiesFilters(row, modelName, filters); !ok {
		if kind == dto.FilterRequestPath || kind == dto.FilterResponsesWebSocket {
			return "protocol_unavailable"
		}
		return string(kind)
	}
	return ""
}

func routeOperation(c *gin.Context) int {
	operation := relayconstant.Path2RelayMode(c.Request.URL.Path)
	if operation == relayconstant.RelayModeUnknown {
		operation = c.GetInt("relay_mode")
	}
	return operation
}
