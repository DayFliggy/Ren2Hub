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

const routeReasonChannelUnavailable = "channel_unavailable"

func routeChannelReason(channel *model.Channel, route model.TokenRouting, manual bool) string {
	if !model.ChannelRoutable(channel) {
		return routeReasonChannelUnavailable
	}
	return routePolicyReason(channel, route, manual)
}

// routePolicyReason 只判 Key 侧策略（倍率上限、手动列表）；渠道可用性由调用方先判。
func routePolicyReason(channel *model.Channel, route model.TokenRouting, manual bool) string {
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

// routeCandidates 返回当前 Key 和模型可选的渠道：模型能力、ChannelRoutable、请求过滤器和
// Key 侧策略都满足，精确模型名一档优先；手动路由按列表顺序返回。
func routeCandidates(ctx context.Context, scope routeScope, modelName string, filters []dto.ChannelFilter) ([]*model.Channel, error) {
	if modelName == "" || !scope.allowsModel(modelName) {
		return []*model.Channel{}, nil
	}
	route := scope.route
	result, err := model.RouteChannelRowsAccepting(ctx, modelName, filters, func(row *model.Channel) bool {
		return routePolicyReason(row, route, true) == ""
	})
	if err != nil {
		return nil, err
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
		if mappingErr != nil {
			upstreamModel = ""
		}
		// 健康档来自熔断状态（B7），错误率只保留在快照里供观测，不再参与排序。
		health, weightFactor := routeObservations.routeHealth(c.Id, upstreamModel)
		r.health = health
		if upstreamModel != "" {
			if ob := routeObservations.get(c.Id, upstreamModel, operation, stream); ob.LatencyKnown {
				r.latency = observationBand(ob.LatencyMS, 3000, 10000)
			}
		}
		hash := sha256.Sum256([]byte(requestID + ":" + strconv.Itoa(c.Id)))
		uniform := (float64(binary.BigEndian.Uint64(hash[:8])>>11) + 1) / (float64(uint64(1)<<53) + 1)
		r.rank = -math.Log(uniform) / (float64(max(c.GetWeight(), 1)) * weightFactor)
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
	scope, err := requestRouteScope(c)
	if err != nil {
		return nil, err
	}
	rows, err := routeCandidates(c.Request.Context(), scope, modelName, GetChannelConstraints(c).Filters)
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
		selected, err := acquireRouteChannel(c, channel)
		if errors.Is(err, ErrConcurrencyFull) {
			busy = true
			continue
		}
		return selected, err
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
	scope, err := requestRouteScope(c)
	if err != nil {
		return nil, err
	}
	rows, err := routeCandidates(c.Request.Context(), scope, modelName, GetChannelConstraints(c).Filters)
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
	if !model.ChannelRoutable(channel) {
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

// previewRouteReason 只给路由预览用：预览列出全部渠道，所以要逐个说明不可选的原因，
// 包括渠道是否服务该模型。选路走 routeCandidates，不经过这里。
// previewRouteReason 判单个渠道；exact 表示它声明了请求的精确模型名。精确一档的取舍由
// PreviewTokenRouting 在所有渠道判完后统一处理，与 routeCandidates 一致。
func previewRouteReason(row *model.Channel, scope routeScope, modelName string, abilities []string, filters []dto.ChannelFilter) (reason string, exact bool) {
	if row == nil {
		return routeReasonChannelUnavailable, false
	}
	if !scope.allowsModel(modelName) {
		return "model_not_allowed", false
	}
	if reason := routeChannelReason(row, scope.route, true); reason != "" {
		return reason, false
	}
	matched := false
	for _, ability := range abilities {
		if ability == modelName {
			exact = true
		}
		if ability == modelName || ability == ratio_setting.RoutingMatchModelName(modelName) {
			matched = true
		}
	}
	if !matched {
		return "model_unavailable", false
	}
	if ok, kind := model.ChannelSatisfiesFilters(row, modelName, filters); !ok {
		if kind == dto.FilterRequestPath || kind == dto.FilterResponsesWebSocket {
			return "protocol_unavailable", false
		}
		return string(kind), false
	}
	return "", exact
}

func routeOperation(c *gin.Context) int {
	operation := relayconstant.Path2RelayMode(c.Request.URL.Path)
	if operation == relayconstant.RelayModeUnknown {
		operation = c.GetInt("relay_mode")
	}
	return operation
}
