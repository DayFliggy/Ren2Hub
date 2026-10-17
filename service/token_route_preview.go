package service

import (
	"context"
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

type TokenRoutePreviewEntry struct {
	ChannelID int    `json:"channel_id"`
	Name      string `json:"name,omitempty"`
	Eligible  bool   `json:"eligible"`
	Reason    string `json:"reason"`
	Group     string `json:"group,omitempty"`
}

type TokenRoutePreview struct {
	Mode    string                   `json:"mode"`
	Version int64                    `json:"version"`
	Model   string                   `json:"model"`
	Path    string                   `json:"path"`
	Groups  []string                 `json:"groups"`
	Entries []TokenRoutePreviewEntry `json:"entries"`
}

// Preview only evaluates published ability and protocol filters. It does not
// run the weighted selector, acquire credentials or reserve/pay any quota.
func PreviewTokenRouting(ctx context.Context, userID, tokenID int, modelName, requestPath string) (TokenRoutePreview, error) {
	result := TokenRoutePreview{Model: modelName, Path: requestPath, Entries: []TokenRoutePreviewEntry{}}
	if strings.TrimSpace(modelName) == "" || len(modelName) > 256 || !strings.HasPrefix(requestPath, "/") || strings.ContainsAny(requestPath, "?#\r\n") || len(requestPath) > 512 {
		return result, model.ErrTokenRouteInvalid
	}
	token, err := model.GetTokenByIds(tokenID, userID)
	if err != nil {
		return result, err
	}
	routing, err := model.ReadTokenRouting(ctx, tokenID)
	if err != nil {
		return result, err
	}
	result.Mode, result.Version = routing.Mode, routing.Version
	result.Groups, err = tokenRouteGroups(userID, token)
	if err != nil {
		return result, err
	}
	channels, abilities, err := model.PublicRouteChannels(ctx, result.Groups)
	if err != nil {
		return result, err
	}
	public := make(map[int]model.Channel, len(channels))
	for _, channel := range channels {
		public[channel.Id] = channel
	}
	entries := routing.Entries
	if routing.Mode == model.TokenRouteAuto {
		entries = make([]model.TokenRouteEntry, 0, len(channels))
		for _, channel := range channels {
			entries = append(entries, model.TokenRouteEntry{ChannelID: channel.Id, Enabled: true})
		}
	}
	filters := []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: requestPath}}
	normalized := ratio_setting.RoutingMatchModelName(modelName)
	for _, entry := range entries {
		item := TokenRoutePreviewEntry{ChannelID: entry.ChannelID, Reason: "unavailable"}
		channel, visible := public[entry.ChannelID]
		if visible {
			item.Name = channel.Name
		}
		switch {
		case !entry.Enabled:
			item.Reason = "entry_disabled"
		case token.ModelLimitsEnabled && !TokenModelLimitAllows(token.GetModelLimitsMap(), modelName):
			item.Reason = "model_not_allowed"
		case !visible || channel.Status != common.ChannelStatusEnabled || channel.Type == constant.ChannelTypeCodex:
			// Do not expose metadata for a deleted, disabled or newly unauthorized channel.
		default:
			item.Reason = "group_model_unavailable"
			for _, group := range result.Groups {
				for _, ability := range abilities {
					if ability.ChannelId == entry.ChannelID && ability.Group == group && (ability.Model == modelName || ability.Model == normalized) {
						item.Group = group
						break
					}
				}
				if item.Group != "" {
					break
				}
			}
			if item.Group != "" {
				// The public projection intentionally omits settings. Load them internally
				// only to evaluate the same Advanced Custom protocol filter as dispatch.
				full, loadErr := model.GetChannelById(entry.ChannelID, false)
				if loadErr != nil {
					if !errors.Is(loadErr, gorm.ErrRecordNotFound) {
						return result, loadErr
					}
					item.Reason, item.Group = "unavailable", ""
				} else if allowed, _ := model.ChannelSatisfiesFilters(full, modelName, filters); !allowed {
					item.Reason = "protocol_unavailable"
				} else {
					item.Eligible, item.Reason = true, "eligible"
				}
			}
		}
		result.Entries = append(result.Entries, item)
	}
	return result, nil
}
