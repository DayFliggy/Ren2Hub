package service

import (
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func GetChannelConstraints(c *gin.Context) *dto.ChannelConstraints {
	if c == nil {
		return &dto.ChannelConstraints{}
	}
	if existing, ok := common.GetContextKeyType[*dto.ChannelConstraints](c, constant.ContextKeyChannelConstraints); ok && existing != nil {
		return existing
	}
	constraints := &dto.ChannelConstraints{}
	common.SetContextKey(c, constant.ContextKeyChannelConstraints, constraints)
	return constraints
}

func AppendTaskPluginIdentityFilter(c *gin.Context, pluginKey string) {
	if c == nil {
		return
	}
	channelTypes, pluginKeys := pinnedTaskPluginIdentities(c, pluginKey)
	filter := dto.ChannelFilter{
		Kind:                   dto.FilterTaskPluginIdentity,
		TaskPluginKey:          pluginKey,
		TaskPluginChannelTypes: channelTypes,
		TaskPluginKeys:         pluginKeys,
	}
	if c.Request != nil && c.Request.URL != nil {
		// An ordinary request may run on a plugin channel whose relay routes serve its path.
		filter.RequestPath = c.Request.URL.Path
	}
	GetChannelConstraints(c).AddFilter(filter)
}

type RetryParam struct {
	Ctx         *gin.Context
	ModelName   string
	RequestPath string
	Retry       *int
}

func (p *RetryParam) GetRetry() int {
	if p.Retry == nil {
		return 0
	}
	return *p.Retry
}

func (p *RetryParam) SetRetry(retry int) {
	p.Retry = &retry
}

func (p *RetryParam) IncreaseRetry() {
	if p.Retry == nil {
		p.Retry = new(int)
	}
	*p.Retry++
}

func pinnedTaskPluginIdentities(c *gin.Context, expected string) ([]int, []string) {
	if c == nil || expected == "" {
		return nil, nil
	}
	// A shared host endpoint and a native route several plugins declare both
	// admit a channel of any candidate.
	var generation *jsplugin.RoutingGeneration
	var candidates []*jsplugin.LoadedPlugin
	if value, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint); exists {
		if pinned, ok := value.(jsplugin.PinnedEndpoint); ok {
			if channelTypes, keys := taskPluginEndpointIdentities(pinned, expected); len(keys) > 0 {
				return channelTypes, keys
			}
		}
	} else if value, exists := c.Get(jsplugin.ContextKeyPinnedRoute); exists {
		if pinned, ok := value.(jsplugin.PinnedRoute); ok && len(pinned.Candidates) > 1 {
			generation = pinned.Generation
			for _, candidate := range pinned.Candidates {
				candidates = append(candidates, candidate.Plugin)
			}
		}
	}
	if generation != nil {
		if channelTypes, keys := taskPluginIdentities(generation, candidates, expected); len(keys) > 0 {
			return channelTypes, keys
		}
	}
	value, exists := c.Get(jsplugin.ContextKeyPinnedPlugin)
	pinned, ok := value.(jsplugin.PinnedPlugin)
	if !exists || !ok || pinned.Generation == nil || pinned.Plugin == nil || pinned.Plugin.Meta.Key != expected {
		return nil, nil
	}
	return taskPluginIdentities(pinned.Generation, []*jsplugin.LoadedPlugin{pinned.Plugin}, expected)
}

// ChannelSelectError explains why SelectChannelForRequest found no channel.
// Callers render it for their transport: the HTTP distributor localizes
// MessageID with its own helpers and the Responses WebSocket relay wraps it in
// a NewAPIError. Message is set instead of MessageID when the text is a fixed
// error code that clients match on.
type ChannelSelectError struct {
	StatusCode int
	Code       types.ErrorCode
	MessageID  string
	Params     map[string]any
	Message    string
	// FilterKind and Channel identify a candidate rejected by request filters.
	FilterKind dto.ChannelFilterKind
	Channel    *model.Channel
	// NoAvailableChannel marks the "no channel for this model"
	// outcome so the distributor can name the claiming task plugin.
	NoAvailableChannel bool
}

// SelectChannelForRequest applies shared eligibility, ordering and immediate admission.
func SelectChannelForRequest(c *gin.Context, modelName string, retry *RetryParam) (*model.Channel, *ChannelSelectError) {
	constraints := GetChannelConstraints(c)
	pin, pinned, overridden := constraints.ResolvedPin()
	for _, lost := range overridden {
		logger.LogWarn(c, common.LogText(
			"channel pin overridden: winning_source=%s winning_channel_id=%d overridden_source=%s overridden_channel_id=%d",
			pin.Source, pin.ChannelId, lost.Source, lost.ChannelId,
		))
	}
	if pinned && pin.Source == dto.PinSourceOriginTask {
		return selectOriginTaskChannel(c, pin.ChannelId, modelName)
	}
	if err := LoadTokenRouteConstraints(c); err != nil {
		return nil, tokenRouteScopeFailure(c, err)
	}
	if !pinned && !HasPrivateTokenRoute(c) && (retry == nil || retry.GetRetry() == 0) {
		if row, selectErr, decided := selectAffinityChannel(c, modelName); decided {
			return row, selectErr
		}
	}
	channel, err := selectQualifiedChannel(c, modelName)
	if err != nil {
		setRouteRetryAfter(c, err, retry == nil || retry.GetRetry() == 0)
		// The selector already logged this scan, including failures before the relay starts.
		return nil, classifyRouteLoadFailure(err)
	}
	if channel == nil && pinned {
		channel, err := model.CacheGetChannel(pin.ChannelId)
		if err != nil {
			return nil, &ChannelSelectError{StatusCode: http.StatusBadRequest, MessageID: i18n.MsgDistributorInvalidChannelId}
		}
		if channel.Status != common.ChannelStatusEnabled {
			return nil, &ChannelSelectError{StatusCode: http.StatusForbidden, MessageID: i18n.MsgDistributorChannelDisabled}
		}
		if ok, kind := model.ChannelSatisfiesFilters(channel, modelName, constraints.Filters); !ok {
			return nil, &ChannelSelectError{
				StatusCode: http.StatusBadRequest, Code: types.ErrorCode(kind), MessageID: i18n.MsgDistributorNoAvailableChannel,
				Params:     map[string]any{"Model": modelName},
				FilterKind: kind, Channel: channel,
			}
		}
		return nil, &ChannelSelectError{StatusCode: http.StatusForbidden, Code: types.ErrorCodeAccessDenied, Message: "Pinned channel is no longer available for this key and model"}
	}
	if channel == nil {
		return nil, &ChannelSelectError{
			StatusCode: http.StatusServiceUnavailable, Code: types.ErrorCodeModelNotFound, MessageID: i18n.MsgDistributorNoAvailableChannel,
			Params: map[string]any{"Model": modelName}, NoAvailableChannel: true,
		}
	}
	return channel, nil
}

// AppendUsedChannel records an attempted channel in the request's channel
// trail, which the retry log and the consume log's admin_info both read.
func AppendUsedChannel(c *gin.Context, channelID int) {
	c.Set("use_channel", append(c.GetStringSlice("use_channel"), fmt.Sprintf("%d", channelID)))
}
