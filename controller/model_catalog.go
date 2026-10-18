package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetModelCatalog(c *gin.Context) {
	tokenID := 0
	if value := c.Query("token_id"); value != "" {
		id, err := strconv.Atoi(value)
		if err != nil || id <= 0 {
			common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("invalid token_id"))
			return
		}
		tokenID = id
	}
	result, err := service.BuildModelCatalog(c.Request.Context(), c.GetInt("id"), tokenID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
