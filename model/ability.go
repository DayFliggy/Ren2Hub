package model

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Ability struct {
	Model     string  `json:"model" gorm:"type:varchar(255);primaryKey;autoIncrement:false"`
	ChannelId int     `json:"channel_id" gorm:"primaryKey;autoIncrement:false;index"`
	Enabled   bool    `json:"enabled"`
	Priority  *int64  `json:"priority" gorm:"bigint;default:0;index"`
	Weight    uint    `json:"weight" gorm:"default:0;index"`
	Tag       *string `json:"tag" gorm:"index"`
}

type AbilityWithChannel struct {
	Ability
	ChannelType int `json:"channel_type"`
}

func GetAllEnableAbilityWithChannels() ([]AbilityWithChannel, error) {
	var abilities []AbilityWithChannel
	err := DB.Table("abilities").
		Select("abilities.*, channels.type as channel_type").
		Joins("left join channels on abilities.channel_id = channels.id").
		Where("abilities.enabled = ?", true).
		Scan(&abilities).Error
	return abilities, err
}

func GetEnabledModels() []string {
	var models []string
	// Find distinct models
	DB.Table("abilities").Where("enabled = ?", true).Distinct("model").Pluck("model", &models)
	return models
}

func GetAllEnableAbilities() []Ability {
	var abilities []Ability
	DB.Find(&abilities, "enabled = ?", true)
	return abilities
}

func (channel *Channel) AddAbilities(tx *gorm.DB) error {
	if tx == nil {
		tx = DB
	}
	seen := make(map[string]bool)
	abilities := make([]Ability, 0)
	for _, name := range strings.Split(channel.Models, ",") {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		abilities = append(abilities, Ability{
			Model: name, ChannelId: channel.Id,
			Enabled:  channel.Status == common.ChannelStatusEnabled,
			Priority: channel.Priority, Weight: uint(channel.GetWeight()), Tag: channel.Tag,
		})
	}
	for _, chunk := range lo.Chunk(abilities, 50) {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&chunk).Error; err != nil {
			return err
		}
	}
	return nil
}

func (channel *Channel) DeleteAbilities() error {
	err := DB.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error
	if err == nil {
		NotifyChannelCapabilityChanged(channel.Id)
	}
	return err
}

// UpdateAbilities updates abilities of this channel.
// Make sure the channel is completed before calling this function.
func (channel *Channel) UpdateAbilities(tx *gorm.DB) error {
	if tx == nil {
		err := DB.Transaction(func(tx *gorm.DB) error { return channel.UpdateAbilities(tx) })
		if err == nil {
			NotifyChannelCapabilityChanged(channel.Id)
		}
		return err
	}
	if err := tx.Where("channel_id = ?", channel.Id).Delete(&Ability{}).Error; err != nil {
		return err
	}
	return channel.AddAbilities(tx)
}

func UpdateAbilityStatus(channelId int, status bool) error {
	err := DB.Model(&Ability{}).Where("channel_id = ?", channelId).Select("enabled").Update("enabled", status).Error
	if err == nil {
		NotifyChannelCapabilityChanged(channelId)
	}
	return err
}

func UpdateAbilityStatusByTag(tag string, status bool) error {
	var channelIDs []int
	if err := DB.Model(&Ability{}).Where("tag = ?", tag).Distinct("channel_id").Pluck("channel_id", &channelIDs).Error; err != nil {
		return err
	}
	err := DB.Model(&Ability{}).Where("tag = ?", tag).Select("enabled").Update("enabled", status).Error
	if err == nil {
		for _, channelID := range channelIDs {
			NotifyChannelCapabilityChanged(channelID)
		}
	}
	return err
}

func UpdateAbilityByTag(tag string, newTag *string, priority *int64, weight *uint) error {
	var channelIDs []int
	if err := DB.Model(&Ability{}).Where("tag = ?", tag).Distinct("channel_id").Pluck("channel_id", &channelIDs).Error; err != nil {
		return err
	}
	ability := Ability{}
	if newTag != nil {
		ability.Tag = newTag
	}
	if priority != nil {
		ability.Priority = priority
	}
	if weight != nil {
		ability.Weight = *weight
	}
	err := DB.Model(&Ability{}).Where("tag = ?", tag).Updates(ability).Error
	if err == nil {
		for _, channelID := range channelIDs {
			NotifyChannelCapabilityChanged(channelID)
		}
	}
	return err
}

var fixLock = sync.Mutex{}

func FixAbility() (int, int, error) {
	lock := fixLock.TryLock()
	if !lock {
		return 0, 0, errors.New("已经有一个修复任务在运行中，请稍后再试")
	}
	defer fixLock.Unlock()

	// truncate abilities table
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		err := DB.Exec("DELETE FROM abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	} else {
		err := DB.Exec("TRUNCATE TABLE abilities").Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Truncate abilities failed: %s", err.Error()))
			return 0, 0, err
		}
	}
	var channels []*Channel
	// Find all channels
	err := DB.Model(&Channel{}).Find(&channels).Error
	if err != nil {
		return 0, 0, err
	}
	if len(channels) == 0 {
		return 0, 0, nil
	}
	successCount := 0
	failCount := 0
	for _, chunk := range lo.Chunk(channels, 50) {
		ids := lo.Map(chunk, func(c *Channel, _ int) int { return c.Id })
		// Delete all abilities of this channel
		err = DB.Where("channel_id IN ?", ids).Delete(&Ability{}).Error
		if err != nil {
			common.SysLog(fmt.Sprintf("Delete abilities failed: %s", err.Error()))
			failCount += len(chunk)
			continue
		}
		// Then add new abilities
		for _, channel := range chunk {
			err = channel.AddAbilities(nil)
			if err != nil {
				common.SysLog(fmt.Sprintf("Add abilities for channel %d failed: %s", channel.Id, err.Error()))
				failCount++
			} else {
				successCount++
			}
		}
	}
	InitChannelCache()
	for _, channel := range channels {
		NotifyChannelCapabilityChanged(channel.Id)
	}
	return successCount, failCount, nil
}
