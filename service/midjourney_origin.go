package service

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
)

// MidjourneyOriginTaskID applies a Midjourney submission's relay mode to the
// request and names the task it continues. It returns the effective relay mode
// (a plus action becomes a change) and the origin mjId, empty for requests
// that create a task. Missing continuation fields are reported in the order
// the submission handler always checked them. The handler and Distribute both
// use it, so they agree on which requests continue which task.
func MidjourneyOriginTaskID(relayMode int, request *dto.MidjourneyRequest) (int, string, *dto.MidjourneyResponse) {
	if relayMode == relayconstant.RelayModeMidjourneyAction {
		if mjErr := CoverPlusActionToNormalAction(request); mjErr != nil {
			return relayMode, "", mjErr
		}
		relayMode = relayconstant.RelayModeMidjourneyChange
	}
	if relayMode == relayconstant.RelayModeMidjourneyVideo {
		request.Action = constant.MjActionVideo
	}
	switch relayMode {
	case relayconstant.RelayModeMidjourneyImagine:
		if request.Prompt == "" {
			return relayMode, "", MidjourneyErrorWrapper(constant.MjRequestError, "prompt_is_required")
		}
		request.Action = constant.MjActionImagine
		return relayMode, "", nil
	case relayconstant.RelayModeMidjourneyDescribe:
		request.Action = constant.MjActionDescribe
		return relayMode, "", nil
	case relayconstant.RelayModeMidjourneyEdits:
		request.Action = constant.MjActionEdits
		return relayMode, "", nil
	case relayconstant.RelayModeMidjourneyShorten:
		request.Action = constant.MjActionShorten
		return relayMode, "", nil
	case relayconstant.RelayModeMidjourneyBlend:
		request.Action = constant.MjActionBlend
		return relayMode, "", nil
	case relayconstant.RelayModeMidjourneyUpload:
		request.Action = constant.MjActionUpload
		return relayMode, "", nil
	}
	if request.TaskId == "" {
		return relayMode, "", nil
	}
	switch relayMode {
	case relayconstant.RelayModeMidjourneyChange:
		if request.Action == "" {
			return relayMode, "", MidjourneyErrorWrapper(constant.MjRequestError, "action_is_required")
		}
		if request.Index == 0 {
			return relayMode, "", MidjourneyErrorWrapper(constant.MjRequestError, "index_is_required")
		}
		return relayMode, request.TaskId, nil
	case relayconstant.RelayModeMidjourneySimpleChange:
		if request.Content == "" {
			return relayMode, "", MidjourneyErrorWrapper(constant.MjRequestError, "content_is_required")
		}
		params := ConvertSimpleChangeParams(request.Content)
		if params == nil {
			return relayMode, "", MidjourneyErrorWrapper(constant.MjRequestError, "content_parse_failed")
		}
		request.Action = params.Action
		return relayMode, params.TaskId, nil
	case relayconstant.RelayModeMidjourneyModal:
		request.Action = constant.MjActionModal
		return relayMode, request.TaskId, nil
	case relayconstant.RelayModeMidjourneyVideo:
		return relayMode, request.TaskId, nil
	}
	return relayMode, "", nil
}
