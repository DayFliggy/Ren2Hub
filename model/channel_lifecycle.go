package model

import (
	"github.com/QuantumNous/new-api/common"
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

// EffectiveStatus is the status shown to administrators and used for counts.
// A retired row keeps its stored status for history but projects as manually
// disabled, because it can no longer serve or be re-enabled.
func (channel *Channel) EffectiveStatus() int {
	if constant.IsRetiredChannelType(channel.Type) {
		return common.ChannelStatusManuallyDisabled
	}
	return channel.Status
}

// IsServable reports whether the channel may count as enabled: its status is
// enabled and its type is not retired. WhereChannelServable is the SQL form.
func (channel *Channel) IsServable() bool {
	return channel.EffectiveStatus() == common.ChannelStatusEnabled
}

// WhereChannelServable restricts a query that reads the channels table (directly
// or through a join) to rows for which IsServable is true.
func WhereChannelServable(query *gorm.DB) *gorm.DB {
	query = query.Where("channels.status = ?", common.ChannelStatusEnabled)
	return excludeChannelTypes(query, "channels.type", constant.RetiredChannelTypes())
}

// WhereChannelUnservable is the exact complement of WhereChannelServable.
// Treat a nullable legacy status as unavailable; an empty retired list leaves
// only the status test because GORM's IN (NULL) branch never matches.
func WhereChannelUnservable(query *gorm.DB) *gorm.DB {
	return query.Where("(channels.status IS NULL OR channels.status <> ? OR channels.type IN ?)", common.ChannelStatusEnabled, constant.RetiredChannelTypes())
}

// excludeChannelTypes adds no condition for an empty list. GORM renders an empty
// NOT IN as NOT IN (NULL), which matches no rows and would hide every channel.
func excludeChannelTypes(query *gorm.DB, column string, channelTypes []int) *gorm.DB {
	if len(channelTypes) == 0 {
		return query
	}
	return query.Where(column+" NOT IN ?", channelTypes)
}

// Historical abilities must not make a retired channel visible in a live catalog.
func liveChannelIDs() *gorm.DB {
	return excludeChannelTypes(DB.Model(&Channel{}).Select("id"), "type", constant.RetiredChannelTypes())
}
