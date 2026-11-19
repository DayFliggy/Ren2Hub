package service

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
)

// EnableRecoveredChannelKey puts an automatically disabled key of a multi-key
// channel back into the rotation after a successful test of that key. It
// writes under the channel's polling lock and row lock, only while the stored
// key at keyIndex is still the tested key and still automatically disabled, so
// a manual disable is never undone. The channel itself is enabled again when
// it was disabled only because every key was. EnableChannel cannot do this:
// like upstream UpdateChannelStatus it writes nothing for an enabled channel.
func EnableRecoveredChannelKey(ctx context.Context, channelID, keyIndex int, key string) (bool, error) {
	changed := false
	stored, err := model.UpdateScheduledChannelLocked(ctx, channelID, func(current *model.Channel) error {
		keys := current.GetKeys()
		if !current.ChannelInfo.IsMultiKey || keyIndex >= len(keys) || keys[keyIndex] != key {
			return nil
		}
		changed = model.SetScheduledChannelKeyStatus(current, keyIndex, common.ChannelStatusEnabled, "")
		return nil
	})
	if err != nil || !changed {
		return false, err
	}
	params := map[string]any{"Name": stored.Name, "Id": stored.Id}
	NotifyChannelWatchers(formatNotifyType(stored.Id, common.ChannelStatusEnabled), func(lang string) (string, string) {
		return i18n.Translate(lang, i18n.MsgChannelNotifyEnabledSubject, params), i18n.Translate(lang, i18n.MsgChannelNotifyEnabledContent, params)
	})
	return true, nil
}
