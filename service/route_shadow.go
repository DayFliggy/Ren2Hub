package service

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

const (
	ShadowRouteSource = "auto_lab"
	// Version 2 distinguishes an uncomputed request-time qualification from
	// an explicit allow or deny result. Shadow runs before billing and prompt
	// inspection, so those facts must not be inferred as allowed.
	RouteShadowQualificationVersion = 2

	ShadowReasonSameChannel          = "same_channel"
	ShadowReasonDifferentPriority    = "different_priority"
	ShadowReasonLegacyOnly           = "legacy_only"
	ShadowReasonShadowOnly           = "shadow_only"
	ShadowReasonUnknownCapability    = "unknown_capability"
	ShadowReasonPathFilterDifference = "path_filter_difference"
	ShadowReasonAbilityFilter        = "ability_filter_difference"
	ShadowReasonTokenFilter          = "token_permission_difference"
	ShadowReasonSnapshotStale        = "snapshot_stale"
	ShadowReasonMappingConflict      = "mapping_conflict"

	ShadowFilterSnapshotUnavailable = "snapshot_unavailable"
	ShadowFilterSnapshotStale       = "snapshot_stale"
	ShadowFilterUnknownCapability   = "unknown_capability"
	ShadowFilterUnsupported         = "unsupported_capability"
	ShadowFilterChannelDisabled     = "channel_disabled"
	ShadowFilterAbilityDisabled     = "ability_disabled"
	ShadowFilterGroupForbidden      = "group_forbidden"
	ShadowFilterTokenForbidden      = "token_model_forbidden"
	ShadowFilterPathUnsupported     = "path_unsupported"
	ShadowFilterPriceForbidden      = "price_forbidden"
	ShadowFilterSecurityForbidden   = "security_forbidden"
	ShadowFilterEntitlementRevoked  = "entitlement_revoked"
	ShadowFilterMappingConflict     = "mapping_conflict"
)

type LegacySelectionTrace struct {
	CandidateIDs      []int           `json:"candidate_ids,omitempty"`
	PriorityLayers    map[int64][]int `json:"priority_layers,omitempty"`
	FilteredReasons   map[string]int  `json:"filtered_reasons,omitempty"`
	SelectedChannelID int             `json:"selected_channel_id,omitempty"`
	SelectedGroup     string          `json:"selected_group,omitempty"`
	RetryAttempt      int             `json:"retry_attempt"`
	AffinityHit       bool            `json:"affinity_hit"`
	AutoGroup         string          `json:"auto_group,omitempty"`
}

type RouteShadowRequest struct {
	RequestID                string
	UserID                   int
	TokenID                  int
	RequestModel             string
	NormalizedRequestModel   string
	RequestPath              string
	EndpointType             string
	UserGroup                string
	TokenModelLimitEnabled   bool
	TokenModelLimit          map[string]bool
	EntitledChannels         map[int]bool
	PriceEligible            bool
	PriceEligibilityKnown    bool
	SecurityAllowed          bool
	SecurityEligibilityKnown bool
	SnapshotVersion          int64
	ChannelStatuses          map[int]int
	Legacy                   LegacySelectionTrace
}

func BuildRouteShadowRequest(c *gin.Context, requestModel, requestPath string, retry int) RouteShadowRequest {
	effectiveGroup := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if effectiveGroup == "" || effectiveGroup == "auto" {
		effectiveGroup = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	}
	request := RouteShadowRequest{
		RequestID:    c.GetString(common.RequestIdKey),
		UserID:       common.GetContextKeyInt(c, constant.ContextKeyUserId),
		TokenID:      common.GetContextKeyInt(c, constant.ContextKeyTokenId),
		RequestModel: requestModel,
		RequestPath:  requestPath,
		UserGroup:    effectiveGroup,
		EndpointType: endpointTypeForRequestPath(requestPath),
		Legacy: LegacySelectionTrace{
			RetryAttempt: retry,
		},
	}
	request.NormalizedRequestModel = modellab.NormalizeModel(requestModel)
	if _, ok := c.Get(ginKeyChannelAffinityLogInfo); ok {
		request.Legacy.AffinityHit = true
	}
	request.Legacy.AutoGroup = common.GetContextKeyString(c, constant.ContextKeyAutoGroup)
	request.TokenModelLimitEnabled = common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled)
	if value, ok := common.GetContextKey(c, constant.ContextKeyTokenModelLimit); ok {
		request.TokenModelLimit, _ = value.(map[string]bool)
	}
	return request
}

type RouteShadowCandidate struct {
	ChannelID       int                  `json:"channel_id"`
	RequestModel    string               `json:"request_model"`
	ActualModel     string               `json:"actual_model"`
	LabSlug         string               `json:"lab_slug"`
	Priority        int64                `json:"priority"`
	Weight          int                  `json:"weight"`
	SnapshotVersion int64                `json:"snapshot_version"`
	CatalogVersion  string               `json:"catalog_version"`
	FilterReason    string               `json:"filter_reason,omitempty"`
	Score           *RouteScoreBreakdown `json:"score_breakdown,omitempty"`
}

type RouteShadowDecision struct {
	Event                    string                 `json:"event"`
	RequestID                string                 `json:"request_id"`
	UserID                   int                    `json:"user_id"`
	TokenID                  int                    `json:"token_id"`
	RouteSource              string                 `json:"route_source"`
	RequestModel             string                 `json:"request_model"`
	NormalizedRequestModel   string                 `json:"normalized_request_model"`
	RequestPath              string                 `json:"request_path"`
	UserGroup                string                 `json:"user_group"`
	TokenModelLimitEnabled   bool                   `json:"token_model_limit_enabled,omitempty"`
	TokenModelLimit          map[string]bool        `json:"token_model_limit,omitempty"`
	EntitledChannels         map[int]bool           `json:"entitled_channels,omitempty"`
	ChannelStatuses          map[int]int            `json:"channel_statuses,omitempty"`
	PriceEligible            bool                   `json:"price_eligible"`
	PriceEligibilityKnown    bool                   `json:"price_eligibility_known"`
	SecurityAllowed          bool                   `json:"security_allowed"`
	SecurityEligibilityKnown bool                   `json:"security_eligibility_known"`
	RuntimeRecheckRequired   bool                   `json:"runtime_recheck_required"`
	RuntimeRecheckReasons    []string               `json:"runtime_recheck_reasons,omitempty"`
	QualificationVersion     int                    `json:"qualification_version,omitempty"`
	ActualModel              string                 `json:"actual_model,omitempty"`
	LabSlug                  string                 `json:"lab_slug,omitempty"`
	EndpointType             string                 `json:"endpoint_type,omitempty"`
	CatalogVersion           string                 `json:"catalog_version,omitempty"`
	SnapshotVersion          int64                  `json:"snapshot_version,omitempty"`
	ShadowCandidates         []RouteShadowCandidate `json:"shadow_candidates"`
	LegacyCandidateIDs       []int                  `json:"legacy_candidate_ids,omitempty"`
	LegacyChannelID          int                    `json:"legacy_channel_id,omitempty"`
	ShadowPreferredID        int                    `json:"shadow_preferred_channel_id,omitempty"`
	ScoreShadowEnabled       bool                   `json:"score_shadow_enabled"`
	ScoreShadowPreferredID   int                    `json:"score_shadow_preferred_channel_id,omitempty"`
	ScoreShadowDifference    string                 `json:"score_shadow_difference,omitempty"`
	ScoreShadowError         string                 `json:"score_shadow_error,omitempty"`
	ScoreMetricsUnavailable  bool                   `json:"score_metrics_unavailable,omitempty"`
	LegacyTrace              LegacySelectionTrace   `json:"legacy_trace"`
	FilterReasonCounts       map[string]int         `json:"filter_reason_counts,omitempty"`
	DifferenceReasons        []string               `json:"difference_reasons,omitempty"`
	HasUnknown               bool                   `json:"has_unknown"`
	HasMappingConflict       bool                   `json:"has_mapping_conflict"`
	HasMixed                 bool                   `json:"has_mixed"`
	HasUnauthorized          bool                   `json:"has_unauthorized"`
	RetryAttempt             int                    `json:"retry_attempt"`
	GeneratedAt              int64                  `json:"generated_at"`
}

func SelectRouteShadow(request RouteShadowRequest) RouteShadowDecision {
	index, _ := routeCapabilityIndex.Load().(*capabilityIndex)
	return selectRouteShadowWithIndex(request, index)
}

func selectRouteShadowWithIndex(request RouteShadowRequest, index *capabilityIndex) RouteShadowDecision {
	request.RequestModel = strings.TrimSpace(request.RequestModel)
	if request.NormalizedRequestModel == "" {
		request.NormalizedRequestModel = modellab.NormalizeModel(request.RequestModel)
	}
	runtimeRecheckReasons := make([]string, 0, 2)
	if !request.PriceEligibilityKnown {
		runtimeRecheckReasons = append(runtimeRecheckReasons, "price_qualification")
	}
	if !request.SecurityEligibilityKnown {
		runtimeRecheckReasons = append(runtimeRecheckReasons, "security_policy")
	}
	decision := RouteShadowDecision{
		Event:                    "route_shadow_decision",
		RequestID:                request.RequestID,
		UserID:                   request.UserID,
		TokenID:                  request.TokenID,
		RouteSource:              ShadowRouteSource,
		RequestModel:             request.RequestModel,
		NormalizedRequestModel:   request.NormalizedRequestModel,
		RequestPath:              request.RequestPath,
		UserGroup:                request.UserGroup,
		TokenModelLimitEnabled:   request.TokenModelLimitEnabled,
		TokenModelLimit:          cloneStringBoolMap(request.TokenModelLimit),
		EntitledChannels:         cloneBoolMap(request.EntitledChannels),
		ChannelStatuses:          cloneIntMap(request.ChannelStatuses),
		PriceEligible:            request.PriceEligible,
		PriceEligibilityKnown:    request.PriceEligibilityKnown,
		SecurityAllowed:          request.SecurityAllowed,
		SecurityEligibilityKnown: request.SecurityEligibilityKnown,
		RuntimeRecheckRequired:   len(runtimeRecheckReasons) > 0,
		RuntimeRecheckReasons:    runtimeRecheckReasons,
		QualificationVersion:     RouteShadowQualificationVersion,
		EndpointType:             request.EndpointType,
		LegacyCandidateIDs:       append([]int(nil), request.Legacy.CandidateIDs...),
		LegacyChannelID:          request.Legacy.SelectedChannelID,
		LegacyTrace:              request.Legacy,
		FilterReasonCounts:       make(map[string]int),
		RetryAttempt:             request.Legacy.RetryAttempt,
		GeneratedAt:              time.Now().UnixMilli(),
	}
	if request.NormalizedRequestModel == "" {
		decision.DifferenceReasons = compareShadowDecision(decision)
		return decision
	}

	decision.CatalogVersion = modellab.DefaultCatalog().Version

	if index == nil {
		decision.FilterReasonCounts[ShadowFilterSnapshotUnavailable]++
		decision.DifferenceReasons = compareShadowDecision(decision)
		return decision
	}
	for _, candidate := range index.ByRequestModel[request.NormalizedRequestModel] {
		if candidate.Mixed {
			decision.HasMixed = true
		}
		shadowCandidate := RouteShadowCandidate{
			ChannelID:       candidate.Capability.ChannelID,
			RequestModel:    candidate.Capability.RequestModel,
			ActualModel:     candidate.Capability.ActualModel,
			LabSlug:         candidate.Capability.LabSlug,
			Priority:        candidate.Priority,
			Weight:          candidate.Weight,
			SnapshotVersion: candidate.Capability.SnapshotVersion,
			CatalogVersion:  candidate.Capability.CatalogVersion,
		}
		if reason := shadowCandidateFilter(request, candidate); reason != "" {
			shadowCandidate.FilterReason = reason
			decision.FilterReasonCounts[reason]++
			if reason == ShadowFilterUnknownCapability {
				decision.HasUnknown = true
			}
			if reason == ShadowFilterMappingConflict {
				decision.HasMappingConflict = true
			}
			if reason == ShadowFilterGroupForbidden || reason == ShadowFilterTokenForbidden || reason == ShadowFilterEntitlementRevoked {
				decision.HasUnauthorized = true
			}
			decision.ShadowCandidates = append(decision.ShadowCandidates, shadowCandidate)
			continue
		}
		decision.ShadowCandidates = append(decision.ShadowCandidates, shadowCandidate)
	}
	eligible := make([]RouteShadowCandidate, 0, len(decision.ShadowCandidates))
	for _, candidate := range decision.ShadowCandidates {
		if candidate.FilterReason == "" {
			eligible = append(eligible, candidate)
		}
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		if eligible[i].Priority != eligible[j].Priority {
			return eligible[i].Priority > eligible[j].Priority
		}
		return eligible[i].ChannelID < eligible[j].ChannelID
	})
	if len(eligible) > 0 {
		preferred := eligible[0]
		decision.ShadowPreferredID = preferred.ChannelID
		decision.ActualModel = preferred.ActualModel
		decision.LabSlug = preferred.LabSlug
		decision.CatalogVersion = preferred.CatalogVersion
		decision.SnapshotVersion = preferred.SnapshotVersion
	}
	decision.DifferenceReasons = compareShadowDecision(decision)
	return decision
}

func cloneBoolMap(values map[int]bool) map[int]bool {
	if values == nil {
		return nil
	}
	copyValues := make(map[int]bool, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

func cloneStringBoolMap(values map[string]bool) map[string]bool {
	if values == nil {
		return nil
	}
	copyValues := make(map[string]bool, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

func cloneIntMap(values map[int]int) map[int]int {
	if values == nil {
		return nil
	}
	copyValues := make(map[int]int, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

func shadowCandidateFilter(request RouteShadowRequest, candidate indexedCapability) string {
	entitled := true
	if request.EntitledChannels != nil {
		if value, ok := request.EntitledChannels[candidate.Capability.ChannelID]; ok {
			entitled = value
		}
	}
	channelStatus := candidate.ChannelStatus
	if status, ok := request.ChannelStatuses[candidate.Capability.ChannelID]; ok {
		channelStatus = status
	}
	result := filterRouteCapability(routeCapabilityFilterInput{
		Capability:               candidate.Capability,
		SnapshotVersion:          request.SnapshotVersion,
		ChannelStatus:            channelStatus,
		ChannelType:              candidate.ChannelType,
		AbilityEnabled:           len(candidate.AbilityGroups) > 0,
		AbilityAllowed:           false,
		AbilityGroups:            candidate.AbilityGroups,
		UserGroup:                request.UserGroup,
		TokenLimitEnabled:        request.TokenModelLimitEnabled,
		TokenLimit:               request.TokenModelLimit,
		RequestModel:             request.RequestModel,
		NormalizedModel:          request.NormalizedRequestModel,
		RequestPath:              request.RequestPath,
		EndpointType:             request.EndpointType,
		Entitled:                 entitled,
		PriceEligible:            request.PriceEligible,
		PriceEligibilityKnown:    request.PriceEligibilityKnown,
		SecurityAllowed:          request.SecurityAllowed,
		SecurityEligibilityKnown: request.SecurityEligibilityKnown,
		Advanced:                 candidate.Advanced,
	})
	return result.Reason
}

func stringListContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func tokenAllowsShadowModel(limit map[string]bool, modelName string) bool {
	if len(limit) == 0 {
		return false
	}
	if limit[modelName] {
		return true
	}
	return limit[ratio_setting.FormatMatchingModelName(modelName)]
}

func compareShadowDecision(decision RouteShadowDecision) []string {
	reasons := make([]string, 0, 4)
	switch {
	case decision.LegacyChannelID > 0 && decision.ShadowPreferredID == decision.LegacyChannelID:
		reasons = append(reasons, ShadowReasonSameChannel)
	case decision.LegacyChannelID > 0 && decision.ShadowPreferredID > 0:
		legacyPriority, shadowPriority := priorityForChannel(decision.LegacyTrace, decision.LegacyChannelID), priorityForShadow(decision.ShadowCandidates, decision.ShadowPreferredID)
		if legacyPriority != shadowPriority {
			reasons = append(reasons, ShadowReasonDifferentPriority)
		} else {
			reasons = append(reasons, ShadowReasonShadowOnly)
		}
	case decision.LegacyChannelID > 0:
		reasons = append(reasons, ShadowReasonLegacyOnly)
	case decision.ShadowPreferredID > 0:
		reasons = append(reasons, ShadowReasonShadowOnly)
	}
	if decision.FilterReasonCounts[ShadowFilterUnknownCapability] > 0 {
		reasons = append(reasons, ShadowReasonUnknownCapability)
	}
	if decision.FilterReasonCounts[ShadowFilterMappingConflict] > 0 {
		reasons = append(reasons, ShadowReasonMappingConflict)
	}
	if decision.FilterReasonCounts[ShadowFilterPathUnsupported] > 0 {
		reasons = append(reasons, ShadowReasonPathFilterDifference)
	}
	if decision.FilterReasonCounts[ShadowFilterAbilityDisabled] > 0 || decision.FilterReasonCounts[ShadowFilterGroupForbidden] > 0 {
		reasons = append(reasons, ShadowReasonAbilityFilter)
	}
	if decision.FilterReasonCounts[ShadowFilterTokenForbidden] > 0 {
		reasons = append(reasons, ShadowReasonTokenFilter)
	}
	if decision.FilterReasonCounts[ShadowFilterSnapshotStale] > 0 {
		reasons = append(reasons, ShadowReasonSnapshotStale)
	}
	return reasons
}

func priorityForChannel(trace LegacySelectionTrace, channelID int) int64 {
	for priority, channels := range trace.PriorityLayers {
		for _, candidateID := range channels {
			if candidateID == channelID {
				return priority
			}
		}
	}
	return 0
}

func priorityForShadow(candidates []RouteShadowCandidate, channelID int) int64 {
	for _, candidate := range candidates {
		if candidate.ChannelID == channelID {
			return candidate.Priority
		}
	}
	return 0
}

func MaybeRecordLegacySelection(ctx context.Context, request RouteShadowRequest) {
	if !routeShadowEnabled(request) {
		return
	}
	enrichShadowRequestCurrentState(ctx, &request)
	decision := SelectRouteShadow(request)
	AttachRouteScoreShadow(ctx, &decision)
	observeShadowDecision(decision)
	EnqueueRouteShadowDecision(ctx, decision)
}

// enrichShadowRequestCurrentState adds only request-time status facts. The
// capability index remains immutable and active-snapshot fenced, while a
// channel disable or entitlement revocation takes effect without waiting for
// the next capability refresh. This function is called only after the Shadow
// feature gate, so disabled Shadow adds no database work to the hot path.
func enrichShadowRequestCurrentState(ctx context.Context, request *RouteShadowRequest) {
	if request == nil || model.DB == nil {
		return
	}
	index, _ := routeCapabilityIndex.Load().(*capabilityIndex)
	if index != nil {
		ids := make([]int, 0)
		seen := make(map[int]struct{})
		for _, candidate := range index.ByRequestModel[request.NormalizedRequestModel] {
			if candidate.Capability.ChannelID <= 0 {
				continue
			}
			if _, exists := seen[candidate.Capability.ChannelID]; exists {
				continue
			}
			seen[candidate.Capability.ChannelID] = struct{}{}
			ids = append(ids, candidate.Capability.ChannelID)
		}
		if len(ids) > 0 {
			var channels []model.Channel
			if err := model.DB.WithContext(ctx).Select("id", "status").Where("id IN ?", ids).Find(&channels).Error; err == nil {
				request.ChannelStatuses = make(map[int]int, len(channels))
				for _, channel := range channels {
					request.ChannelStatuses[channel.Id] = channel.Status
				}
			}
		}
	}
	if request.UserID <= 0 {
		return
	}
	var entitlements []model.UserChannelEntitlement
	if err := model.DB.WithContext(ctx).Where("user_id = ? AND source = ?", request.UserID, model.RouteSourcePlatform).Find(&entitlements).Error; err != nil {
		return
	}
	request.EntitledChannels = make(map[int]bool, len(entitlements))
	for _, entitlement := range entitlements {
		request.EntitledChannels[entitlement.ChannelID] = entitlementIsActiveForShadow(entitlement)
	}
}

func entitlementIsActiveForShadow(entitlement model.UserChannelEntitlement) bool {
	return entitlement.Status == model.RouteEntitlementStatusEnabled && entitlement.RevokedAt == 0 &&
		(entitlement.ExpiresAt == 0 || entitlement.ExpiresAt > time.Now().Unix())
}

// RecordLegacySelectionAndShadow is the integration boundary used by relay
// selectors. Candidate tracing is deferred until after the feature gate, so a
// disabled Shadow deployment has no extra database work.
func RecordLegacySelectionAndShadow(ctx context.Context, request RouteShadowRequest, legacyGroup string, legacyChannelID int) {
	if !routeShadowEnabled(request) {
		return
	}
	request.Legacy = BuildLegacySelectionTrace(legacyGroup, request.RequestModel, request.RequestPath, request.Legacy.RetryAttempt, legacyChannelID)
	request.Legacy.SelectedGroup = legacyGroup
	if legacyGroup != "" && legacyGroup != "auto" {
		request.UserGroup = legacyGroup
	}
	MaybeRecordLegacySelection(ctx, request)
}

func endpointTypeForRequestPath(requestPath string) string {
	requestPath = strings.TrimSpace(requestPath)
	switch {
	case strings.HasPrefix(requestPath, "/v1/chat/completions"), strings.HasPrefix(requestPath, "/pg/chat/completions"):
		return string(constant.EndpointTypeOpenAI)
	case strings.HasPrefix(requestPath, "/v1/responses/compact"):
		return string(constant.EndpointTypeOpenAIResponseCompact)
	case strings.HasPrefix(requestPath, "/v1/responses"):
		return string(constant.EndpointTypeOpenAIResponse)
	case strings.HasPrefix(requestPath, "/v1/alpha/search"):
		return string(constant.EndpointTypeOpenAIAlphaSearch)
	case strings.HasPrefix(requestPath, "/v1/messages"):
		return string(constant.EndpointTypeAnthropic)
	case strings.HasPrefix(requestPath, "/v1/rerank"):
		return string(constant.EndpointTypeJinaRerank)
	case strings.HasPrefix(requestPath, "/v1/images/generations"):
		return string(constant.EndpointTypeImageGeneration)
	case strings.HasPrefix(requestPath, "/v1/embeddings"):
		return string(constant.EndpointTypeEmbeddings)
	case strings.HasPrefix(requestPath, "/v1beta/models"), strings.HasPrefix(requestPath, "/v1/models"):
		return string(constant.EndpointTypeGemini)
	default:
		return ""
	}
}

func routeShadowEnabled(request RouteShadowRequest) bool {
	if !common.GetEnvOrDefaultBool("ROUTE_SHADOW_ENABLED", false) {
		return false
	}
	if !allowlistMatches("ROUTE_SHADOW_USER_IDS", request.UserID) ||
		!allowlistMatches("ROUTE_SHADOW_TOKEN_IDS", request.TokenID) {
		return false
	}
	if !modelAllowlistMatches(request.RequestModel) {
		return false
	}
	return true
}

func allowlistMatches(env string, value int) bool {
	allowlist := strings.TrimSpace(common.GetEnvOrDefaultString(env, ""))
	if allowlist == "" {
		return true
	}
	for _, part := range strings.Split(allowlist, ",") {
		if strings.TrimSpace(part) == fmtInt(value) {
			return true
		}
	}
	return false
}

func modelAllowlistMatches(modelName string) bool {
	return modelAllowlistMatchesForEnv("ROUTE_SHADOW_MODELS", modelName)
}

func modelAllowlistMatchesForEnv(env, modelName string) bool {
	allowlist := strings.TrimSpace(common.GetEnvOrDefaultString(env, ""))
	if allowlist == "" {
		return true
	}
	normalized := modellab.NormalizeModel(modelName)
	for _, part := range strings.Split(allowlist, ",") {
		if modellab.NormalizeModel(strings.TrimSpace(part)) == normalized {
			return true
		}
	}
	return false
}

func fmtInt(value int) string {
	if value == 0 {
		return "0"
	}
	return strconv.Itoa(value)
}

type shadowMetrics struct {
	Decisions                  atomic.Uint64
	Diffs                      atomic.Uint64
	Unknown                    atomic.Uint64
	Mixed                      atomic.Uint64
	Unauthorized               atomic.Uint64
	SnapshotStale              atomic.Uint64
	EventsDropped              atomic.Uint64
	RefreshSuccess             atomic.Uint64
	RefreshFailure             atomic.Uint64
	SnapshotConflicts          atomic.Uint64
	EventAttempted             atomic.Uint64
	EventWritten               atomic.Uint64
	EventWriteFailed           atomic.Uint64
	EventEncodeFailed          atomic.Uint64
	ScoreDecisions             atomic.Uint64
	ScoreDiffs                 atomic.Uint64
	ScoreUnavailable           atomic.Uint64
	mu                         sync.Mutex
	DiffReasons                map[string]uint64
	RefreshMu                  sync.Mutex
	RefreshScanMS              []int64
	RefreshPublishMS           []int64
	RefreshDetectionToActiveMS []int64
	ModelMu                    sync.Mutex
	Models                     map[string]shadowModelMetrics
}

type shadowModelMetrics struct {
	Decisions uint64
	Resolved  uint64
}

var routeShadowMetrics = shadowMetrics{
	DiffReasons: make(map[string]uint64),
	Models:      make(map[string]shadowModelMetrics),
}

type RouteShadowMetricsSnapshot struct {
	RouteShadowDecisionsTotal                    uint64            `json:"route_shadow_decisions_total"`
	RouteShadowDiffTotal                         uint64            `json:"route_shadow_diff_total"`
	RouteShadowUnknownTotal                      uint64            `json:"route_shadow_unknown_total"`
	RouteShadowMixedTotal                        uint64            `json:"route_shadow_mixed_total"`
	RouteShadowUnauthorizedTotal                 uint64            `json:"route_shadow_unauthorized_candidate_total"`
	RouteShadowSnapshotStaleTotal                uint64            `json:"route_shadow_snapshot_stale_total"`
	RouteShadowEventDroppedTotal                 uint64            `json:"route_shadow_event_dropped_total"`
	RouteCapabilityRefreshSuccessTotal           uint64            `json:"route_capability_refresh_success_total"`
	RouteCapabilityRefreshFailureTotal           uint64            `json:"route_capability_refresh_failure_total"`
	RouteCapabilitySnapshotConflicts             uint64            `json:"route_capability_snapshot_version_conflict_total"`
	RouteCapabilityRefreshLagSeconds             int64             `json:"route_capability_refresh_lag_seconds"`
	RouteCapabilityRefreshScanP95MS              int64             `json:"route_capability_refresh_scan_p95_ms"`
	RouteCapabilityRefreshPublishP95MS           int64             `json:"route_capability_refresh_publish_p95_ms"`
	RouteCapabilityRefreshDetectionToActiveP95MS int64             `json:"route_capability_refresh_detection_to_active_p95_ms"`
	RouteShadowEventAttemptedTotal               uint64            `json:"route_shadow_event_attempted_total"`
	RouteShadowEventWrittenTotal                 uint64            `json:"route_shadow_event_written_total"`
	RouteShadowEventWriteFailureTotal            uint64            `json:"route_shadow_event_write_failure_total"`
	RouteShadowEventEncodeFailureTotal           uint64            `json:"route_shadow_event_encode_failure_total"`
	RouteScoreShadowDecisionsTotal               uint64            `json:"route_score_shadow_decisions_total"`
	RouteScoreShadowDiffTotal                    uint64            `json:"route_score_shadow_diff_total"`
	RouteScoreShadowUnavailableTotal             uint64            `json:"route_score_shadow_metrics_unavailable_total"`
	RouteLeaseAcquireFailureTotal                uint64            `json:"route_lease_acquire_failure_total"`
	RouteLeaseRenewFailureTotal                  uint64            `json:"route_lease_renew_failure_total"`
	RouteLeaseReleaseFailureTotal                uint64            `json:"route_lease_release_failure_total"`
	DifferenceReasons                            map[string]uint64 `json:"difference_reasons,omitempty"`
}

func RouteShadowMetrics() RouteShadowMetricsSnapshot {
	routeShadowMetrics.mu.Lock()
	reasons := make(map[string]uint64, len(routeShadowMetrics.DiffReasons))
	for key, value := range routeShadowMetrics.DiffReasons {
		reasons[key] = value
	}
	routeShadowMetrics.mu.Unlock()
	return RouteShadowMetricsSnapshot{
		RouteShadowDecisionsTotal:                    routeShadowMetrics.Decisions.Load(),
		RouteShadowDiffTotal:                         routeShadowMetrics.Diffs.Load(),
		RouteShadowUnknownTotal:                      routeShadowMetrics.Unknown.Load(),
		RouteShadowMixedTotal:                        routeShadowMetrics.Mixed.Load(),
		RouteShadowUnauthorizedTotal:                 routeShadowMetrics.Unauthorized.Load(),
		RouteShadowSnapshotStaleTotal:                routeShadowMetrics.SnapshotStale.Load(),
		RouteShadowEventDroppedTotal:                 routeShadowMetrics.EventsDropped.Load(),
		RouteCapabilityRefreshSuccessTotal:           routeShadowMetrics.RefreshSuccess.Load(),
		RouteCapabilityRefreshFailureTotal:           routeShadowMetrics.RefreshFailure.Load(),
		RouteCapabilitySnapshotConflicts:             routeShadowMetrics.SnapshotConflicts.Load(),
		RouteCapabilityRefreshLagSeconds:             RouteCapabilityRefreshLagSeconds(),
		RouteCapabilityRefreshScanP95MS:              routeCapabilityRefreshP95(true),
		RouteCapabilityRefreshPublishP95MS:           routeCapabilityRefreshP95(false),
		RouteCapabilityRefreshDetectionToActiveP95MS: routeCapabilityRefreshDetectionToActiveP95(),
		RouteShadowEventAttemptedTotal:               routeShadowMetrics.EventAttempted.Load(),
		RouteShadowEventWrittenTotal:                 routeShadowMetrics.EventWritten.Load(),
		RouteShadowEventWriteFailureTotal:            routeShadowMetrics.EventWriteFailed.Load(),
		RouteShadowEventEncodeFailureTotal:           routeShadowMetrics.EventEncodeFailed.Load(),
		RouteScoreShadowDecisionsTotal:               routeShadowMetrics.ScoreDecisions.Load(),
		RouteScoreShadowDiffTotal:                    routeShadowMetrics.ScoreDiffs.Load(),
		RouteScoreShadowUnavailableTotal:             routeShadowMetrics.ScoreUnavailable.Load(),
		RouteLeaseAcquireFailureTotal:                RouteLeaseMetrics().AcquireFailures,
		RouteLeaseRenewFailureTotal:                  RouteLeaseMetrics().RenewFailures,
		RouteLeaseReleaseFailureTotal:                RouteLeaseMetrics().ReleaseFailures,
		DifferenceReasons:                            reasons,
	}
}

func observeShadowEventAttempt() {
	routeShadowMetrics.EventAttempted.Add(1)
}

func observeShadowEventWritten() {
	routeShadowMetrics.EventWritten.Add(1)
}

func observeShadowEventWriteFailure() {
	routeShadowMetrics.EventWriteFailed.Add(1)
}

func observeShadowEventEncodeFailure() {
	routeShadowMetrics.EventEncodeFailed.Add(1)
}

func observeCapabilityRefreshDurations(scan, publish time.Duration) {
	routeShadowMetrics.RefreshMu.Lock()
	defer routeShadowMetrics.RefreshMu.Unlock()
	const maxSamples = 256
	if scan >= 0 {
		routeShadowMetrics.RefreshScanMS = appendBoundedDuration(routeShadowMetrics.RefreshScanMS, scan, maxSamples)
	}
	if publish >= 0 {
		routeShadowMetrics.RefreshPublishMS = appendBoundedDuration(routeShadowMetrics.RefreshPublishMS, publish, maxSamples)
	}
}

func observeCapabilityRefreshDetectionToActive(duration time.Duration) {
	if duration < 0 {
		return
	}
	routeShadowMetrics.RefreshMu.Lock()
	defer routeShadowMetrics.RefreshMu.Unlock()
	routeShadowMetrics.RefreshDetectionToActiveMS = appendBoundedDuration(
		routeShadowMetrics.RefreshDetectionToActiveMS,
		duration,
		256,
	)
}

func appendBoundedDuration(samples []int64, duration time.Duration, maxSamples int) []int64 {
	samples = append(samples, duration.Milliseconds())
	if len(samples) > maxSamples {
		return samples[len(samples)-maxSamples:]
	}
	return samples
}

func routeCapabilityRefreshP95(scan bool) int64 {
	routeShadowMetrics.RefreshMu.Lock()
	var samples []int64
	if scan {
		samples = append(samples, routeShadowMetrics.RefreshScanMS...)
	} else {
		samples = append(samples, routeShadowMetrics.RefreshPublishMS...)
	}
	routeShadowMetrics.RefreshMu.Unlock()
	if len(samples) == 0 {
		return 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	index := (len(samples)*95 + 99) / 100
	if index < 1 {
		index = 1
	}
	if index > len(samples) {
		index = len(samples)
	}
	return samples[index-1]
}

func routeCapabilityRefreshDetectionToActiveP95() int64 {
	routeShadowMetrics.RefreshMu.Lock()
	samples := append([]int64(nil), routeShadowMetrics.RefreshDetectionToActiveMS...)
	routeShadowMetrics.RefreshMu.Unlock()
	if len(samples) == 0 {
		return 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	index := (len(samples)*95 + 99) / 100
	if index < 1 {
		index = 1
	}
	if index > len(samples) {
		index = len(samples)
	}
	return samples[index-1]
}

func observeShadowDecision(decision RouteShadowDecision) {
	routeShadowMetrics.Decisions.Add(1)
	if decision.ScoreShadowEnabled {
		routeShadowMetrics.ScoreDecisions.Add(1)
		if decision.ScoreShadowError != "" {
			routeShadowMetrics.ScoreUnavailable.Add(1)
		}
		if decision.ScoreShadowDifference != "" && decision.ScoreShadowDifference != RouteScoreShadowSameChannel {
			routeShadowMetrics.ScoreDiffs.Add(1)
		}
	}
	if shadowDecisionHasDifference(decision.DifferenceReasons) {
		routeShadowMetrics.Diffs.Add(1)
	}
	if len(decision.DifferenceReasons) > 0 {
		routeShadowMetrics.mu.Lock()
		for _, reason := range decision.DifferenceReasons {
			routeShadowMetrics.DiffReasons[reason]++
		}
		routeShadowMetrics.mu.Unlock()
	}
	if decision.HasUnknown {
		routeShadowMetrics.Unknown.Add(1)
	}
	if decision.HasMixed {
		routeShadowMetrics.Mixed.Add(1)
	}
	unauthorizedCandidates := decision.FilterReasonCounts[ShadowFilterGroupForbidden] +
		decision.FilterReasonCounts[ShadowFilterTokenForbidden] +
		decision.FilterReasonCounts[ShadowFilterEntitlementRevoked]
	if unauthorizedCandidates > 0 {
		routeShadowMetrics.Unauthorized.Add(uint64(unauthorizedCandidates))
	}
	if decision.FilterReasonCounts[ShadowFilterSnapshotStale] > 0 {
		routeShadowMetrics.SnapshotStale.Add(1)
	}
	if decision.NormalizedRequestModel != "" {
		routeShadowMetrics.ModelMu.Lock()
		stats := routeShadowMetrics.Models[decision.NormalizedRequestModel]
		stats.Decisions++
		if decision.LabSlug != "" {
			stats.Resolved++
		}
		routeShadowMetrics.Models[decision.NormalizedRequestModel] = stats
		routeShadowMetrics.ModelMu.Unlock()
	}
}

func routeShadowModelMetrics() map[string]shadowModelMetrics {
	routeShadowMetrics.ModelMu.Lock()
	defer routeShadowMetrics.ModelMu.Unlock()
	result := make(map[string]shadowModelMetrics, len(routeShadowMetrics.Models))
	for modelName, stats := range routeShadowMetrics.Models {
		result[modelName] = stats
	}
	return result
}

func shadowDecisionHasDifference(reasons []string) bool {
	for _, reason := range reasons {
		if reason != ShadowReasonSameChannel {
			return true
		}
	}
	return false
}
