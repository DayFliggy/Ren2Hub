package service

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const nodeSyncInterval = 3 * time.Second

// Keep local load diagnostics bounded even if many plugins fail at once.
const (
	nodeSyncMessageLimit = 300
	nodeSyncErrorsLimit  = 50
)

// NodeSyncTopicState is what this node has applied of one topic: every write
// whose sync task id is at most Revision. Errors lists the items of the topic
// the node failed to load, by key.
type NodeSyncTopicState struct {
	Revision  int64             `json:"revision"`
	AppliedAt int64             `json:"applied_at"`
	Error     string            `json:"error,omitempty"`
	Errors    map[string]string `json:"errors,omitempty"`
}

type nodeSyncRegistry struct {
	mu      sync.Mutex
	reloads map[string]func(ctx context.Context) error
	states  map[string]NodeSyncTopicState
	// startup holds the revisions read before the startup loads; nil when
	// that read failed, and every topic then reloads on the first tick.
	startup map[string]int64
	// listFailed logs a failing topic read once, not on every tick.
	listFailed bool
}

var nodeSync = newNodeSyncRegistry()

func newNodeSyncRegistry() *nodeSyncRegistry {
	return &nodeSyncRegistry{reloads: map[string]func(context.Context) error{}, states: map[string]NodeSyncTopicState{}}
}

// RegisterSyncTopic makes this node run reload whenever another write bumps
// topic. Register every topic before StartNodeSync.
func RegisterSyncTopic(topic string, reload func(ctx context.Context) error) {
	nodeSync.mu.Lock()
	defer nodeSync.mu.Unlock()
	nodeSync.reloads[topic] = reload
}

// SetSyncTopicErrors records the items of topic this node failed to load, by
// key; nil or empty clears them.
func SetSyncTopicErrors(topic string, errors map[string]string) {
	var capped map[string]string
	if len(errors) > 0 {
		keys := slices.Sorted(maps.Keys(errors))
		keys = keys[:min(len(keys), nodeSyncErrorsLimit)]
		capped = make(map[string]string, len(keys))
		for _, key := range keys {
			capped[key] = capSyncMessage(errors[key])
		}
	}
	nodeSync.mu.Lock()
	defer nodeSync.mu.Unlock()
	state := nodeSync.states[topic]
	state.Errors = capped
	nodeSync.states[topic] = state
}

// NodeSyncStates returns a copy of what this node has applied, by topic.
func NodeSyncStates() map[string]NodeSyncTopicState {
	nodeSync.mu.Lock()
	defer nodeSync.mu.Unlock()
	states := make(map[string]NodeSyncTopicState, len(nodeSync.states))
	for topic, state := range nodeSync.states {
		state.Errors = maps.Clone(state.Errors)
		states[topic] = state
	}
	return states
}

// ReadStartupSyncRevisions reads the newest sync task of every topic before
// the startup loads, so a write made while they run is reloaded once the loop
// starts.
func ReadStartupSyncRevisions() {
	revisions, err := model.GetLatestSyncRevisions()
	if err != nil {
		common.SysError(common.LogText("failed to read sync topics: %v", err))
		return
	}
	nodeSync.mu.Lock()
	defer nodeSync.mu.Unlock()
	nodeSync.startup = revisions
}

// SyncTopicLoadFailed records that this node's startup load of topic failed:
// StartNodeSync then leaves the topic unapplied, so the first tick loads it
// again, and the error shows until a load succeeds.
func SyncTopicLoadFailed(topic string, err error) {
	nodeSync.mu.Lock()
	defer nodeSync.mu.Unlock()
	state := nodeSync.states[topic]
	state.Error = capSyncMessage(err.Error())
	nodeSync.states[topic] = state
}

// StartNodeSync records the revisions read at startup as applied, then every
// 3 seconds reloads each registered topic that has a newer sync task.
// Call it after the startup loads.
func StartNodeSync() {
	applyStartupSyncRevisions()
	gopool.Go(func() {
		ticker := time.NewTicker(nodeSyncInterval)
		defer ticker.Stop()
		for range ticker.C {
			syncNodeTopics(context.Background())
		}
	})
}

// applyStartupSyncRevisions records the revisions read before the startup
// loads as applied. A topic whose startup load failed, or every topic when that
// read failed, stays unapplied: the first tick loads it again, and the node
// reports it as not synced until then.
func applyStartupSyncRevisions() {
	nodeSync.mu.Lock()
	defer nodeSync.mu.Unlock()
	now := common.GetTimestamp()
	for topic := range nodeSync.reloads {
		state := nodeSync.states[topic]
		if nodeSync.startup != nil && state.Error == "" {
			state.Revision, state.AppliedAt = nodeSync.startup[topic], now
		}
		nodeSync.states[topic] = state
	}
}

// syncNodeTopics runs the reload of each registered topic whose newest sync
// task is newer than the one this node applied, and reports whether any ran.
// A success records the task read before the reload, so a write made during
// the reload runs it again on the next tick; a failure keeps the old one.
func syncNodeTopics(ctx context.Context) bool {
	nodeSync.mu.Lock()
	reloads := maps.Clone(nodeSync.reloads)
	states := maps.Clone(nodeSync.states)
	nodeSync.mu.Unlock()
	topics := slices.Sorted(maps.Keys(reloads))
	taskTypes := make([]string, len(topics))
	for i, topic := range topics {
		taskTypes[i] = model.SyncTaskType(topic)
	}
	revisions, err := model.GetLatestSystemTaskIDs(taskTypes)
	nodeSync.mu.Lock()
	failedBefore := nodeSync.listFailed
	nodeSync.listFailed = err != nil
	nodeSync.mu.Unlock()
	if err != nil {
		if !failedBefore {
			common.SysError(common.LogText("failed to read sync topics: %v", err))
		}
		return false
	}
	ran := false
	for _, topic := range topics {
		revision := revisions[model.SyncTaskType(topic)]
		if state := states[topic]; state.AppliedAt != 0 && revision <= state.Revision {
			continue
		}
		ran = true
		err := func() (err error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					err = fmt.Errorf("reload panicked: %v", recovered)
				}
			}()
			return reloads[topic](ctx)
		}()
		nodeSync.mu.Lock()
		state := nodeSync.states[topic]
		previousError := state.Error
		if err != nil {
			state.Error = capSyncMessage(err.Error())
		} else {
			state.Revision, state.AppliedAt, state.Error = revision, common.GetTimestamp(), ""
		}
		nodeSync.states[topic] = state
		nodeSync.mu.Unlock()
		if err != nil && state.Error != previousError {
			common.SysError(common.LogText("failed to reload sync topic %s: %v", topic, err))
		}
	}
	return ran
}

func capSyncMessage(message string) string {
	if utf8.RuneCountInString(message) <= nodeSyncMessageLimit {
		return message
	}
	return string([]rune(message)[:nodeSyncMessageLimit])
}

// finishAppliedSyncTasks acknowledges only revisions applied by this process.
// Unregistered topics have no local cache to refresh. Failed or not-yet-run
// reloads keep their tasks pending so the next tick can retry them.
func finishAppliedSyncTasks() {
	taskTypes, err := model.PendingSyncTaskTypes()
	if err != nil {
		logger.LogWarn(context.Background(), common.LogText("sync task query failed: %v", err))
		return
	}
	if len(taskTypes) == 0 {
		return
	}
	newest, err := model.GetLatestSystemTasks(taskTypes)
	if err != nil {
		logger.LogWarn(context.Background(), common.LogText("sync task query failed: %v", err))
		return
	}
	nodeSync.mu.Lock()
	states := maps.Clone(nodeSync.states)
	reloads := maps.Clone(nodeSync.reloads)
	nodeSync.mu.Unlock()
	for _, taskType := range taskTypes {
		task := newest[taskType]
		if task == nil {
			continue
		}
		topic, _ := model.SyncTaskTopic(taskType)
		throughID, total := task.ID, 0
		state := states[topic]
		if _, registered := reloads[topic]; registered {
			total = 1
			throughID = state.Revision
			if state.AppliedAt == 0 {
				throughID = 0
			}
		}
		if throughID > 0 {
			if err := model.FinishSyncTasks(taskType, throughID, SystemTaskProgress{Total: total, Processed: total, Progress: 100}); err != nil {
				logger.LogWarn(context.Background(), common.LogText("sync task update failed: type=%s err=%v", taskType, err))
				continue
			}
		}
		if task.Status != model.SystemTaskStatusPending || task.ID <= throughID {
			continue
		}
		if err := model.UpdateSyncTaskProgress(task, SystemTaskProgress{Total: total}, state.Error); err != nil {
			logger.LogWarn(context.Background(), common.LogText("sync task update failed: type=%s err=%v", taskType, err))
		}
	}
}
