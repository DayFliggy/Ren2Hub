package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func FetchUpstreamRatios(c *gin.Context) {
	var request struct {
		Sources []string `json:"sources"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("invalid price source request"))
		return
	}
	result, err := service.FetchModelPriceDrafts(c.Request.Context(), request.Sources)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
		return
	}
	// A failed source has status=error even when other sources are usable. The
	// envelope acknowledges completion; it does not imply all downloads worked.
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
