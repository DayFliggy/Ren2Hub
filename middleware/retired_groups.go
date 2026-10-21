package middleware

import (
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// RejectRetiredGroups prevents a legacy client from receiving success for an
// ignored routing/price restriction. Provider requests never use this middleware.
func RejectRetiredGroups() gin.HandlerFunc {
	return func(c *gin.Context) {
		retired := []string{"group", "auto_groups", "cross_group_retry"}
		reject := func() {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "message": "business groups have been retired; configure channel routing instead"})
		}
		for _, key := range retired {
			if _, present := c.Request.URL.Query()[key]; present {
				reject()
				return
			}
		}
		path := c.FullPath()
		checkBody := strings.HasPrefix(path, "/api/channel/") || path == "/api/token/" || path == "/api/user/" || path == "/api/user/self" || strings.HasSuffix(path, "/routing")
		if !checkBody || (c.Request.Method != http.MethodPost && c.Request.Method != http.MethodPut && c.Request.Method != http.MethodPatch) {
			c.Next()
			return
		}
		// 不看 Content-Type：管理端 handler 不论请求头都按 JSON 解析，缺少或伪造请求头
		// 不能绕过检查。只有空请求体没有可检查的字段。
		body, err := common.GetBodyStorage(c)
		if err != nil {
			status := http.StatusBadRequest
			if common.IsRequestBodyTooLargeError(err) {
				status = http.StatusRequestEntityTooLarge
			}
			c.AbortWithStatusJSON(status, gin.H{"success": false, "message": err.Error()})
			return
		}
		if body.Size() == 0 {
			c.Next()
			return
		}
		var value map[string]common.RawMessage
		err = common.DecodeJson(body, &value)
		if _, seekErr := body.Seek(0, io.SeekStart); seekErr != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"success": false, "message": seekErr.Error()})
			return
		}
		c.Request.Body = io.NopCloser(body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid JSON request"})
			return
		}
		for _, key := range retired {
			if _, present := value[key]; present {
				reject()
				return
			}
		}
		if channel, ok := value["channel"]; ok {
			var nested map[string]common.RawMessage
			if common.Unmarshal(channel, &nested) == nil {
				for _, key := range retired {
					if _, present := nested[key]; present {
						reject()
						return
					}
				}
			}
		}
		c.Next()
	}
}
