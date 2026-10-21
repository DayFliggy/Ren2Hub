package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
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

// publishChannelPriorityChanges 在优先级已提交后发布路由变更。发布失败只记日志：
// 写入已经生效，未确认的发布会让准入在下一次读取时重新读库。
func publishChannelPriorityChanges(changes []model.ChannelPriorityChange) {
	if model.ChannelRoutingChanged == nil {
		return
	}
	for _, change := range changes {
		if err := model.ChannelRoutingChanged(change.ID); err != nil {
			common.SysError(common.LogText("failed to publish channel routing after priority reorder: channel_id=%d, error=%v", change.ID, err))
		}
	}
}

func rejectBusyChannelManagement(c *gin.Context, err error) bool {
	if !errors.Is(err, service.ErrConcurrencyFull) {
		return false
	}
	c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "message": err.Error()})
	return true
}
