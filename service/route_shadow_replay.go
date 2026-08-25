package service

import (
	"context"
	"errors"
	"fmt"
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
	if err := common.Unmarshal(data, &stored); err != nil {
		return RouteShadowDecision{}, fmt.Errorf("%w: %v", ErrRouteShadowReplayInvalid, err)
	}
	var envelope map[string]any
	if err := common.Unmarshal(data, &envelope); err != nil {
		return RouteShadowDecision{}, fmt.Errorf("%w: %v", ErrRouteShadowReplayInvalid, err)
	}
	if strings.TrimSpace(stored.RequestID) == "" ||
		strings.TrimSpace(stored.RequestModel) == "" ||
		strings.TrimSpace(stored.RequestPath) == "" ||
		strings.TrimSpace(stored.UserGroup) == "" ||
		stored.SnapshotVersion <= 0 {
		return RouteShadowDecision{}, ErrRouteShadowReplayInvalid
	}
	if _, ok := envelope["legacy_trace"]; !ok {
		return RouteShadowDecision{}, ErrRouteShadowReplayInvalid
	}
	if len(stored.ShadowCandidates) == 0 {
		return RouteShadowDecision{}, ErrRouteShadowReplayInvalid
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
		RequestID:              stored.RequestID,
		UserID:                 stored.UserID,
		TokenID:                stored.TokenID,
		RequestModel:           stored.RequestModel,
		NormalizedRequestModel: normalized,
		RequestPath:            stored.RequestPath,
		EndpointType:           stored.EndpointType,
		UserGroup:              stored.UserGroup,
		PriceEligible:          true,
		SecurityAllowed:        true,
		Legacy:                 stored.LegacyTrace,
	}
	return selectRouteShadowWithIndex(request, index), nil
}
