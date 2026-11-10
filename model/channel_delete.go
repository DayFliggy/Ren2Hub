package model

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DeleteDisabledChannelIDs 删除全部已禁用渠道并返回实际删除的 ID。上游 DeleteDisabledChannel
// 只返回行数，调用方另读的 ID 列表可能漏掉读后被禁用的渠道，删除后的并发与观察清理就会
// 残留；这里在同一事务内锁住并删除同一批行。同时删除这些渠道的能力行，与单个删除一致，
// 避免孤儿能力被复用同一 ID 的新渠道继承。
func DeleteDisabledChannelIDs() ([]int, error) {
	var ids []int
	err := DB.Transaction(func(tx *gorm.DB) error {
		disabled := []int{common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled}
		var candidates []int
		if err := tx.Model(&Channel{}).Where("status IN ?", disabled).Pluck("id", &candidates).Error; err != nil {
			return err
		}
		channels, err := lockChannels(tx, candidates, clause.LockingStrengthUpdate)
		if err != nil {
			return err
		}
		for _, channel := range channels {
			if slices.Contains(disabled, channel.Status) {
				ids = append(ids, channel.Id)
			}
		}
		for chunk := range slices.Chunk(ids, 200) {
			if err := tx.Where("id IN ?", chunk).Delete(&Channel{}).Error; err != nil {
				return err
			}
			if err := tx.Where("channel_id IN ?", chunk).Delete(&Ability{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}
