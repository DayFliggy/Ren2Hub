package service

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// ShouldCopyUpstreamHeader 保留本地 request ID，并隔离凭据、Cookie 与逐跳头。
// source 包含 Connection 声明的动态逐跳头；所有响应复制入口传完整头集合。
func ShouldCopyUpstreamHeader(c *gin.Context, name string, values []string, source ...http.Header) bool {
	if strings.EqualFold(name, common.RequestIdKey) {
		if c != nil && len(values) > 0 {
			c.Set(common.UpstreamRequestIdKey, values[0])
		}
		return false
	}
	if len(values) == 0 {
		return false
	}
	switch strings.ToLower(name) {
	case "content-length", "set-cookie", "set-cookie2", "cookie", "authorization", "proxy-authorization", "proxy-authenticate", "www-authenticate", "x-api-key", "x-goog-api-key", "sec-websocket-protocol", "connection", "proxy-connection", "keep-alive", "te", "trailer", "transfer-encoding", "upgrade":
		return false
	}
	for _, headers := range source {
		for key, lines := range headers {
			if !strings.EqualFold(key, "Connection") {
				continue
			}
			for _, line := range lines {
				for _, token := range strings.Split(line, ",") {
					if strings.EqualFold(strings.TrimSpace(token), name) {
						return false
					}
				}
			}
		}
	}
	return true
}
