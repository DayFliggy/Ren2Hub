package service

import (
	"strings"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// RequestRequirements is the single description of what a request needs from a
// channel. It is derived once per request and shared by candidate filtering,
// the manual preview and conversion planning, so those three cannot disagree
// about whether a candidate is eligible.
//
// Before this type existed the request path checked the endpoint type, native
// Responses support and Compact stage support, while the preview checked only
// the endpoint type. A preview therefore reported candidates as usable that the
// request path would reject.
type RequestRequirements struct {
	RequestModel string
	RequestPath  string
	// EndpointType is the capability bucket the path maps to. An empty value
	// means the path is not routable, which the shared filter rejects.
	EndpointType string
	// NativeResponsesRequired marks Responses requests carrying opaque
	// server-side compaction state, which cannot survive a converter pivot.
	NativeResponsesRequired bool
	CompactStage            relaycommon.CompactAttemptStage
	// CompactBaseModel is the base model a Compact request resolves to. It is
	// empty outside Compact so callers cannot accidentally apply Compact rules.
	CompactBaseModel string
}

// RouteEndpointTypeForPath exposes the endpoint-type derivation so callers
// outside this package can label an observation with the same protocol the
// selector scored with.
func RouteEndpointTypeForPath(requestPath string) string {
	return endpointTypeForRequestPath(requestPath)
}

// CompactStageForRequest derives the initial Compact stage from the request
// path and model, the same way the distributor does before its first attempt.
// A retry may narrow Exact to Base, so the request path passes its own stage
// rather than re-deriving it here.
func CompactStageForRequest(requestPath, requestModel string) relaycommon.CompactAttemptStage {
	if !strings.HasPrefix(requestPath, "/v1/responses/compact") {
		return relaycommon.CompactAttemptNone
	}
	if ratio_setting.IsGPTCompactBaseModel(ratio_setting.CompactBaseModelName(requestModel)) {
		return relaycommon.CompactAttemptExact
	}
	return relaycommon.CompactAttemptBase
}

// NewRequestRequirements derives the requirements for one request. Callers pass
// facts already established by auth and distribution; nothing here reads the
// request body or contacts a provider.
func NewRequestRequirements(requestModel, requestPath string, nativeResponsesRequired bool, stage relaycommon.CompactAttemptStage) RequestRequirements {
	requirements := RequestRequirements{
		RequestModel:            strings.TrimSpace(requestModel),
		RequestPath:             requestPath,
		EndpointType:            endpointTypeForRequestPath(requestPath),
		NativeResponsesRequired: nativeResponsesRequired,
		CompactStage:            stage,
	}
	if stage != relaycommon.CompactAttemptNone {
		requirements.CompactBaseModel = ratio_setting.CompactBaseModelName(requirements.RequestModel)
	}
	return requirements
}

// ChannelMeetsRequirements reports the filter reason for a channel that cannot
// serve these requirements, or an empty string when it can. It covers only the
// protocol and capability facts carried by RequestRequirements; snapshot,
// entitlement, token and price checks stay in filterRouteCapability.
//
// abilityModels is the channel's enabled ability models and is consulted only
// for Compact, where stage support depends on which model variants exist.
func (r RequestRequirements) ChannelMeetsRequirements(channel *model.Channel, abilityModels map[string]bool) string {
	if channel == nil {
		return RouteFilterChannelDisabled
	}
	if r.NativeResponsesRequired && !ChannelSupportsNativeResponses(channel, r.RequestModel) {
		return RouteFilterPathUnsupported
	}
	if r.CompactStage == relaycommon.CompactAttemptNone {
		return ""
	}
	base := r.CompactBaseModel
	if !compactChannelSupportsStage(channel, abilityModels, base, ratio_setting.WithCompactModelSuffix(base), r.CompactStage) {
		return RouteFilterPathUnsupported
	}
	return ""
}

// CandidateLookupModels lists the request models to search in the capability
// index, in priority order. Compact resolves its exact variant before the base
// model; every other protocol looks up exactly one model.
func (r RequestRequirements) CandidateLookupModels(normalize func(string) string) []string {
	if r.CompactStage == relaycommon.CompactAttemptNone {
		return []string{normalize(r.RequestModel)}
	}
	base := normalize(r.CompactBaseModel)
	if r.CompactStage != relaycommon.CompactAttemptExact {
		return []string{base}
	}
	return []string{normalize(ratio_setting.WithCompactModelSuffix(r.CompactBaseModel)), base}
}
