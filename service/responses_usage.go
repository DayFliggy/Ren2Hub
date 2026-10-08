package service

import (
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/tokenkit"
)

// ResponsesUsageAccumulator collects provider facts until the stream owner
// finishes, including completed image calls preceding an unexpected disconnect.
type ResponsesUsageAccumulator struct {
	info           *relaycommon.RelayInfo
	usage          *dto.Usage
	outputText     strings.Builder
	imageCounter   relaycommon.ImageGenerationCallCounter
	imageCommitted bool
	finished       bool
	terminalUsage  bool
}

func NewResponsesUsageAccumulator(info *relaycommon.RelayInfo) *ResponsesUsageAccumulator {
	return &ResponsesUsageAccumulator{info: info, usage: &dto.Usage{UsageSource: "missing"}}
}

// Observe feeds one decoded stream event. raw is the same event's wire bytes;
// the vendor tool-usage reader runs on it at terminal events.
func (a *ResponsesUsageAccumulator) Observe(event *dto.ResponsesStreamResponse, raw []byte) {
	if a == nil || event == nil || a.finished {
		return
	}
	if event.Response != nil {
		a.info.ObserveResponseModel(event.Response.Model)
		a.info.ObserveActualServiceTier(event.Response.ServiceTier)
	}
	if event.Response != nil && event.Response.Usage != nil {
		// Responses usage is a cumulative snapshot, not a token delta.
		a.usage = relayconvert.UsageFromResponsesUsage(event.Response.Usage)
	}
	switch event.Type {
	case "response.completed", "response.done", "response.failed", "response.incomplete", "response.cancelled", "response.canceled", "response.error", "error":
		a.terminalUsage = event.Response != nil && event.Response.Usage != nil
		if observation := a.usage.Observation; observation != nil {
			a.terminalUsage = a.terminalUsage && observation.Complete
			if (event.Type == "response.completed" || event.Type == "response.done") && event.Response != nil && !relaycommon.IsNonBillableResponsesStatus(event.Response.Status) {
				observation.Terminal = "completed"
			}
		}
		if !a.imageCommitted {
			// Images that completed before any terminal, failed ones included,
			// were delivered and stay billable; the counter still skips
			// unfinished image items. Web, file and function calls already
			// counted from output_item.done stay counted on every terminal, as
			// in NewAPI; the terminal snapshot adds the ones not yet seen only
			// when the response completed.
			completed := (event.Type == "response.completed" || event.Type == "response.done") &&
				(event.Response == nil || !relaycommon.IsNonBillableResponsesStatus(event.Response.Status))
			if event.Response != nil {
				for i := range event.Response.Output {
					a.imageCounter.Observe(&event.Response.Output[i], &i)
					if completed {
						a.info.ObserveBillableToolCall(&event.Response.Output[i], &i)
					}
				}
			}
			a.imageCounter.Commit(a.info)
			a.imageCommitted = true
		}
		// Vendor counts are cumulative on the terminal event and replace the
		// tool items counted above and from output_item.done.
		a.info.ApplyVendorToolUsage(raw)
	case "response.output_text.delta":
		a.outputText.WriteString(event.Delta)
	case dto.ResponsesOutputTypeItemDone:
		if event.Item == nil {
			return
		}
		switch event.Item.Type {
		case dto.BuildInCallWebSearchCall, dto.BuildInCallFileSearchCall, dto.BuildInCallFunctionCall:
			a.info.ObserveBillableToolCall(event.Item, event.OutputIndex)
		case dto.ResponsesOutputTypeImageGenerationCall:
			if !a.imageCommitted {
				a.imageCounter.Observe(event.Item, event.OutputIndex)
			}
		}
	}
}

func (a *ResponsesUsageAccumulator) Finish() *dto.Usage {
	if a.finished {
		return a.usage
	}
	a.finished = true
	// A final image item can already have reached the client before the stream
	// disconnects, so completed image usage is retained even without a terminal.
	if !a.imageCommitted {
		a.imageCounter.Commit(a.info)
		a.imageCommitted = true
	}
	estimated := false
	if !a.terminalUsage && a.usage.CompletionTokens == 0 && a.outputText.Len() > 0 {
		a.usage.CompletionTokens = tokenkit.Count(a.info.UpstreamModelName, a.outputText.String())
		a.usage.OutputTokens = a.usage.CompletionTokens
		a.usage.UsageSource = "estimated_partial"
		estimated = true
	}
	if !a.terminalUsage && a.usage.PromptTokens == 0 && a.usage.CompletionTokens != 0 {
		a.usage.PromptTokens = a.info.GetEstimatePromptTokens()
		a.usage.InputTokens = a.usage.PromptTokens
		estimated = true
	}
	if estimated || a.usage.TotalTokens == 0 {
		a.usage.TotalTokens = a.usage.PromptTokens + a.usage.CompletionTokens
	}
	// Preserve an upstream billing dialect sidecar; only refresh the native
	// Responses snapshot when a local estimate filled its missing counters.
	if estimated && (a.usage.BillingUsage == nil || a.usage.BillingUsage.Source == dto.BillingUsageSourceOAIResponses) {
		a.usage.BillingUsage = dto.NewOpenAIResponsesBillingUsage(a.usage)
		if a.usage.BillingUsage != nil {
			a.usage.BillingUsage.Estimated = true
		}
	}
	if !a.terminalUsage && !estimated {
		a.usage.UsageSource = "partial"
	}
	return a.usage
}
