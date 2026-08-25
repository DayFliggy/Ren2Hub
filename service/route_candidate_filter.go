package service

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// routeCapabilityFilterInput contains only request-time facts shared by the
// manual preview and the automatic Shadow selector. Profile and Entry state
// remain outside this filter because they do not exist in automatic routing.
type routeCapabilityFilterInput struct {
	Capability               model.ChannelModelCapability
	SnapshotVersion          int64
	ChannelStatus            int
	ChannelType              int
	AbilityEnabled           bool
	AbilityAllowed           bool
	AbilityGroups            []string
	UserGroup                string
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

// filterRouteCapability is the shared static qualification boundary. It does
// not perform billing, quota reservation, health changes, or live selection.
func filterRouteCapability(input routeCapabilityFilterInput) routeCapabilityFilterResult {
	result := routeCapabilityFilterResult{}

	if input.ChannelStatus != common.ChannelStatusEnabled {
		result.Reason = ShadowFilterChannelDisabled
		return result
	}
	if input.RequireSnapshot && input.SnapshotVersion <= 0 {
		result.Reason = ShadowFilterSnapshotUnavailable
		return result
	}
	if input.Capability.ChannelID == 0 {
		result.Reason = ShadowFilterUnknownCapability
		return result
	}
	if input.SnapshotVersion > 0 && input.Capability.SnapshotVersion != input.SnapshotVersion {
		result.Reason = ShadowFilterSnapshotStale
		return result
	}
	switch input.Capability.State {
	case model.RouteCapabilityStateConflict:
		result.Reason = ShadowFilterMappingConflict
		return result
	case "", model.RouteCapabilityStateUnresolved:
		result.Reason = ShadowFilterUnknownCapability
		return result
	case model.RouteCapabilityStateUnsupported, model.RouteCapabilityStateDisabled:
		result.Reason = ShadowFilterUnsupported
		return result
	case model.RouteCapabilityStateEligible:
		// Continue with request-time authorization and path checks.
	default:
		result.Reason = ShadowFilterUnknownCapability
		return result
	}
	if strings.TrimSpace(input.Capability.LabSlug) == "" ||
		strings.EqualFold(strings.TrimSpace(input.Capability.Source), "unknown") {
		result.Reason = ShadowFilterUnknownCapability
		return result
	}
	if !input.AbilityEnabled {
		result.Reason = ShadowFilterAbilityDisabled
		return result
	}
	if !input.AbilityAllowed {
		if len(input.AbilityGroups) > 0 {
			allowed := false
			for _, group := range input.AbilityGroups {
				if group == input.UserGroup || IsUserSelectableGroup(input.UserGroup, group) {
					allowed = true
					break
				}
			}
			if !allowed {
				result.Reason = ShadowFilterGroupForbidden
				return result
			}
		} else {
			result.Reason = ShadowFilterGroupForbidden
			return result
		}
	}
	modelName := input.NormalizedModel
	if modelName == "" {
		modelName = input.RequestModel
	}
	if (input.TokenLimitEnabled || input.Token.IsModelLimitsEnabled()) &&
		!tokenAllowsShadowModel(input.TokenLimit, modelName) &&
		!tokenAllowsShadowModel(input.Token.GetModelLimitsMap(), modelName) {
		result.Reason = ShadowFilterTokenForbidden
		return result
	}
	if (input.RequireEndpoint && input.EndpointType == "") ||
		(input.EndpointType != "" && !stringListContains(decodeStringList(input.Capability.EndpointTypes), input.EndpointType)) {
		result.Reason = ShadowFilterPathUnsupported
		return result
	}
	if input.ChannelType == constant.ChannelTypeAdvancedCustom {
		advanced := input.Advanced
		if advanced == nil {
			advanced = advancedCustomPathConfigFromCapability(input.Capability)
		}
		if advanced == nil || !advanced.SupportsPathForModel(input.RequestPath, input.RequestModel) {
			result.Reason = ShadowFilterPathUnsupported
			return result
		}
	}
	if !input.Entitled {
		result.Reason = ShadowFilterEntitlementRevoked
		return result
	}
	if input.PriceEligibilityKnown && !input.PriceEligible {
		result.Reason = ShadowFilterPriceForbidden
		return result
	}
	if input.SecurityEligibilityKnown && !input.SecurityAllowed {
		result.Reason = ShadowFilterSecurityForbidden
		return result
	}
	return result
}
