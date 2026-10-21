package service

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// TokenModelLimitAllows is shared by HTTP, WebSocket and the route editor.
func TokenModelLimitAllows(limit map[string]bool, name string) bool {
	return limit[name] || limit[ratio_setting.FormatMatchingModelName(name)] || limit[ratio_setting.RoutingMatchModelName(name)]
}

func TokenRouteChannels(ctx context.Context, userID, tokenID int) ([]RouteChannel, error) {
	return catalogRouteChannels(ctx, userID, tokenID, false)
}

func GetTokenRouting(ctx context.Context, userID, tokenID int) (model.TokenRouting, error) {
	if _, err := model.GetTokenByIds(tokenID, userID); err != nil {
		return model.TokenRouting{}, err
	}
	return model.ReadTokenRouting(ctx, tokenID)
}

func PutTokenRouting(ctx context.Context, userID, tokenID int, input model.TokenRouting) (model.TokenRouting, error) {
	if err := model.ValidateTokenRouting(input); err != nil {
		return model.TokenRouting{}, err
	}
	channels, err := TokenRouteChannels(ctx, userID, tokenID)
	if err != nil {
		return model.TokenRouting{}, err
	}
	allowed := make(map[int]bool, len(channels))
	for _, channel := range channels {
		allowed[channel.ID] = channel.FilterReason != routeReasonChannelUnavailable
	}
	return model.SaveTokenRouting(ctx, userID, tokenID, input, allowed)
}

// routeScope 是一次选路用到的 Key 侧数据：路由配置和模型限制。HTTP 请求在
// LoadTokenRouteConstraints 里读一次，首轮、亲和、重试都复用同一份快照。
type routeScope struct {
	route         model.TokenRouting
	limitsEnabled bool
	limits        map[string]bool
}

func autoRouteScope() routeScope {
	return routeScope{route: model.TokenRouting{TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteAuto}}}
}

func newRouteScope(route model.TokenRouting, token *model.Token) routeScope {
	return routeScope{route: route, limitsEnabled: token.ModelLimitsEnabled, limits: token.GetModelLimitsMap()}
}

func (s routeScope) allowsModel(name string) bool {
	return !s.limitsEnabled || TokenModelLimitAllows(s.limits, name)
}

// loadRouteScope 从库里读 Key 的路由和模型限制；没有 Key 的调用按自动路由、不限模型处理。
func loadRouteScope(ctx context.Context, tokenID int) (routeScope, error) {
	if tokenID <= 0 {
		return autoRouteScope(), nil
	}
	route, err := model.ReadTokenRouting(ctx, tokenID)
	if err != nil {
		return routeScope{}, err
	}
	token, err := model.GetTokenById(tokenID)
	if err != nil {
		return routeScope{}, err
	}
	return newRouteScope(route, token), nil
}

// LoadTokenRouteConstraints runs once per authenticated request (including each
// response.create). A saved empty manual route is a deny-all filter.
func LoadTokenRouteConstraints(c *gin.Context) error {
	if c == nil {
		return nil
	}
	if _, loaded := c.Get(ginKeyTokenRouteScope); loaded {
		return nil
	}
	tokenID := common.GetContextKeyInt(c, constant.ContextKeyTokenId)
	if tokenID <= 0 {
		return nil
	}
	scope, err := loadRouteScope(c.Request.Context(), tokenID)
	if err != nil {
		return err
	}
	c.Set(ginKeyTokenRouteScope, scope)
	c.Set("token_route_version", scope.route.Version)
	if scope.route.Mode != model.TokenRouteManual {
		return nil
	}
	ids := make([]int, 0, len(scope.route.Entries))
	for _, entry := range scope.route.Entries {
		if entry.Enabled {
			ids = append(ids, entry.ChannelID)
		}
	}
	GetChannelConstraints(c).AddFilter(dto.ChannelFilter{Kind: dto.FilterTokenRoute, ChannelIDs: ids})
	return nil
}

const ginKeyTokenRouteScope = "token_route_scope"

// requestRouteScope 返回本请求已加载的快照；没有 Key 的调用按自动路由处理。
func requestRouteScope(c *gin.Context) (routeScope, error) {
	if err := LoadTokenRouteConstraints(c); err != nil {
		return routeScope{}, err
	}
	value, loaded := c.Get(ginKeyTokenRouteScope)
	if !loaded {
		return autoRouteScope(), nil
	}
	return value.(routeScope), nil
}

func HasPrivateTokenRoute(c *gin.Context) bool {
	for _, filter := range GetChannelConstraints(c).Filters {
		if filter.Kind == dto.FilterTokenRoute {
			return true
		}
	}
	return false
}

// CheckLockedTokenRoute 服务长连接：每次都从库重读路由和模型限制，会话中途的变更立即生效。
func CheckLockedTokenRoute(c *gin.Context, channelID int, modelName string) error {
	scope, err := loadRouteScope(c.Request.Context(), common.GetContextKeyInt(c, constant.ContextKeyTokenId))
	if err != nil {
		return err
	}
	rows, err := routeCandidates(c.Request.Context(), scope, modelName, GetChannelConstraints(c).Filters)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Id == channelID {
			return nil
		}
	}
	return fmt.Errorf("connection channel is no longer available for this key; reconnect required: %w", model.ErrTokenRouteForbidden)
}
