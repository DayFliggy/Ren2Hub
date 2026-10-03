package model

import (
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

// ValidateMutableChannels checks the entire selected batch before any writes.
// Retired rows remain readable and deletable, but cannot be repurposed with
// credentials whose provider semantics are no longer supported.
func ValidateMutableChannels(query *gorm.DB) error {
	var channelTypes []int
	if err := query.Session(&gorm.Session{}).Distinct("type").Pluck("type", &channelTypes).Error; err != nil {
		return err
	}
	for _, channelType := range channelTypes {
		if err := constant.ValidateChannelTypeLifecycle(channelType); err != nil {
			return err
		}
	}
	return nil
}

// Historical abilities must not make a retired channel visible in a live catalog.
func liveChannelIDs() *gorm.DB {
	return DB.Model(&Channel{}).Select("id").Where("type NOT IN ?", constant.RetiredChannelTypes())
}
