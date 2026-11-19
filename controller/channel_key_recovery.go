package controller

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
)

// keyRecoveryPerChannel caps the automatically disabled keys that one
// scheduled round tests per channel.
const keyRecoveryPerChannel = 3

var errKeyUnderTestChanged = errors.New("the key under test or its channel is no longer eligible for recovery")

// channelKeyUnderTest is one automatically disabled key of a multi-key
// channel, tested on its own outside the rotation. testChannel narrows the
// channel it reloads to this key and records no routing evidence: the key
// serves no requests, so its result says nothing about the channel's route
// health.
type channelKeyUnderTest struct {
	index int
	key   string
}

// apply returns a single-key copy of the reloaded channel that sends with the
// key under test, or an error when service.RecoverableChannelKeys no longer
// allows the key, for example after a manual disable of the key or channel.
func (k channelKeyUnderTest) apply(channel *model.Channel) (*model.Channel, error) {
	if key, ok := service.RecoverableChannelKeys(channel)[k.index]; !ok || key != k.key {
		return nil, errKeyUnderTestChanged
	}
	narrowed := *channel
	narrowed.Key, narrowed.Keys = k.key, nil
	narrowed.ChannelInfo.IsMultiKey = false
	return &narrowed, nil
}

type keyRecoverySlot struct{ channelID, index int }

// keyRecoveryTested remembers when each disabled key was last tested in this
// process, so later rounds reach every key despite the per-channel cap.
var keyRecoveryTested = struct {
	sync.Mutex
	at map[keyRecoverySlot]time.Time
}{at: map[keyRecoverySlot]time.Time{}}

// recoverAutoDisabledKeys runs after a channel test run the scheduler started,
// in every test mode, when automatic enabling is on; a run an administrator
// started does not call it. Manually disabled channels and keys are skipped; a
// channel disabled because every key was is covered.
func recoverAutoDisabledKeys(ctx context.Context, channels []*model.Channel, testUserID int, concurrency int) {
	if !common.AutomaticEnableChannelEnabled {
		return
	}
	plans := keyRecoveryPlans(channels)
	candidates := make([]*model.Channel, 0, len(plans))
	for _, channel := range channels {
		if len(plans[channel.Id]) > 0 {
			candidates = append(candidates, channel)
		}
	}
	summary := runChannelTestWorkers(ctx, candidates, concurrency, func(ctx context.Context, channel *model.Channel) channelTestSummary {
		return recoverChannelKeys(ctx, channel, plans[channel.Id], testUserID)
	}, nil)
	if summary.Tested > 0 {
		common.SysLog(common.LogText("automatically disabled keys tested=%d recovered=%d", summary.Tested, summary.Enabled))
	}
}

// keyRecoveryPlans picks up to keyRecoveryPerChannel disabled keys of each
// channel, those tested longest ago first, and forgets keys that are no
// longer disabled.
func keyRecoveryPlans(channels []*model.Channel) map[int][]channelKeyUnderTest {
	keyRecoveryTested.Lock()
	defer keyRecoveryTested.Unlock()
	seen := map[keyRecoverySlot]time.Time{}
	plans := map[int][]channelKeyUnderTest{}
	for _, channel := range channels {
		var disabled []channelKeyUnderTest
		for index, key := range service.RecoverableChannelKeys(channel) {
			disabled = append(disabled, channelKeyUnderTest{index: index, key: key})
			slot := keyRecoverySlot{channel.Id, index}
			seen[slot] = keyRecoveryTested.at[slot]
		}
		sort.Slice(disabled, func(i, j int) bool {
			a, b := seen[keyRecoverySlot{channel.Id, disabled[i].index}], seen[keyRecoverySlot{channel.Id, disabled[j].index}]
			if !a.Equal(b) {
				return a.Before(b)
			}
			return disabled[i].index < disabled[j].index
		})
		plans[channel.Id] = disabled[:min(len(disabled), keyRecoveryPerChannel)]
	}
	keyRecoveryTested.at = seen
	return plans
}

func recoverChannelKeys(ctx context.Context, channel *model.Channel, keys []channelKeyUnderTest, testUserID int) channelTestSummary {
	summary := channelTestSummary{}
	for _, k := range keys {
		keyRecoveryTested.Lock()
		keyRecoveryTested.at[keyRecoverySlot{channel.Id, k.index}] = time.Now()
		keyRecoveryTested.Unlock()
		result := testChannel(ctx, channel, testUserID, "", "", false, k)
		if ctx.Err() != nil {
			return summary
		}
		summary.Tested++
		if result.localErr != nil || result.newAPIError != nil {
			summary.Failed++
			continue
		}
		summary.Succeeded++
		enabled, err := service.EnableRecoveredChannelKey(ctx, channel.Id, k.index, k.key)
		if err != nil {
			common.SysError(common.LogText("failed to enable recovered key: channel_id=%d, key_index=%d, error=%v", channel.Id, k.index, err))
			continue
		}
		if enabled {
			summary.Enabled++
		}
	}
	return summary
}
