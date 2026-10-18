package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/pricesource"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

func GetPriceSources(c *gin.Context) { common.ApiSuccess(c, pricesource.Sources()) }

func fetchFixedPriceDrafts(c *gin.Context, ids []string) {
	if _, err := pricesource.Select(ids); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
		return
	}
	sources, err := service.FetchPriceDrafts(c.Request.Context(), ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"sources": sources})
}
