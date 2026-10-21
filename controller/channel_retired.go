package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/constant"
)

// rejectRetiredChannelType 是渠道写入和拉取上游模型共用的 type 57 退役拒绝，
// 保证管理员 key 不会因为任何一条路径被发往已退役的订阅账号上游。
func rejectRetiredChannelType(channelType int) error {
	if channelType == constant.ChannelTypeCodex {
		return errors.New("channel type 57 has been retired")
	}
	return nil
}
