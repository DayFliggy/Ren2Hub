package service

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
)

// RecoverableChannelKeys returns, by slot, the keys of a channel that the
// scheduled key recovery may test and enable. It is the one eligibility rule
// for planning, for the reloaded channel before the test request and for the
// locked row at the write: a multi-key channel that is enabled or
// automatically disabled and is not a task plugin channel (a plugin locates
// and refreshes credentials by slot, which a channel narrowed to one key
// breaks), and a slot in range that is automatically disabled and whose text
// is unique in the channel (the native status write updates the first key
// with that text).
func RecoverableChannelKeys(channel *model.Channel) map[int]string {
	if !channel.ChannelInfo.IsMultiKey || channel.Type == constant.ChannelTypeTaskPlugin ||
		channel.Status != common.ChannelStatusEnabled && channel.Status != common.ChannelStatusAutoDisabled {
		return nil
	}
	keys := channel.GetKeys()
	uses := make(map[string]int, len(keys))
	for _, key := range keys {
		uses[key]++
	}
	recoverable := map[int]string{}
	for index, status := range channel.ChannelInfo.MultiKeyStatusList {
		if index < 0 || index >= len(keys) || status != common.ChannelStatusAutoDisabled || uses[keys[index]] != 1 {
			continue
		}
		recoverable[index] = keys[index]
	}
	return recoverable
}

// EnableRecoveredChannelKey puts an automatically disabled key of a multi-key
// channel back into the rotation after a successful test of that key; epoch
// is RouteConfigEpoch read before the test reloaded the channel. Under the
// channel's polling lock and row lock it writes only when no change of the
// channel was published since epoch (for example an administrator edit of
// the address, so a result from the old configuration is dropped) and
// the locked row still makes the tested key at keyIndex recoverable, so a
// manual disable is never undone. An edit committed but not yet published
// when the lock is taken is not seen. The channel itself is enabled again
// when it was disabled only because every key was. EnableChannel cannot do
// this: like upstream UpdateChannelStatus it writes nothing for an enabled
// channel. It notifies only when it wrote.
func EnableRecoveredChannelKey(ctx context.Context, channelID, keyIndex int, key string, epoch uint64) (bool, error) {
	changed := false
	stored, err := model.UpdateScheduledChannelLocked(ctx, channelID, func(current *model.Channel) error {
		if routeConfigChangedSince(epoch, channelID) {
			return nil
		}
		if stored, ok := RecoverableChannelKeys(current)[keyIndex]; !ok || stored != key {
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
