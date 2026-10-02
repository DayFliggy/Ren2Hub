package controller

import (
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

func GetUserModelManifest(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	tokenID := 0
	if raw, exists := c.GetQuery("token_id"); exists {
		var err error
		tokenID, err = strconv.Atoi(raw)
		if err != nil || tokenID <= 0 {
			common.ApiError(c, errors.New("invalid token_id"))
			return
		}
	}
	manifest, err := service.BuildModelManifest(c.Request.Context(), service.ModelManifestRequest{UserID: c.GetInt("id"), TokenID: tokenID})
	if err != nil {
		_ = c.Error(err)
		common.ApiError(c, errors.New("model manifest is unavailable for this token"))
		return
	}
	common.ApiSuccess(c, manifest)
}

func GetTokenClientConfig(c *gin.Context) {
	tokenID, err := strconv.Atoi(c.Param("id"))
	if err != nil || tokenID <= 0 {
		common.ApiError(c, errors.New("invalid token_id"))
		return
	}
	manifest, err := service.BuildModelManifest(c.Request.Context(), service.ModelManifestRequest{UserID: c.GetInt("id"), TokenID: tokenID})
	if err != nil {
		_ = c.Error(err)
		common.ApiError(c, errors.New("model manifest is unavailable for this token"))
		return
	}
	configuration, err := service.ExportModelClientConfig(manifest, c.Query("format"), c.Query("model"), system_setting.ServerAddress)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	common.ApiSuccess(c, configuration)
}
