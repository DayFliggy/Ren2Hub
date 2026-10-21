package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func acquireChannelManagement(c *gin.Context, ch *model.Channel) (*service.ConcurrencyPermit, bool) {
	permit, err := service.AcquireChannelConcurrency(c.Request.Context(), ch)
	if err == nil {
		return permit, true
	}
	status := http.StatusServiceUnavailable
	if errors.Is(err, service.ErrConcurrencyFull) {
		status = http.StatusTooManyRequests
	}
	c.JSON(status, gin.H{"success": false, "message": err.Error()})
	return nil, false
}

// attachChannelInFlight 用实时准入状态填充管理端的 in_flight 投影。
// 只有管理端渠道响应调用它，数据库查询不再隐式填充。
func attachChannelInFlight(channels []*model.Channel) {
	for _, channel := range channels {
		channel.InFlight = service.ChannelInFlight(channel.Id)
	}
}

func rejectBusyChannelManagement(c *gin.Context, err error) bool {
	if !errors.Is(err, service.ErrConcurrencyFull) {
		return false
	}
	c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "message": err.Error()})
	return true
}
