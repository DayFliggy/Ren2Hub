package service

import (
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// BuildLegacySelectionTrace observes the candidate set behind the existing
// selector. It is intentionally called only when Shadow mode is enabled, so
// the legacy-disabled path has no extra query or logging work.
func BuildLegacySelectionTrace(group, requestModel, requestPath string, retry int, selectedChannelID int) LegacySelectionTrace {
	trace := LegacySelectionTrace{
		SelectedChannelID: selectedChannelID,
		SelectedGroup:     group,
		RetryAttempt:      retry,
		PriorityLayers:    make(map[int64][]int),
		FilteredReasons:   make(map[string]int),
	}
	if group == "" || requestModel == "" || model.DB == nil {
		return trace
	}
	names := normalizedTraceModels(requestModel)
	groupColumn := "`group`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		groupColumn = `"group"`
	}
	var abilities []model.Ability
	if err := model.DB.Where(groupColumn+" = ? AND model IN ?", group, names).Find(&abilities).Error; err != nil {
		return trace
	}
	channelIDs := make(map[int]struct{})
	enabledByChannel := make(map[int]bool)
	for _, ability := range abilities {
		channelIDs[ability.ChannelId] = struct{}{}
		if ability.Enabled {
			enabledByChannel[ability.ChannelId] = true
		}
	}
	if len(channelIDs) == 0 {
		return trace
	}
	ids := make([]int, 0, len(channelIDs))
	for id := range channelIDs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	var channels []model.Channel
	if err := model.DB.Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return trace
	}
	for index := range channels {
		channel := &channels[index]
		if !enabledByChannel[channel.Id] {
			trace.FilteredReasons[ShadowFilterAbilityDisabled]++
			continue
		}
		if channel.Status != common.ChannelStatusEnabled {
			trace.FilteredReasons[ShadowFilterChannelDisabled]++
			continue
		}
		if !model.ChannelSupportsRequestPath(channel, requestPath, requestModel) {
			trace.FilteredReasons[ShadowFilterPathUnsupported]++
			continue
		}
		trace.CandidateIDs = append(trace.CandidateIDs, channel.Id)
		priority := channel.GetPriority()
		trace.PriorityLayers[priority] = append(trace.PriorityLayers[priority], channel.Id)
	}
	sort.Ints(trace.CandidateIDs)
	for priority := range trace.PriorityLayers {
		sort.Ints(trace.PriorityLayers[priority])
	}
	return trace
}

func normalizedTraceModels(requestModel string) []string {
	values := []string{requestModel}
	for _, value := range []string{ratio_setting.FormatMatchingModelName(requestModel)} {
		if value != "" && value != requestModel {
			values = append(values, value)
		}
	}
	return values
}
