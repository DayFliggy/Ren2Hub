package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetLogGroups(c *gin.Context) {
	var query struct {
		Search string `form:"search" binding:"max=64"`
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid group search"})
		return
	}
	userID := c.GetInt("id")
	if userID <= 0 {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	if c.FullPath() == "/api/log/groups" {
		userID = 0 // This route requires AdminAuth before reaching the handler.
	}
	groups, err := model.ListLogGroups(c.Request.Context(), userID, query.Search)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "log groups unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": groups})
}
