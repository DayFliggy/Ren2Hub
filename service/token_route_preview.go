package service

import (
	"context"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"strings"
)

// 自动模式的加权随机按每个请求的 request id 重新计算，预览给出的只是其中一种排列。
const (
	TokenRouteOrderingManual             = "manual"
	TokenRouteOrderingWeightedPerRequest = "weighted_random_per_request"
)

type TokenRoutePreviewQuery struct {
	Model  string
	Path   string
	Stream bool
}

type TokenRoutePreviewEntry struct {
	ChannelID    int    `json:"channel_id"`
	Name         string `json:"name,omitempty"`
	Eligible     bool   `json:"eligible"`
	Reason       string `json:"reason"`
	ChannelRatio string `json:"channel_ratio"`
}
type TokenRoutePreview struct {
	Mode     string                   `json:"mode"`
	Version  int64                    `json:"version"`
	Model    string                   `json:"model"`
	Path     string                   `json:"path"`
	Stream   bool                     `json:"stream"`
	Ordering string                   `json:"ordering"`
	Entries  []TokenRoutePreviewEntry `json:"entries"`
}

func PreviewTokenRouting(ctx context.Context, userID, tokenID int, query TokenRoutePreviewQuery) (TokenRoutePreview, error) {
	modelName, requestPath := query.Model, query.Path
	out := TokenRoutePreview{Model: modelName, Path: requestPath, Stream: query.Stream, Entries: []TokenRoutePreviewEntry{}}
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
			// 满载只给出原因，不返回其他用户的实时占用数。
			if item.Reason == "" && row.MaxConcurrency > 0 && ChannelInFlight(id) >= row.MaxConcurrency {
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
		out.Ordering = TokenRouteOrderingManual
		for _, entry := range route.Entries {
			item := project(entry.ChannelID)
			if !entry.Enabled {
				item.Eligible = false
				item.Reason = "entry_disabled"
			}
			out.Entries = append(out.Entries, item)
		}
		return out, nil
	}
	out.Ordering = TokenRouteOrderingWeightedPerRequest
	orderAutomaticRoutes(rows, "preview", modelName, relayconstant.Path2RelayMode(requestPath), query.Stream)
	for _, row := range rows {
		item := project(row.Id)
		if item.Reason != "model_not_allowed" && item.Reason != "model_unavailable" {
			out.Entries = append(out.Entries, item)
		}
	}
	return out, nil
}
