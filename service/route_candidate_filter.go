package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/relaycapability"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// routeCapabilityFilterInput contains only request-time facts shared by the
// manual preview and the unified request selector. Profile and Entry state
// remain outside this filter because they do not exist in automatic routing.
type routeCapabilityFilterInput struct {
	Capability               model.ChannelModelCapability
	SnapshotVersion          int64
	ChannelStatus            int
	ChannelType              int
	AbilityEnabled           bool
	Token                    model.Token
	TokenLimitEnabled        bool
	TokenLimit               map[string]bool
	RequestModel             string
	NormalizedModel          string
	RequestPath              string
	EndpointType             string
	Entitled                 bool
	PriceEligible            bool
	PriceEligibilityKnown    bool
	SecurityAllowed          bool
	SecurityEligibilityKnown bool
	Advanced                 *dto.AdvancedCustomConfig
	RequireSnapshot          bool
	RequireEndpoint          bool
}

type routeCapabilityFilterResult struct{ Reason string }

const routeCapabilityMinimumConfidence = 0.9

// filterRouteCapability is the shared static qualification boundary. It does
// not perform billing, quota reservation, health changes, or live selection.
func filterRouteCapability(input routeCapabilityFilterInput) routeCapabilityFilterResult {
	result := routeCapabilityFilterResult{}

	if constant.IsRetiredChannelType(input.ChannelType) {
		result.Reason = RouteFilterChannelRetired
		return result
	}
	if input.ChannelStatus != common.ChannelStatusEnabled {
		result.Reason = RouteFilterChannelDisabled
		return result
	}
	if input.RequireSnapshot && input.SnapshotVersion <= 0 {
		result.Reason = RouteFilterSnapshotUnavailable
		return result
	}
	if input.Capability.ChannelID == 0 {
		result.Reason = RouteFilterUnknownCapability
		return result
	}
	if input.SnapshotVersion > 0 && input.Capability.SnapshotVersion != input.SnapshotVersion {
		result.Reason = RouteFilterSnapshotStale
		return result
	}
	switch input.Capability.State {
	case model.RouteCapabilityStateConflict:
		result.Reason = RouteFilterMappingConflict
		return result
	case "", model.RouteCapabilityStateUnresolved:
		result.Reason = RouteFilterUnknownCapability
		return result
	case model.RouteCapabilityStateUnsupported, model.RouteCapabilityStateDisabled:
		result.Reason = RouteFilterUnsupported
		return result
	case model.RouteCapabilityStateEligible:
		// Continue with request-time authorization and path checks.
	default:
		result.Reason = RouteFilterUnknownCapability
		return result
	}
	if strings.TrimSpace(input.Capability.LabSlug) == "" ||
		strings.EqualFold(strings.TrimSpace(input.Capability.Source), "unknown") {
		result.Reason = RouteFilterUnknownCapability
		return result
	}
	if input.Capability.Confidence < routeCapabilityMinimumConfidence {
		result.Reason = RouteFilterUnknownCapability
		return result
	}
	if !input.AbilityEnabled {
		result.Reason = RouteFilterAbilityDisabled
		return result
	}
	modelName := input.NormalizedModel
	if modelName == "" {
		modelName = input.RequestModel
	}
	if (input.TokenLimitEnabled || input.Token.IsModelLimitsEnabled()) &&
		!tokenAllowsRouteModel(input.TokenLimit, modelName) &&
		!tokenAllowsRouteModel(input.Token.GetModelLimitsMap(), modelName) {
		result.Reason = RouteFilterTokenForbidden
		return result
	}
	if (input.RequireEndpoint && input.EndpointType == "") ||
		(input.EndpointType != "" && !stringListContains(decodeStringList(input.Capability.EndpointTypes), input.EndpointType)) {
		result.Reason = RouteFilterPathUnsupported
		return result
	}
	if !constant.IsAdvancedCustomChannel(input.ChannelType) &&
		(input.EndpointType == string(constant.EndpointTypeAudio) || relaycapability.LegacyOpenAIOperation(input.RequestPath) != "") {
		apiType, _ := common.ChannelType2APIType(input.ChannelType)
		actualModel := input.Capability.ActualModel
		if actualModel == "" {
			actualModel = input.RequestModel
		}
		definition, _ := relaycapability.Describe(apiType, actualModel)
		format := types.RelayFormatOpenAI
		if input.EndpointType == string(constant.EndpointTypeAudio) {
			format = types.RelayFormatOpenAIAudio
		}
		if surface, found := definition.Resolve(format); !found || !surface.SupportsPath(input.RequestPath) {
			result.Reason = RouteFilterPathUnsupported
			return result
		}
	}
	if constant.IsAdvancedCustomChannel(input.ChannelType) {
		advanced := input.Advanced
		if advanced == nil {
			advanced = advancedCustomPathConfigFromCapability(input.Capability)
		}
		if advanced == nil || !advanced.SupportsPathForModel(input.RequestPath, input.RequestModel) {
			result.Reason = RouteFilterPathUnsupported
			return result
		}
	}
	if !input.Entitled {
		result.Reason = RouteFilterEntitlementRevoked
		return result
	}
	if input.PriceEligibilityKnown && !input.PriceEligible {
		result.Reason = RouteFilterPriceForbidden
		return result
	}
	if input.SecurityEligibilityKnown && !input.SecurityAllowed {
		result.Reason = RouteFilterSecurityForbidden
		return result
	}
	return result
}
