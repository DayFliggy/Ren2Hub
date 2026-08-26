package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
)

var ErrRouteShadowReplayInvalid = errors.New("invalid route shadow replay event")
var ErrRouteShadowReplayUnsupported = errors.New("route shadow replay requires a current capability projection")

// ReplayRouteShadowDecision recomputes a Shadow decision from the immutable
// capability versions referenced by a redacted decision event. It never uses
// the live capability index and has no relay, billing, context, or retry side
// effects.
func ReplayRouteShadowDecision(ctx context.Context, data []byte) (RouteShadowDecision, error) {
	var stored RouteShadowDecision
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return RouteShadowDecision{}, fmt.Errorf("%w: %v", ErrRouteShadowReplayInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return RouteShadowDecision{}, ErrRouteShadowReplayInvalid
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return RouteShadowDecision{}, fmt.Errorf("%w: %v", ErrRouteShadowReplayInvalid, err)
	}
	if stored.Event != "route_shadow_decision" || stored.RouteSource != ShadowRouteSource {
		return RouteShadowDecision{}, ErrRouteShadowReplayInvalid
	}
	if strings.TrimSpace(stored.RequestID) == "" ||
		strings.TrimSpace(stored.RequestModel) == "" ||
		strings.TrimSpace(stored.RequestPath) == "" ||
		strings.TrimSpace(stored.UserGroup) == "" ||
		stored.SnapshotVersion <= 0 {
		return RouteShadowDecision{}, ErrRouteShadowReplayInvalid
	}
	legacyTrace, ok := envelope["legacy_trace"]
	if !ok || string(bytes.TrimSpace(legacyTrace)) == "null" {
		return RouteShadowDecision{}, ErrRouteShadowReplayInvalid
	}
	if len(stored.ShadowCandidates) == 0 {
		return RouteShadowDecision{}, ErrRouteShadowReplayInvalid
	}
	// Older events cannot distinguish an omitted qualification from an
	// explicit denial. Never infer permission while replaying them.
	if stored.QualificationVersion != RouteShadowQualificationVersion {
		return RouteShadowDecision{}, ErrRouteShadowReplayUnsupported
	}
	if ctx == nil {
		ctx = context.Background()
	}

	normalized := stored.NormalizedRequestModel
	if normalized == "" {
		normalized = modellab.NormalizeModel(stored.RequestModel)
	}
	index := &capabilityIndex{ByRequestModel: map[string][]indexedCapability{}}
	seen := make(map[string]struct{}, len(stored.ShadowCandidates))
	for _, candidate := range stored.ShadowCandidates {
		if candidate.ChannelID <= 0 || candidate.SnapshotVersion <= 0 {
			return RouteShadowDecision{}, ErrRouteShadowReplayInvalid
		}
		key := fmt.Sprintf("%d:%d", candidate.ChannelID, candidate.SnapshotVersion)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		capabilities, err := model.FindChannelCapabilitySnapshotVersion(ctx, candidate.ChannelID, candidate.SnapshotVersion)
		if err != nil {
			return RouteShadowDecision{}, fmt.Errorf("%w: channel_id=%d snapshot_version=%d: %v", ErrRouteShadowReplayInvalid, candidate.ChannelID, candidate.SnapshotVersion, err)
		}
		for _, capability := range capabilities {
			if capability.RequestModel != normalized {
				continue
			}
			if capability.ProjectionVersion != model.ChannelCapabilityProjectionV1 ||
				capability.ChannelStatus == common.ChannelStatusUnknown {
				return RouteShadowDecision{}, fmt.Errorf("%w: channel_id=%d snapshot_version=%d", ErrRouteShadowReplayUnsupported, candidate.ChannelID, candidate.SnapshotVersion)
			}
			index.ByRequestModel[normalized] = append(index.ByRequestModel[normalized], indexedCapability{
				Capability:    capability,
				ChannelStatus: capability.ChannelStatus,
				Priority:      capability.Priority,
				Weight:        capability.Weight,
				AbilityGroups: decodeStringList(capability.AbilityGroups),
				ChannelType:   capability.ChannelType,
				Advanced:      advancedCustomPathConfigFromCapability(capability),
				Mixed:         capability.IsMixed,
			})
		}
	}
	if len(index.ByRequestModel[normalized]) == 0 {
		return RouteShadowDecision{}, ErrRouteShadowReplayInvalid
	}

	request := RouteShadowRequest{
		RequestID:                stored.RequestID,
		UserID:                   stored.UserID,
		TokenID:                  stored.TokenID,
		RequestModel:             stored.RequestModel,
		NormalizedRequestModel:   normalized,
		RequestPath:              stored.RequestPath,
		EndpointType:             stored.EndpointType,
		UserGroup:                stored.UserGroup,
		TokenModelLimitEnabled:   stored.TokenModelLimitEnabled,
		TokenModelLimit:          stored.TokenModelLimit,
		EntitledChannels:         stored.EntitledChannels,
		ChannelStatuses:          stored.ChannelStatuses,
		PriceEligible:            stored.PriceEligible,
		PriceEligibilityKnown:    stored.PriceEligibilityKnown,
		SecurityAllowed:          stored.SecurityAllowed,
		SecurityEligibilityKnown: stored.SecurityEligibilityKnown,
		Legacy:                   stored.LegacyTrace,
	}
	return selectRouteShadowWithIndex(request, index), nil
}
