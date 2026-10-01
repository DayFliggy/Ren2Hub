package service

import (
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

// A scheduled task is stale after missing two runs plus a minute of claim slack.
const systemTaskStaleSlackSeconds = 60

type SystemTaskTypeSummary struct {
	Type            string `json:"type"`
	Scheduled       bool   `json:"scheduled"`
	Enabled         bool   `json:"enabled"`
	IntervalSeconds int64  `json:"interval_seconds"`
	LastSuccessAt   *int64 `json:"last_success_at"`
	LastFailureAt   *int64 `json:"last_failure_at"`
	LastError       string `json:"last_error"`
	Active          bool   `json:"active"`
	Status          string `json:"status"`
}

// SummarizeSystemTasks reports every registered handler, so the task list is
// owned by the backend registry rather than a client-side constant.
func SummarizeSystemTasks(now time.Time) ([]SystemTaskTypeSummary, error) {
	handlers := registeredSystemTaskHandlers()
	types := make([]string, 0, len(handlers))
	for _, handler := range handlers {
		types = append(types, handler.Type())
	}
	activity, err := model.SummarizeSystemTaskActivity(types)
	if err != nil {
		return nil, err
	}
	summaries := make([]SystemTaskTypeSummary, 0, len(handlers))
	for _, handler := range handlers {
		run := activity[handler.Type()]
		summary := SystemTaskTypeSummary{Type: handler.Type(), Enabled: true, LastError: run.LastError, Active: run.Active}
		if scheduled, ok := handler.(ScheduledSystemTaskHandler); ok {
			summary.Scheduled, summary.Enabled = true, scheduled.Enabled()
			summary.IntervalSeconds = int64(scheduled.Interval().Seconds())
		}
		if run.LastSuccessAt > 0 {
			summary.LastSuccessAt = &run.LastSuccessAt
		}
		if run.LastFailureAt > 0 {
			summary.LastFailureAt = &run.LastFailureAt
		}
		summary.Status = systemTaskHealth(summary, run, now.Unix())
		summaries = append(summaries, summary)
	}
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Type < summaries[j].Type })
	return summaries, nil
}

// systemTaskHealth: "inactive" covers both configuration-disabled handlers and
// pollers with no pending work, because Enabled() does not distinguish them.
func systemTaskHealth(summary SystemTaskTypeSummary, run model.SystemTaskActivity, now int64) string {
	switch {
	case run.Active:
		return "running"
	case summary.Scheduled && !summary.Enabled:
		return "inactive"
	// Runs of one type are serialized, so their IDs disambiguate terminal
	// outcomes that share the model's second-resolution update timestamp.
	case run.LastFailureAt > run.LastSuccessAt || run.LastFailureAt == run.LastSuccessAt && run.LastFailureID > run.LastSuccessID:
		return "failing"
	case summary.Scheduled && now-max(run.LastSuccessAt, common.StartTime) > 2*summary.IntervalSeconds+systemTaskStaleSlackSeconds:
		return "stale"
	case run.LastSuccessAt == 0:
		return "idle"
	default:
		return "ok"
	}
}
