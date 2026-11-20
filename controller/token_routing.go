package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

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
	case errors.Is(err, service.ErrPreviewPathUnsupported):
		status, code, message = http.StatusBadRequest, "preview_path_unsupported", "This path cannot be previewed without decoding a request body"
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
	common.ApiSuccess(c, view)
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
	common.ApiSuccess(c, view)
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
		}
	}
	return nil
}

func GetTokenRoutingPreview(c *gin.Context) {
	id, ok := tokenRoutingID(c)
	if !ok {
		return
	}
	query := service.TokenRoutePreviewQuery{Model: c.Query("model"), Path: c.Query("path"), Transport: c.Query("transport")}
	if value := c.Query("stream"); value != "" {
		stream, err := strconv.ParseBool(value)
		if err != nil {
			tokenRoutingError(c, model.ErrTokenRouteInvalid)
			return
		}
		query.Stream = stream
	}
	preview, err := service.PreviewTokenRouting(c.Request.Context(), c.GetInt("id"), id, query)
	if err != nil {
		tokenRoutingError(c, err)
		return
	}
	common.ApiSuccess(c, preview)
}
