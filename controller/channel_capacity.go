package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/service"
)

// attachChannelInFlight 用实时准入状态填充管理端的 in_flight 投影。
// 只有管理端渠道响应调用它，数据库查询不再隐式填充。
func attachChannelInFlight(channels []*model.Channel) {
	for _, channel := range channels {
		channel.InFlight = service.ChannelInFlight(channel.Id)
	}
}

// clearChannelProjections 丢弃客户端提交的 in_flight 和 lab 只读投影，避免写入路径误用。
func clearChannelProjections(channel *model.Channel) {
	channel.InFlight = 0
	channel.Lab = modellab.Classification{}
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
