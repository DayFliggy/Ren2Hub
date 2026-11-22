package controller

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 非浏览器客户端可以缺省 Fetch Metadata；浏览器的跨来源 GET 不能建立登录会话。
func allowLegacyLoginBrowserOrigin(c *gin.Context) bool {
	values := c.Request.Header.Values("Sec-Fetch-Site")
	if len(values) == 0 || (len(values) == 1 && values[0] == "same-origin") {
		return true
	}
	c.JSON(http.StatusForbidden, gin.H{
		"success": false,
		"message": "Cross-origin login request rejected",
	})
	return false
}
