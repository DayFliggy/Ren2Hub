package service

import (
	"context"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"strings"
)

type TokenRoutePreviewEntry struct {
	ChannelID    int    `json:"channel_id"`
	Name         string `json:"name,omitempty"`
	Eligible     bool   `json:"eligible"`
	Reason       string `json:"reason"`
	ChannelRatio string `json:"channel_ratio"`
	InFlight     int    `json:"in_flight"`
}
type TokenRoutePreview struct {
	Mode    string                   `json:"mode"`
	Version int64                    `json:"version"`
	Model   string                   `json:"model"`
	Path    string                   `json:"path"`
	Entries []TokenRoutePreviewEntry `json:"entries"`
}

func PreviewTokenRouting(ctx context.Context, userID, tokenID int, modelName, requestPath string) (TokenRoutePreview, error) {
	out := TokenRoutePreview{Model: modelName, Path: requestPath, Entries: []TokenRoutePreviewEntry{}}
	if strings.TrimSpace(modelName) == "" || len(modelName) > 256 || !strings.HasPrefix(requestPath, "/") || strings.ContainsAny(requestPath, "?#\r\n") || len(requestPath) > 512 {
		return out, model.ErrTokenRouteInvalid
	}
	token, err := model.GetTokenByIds(tokenID, userID)
	if err != nil {
		return out, err
	}
	route, err := model.ReadTokenRouting(ctx, tokenID)
	if err != nil {
		return out, err
	}
	out.Mode, out.Version = route.Mode, route.Version
	rows, err := model.RouteChannelRows(ctx, "", nil)
	if err != nil {
		return out, err
	}
	ids := make([]int, 0, len(rows))
	byID := map[int]*model.Channel{}
	for _, row := range rows {
		ids = append(ids, row.Id)
		byID[row.Id] = row
	}
	abilities, err := model.EnabledRouteModels(ctx, ids)
	if err != nil {
		return out, err
	}
	filters := []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: requestPath}}
	project := func(id int) TokenRoutePreviewEntry {
		row := byID[id]
		item := TokenRoutePreviewEntry{ChannelID: id, Reason: routeCandidateReason(row, route, token, modelName, abilities[id], filters)}
		if row != nil {
			item.Name = row.Name
			item.ChannelRatio = row.ChannelRatio
			item.InFlight = ChannelInFlight(id)
			if item.Reason == "" && row.MaxConcurrency > 0 && item.InFlight >= row.MaxConcurrency {
				item.Reason = "channel_concurrency_full"
			}
		}
		item.Eligible = item.Reason == ""
		if item.Eligible {
			item.Reason = "eligible"
		}
		return item
	}
	if route.Mode == model.TokenRouteManual {
		for _, entry := range route.Entries {
			item := project(entry.ChannelID)
			if !entry.Enabled {
				item.Eligible = false
				item.Reason = "entry_disabled"
			}
			out.Entries = append(out.Entries, item)
		}
	} else {
		orderAutomaticRoutes(rows, "preview", modelName, relayconstant.Path2RelayMode(requestPath), false)
		for _, row := range rows {
			item := project(row.Id)
			if item.Reason != "model_not_allowed" && item.Reason != "model_unavailable" {
				out.Entries = append(out.Entries, item)
			}
		}
	}
	return out, nil
}
