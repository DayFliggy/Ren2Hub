package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
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

func ReleaseRouteChannel(c *gin.Context) {
	if probe := currentRouteProbe(c); probe != nil {
		routeObservations.releaseProbe(probe)
		c.Set("route_probe", (*routeProbe)(nil))
	}
	if value, ok := c.Get("route_channel_permit"); ok {
		if permit, ok := value.(*ConcurrencyPermit); ok {
			permit.Release()
		}
		c.Set("route_channel_permit", (*ConcurrencyPermit)(nil))
	}
}

// routeScan describes one automatic selection, including a scan that exits before relay starts.
type routeScan struct {
	Round      int                  `json:"round"`
	Candidates int                  `json:"candidates"`
	FullSkips  int                  `json:"full_skips"`
	DurationUS int64                `json:"duration_us"`
	Cooling    int                  `json:"cooling,omitempty"`
	Open       int                  `json:"open,omitempty"`
	ProbeBusy  int                  `json:"probe_busy,omitempty"`
	Ranked     []routeScanCandidate `json:"ranked,omitempty"`
}

func (s *routeScan) record(c *gin.Context, channel *model.Channel, err error) {
	// Plugin decoding may canonicalize the model or pin an origin channel.
	// Publish the successful probe only when Distribute actually reacquires it.
	if state, ok := common.GetContextKeyType[*taskPluginPreselection](c, taskPluginPreselectionKey); ok && state.selecting && err == nil {
		if channel != nil {
			state.scan = s
		}
		return
	}

	result := "no_candidate"
	channelID := 0
	switch {
	case channel != nil:
		result, channelID = "selected", channel.Id
	case errors.Is(err, ErrConcurrencyFull):
		result = "full"
	case err != nil:
		result = "error"
	}
	RequestPolicy(c).AddEvent(PolicyEvent{ChannelID: channelID, Scan: s,
		Decision: PolicyDecision{Action: "scan", Reason: result, Source: "routing"}})
	if channel == nil {
		// Early admission refusals and the last failed retry may have no database log.
		message := s.message(result)
		if err != nil {
			message = common.LogText("%s: %v", message, err)
		}
		common.SysError(common.LogText("%s | %s", c.GetString(common.RequestIdKey), message))
	}
}

func selectQualifiedChannel(c *gin.Context, modelName string) (*model.Channel, error) {
	ReleaseRouteChannel(c)
	BeginRouteObservation(c)
	scope, err := requestRouteScope(c)
	if err != nil {
		logRouteLoadFailure(c, err)
		return nil, err
	}
	pin, pinned, _ := GetChannelConstraints(c).ResolvedPin()
	manual := HasPrivateTokenRoute(c)
	var scan routeScan
	started := time.Now()
	rows, err := routeCandidates(c.Request.Context(), scope, modelName, GetChannelConstraints(c).Filters)
	var selected *model.Channel
	if err == nil {
		scan.Candidates = len(rows)
		used := make(map[int]bool)
		for _, value := range c.GetStringSlice("use_channel") {
			id, _ := strconv.Atoi(value)
			used[id] = true
		}
		available := rows[:0]
		for _, channel := range rows {
			if !used[channel.Id] && (!pinned || channel.Id == pin.ChannelId) {
				available = append(available, channel)
			}
		}
		automatic := !manual && !pinned
		if automatic {
			orderRuntimeRoutes(c, available, modelName, routeOperation(c), common.GetContextKeyBool(c, constant.ContextKeyIsStream))
		}
		selected, err = scan.acquire(c, available, modelName, automatic)
		if selected != nil {
			source := "auto"
			if manual {
				source = "manual"
			}
			if pinned {
				source = "pin"
			}
			if !routePreselecting(c) {
				setRouteDecision(c, selected.Id, source)
			}
		}
	}
	if !manual && !pinned {
		scan.DurationUS = time.Since(started).Microseconds()
		scan.record(c, selected, err)
	} else if err != nil {
		logRouteLoadFailure(c, err)
	}
	return selected, err
}

// AcquirePinnedRouteChannel revalidates a continuation without selecting another channel.
func AcquirePinnedRouteChannel(c *gin.Context, channelID int, modelName string) (*model.Channel, error) {
	row, err := acquireBoundRouteChannel(c, channelID, modelName, boundCapacityOnly)
	if row != nil {
		setRouteDecision(c, row.Id, "pin")
	}
	return row, err
}

// boundRouteGate is how a channel already bound to the request is admitted.
type boundRouteGate int

const (
	// Pins, manual lists and origin tasks: capacity only.
	boundCapacityOnly boundRouteGate = iota
	// Plugin reacquisition: breaker and 429 cooldown.
	boundHealthy
	// Prefer affinity: as boundHealthy, but a binding in severe quality
	// downgrade yields to a better candidate (preferAffinityExit).
	boundPreferAffinity
	// Strict affinity: breaker only; a channel that merely cools down after a
	// 429 is still used, because the session has no other channel.
	boundHealthyAllowCooling
)

// affinityRouteGate is the one admission rule for a session-bound channel,
// shared by affinity selection and plugin reacquisition.
func affinityRouteGate(c *gin.Context) boundRouteGate {
	if RequestPolicy(c).SessionMode == "strict" {
		return boundHealthyAllowCooling
	}
	return boundPreferAffinity
}

func acquireBoundRouteChannel(c *gin.Context, channelID int, modelName string, gate boundRouteGate) (*model.Channel, error) {
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
		switch gate {
		case boundPreferAffinity:
			if row, err := preferAffinityExit(c, rows, channel, modelName); row != nil || err != nil {
				return row, err
			}
			return acquireHealthyRoute(c, channel, modelName, false)
		case boundHealthy:
			return acquireHealthyRoute(c, channel, modelName, false)
		case boundHealthyAllowCooling:
			return acquireHealthyRoute(c, channel, modelName, true)
		}
		return acquireRouteChannel(c, channel)
	}
	return nil, model.ErrTokenRouteForbidden
}

const taskPluginPreselectionKey = "task_plugin_preselection_scan"

type taskPluginPreselection struct {
	selecting bool
	scan      *routeScan
}

// PreselectTaskPluginEndpointChannel chooses a decoder or relay before the final route is known.
func PreselectTaskPluginEndpointChannel(c *gin.Context, modelName string) (*model.Channel, *ChannelSelectError) {
	state := &taskPluginPreselection{selecting: true}
	c.Set(taskPluginPreselectionKey, state)
	channel, err := SelectChannelForRequest(c, modelName, nil)
	state.selecting = false
	return channel, err
}

// ReacquireSelectedRouteChannel admits a channel chosen before user admission
// under the request's current filters. A full or no longer eligible channel
// returns nil so the caller selects again; other failures are reported.
func ReacquireSelectedRouteChannel(c *gin.Context, channelID int, modelName string) (*model.Channel, *ChannelSelectError) {
	var scan *routeScan
	if state, ok := common.GetContextKeyType[*taskPluginPreselection](c, taskPluginPreselectionKey); ok {
		scan, state.scan = state.scan, nil
	}
	started := time.Now()
	_, pinned, _ := GetChannelConstraints(c).ResolvedPin()
	gate := boundHealthy
	if affinityInfo, _ := c.Get(ginKeyChannelAffinityLogInfo); affinityInfo != nil {
		gate = affinityRouteGate(c)
	}
	if pinned || HasPrivateTokenRoute(c) {
		gate = boundCapacityOnly
	}
	channel, err := acquireBoundRouteChannel(c, channelID, modelName, gate)
	if channel != nil && channel.Id != channelID {
		// A downgraded binding yielded to another channel, but the caller
		// prepared this request for the bound one: it selects again.
		ReleaseRouteChannel(c)
		forgetChannelAffinityUse(c)
		return nil, nil
	}
	var pause *routePauseError
	if errors.As(err, &pause) || errors.Is(err, ErrConcurrencyFull) || errors.Is(err, model.ErrTokenRouteForbidden) {
		// The caller selects again, which records affinity use afresh.
		forgetChannelAffinityUse(c)
		return nil, nil
	}
	if scan != nil {
		scan.DurationUS += time.Since(started).Microseconds()
		scan.record(c, channel, err)
	} else if err != nil {
		logRouteLoadFailure(c, err)
	}
	if err != nil {
		return nil, classifyRouteLoadFailure(err)
	}
	setRouteDecision(c, channel.Id, "plugin")
	return channel, nil
}

// errOriginTaskChannelUnavailable: the origin task's channel is deleted (as
// confirmed by the database), disabled or otherwise not routable.
var errOriginTaskChannelUnavailable = errors.New("the channel of the origin task is unavailable")

var errOriginTaskChannelRatioExceeded = errors.New("the origin channel exceeds this token's maximum channel multiplier")

// originTaskFilterError: the origin channel fails an explicit request constraint.
type originTaskFilterError struct{ kind dto.ChannelFilterKind }

func (e *originTaskFilterError) Error() string {
	return fmt.Sprintf("the channel of the origin task does not satisfy %s", e.kind)
}

// originTaskFilters keeps the request's explicit constraints (path, WebSocket
// capability, plugin identity or endpoint naming a plugin). An accepted task
// does not follow the Key's current manual list, and the ordinary-request
// default "no type-61 channel" is not a constraint on the task's identity; the
// task's owner and platform checks are.
func originTaskFilters(filters []dto.ChannelFilter) []dto.ChannelFilter {
	kept := make([]dto.ChannelFilter, 0, len(filters))
	for _, filter := range filters {
		if filter.Kind == dto.FilterTokenRoute {
			continue
		}
		if filter.Kind == dto.FilterTaskPluginIdentity && filter.TaskPluginKey == "" && len(filter.TaskPluginKeys) == 0 && len(filter.TaskPluginChannelTypes) == 0 {
			continue
		}
		kept = append(kept, filter)
	}
	return kept
}

// AcquireOriginTaskRouteChannel is the one admission rule for continuing an
// accepted task on its original channel, used after ownership is verified.
// The Key's manual list is exempt, but its ratio ceiling still applies. The
// caller (Distribute) checks model limits. The channel must still exist, be
// routable with a valid ratio, satisfy explicit constraints and have capacity.
func AcquireOriginTaskRouteChannel(c *gin.Context, channelID int, modelName string) (*model.Channel, error) {
	ReleaseRouteChannel(c)
	BeginRouteObservation(c)
	if err := c.Request.Context().Err(); err != nil {
		return nil, err
	}
	channel, err := model.GetChannelById(channelID, true)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: %w", errOriginTaskChannelUnavailable, err)
	}
	if err != nil {
		return nil, err
	}
	if !model.ChannelRoutable(channel) {
		return nil, errOriginTaskChannelUnavailable
	}
	scope, err := requestRouteScope(c)
	if err != nil {
		return nil, err
	}
	if reason := routePolicyReason(channel, scope.route, false); reason != "" {
		if reason == "channel_ratio_exceeded" {
			return nil, errOriginTaskChannelRatioExceeded
		}
		return nil, fmt.Errorf("invalid origin task channel policy: %s", reason)
	}
	if ok, kind := model.ChannelSatisfiesFilters(channel, modelName, originTaskFilters(GetChannelConstraints(c).Filters)); !ok {
		return nil, &originTaskFilterError{kind: kind}
	}
	selected, err := acquireRouteChannel(c, channel)
	if selected != nil {
		setRouteDecision(c, selected.Id, "pin")
	}
	if errors.Is(err, errConcurrencyResourceDeleted) {
		return nil, fmt.Errorf("%w: %w", errOriginTaskChannelUnavailable, err)
	}
	return selected, err
}

// selectOriginTaskChannel admits an origin-task pin without affinity or
// ordinary selection.
func selectOriginTaskChannel(c *gin.Context, channelID int, modelName string) (*model.Channel, *ChannelSelectError) {
	channel, err := AcquireOriginTaskRouteChannel(c, channelID, modelName)
	if err == nil {
		return channel, nil
	}
	if status, code, message, matched := originTaskRouteFailure(err); matched {
		return nil, &ChannelSelectError{StatusCode: status, Code: types.ErrorCode(code), Message: message}
	}
	return nil, routeLoadFailure(c, err)
}

// originTaskRouteFailure maps the origin-task-only admission failures; other
// failures (capacity, unreadable limit, storage, cancellation) keep the shared
// mapping.
func originTaskRouteFailure(err error) (status int, code string, message string, matched bool) {
	var filterErr *originTaskFilterError
	switch {
	case errors.Is(err, errOriginTaskChannelRatioExceeded):
		return http.StatusBadRequest, "channel_ratio_exceeded", errOriginTaskChannelRatioExceeded.Error(), true
	case errors.Is(err, errOriginTaskChannelUnavailable):
		return http.StatusBadRequest, "origin_task_channel_disabled", "the channel of the origin task is unavailable", true
	case errors.As(err, &filterErr):
		return http.StatusBadRequest, string(filterErr.kind), filterErr.Error(), true
	}
	return 0, "", "", false
}

func acquireRouteChannel(c *gin.Context, channel *model.Channel) (*model.Channel, error) {
	permit, err := AcquireChannelConcurrency(c.Request.Context(), channel)
	if errors.Is(err, errConcurrencyResourceDeleted) {
		// 候选读出后渠道被删除：与读不到渠道相同，按对这个 Key 不可选处理，prefer 亲和据此退回正常选路。
		return nil, fmt.Errorf("%w: %w", model.ErrTokenRouteForbidden, err)
	}
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
// previewRouteReason 判单个渠道；exact 表示它以精确模型名可选。精确一档的取舍由
// PreviewTokenRouting 在所有渠道判完后统一处理，与 routeCandidates 一致。
// 先判渠道是否声明了该模型（declared 含禁用的能力行），不服务该模型的渠道不报可用性和倍率；
// 声明了的再按渠道可用性、Key 策略、能力行启用状态和过滤器说明原因。
func previewRouteReason(row *model.Channel, scope routeScope, modelName string, declared map[string]bool, filters []dto.ChannelFilter) (reason string, exact bool) {
	if row == nil {
		return routeReasonChannelUnavailable, false
	}
	if !scope.allowsModel(modelName) {
		return "model_not_allowed", false
	}
	exactEnabled, exactDeclared := declared[modelName]
	routingEnabled, routingDeclared := declared[ratio_setting.RoutingMatchModelName(modelName)]
	if !exactDeclared && !routingDeclared {
		return "model_unavailable", false
	}
	if reason := routeChannelReason(row, scope.route, true); reason != "" {
		return reason, false
	}
	// 渠道可用但匹配的能力行都已禁用：与选路和目录一致，按不服务该模型处理。
	if !exactEnabled && !routingEnabled {
		return "model_unavailable", false
	}
	if ok, kind := model.ChannelSatisfiesFilters(row, modelName, filters); !ok {
		if kind == dto.FilterRequestPath || kind == dto.FilterResponsesWebSocket {
			return "protocol_unavailable", false
		}
		return string(kind), false
	}
	return "", exactEnabled
}

func routeOperation(c *gin.Context) int {
	operation := relayconstant.Path2RelayMode(c.Request.URL.Path)
	if operation == relayconstant.RelayModeUnknown {
		operation = c.GetInt("relay_mode")
	}
	return operation
}
