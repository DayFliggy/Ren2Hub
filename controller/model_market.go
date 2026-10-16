package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetUserModelMarket(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	market, err := service.BuildModelMarket(c.Request.Context(), c.GetInt("id"))
	if err != nil {
		_ = c.Error(err)
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Model market is unavailable"))
		return
	}
	common.ApiSuccess(c, market)
}
