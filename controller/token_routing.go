package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	maxCandidateModelLimitsBytes = 4096
	maxCandidateModelLimitsCount = 200
)

type tokenRoutingView struct {
	model.TokenRouting
	RouteScope               model.TokenRouteScope `json:"route_scope"`
	EffectiveMode            string                `json:"effective_mode"`
	EnabledRouteChannelCount int                   `json:"enabled_route_channel_count"`
}

func buildTokenRoutingView(route model.TokenRouting) tokenRoutingView {
	return tokenRoutingView{
		TokenRouting:             route,
		RouteScope:               route.Scope(),
		EffectiveMode:            route.EffectiveMode(),
		EnabledRouteChannelCount: route.EnabledCount(),
	}
}

// Creation always starts with the platform-wide automatic route.
func validTokenCreationType(raw []byte) bool {
	if len(raw) == 0 {
		return true
	}
	var mode string
	return common.Unmarshal(raw, &mode) == nil && mode == model.TokenRouteAuto
}

func tokenRoutingID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		tokenRoutingError(c, model.ErrTokenRouteInvalid)
		return 0, false
	}
	return id, true
}

func tokenRoutingError(c *gin.Context, err error) {
	status, code, message := http.StatusInternalServerError, "ROUTING_INTERNAL_ERROR", "Unable to read or save token routing"
	switch {
	case errors.Is(err, model.ErrTokenRouteInvalid):
		status, code, message = http.StatusBadRequest, "ROUTING_INVALID", model.ErrTokenRouteInvalid.Error()
	case errors.Is(err, model.ErrTokenRouteForbidden):
		status, code, message = http.StatusForbidden, "ROUTING_CHANNEL_FORBIDDEN", model.ErrTokenRouteForbidden.Error()
	case errors.Is(err, model.ErrTokenRouteConflict):
		status, code, message = http.StatusConflict, "ROUTING_VERSION_CONFLICT", model.ErrTokenRouteConflict.Error()
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, code, message = http.StatusNotFound, "ROUTING_NOT_FOUND", "Token not found"
	default:
		common.SysError(common.LogText("token routing error: %s", err.Error()))
	}
	common.ApiErrorStatus(c, status, common.NewMessage(message), gin.H{"code": code})
}

func GetTokenRouting(c *gin.Context) {
	id, ok := tokenRoutingID(c)
	if !ok {
		return
	}
	view, err := service.GetTokenRouting(c.Request.Context(), c.GetInt("id"), id)
	if err != nil {
		tokenRoutingError(c, err)
		return
	}
	common.ApiSuccess(c, buildTokenRoutingView(view))
}

func PutTokenRouting(c *gin.Context) {
	id, ok := tokenRoutingID(c)
	if !ok {
		return
	}
	var input struct {
		SelectedOnly    bool                    `json:"selected_only"`
		Mode            string                  `json:"mode"`
		Version         *int64                  `json:"version"`
		MaxChannelRatio *string                 `json:"max_channel_ratio"`
		Entries         []model.TokenRouteEntry `json:"entries"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Version == nil {
		tokenRoutingError(c, model.ErrTokenRouteInvalid)
		return
	}
	view, err := service.PutTokenRouting(c.Request.Context(), c.GetInt("id"), id, model.TokenRouting{TokenRouteProfile: model.TokenRouteProfile{Mode: input.Mode, SelectedOnly: input.SelectedOnly, Version: *input.Version, MaxChannelRatio: input.MaxChannelRatio}, Entries: input.Entries})
	if err != nil {
		tokenRoutingError(c, err)
		return
	}
	common.ApiSuccess(c, buildTokenRoutingView(view))
}

func GetTokenRoutingChannels(c *gin.Context) {
	id, ok := tokenRoutingID(c)
	if !ok {
		return
	}
	channels, err := service.TokenRouteChannels(c.Request.Context(), c.GetInt("id"), id)
	if err != nil {
		tokenRoutingError(c, err)
		return
	}
	common.ApiSuccess(c, channels)
}

func GetTokenRoutingCandidateChannels(c *gin.Context) {
	rawLimits := c.Query("model_limits")
	if len(rawLimits) > maxCandidateModelLimitsBytes {
		tokenRoutingError(c, model.ErrTokenRouteInvalid)
		return
	}
	var modelLimits []string
	if strings.TrimSpace(rawLimits) != "" {
		for _, part := range strings.Split(rawLimits, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				continue
			}
			modelLimits = append(modelLimits, name)
			if len(modelLimits) > maxCandidateModelLimitsCount {
				tokenRoutingError(c, model.ErrTokenRouteInvalid)
				return
			}
		}
	}
	var maxRatio *string
	if rawRatio := strings.TrimSpace(c.Query("max_channel_ratio")); rawRatio != "" {
		maxRatio = &rawRatio
	}
	channels, err := service.TokenRouteCandidateChannels(c.Request.Context(), modelLimits, maxRatio)
	if err != nil {
		tokenRoutingError(c, err)
		return
	}
	common.ApiSuccess(c, channels)
}

func fillTokenRoutingModes(responses []*tokenResponse) error {
	ids := make([]int, 0, len(responses))
	for _, response := range responses {
		if response != nil {
			ids = append(ids, response.Id)
		}
	}
	modes, err := model.TokenRoutingModes(ids)
	if err != nil {
		return err
	}
	for _, response := range responses {
		if response == nil {
			continue
		}
		if summary, ok := modes[response.Id]; ok {
			response.Type = summary.Mode
			response.SelectedOnly = summary.SelectedOnly
			response.RouteChannelCount = summary.RouteChannelCount
			response.EnabledRouteChannelCount = summary.EnabledRouteChannelCount
			response.RouteScope = summary.RouteScope
			response.EffectiveMode = summary.EffectiveMode
		}
	}
	return nil
}
