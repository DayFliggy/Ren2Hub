package model

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// failAbilityWrites makes every update of the abilities table fail.
func failAbilityWrites(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("test:fail_ability_write", func(tx *gorm.DB) {
		if strings.HasSuffix(tx.Statement.Table, "abilities") {
			_ = tx.AddError(errors.New("ability write failed"))
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove("test:fail_ability_write") })
}

func requireChannelAndAbilities(t *testing.T, channelID, status int) {
	t.Helper()
	var stored Channel
	require.NoError(t, DB.First(&stored, "id = ?", channelID).Error)
	require.Equal(t, status, stored.Status)
	var abilities []Ability
	require.NoError(t, DB.Where("channel_id = ?", channelID).Find(&abilities).Error)
	require.NotEmpty(t, abilities)
	for _, ability := range abilities {
		require.Equal(t, status == common.ChannelStatusEnabled, ability.Enabled, "abilities follow the stored channel status")
	}
}

// Every writer of a channel's status writes its abilities in the same
// transaction under the channel row lock, so the two tables always agree.
func TestChannelStatusWritersKeepAbilitiesWithTheChannel(t *testing.T) {
	useScheduledChannelDatabase(t)
	previousCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousCache })
	insert := func(t *testing.T, name string) int {
		t.Helper()
		channel := Channel{Type: constant.ChannelTypeTaskPlugin, Name: name, Key: "key", Status: common.ChannelStatusEnabled, Models: name}
		require.NoError(t, channel.Insert())
		return channel.Id
	}

	t.Run("an enable that finishes while an older disable publishes wins in both tables", func(t *testing.T) {
		channelID := insert(t, "status-interleave")
		reached, resume := make(chan struct{}), make(chan struct{})
		first := true
		recordChannelPublications(t, func(id int) error {
			if id == channelID && first {
				first = false
				close(reached)
				<-resume
			}
			return nil
		})
		disabled := make(chan ChannelStatusUpdateResult, 1)
		go func() {
			disabled <- UpdateChannelStatusDetailed(channelID, "key", common.ChannelStatusAutoDisabled, "automatic")
		}()
		select {
		case <-reached:
		case <-time.After(5 * time.Second):
			t.Fatal("the disable did not reach publication")
		}
		enabled := UpdateChannelStatusDetailed(channelID, "", common.ChannelStatusEnabled, "admin")
		close(resume)
		require.True(t, (<-disabled).Changed)
		require.True(t, enabled.Changed)
		requireChannelAndAbilities(t, channelID, common.ChannelStatusEnabled)
	})

	writers := map[string]func(int) error{
		"status": func(id int) error {
			if UpdateChannelStatusDetailed(id, "key", common.ChannelStatusAutoDisabled, "automatic").Changed {
				return nil
			}
			return errors.New("status not written")
		},
		"refresh": func(id int) error {
			_, err := UpdateChannelLocked(id, func(channel *Channel) error {
				channel.Status = common.ChannelStatusAutoDisabled
				return nil
			})
			return err
		},
		"login": func(id int) error {
			return UpdateTaskPluginChannelLocked(id, func(channel *Channel, _ []*Channel) error {
				channel.Status = common.ChannelStatusAutoDisabled
				return nil
			})
		},
		"scheduled": func(id int) error {
			_, err := UpdateScheduledChannelLocked(context.Background(), id, func(channel *Channel) error {
				channel.Status = common.ChannelStatusAutoDisabled
				return nil
			})
			return err
		},
	}
	for name, write := range writers {
		t.Run(name+" rolls the status back when the ability write fails", func(t *testing.T) {
			channelID := insert(t, "ability-failure-"+name)
			published := recordChannelPublications(t, nil)
			failAbilityWrites(t)
			require.Error(t, write(channelID))
			require.Empty(t, *published, "a rolled-back status is not published")
			_ = DB.Callback().Update().Remove("test:fail_ability_write")
			requireChannelAndAbilities(t, channelID, common.ChannelStatusEnabled)
		})
	}
}
