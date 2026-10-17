package service

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

type TokenRouteChannel struct {
	ID     int      `json:"id"`
	Name   string   `json:"name"`
	Type   int      `json:"type"`
	Status int      `json:"status"`
	Models []string `json:"models"`
}

// TokenModelLimitAllows is shared by HTTP, WebSocket and the route editor.
func TokenModelLimitAllows(limit map[string]bool, name string) bool {
	return limit[name] || limit[ratio_setting.FormatMatchingModelName(name)] || limit[ratio_setting.RoutingMatchModelName(name)]
}

func TokenRouteChannels(ctx context.Context, userID, tokenID int) ([]TokenRouteChannel, error) {
	token, err := model.GetTokenByIds(tokenID, userID)
	if err != nil {
		return nil, err
	}
	groups, err := tokenRouteGroups(userID, token)
	if err != nil {
		return nil, err
	}
	channels, abilities, err := model.PublicRouteChannels(ctx, groups)
	if err != nil {
		return nil, err
	}
	models := make(map[int]map[string]bool)
	limit := token.GetModelLimitsMap()
	// Index concrete allowed requests in the same direction as dispatch:
	// request -> exact/normalized ability. Normalizing both sides would also
	// admit a different concrete model in the same family.
	requestsByAbility := make(map[string][]string)
	if token.ModelLimitsEnabled {
		for name := range limit {
			requestsByAbility[name] = append(requestsByAbility[name], name)
			if normalized := ratio_setting.RoutingMatchModelName(name); normalized != name {
				requestsByAbility[normalized] = append(requestsByAbility[normalized], name)
			}
		}
	}
	for _, ability := range abilities {
		names := []string{ability.Model}
		if token.ModelLimitsEnabled {
			names = requestsByAbility[ability.Model]
			if TokenModelLimitAllows(limit, ability.Model) {
				// A declared wildcard may authorize the ability name itself.
				names = append(slices.Clone(names), ability.Model)
			}
		}
		if models[ability.ChannelId] == nil {
			models[ability.ChannelId] = map[string]bool{}
		}
		for _, name := range names {
			models[ability.ChannelId][name] = true
		}
	}
	result := []TokenRouteChannel{}
	for _, channel := range channels {
		if channel.Type == constant.ChannelTypeCodex || len(models[channel.Id]) == 0 {
			continue
		}
		names := make([]string, 0, len(models[channel.Id]))
		for name := range models[channel.Id] {
			names = append(names, name)
		}
		sort.Strings(names)
		result = append(result, TokenRouteChannel{ID: channel.Id, Name: channel.Name, Type: channel.Type, Status: channel.Status, Models: names})
	}
	return result, nil
}

func tokenRouteGroups(userID int, token *model.Token) ([]string, error) {
	user, err := model.GetUserById(userID, false)
	if err != nil {
		return nil, err
	}
	userGroup := user.Group
	group := token.Group
	if group == "" {
		group = userGroup
	}
	groups := []string{}
	if group == "auto" {
		if !GroupInUserUsableGroups(userGroup, group) {
			return []string{}, nil
		}
		if token.AutoGroups == "" {
			groups = GetUserAutoGroup(userGroup)
		} else {
			configured, err := token.GetAutoGroups()
			if err != nil {
				return nil, err
			}
			groups = FilterUserTokenAutoGroups(userGroup, configured)
		}
	} else if IsUserSelectableGroup(userGroup, group) {
		groups = []string{group}
	}
	return groups, nil
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
		allowed[channel.ID] = true
	}
	return model.SaveTokenRouting(ctx, userID, tokenID, input, allowed)
}

// LoadTokenRouteConstraints runs once per authenticated request (including each
// response.create). A saved empty manual route is a deny-all filter.
func LoadTokenRouteConstraints(c *gin.Context) error {
	if c == nil {
		return nil
	}
	if _, loaded := c.Get("token_route_loaded"); loaded {
		return nil
	}
	tokenID := common.GetContextKeyInt(c, constant.ContextKeyTokenId)
	if tokenID <= 0 {
		return nil
	}
	view, err := model.ReadTokenRouting(c.Request.Context(), tokenID)
	if err != nil {
		return err
	}
	c.Set("token_route_loaded", true)
	c.Set("token_route_version", view.Version)
	if view.Mode != model.TokenRouteManual {
		return nil
	}
	// Continuations are already authorized against the original task. Editing
	// a preference must not move or strand work that was previously submitted.
	if pin, found, _ := GetChannelConstraints(c).ResolvedPin(); found && pin.Source == dto.PinSourceOriginTask {
		return nil
	}
	ids := make([]int, 0, len(view.Entries))
	for _, entry := range view.Entries {
		if entry.Enabled {
			ids = append(ids, entry.ChannelID)
		}
	}
	GetChannelConstraints(c).AddFilter(dto.ChannelFilter{Kind: dto.FilterTokenRoute, ChannelIDs: ids})
	return nil
}

func HasPrivateTokenRoute(c *gin.Context) bool {
	for _, filter := range GetChannelConstraints(c).Filters {
		if filter.Kind == dto.FilterTokenRoute {
			return true
		}
	}
	return false
}

// Retrying must never reselect an earlier private entry, even when auto-group
// retry resets the upstream retry counter.
func tokenRouteAttemptFilters(c *gin.Context, filters []dto.ChannelFilter) []dto.ChannelFilter {
	if !HasPrivateTokenRoute(c) {
		return filters
	}
	used := map[int]bool{}
	for _, value := range c.GetStringSlice("use_channel") {
		if id, err := strconv.Atoi(value); err == nil {
			used[id] = true
		}
	}
	result := append([]dto.ChannelFilter(nil), filters...)
	for index, filter := range result {
		if filter.Kind != dto.FilterTokenRoute {
			continue
		}
		ids := make([]int, 0, len(filter.ChannelIDs))
		for _, id := range filter.ChannelIDs {
			if !used[id] {
				ids = append(ids, id)
			}
		}
		result[index].ChannelIDs = ids
	}
	return result
}

func CheckLockedTokenRoute(c *gin.Context, channelID int, modelName, lockedGroup string) error {
	if err := LoadTokenRouteConstraints(c); err != nil {
		return err
	}
	for _, filter := range GetChannelConstraints(c).Filters {
		if filter.Kind != dto.FilterTokenRoute {
			continue
		}
		if !slices.Contains(filter.ChannelIDs, channelID) {
			return fmt.Errorf("the connection channel is no longer in the token's private route; reconnect required: %w", model.ErrTokenRouteForbidden)
		}
		group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
		if group == "auto" {
			if !slices.Contains(GetRequestAutoGroups(c, common.GetContextKeyString(c, constant.ContextKeyUserGroup)), lockedGroup) {
				return fmt.Errorf("the connection group is no longer allowed: %w", model.ErrTokenRouteForbidden)
			}
			group = lockedGroup
		}
		_, allowed, err := privatePinnedGroup(c, channelID, modelName, group)
		if err != nil {
			return err
		}
		if !allowed {
			return fmt.Errorf("the connection channel is no longer allowed for this group and model: %w", model.ErrTokenRouteForbidden)
		}
		if common.GetContextKeyString(c, constant.ContextKeyUsingGroup) == "auto" {
			common.SetContextKey(c, constant.ContextKeyAutoGroup, group)
		}
		return nil
	}
	return nil
}

func privatePinnedGroup(c *gin.Context, channelID int, modelName, group string) (string, bool, error) {
	if common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		limits, ok := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyTokenModelLimit)
		if !ok || !TokenModelLimitAllows(limits, modelName) {
			return "", false, nil
		}
	}
	groups := []string{group}
	if group == "auto" {
		groups = GetRequestAutoGroups(c, common.GetContextKeyString(c, constant.ContextKeyUserGroup))
	}
	for _, candidate := range groups {
		enabled, err := model.CheckChannelEnabledForGroupModel(candidate, modelName, channelID)
		if err != nil {
			return "", false, err
		}
		if enabled {
			return candidate, true, nil
		}
	}
	return "", false, nil
}
