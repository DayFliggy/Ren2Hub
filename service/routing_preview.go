package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/pkg/relaycapability"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
)

const (
	RoutePreviewSelectionOrdered  = "ordered"
	RoutePreviewSelectionWeighted = "weighted"

	RoutePreviewFilterActiveGroupMissing = "active_group_missing"
	RoutePreviewFilterProfileDisabled    = "profile_disabled"
	RoutePreviewFilterGroupDisabled      = "group_disabled"
	RoutePreviewFilterEntryDisabled      = "entry_disabled"
	RoutePreviewFilterChannelMissing     = "channel_not_found"
	RoutePreviewFilterSourceUnsupported  = "source_unsupported"
	RoutePreviewFilterConversion         = "conversion_unavailable"
)

// RouteProfilePreviewInput is intentionally limited to request facts that are
// needed for a static manual-route preview. It never selects or reserves a
// live channel.
type RouteProfilePreviewInput struct {
	Model   string          `json:"model"`
	Path    string          `json:"path"`
	Request json.RawMessage `json:"request,omitempty"`
}

type RouteProfilePreview struct {
	TokenID                int                              `json:"token_id"`
	Mode                   string                           `json:"mode"`
	PreviewID              string                           `json:"preview_id"`
	GeneratedAt            int64                            `json:"generated_at"`
	ProfileID              int                              `json:"profile_id"`
	ProfileVersion         int64                            `json:"profile_version"`
	RequestModel           string                           `json:"request_model"`
	NormalizedModel        string                           `json:"normalized_model"`
	Path                   string                           `json:"path"`
	EndpointType           string                           `json:"endpoint_type"`
	ActiveGroup            *model.UserRouteGroup            `json:"active_group,omitempty"`
	Policy                 *model.RoutePolicy               `json:"policy,omitempty"`
	Entries                []RoutePreviewEntry              `json:"entries"`
	CandidateChannelIDs    []int                            `json:"candidate_channel_ids"`
	SelectionMode          string                           `json:"selection_mode"`
	PreferredChannelID     int                              `json:"preferred_channel_id,omitempty"`
	FilterReasonCounts     map[string]int                   `json:"filter_reason_counts"`
	HasMixed               bool                             `json:"has_mixed"`
	RuntimeRecheckRequired bool                             `json:"runtime_recheck_required"`
	RuntimeRecheckReasons  []string                         `json:"runtime_recheck_reasons"`
	LiveSelection          bool                             `json:"live_selection"`
	RequestRequirements    relayconvert.RequestRequirements `json:"request_requirements"`
	ConversionPolicy       types.ConversionLossPolicy       `json:"conversion_policy"`
}

type RoutePreviewEntry struct {
	Priority        int64                        `json:"priority"`
	Score           *RouteScoreBreakdown         `json:"score_breakdown,omitempty"`
	EntryID         int                          `json:"entry_id"`
	ChannelID       int                          `json:"channel_id"`
	Position        int                          `json:"position"`
	Weight          int                          `json:"weight"`
	RequestModel    string                       `json:"request_model"`
	ActualModel     string                       `json:"actual_model"`
	LabSlug         string                       `json:"lab_slug"`
	SnapshotVersion int64                        `json:"snapshot_version"`
	CatalogVersion  string                       `json:"catalog_version"`
	CapabilityState string                       `json:"capability_state"`
	Health          RoutePreviewHealthSummary    `json:"health"`
	FilterReason    string                       `json:"filter_reason,omitempty"`
	ConversionPlan  *relayconvert.ConversionPlan `json:"conversion_plan,omitempty"`
	Diagnostics     []types.ConversionDiagnostic `json:"diagnostics"`
}

// RoutePreviewHealthSummary is the non-sensitive aggregate health state for
// one channel and canonical request model. Key-scoped observations remain
// internal and are intentionally not exposed through user routing APIs.
type RoutePreviewHealthSummary struct {
	State               string `json:"state"`
	FailureCount        int    `json:"failure_count"`
	CooldownUntil       int64  `json:"cooldown_until"`
	HealthEpoch         int64  `json:"health_epoch"`
	LastLatencyMS       int64  `json:"last_latency_ms"`
	FirstTokenLatencyMS int64  `json:"first_token_latency_ms"`
	UpdatedAt           int64  `json:"updated_at"`
}

type RouteCapabilityUserAccess struct {
	Enabled bool
	Allowed bool
}

// loadManualRoutePreviewContext preserves legacy group and excluded-entry metadata.
// Qualification and candidate ordering are supplied by SelectLiveTokenRoute.
func loadManualRoutePreviewContext(ctx context.Context, userID, profileID int, input RouteProfilePreviewInput, requirements relayconvert.RequestRequirements) (*RouteProfilePreview, error) {
	input.Model = strings.TrimSpace(input.Model)
	input.Path = strings.TrimSpace(input.Path)
	normalizedModel := modellab.NormalizeModel(input.Model)
	if normalizedModel == "" || input.Path == "" {
		return nil, fmt.Errorf("%w: model and path are required", ErrRouteProfileValidation)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	view, err := GetUserRouteProfile(userID, profileID)
	if err != nil {
		return nil, err
	}
	if view.Profile.Mode != model.RouteModeManual {
		return nil, fmt.Errorf("%w: manual route profile is required", ErrRouteProfileValidation)
	}
	var token model.Token
	if err := model.DB.WithContext(ctx).Where("id = ? AND user_id = ?", view.Profile.TokenID, userID).First(&token).Error; err != nil {
		return nil, err
	}
	conversionPolicy := types.ConversionLossPolicy(token.ConversionPolicy)
	if conversionPolicy == "" {
		conversionPolicy = types.ConversionLossPolicySafe
	}
	if err := conversionPolicy.Validate(); err != nil {
		return nil, fmt.Errorf("%w: invalid token conversion policy", ErrRouteProfileValidation)
	}
	preview := &RouteProfilePreview{
		ProfileID:              view.Profile.ID,
		ProfileVersion:         view.Profile.Version,
		RequestModel:           input.Model,
		NormalizedModel:        normalizedModel,
		Path:                   input.Path,
		EndpointType:           endpointTypeForRequestPath(input.Path),
		Entries:                make([]RoutePreviewEntry, 0),
		CandidateChannelIDs:    make([]int, 0),
		SelectionMode:          RoutePreviewSelectionOrdered,
		FilterReasonCounts:     make(map[string]int),
		RuntimeRecheckRequired: true,
		RuntimeRecheckReasons:  []string{"price_qualification", "quota_qualification", "security_policy"},
		LiveSelection:          false,
		RequestRequirements:    requirements,
		ConversionPolicy:       conversionPolicy,
	}
	if requirements.NativeRequired {
		preview.RuntimeRecheckReasons = append(preview.RuntimeRecheckReasons, "state_ownership")
	}
	activeGroup := findActiveRouteGroup(view)
	if activeGroup == nil {
		preview.FilterReasonCounts[RoutePreviewFilterActiveGroupMissing]++
		return preview, nil
	}
	group := activeGroup.Group
	policy := activeGroup.Policy
	preview.ActiveGroup = &group
	preview.Policy = &policy

	channelIDs := routeGroupChannelIDs(activeGroup.Entries)
	channels, err := loadRoutePreviewChannels(ctx, channelIDs)
	if err != nil {
		return nil, err
	}
	capabilities, err := model.FindActiveChannelCapabilities(ctx, channelIDs, normalizedModel, "")
	if err != nil {
		return nil, err
	}
	activeSnapshots, err := loadRoutePreviewSnapshots(ctx, channelIDs)
	if err != nil {
		return nil, err
	}
	abilityAccess, err := FindUserRouteCapabilityAccess(ctx, userID, capabilities)
	if err != nil {
		return nil, err
	}
	entitlements, err := loadRoutePreviewEntitlements(ctx, userID, channelIDs)
	if err != nil {
		return nil, err
	}
	healthByChannel, err := loadRoutePreviewHealth(ctx, channelIDs, normalizedModel)
	if err != nil {
		return nil, err
	}
	capabilityByChannel := make(map[int]model.ChannelModelCapability, len(capabilities))
	for _, capability := range capabilities {
		capabilityByChannel[capability.ChannelID] = capability
	}

	now := time.Now()
	for _, entry := range activeGroup.Entries {
		item := RoutePreviewEntry{
			EntryID:         entry.ID,
			ChannelID:       entry.ChannelID,
			Position:        entry.Position,
			Weight:          entry.Weight,
			RequestModel:    normalizedModel,
			CapabilityState: model.RouteCapabilityStateUnresolved,
			Health:          defaultRoutePreviewHealthSummary(),
			Diagnostics:     make([]types.ConversionDiagnostic, 0),
		}
		health := healthByChannel[entry.ChannelID]
		if health.ID != 0 {
			item.Health = routePreviewHealthSummary(health)
		}
		activeSnapshot := activeSnapshots[entry.ChannelID]
		item.FilterReason = routePreviewFilterReason(routePreviewFilterInput{
			Profile:         view.Profile,
			Group:           group,
			Entry:           entry,
			Channel:         channels[entry.ChannelID],
			ChannelExists:   channels[entry.ChannelID].Id > 0,
			Capability:      capabilityByChannel[entry.ChannelID],
			SnapshotVersion: activeSnapshot.ActiveVersion,
			Ability:         abilityAccess[capabilityByChannel[entry.ChannelID].ID],
			Entitlement:     entitlements[entry.ChannelID],
			Token:           token,
			RequestModel:    input.Model,
			NormalizedModel: normalizedModel,
			RequestPath:     input.Path,
			EndpointType:    preview.EndpointType,
		})
		if item.FilterReason == "" && !RouteHealthReadOnlyUsable(health, now) {
			item.FilterReason = RouteCandidateFilterHealthUnavailable
		}
		if item.FilterReason == "" {
			channel := channels[entry.ChannelID]
			hasAvailableKey, keyErr := RouteChannelHasReadOnlyAvailableKey(ctx, &channel, normalizedModel, now)
			if keyErr != nil {
				return nil, keyErr
			}
			if !hasAvailableKey {
				item.FilterReason = RouteFilterKeyUnavailable
			}
		}
		item.SnapshotVersion = activeSnapshot.ActiveVersion
		item.CatalogVersion = activeSnapshot.CatalogVersion
		if capability, ok := capabilityByChannel[entry.ChannelID]; ok {
			preview.HasMixed = preview.HasMixed || capability.IsMixed
			item.ActualModel = capability.ActualModel
			item.LabSlug = capability.LabSlug
			item.SnapshotVersion = capability.SnapshotVersion
			item.CatalogVersion = capability.CatalogVersion
			item.CapabilityState = capability.State
		}

		if item.FilterReason != "" {
			preview.FilterReasonCounts[item.FilterReason]++
		}
		preview.Entries = append(preview.Entries, item)
	}

	if policy.LoadBalance {
		preview.SelectionMode = RoutePreviewSelectionWeighted
	}
	return preview, nil
}

func analyzeRoutePreviewRequest(input RouteProfilePreviewInput) (relayconvert.RequestRequirements, error) {
	if len(input.Request) > 1024*1024 {
		return relayconvert.RequestRequirements{}, fmt.Errorf("request sample exceeds 1 MiB")
	}
	return AnalyzeRouteRequest(input.Model, input.Path, input.Request)
}

// AnalyzeRouteRequest shares request semantics between preview and dispatch.
// The caller owns body-size enforcement; analysis never resolves media.
func AnalyzeRouteRequest(modelName, path string, payload []byte) (relayconvert.RequestRequirements, error) {
	if !SupportsRouteRequestAnalysis(path) {
		return relayconvert.RequestRequirements{}, fmt.Errorf("request path does not support conversion preview")
	}
	var request any
	switch constant.EndpointType(endpointTypeForRequestPath(path)) {
	case constant.EndpointTypeOpenAI:
		request = &dto.GeneralOpenAIRequest{}
	case constant.EndpointTypeAnthropic:
		request = &dto.ClaudeRequest{}
	case constant.EndpointTypeGemini:
		request = &dto.GeminiChatRequest{}
	case constant.EndpointTypeOpenAIResponse:
		request = &dto.OpenAIResponsesRequest{}
	case constant.EndpointTypeOpenAIResponseCompact:
		request = &dto.OpenAIResponsesCompactionRequest{}
	case constant.EndpointTypeEmbeddings:
		request = &dto.EmbeddingRequest{}
	case constant.EndpointTypeJinaRerank:
		request = &dto.RerankRequest{}
	case constant.EndpointTypeImageGeneration:
		request = &dto.ImageRequest{}
	case constant.EndpointTypeAudio:
		request = &dto.AudioRequest{}
	default:
		return relayconvert.RequestRequirements{}, fmt.Errorf("request path does not support conversion preview")
	}
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	var object map[string]json.RawMessage
	if err := common.Unmarshal(payload, &object); err != nil || object == nil {
		return relayconvert.RequestRequirements{}, fmt.Errorf("request sample must be a JSON object")
	}
	if err := common.Unmarshal(payload, request); err != nil {
		return relayconvert.RequestRequirements{}, fmt.Errorf("request sample does not match the request protocol")
	}
	requirements, err := relayconvert.AnalyzeRequestJSON(request, payload)
	if err != nil {
		return requirements, err
	}
	bodyModel, routeModel := requirements.Model, modelName
	if bodyModel != "" && modellab.NormalizeModel(bodyModel) != modellab.NormalizeModel(routeModel) {
		return requirements, fmt.Errorf("request sample model must match the preview model")
	}
	requirements.Model = modelName
	if operation := relaycapability.LegacyOpenAIOperation(path); operation != "" {
		requirements.Operation = operation
	}
	if requirements.Format == types.RelayFormatGemini {
		_, operation, hasOperation := strings.Cut(path, ":")
		if !hasOperation || (operation != "generateContent" && operation != "streamGenerateContent") {
			return requirements, fmt.Errorf("request path does not match the Gemini generation protocol")
		}
		_, pathModel, hasModel := strings.Cut(strings.Split(path, ":")[0], "/models/")
		if !hasModel || modellab.NormalizeModel(pathModel) != modellab.NormalizeModel(modelName) {
			return requirements, fmt.Errorf("request path model must match the preview model")
		}
		requirements.Stream = operation == "streamGenerateContent"
	}
	return requirements, nil
}

// SupportsRouteRequestAnalysis describes concrete generation paths, not path
// prefixes. Preview and dispatch must analyze the same request surfaces.
func SupportsRouteRequestAnalysis(path string) bool {
	switch path {
	case "/v1/chat/completions", "/pg/chat/completions", "/v1/responses", "/v1/responses/compact", "/v1/messages",
		"/v1/completions", "/v1/moderations", "/v1/edits", "/v1/embeddings", "/v1/rerank",
		"/v1/images/generations", "/v1/images/edits", "/v1/audio/speech", "/v1/audio/transcriptions", "/v1/audio/translations":
		return true
	}
	for _, prefix := range []string{"/v1beta/models/", "/v1/models/"} {
		if modelAndOperation, ok := strings.CutPrefix(path, prefix); ok {
			modelName, operation, found := strings.Cut(modelAndOperation, ":")
			return found && modelName != "" && !strings.ContainsAny(modelName, "?#") &&
				(operation == "generateContent" || operation == "streamGenerateContent")
		}
	}
	return false
}

// PlanChannelConversion resolves the same read-only host surface for preview
// and execution. It does not initialize credentials, download media or debit.
func PlanChannelConversion(requirements relayconvert.RequestRequirements, channel *model.Channel, path, actualModel string, policy types.ConversionLossPolicy) (relayconvert.ConversionPlan, error) {
	if operation := relaycapability.LegacyOpenAIOperation(path); operation != "" {
		requirements.Operation = operation
	}
	plan := relayconvert.ConversionPlan{From: requirements.Format, Operation: requirements.Operation, Policy: policy}
	apiType, registered := common.ChannelType2APIType(channel.Type)
	if !registered {
		return plan, fmt.Errorf("channel has no registered request adaptor")
	}
	definition, found := relaycapability.DescribeForCredentials(apiType, actualModel, channel.GetKeys(), channel.GetOtherSettings())
	if !found {
		return plan, fmt.Errorf("channel has no registered request adaptor")
	}
	surface, supported := definition.Resolve(requirements.Format)
	if apiType == constant.APITypeAdvancedCustom {
		config := channel.GetOtherSettings().AdvancedCustom
		if config == nil {
			return plan, fmt.Errorf("custom channel has no configured routes")
		}
		route, matched := config.MatchPathForModel(strings.Split(path, "?")[0], requirements.Model)
		if !matched {
			return plan, fmt.Errorf("custom channel does not implement this request path")
		}
		surface = relaycapability.Surface{Incoming: requirements.Format, Target: requirements.Format, Native: true, Stream: true}
		if operation := relaycapability.LegacyOpenAIOperation(path); operation != "" && operation != "edit" {
			surface.NativeOperations = []string{operation}
		}
		if route.Converter == dto.AdvancedCustomConverterSGLangRerank {
			if requirements.Format != types.RelayFormatRerank {
				return plan, fmt.Errorf("SGLang rerank route requires rerank request")
			}
		} else if route.Converter != "" && route.Converter != relayconvert.ConverterNone {
			spec, exists := relayconvert.LookupRequestConverter(route.Converter)
			if !exists || spec.From != requirements.Format || !dto.IsAdvancedCustomConverterAllowed(route.Converter) {
				return plan, fmt.Errorf("custom channel conversion does not support this request protocol")
			}
			surface.Target, surface.Native = spec.To, false
		}
		supported = true
	}
	if !supported {
		return plan, fmt.Errorf("channel does not implement %s requests", requirements.Format)
	}
	if !surface.SupportsPath(path) {
		return plan, fmt.Errorf("channel does not implement this request path")
	}
	if requirements.Operation == "completion" && apiType == constant.APITypeMoonshot {
		if _, special := constant.ChannelSpecialBases[channel.GetBaseURL()]; special {
			return plan, fmt.Errorf("channel endpoint does not implement legacy completions")
		}
	}
	if requirements.Format == types.RelayFormatClaude && apiType == constant.APITypeVolcEngine {
		if _, native := constant.ChannelSpecialBases[channel.GetBaseURL()]; native {
			surface.Target, surface.Native = types.RelayFormatClaude, true
		}
	}
	if requirements.Format == types.RelayFormatClaude && apiType == constant.APITypeAli {
		for _, pattern := range strings.Split(common.GetEnvOrDefaultString("ALI_ANTHROPIC_MESSAGES_MODELS", "qwen,deepseek-v4,kimi,glm,minimax-m"), ",") {
			if pattern = strings.ToLower(strings.TrimSpace(pattern)); pattern != "" && strings.Contains(strings.ToLower(actualModel), pattern) {
				surface.Target, surface.Native = types.RelayFormatClaude, true
				break
			}
		}
	}
	passthrough := model_setting.GetGlobalSettings().PassThroughRequestEnabled || channel.GetSetting().PassThroughBodyEnabled
	if apiType == constant.APITypeAdvancedCustom && channel.GetOtherSettings().AdvancedCustom.BodyPassThroughEnabled(path, requirements.Model) {
		passthrough = true
	}
	if requirements.Operation == "text" && (requirements.Format == types.RelayFormatOpenAI || requirements.Format == types.RelayFormatClaude) && !passthrough &&
		ShouldChatCompletionsUseResponsesGlobal(channel.Id, channel.Type, requirements.Model) {
		if _, supported := definition.Resolve(types.RelayFormatOpenAIResponses); !supported {
			return plan, fmt.Errorf("configured Responses upgrade is not implemented by selected channel")
		}
		surface.Target, surface.Native = types.RelayFormatOpenAIResponses, false
	}
	if passthrough && !surface.Native {
		plan.To = surface.Target
		return plan, fmt.Errorf("raw request passthrough requires a native protocol surface")
	}
	if requirements.NativeRequired && !surface.Native {
		plan.To = surface.Target
		plan.Diagnostics = append(plan.Diagnostics, types.ConversionDiagnostic{
			Code: "native_state_required", Path: strings.Join(requirements.NativeState, ","),
			Message: "provider-owned state requires a native protocol surface", Severity: types.ConversionDiagnosticError,
			From: requirements.Format, To: surface.Target,
		})
		return plan, &types.ConversionLossError{Diagnostics: plan.Diagnostics}
	}
	if slices.Contains(requirements.NativeState, "prompt") && !relaycapability.SupportsReferenceDomain(channel.Type, surface.Target, "openai_prompt") {
		plan.To = surface.Target
		plan.Diagnostics = append(plan.Diagnostics, types.ConversionDiagnostic{
			Code: "reference_domain_unsupported", Path: "prompt", Message: "channel does not implement the OpenAI prompt reference domain",
			Severity: types.ConversionDiagnosticError, From: requirements.Format, To: surface.Target,
		})
		return plan, &types.ConversionLossError{Diagnostics: plan.Diagnostics}
	}
	if passthrough && surface.Native {
		requirements.Diagnostics = slices.DeleteFunc(slices.Clone(requirements.Diagnostics), func(diagnostic types.ConversionDiagnostic) bool {
			return diagnostic.Code == "unknown_request_field"
		})
	}
	plan, err := relayconvert.PlanConversion(requirements, surface.Target, policy)
	if err != nil {
		return plan, err
	}
	plan.StreamSupported = plan.StreamSupported && surface.Stream
	plan.Native = plan.Native && surface.Native
	if apiType == constant.APITypeVertexAi && surface.Target == types.RelayFormatGemini &&
		model_setting.GetGeminiSettings().RemoveFunctionResponseIdEnabled {
		diagnostics, projectionErr := requirements.ToolIdentity.CheckNameOnly(requirements.Format, plan.Policy)
		plan.Diagnostics = append(plan.Diagnostics, diagnostics...)
		if projectionErr != nil {
			return plan, projectionErr
		}
	}
	if surface.TextProjection != nil {
		plan.RequestProjection = surface.TextProjection.ID
		if channel.GetSetting().SystemPrompt != "" && !slices.Contains(surface.TextProjection.Roles, (&dto.GeneralOpenAIRequest{Model: actualModel}).GetSystemRoleName()) {
			plan.Diagnostics = append(plan.Diagnostics, types.ConversionDiagnostic{
				Code: "channel_system_prompt_unsupported", Path: "channel.system_prompt",
				Message: "selected projection cannot preserve the channel system prompt", Severity: types.ConversionDiagnosticError,
				From: requirements.Format, To: surface.Target,
			})
			return plan, &types.ConversionLossError{Diagnostics: plan.Diagnostics}
		}
		diagnostics, projectionErr := surface.TextProjection.Check(requirements, plan.Policy)
		plan.Diagnostics = append(plan.Diagnostics, diagnostics...)
		if projectionErr != nil {
			return plan, projectionErr
		}
	}
	if requirements.Stream && !plan.StreamSupported && (requirements.Operation == "text" || requirements.Operation == "completion" || requirements.Operation == "audio") {
		return plan, fmt.Errorf("channel does not implement streaming responses for this request")
	}
	return plan, nil
}

func findActiveRouteGroup(view *RouteProfileView) *RouteGroupView {
	if view == nil || view.Profile.ActiveGroupID == nil {
		return nil
	}
	for index := range view.Groups {
		if view.Groups[index].Group.ID == *view.Profile.ActiveGroupID {
			return &view.Groups[index]
		}
	}
	return nil
}

func routeGroupChannelIDs(entries []model.UserRouteEntry) []int {
	seen := make(map[int]struct{}, len(entries))
	channelIDs := make([]int, 0, len(entries))
	for _, entry := range entries {
		if entry.ChannelID <= 0 {
			continue
		}
		if _, exists := seen[entry.ChannelID]; exists {
			continue
		}
		seen[entry.ChannelID] = struct{}{}
		channelIDs = append(channelIDs, entry.ChannelID)
	}
	sort.Ints(channelIDs)
	return channelIDs
}

func loadRoutePreviewChannels(ctx context.Context, channelIDs []int) (map[int]model.Channel, error) {
	channels := make(map[int]model.Channel, len(channelIDs))
	if len(channelIDs) == 0 {
		return channels, nil
	}
	var rows []model.Channel
	if err := model.DB.WithContext(ctx).Where("id IN ?", channelIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, channel := range rows {
		channels[channel.Id] = channel
	}
	return channels, nil
}

type routePreviewSnapshot struct {
	ActiveVersion  int64
	CatalogVersion string
}

func loadRoutePreviewSnapshots(ctx context.Context, channelIDs []int) (map[int]routePreviewSnapshot, error) {
	snapshots := make(map[int]routePreviewSnapshot, len(channelIDs))
	if len(channelIDs) == 0 {
		return snapshots, nil
	}
	rows, err := model.FindActiveChannelCapabilitySnapshots(ctx, channelIDs)
	if err != nil {
		return nil, err
	}
	for _, snapshot := range rows {
		snapshots[snapshot.ChannelID] = routePreviewSnapshot{
			ActiveVersion:  snapshot.ActiveVersion,
			CatalogVersion: snapshot.CatalogVersion,
		}
	}
	return snapshots, nil
}

// FindUserRouteCapabilityAccess checks current model availability against
// immutable capability rows. Retired Ability groups do not authorize models;
// callers enforce Token scope and channel entitlement separately.
func FindUserRouteCapabilityAccess(ctx context.Context, userID int, capabilities []model.ChannelModelCapability) (map[int]RouteCapabilityUserAccess, error) {
	access := make(map[int]RouteCapabilityUserAccess, len(capabilities))
	if len(capabilities) == 0 {
		return access, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var user model.User
	if err := model.DB.WithContext(ctx).Select("id").Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}
	capabilityIDsByChannelModel := make(map[int]map[string][]int, len(capabilities))
	channelIDs := make([]int, 0, len(capabilities))
	seenChannels := make(map[int]struct{}, len(capabilities))
	for _, capability := range capabilities {
		requestModel := modellab.NormalizeModel(capability.RequestModel)
		if capability.ID <= 0 || capability.ChannelID <= 0 || requestModel == "" {
			continue
		}
		if _, exists := capabilityIDsByChannelModel[capability.ChannelID]; !exists {
			capabilityIDsByChannelModel[capability.ChannelID] = make(map[string][]int)
		}
		capabilityIDsByChannelModel[capability.ChannelID][requestModel] = append(capabilityIDsByChannelModel[capability.ChannelID][requestModel], capability.ID)
		if _, exists := seenChannels[capability.ChannelID]; !exists {
			seenChannels[capability.ChannelID] = struct{}{}
			channelIDs = append(channelIDs, capability.ChannelID)
		}
	}
	var rows []model.Ability
	if err := model.DB.WithContext(ctx).Where("channel_id IN ?", channelIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, ability := range rows {
		capabilityIDs := capabilityIDsByChannelModel[ability.ChannelId][modellab.NormalizeModel(ability.Model)]
		if len(capabilityIDs) == 0 || !ability.Enabled {
			continue
		}
		for _, capabilityID := range capabilityIDs {
			value := access[capabilityID]
			value.Enabled = true
			value.Allowed = true
			access[capabilityID] = value
		}
	}
	return access, nil
}

func loadRoutePreviewEntitlements(ctx context.Context, userID int, channelIDs []int) (map[int]model.UserChannelEntitlement, error) {
	entitlements := make(map[int]model.UserChannelEntitlement, len(channelIDs))
	if len(channelIDs) == 0 {
		return entitlements, nil
	}
	var rows []model.UserChannelEntitlement
	if err := model.DB.WithContext(ctx).Where("user_id = ? AND channel_id IN ? AND source = ?", userID, channelIDs, model.RouteSourcePlatform).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, entitlement := range rows {
		entitlements[entitlement.ChannelID] = entitlement
	}
	return entitlements, nil
}

func loadRoutePreviewHealth(ctx context.Context, channelIDs []int, canonicalModel string) (map[int]model.ChannelHealth, error) {
	healthByChannel := make(map[int]model.ChannelHealth, len(channelIDs))
	if len(channelIDs) == 0 {
		return healthByChannel, nil
	}
	var rows []model.ChannelHealth
	if err := model.DB.WithContext(ctx).
		Where("channel_id IN ? AND model = ? AND key_scope = ?", channelIDs, canonicalModel, "").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, health := range rows {
		healthByChannel[health.ChannelID] = health
	}
	return healthByChannel, nil
}

func defaultRoutePreviewHealthSummary() RoutePreviewHealthSummary {
	return RoutePreviewHealthSummary{
		State:       model.RouteHealthStateClosed,
		HealthEpoch: 1,
	}
}

func routePreviewHealthSummary(health model.ChannelHealth) RoutePreviewHealthSummary {
	summary := RoutePreviewHealthSummary{
		State:               health.State,
		FailureCount:        health.FailureCount,
		CooldownUntil:       health.CooldownUntil,
		HealthEpoch:         health.HealthEpoch,
		LastLatencyMS:       health.LastLatencyMS,
		FirstTokenLatencyMS: health.FirstTokenLatencyMS,
		UpdatedAt:           health.UpdatedAt,
	}
	if summary.State == "" {
		summary.State = model.RouteHealthStateClosed
	}
	if summary.HealthEpoch <= 0 {
		summary.HealthEpoch = 1
	}
	return summary
}

type routePreviewFilterInput struct {
	Profile         model.UserRouteProfile
	Group           model.UserRouteGroup
	Entry           model.UserRouteEntry
	Channel         model.Channel
	ChannelExists   bool
	Capability      model.ChannelModelCapability
	SnapshotVersion int64
	Ability         RouteCapabilityUserAccess
	Entitlement     model.UserChannelEntitlement
	Token           model.Token
	RequestModel    string
	NormalizedModel string
	RequestPath     string
	EndpointType    string
}

func routePreviewFilterReason(input routePreviewFilterInput) string {
	if input.Profile.Status != model.RouteProfileStatusEnabled {
		return RoutePreviewFilterProfileDisabled
	}
	if !input.Group.Enabled {
		return RoutePreviewFilterGroupDisabled
	}
	if !input.Entry.Enabled {
		return RoutePreviewFilterEntryDisabled
	}
	if input.Entry.Source != model.RouteSourcePlatform {
		return RoutePreviewFilterSourceUnsupported
	}
	if !input.ChannelExists {
		return RoutePreviewFilterChannelMissing
	}
	if input.Token.Status != common.TokenStatusEnabled ||
		(input.Token.ExpiredTime != -1 && input.Token.ExpiredTime <= common.GetTimestamp()) {
		return RouteFilterTokenForbidden
	}
	entitled := input.Entitlement.ID == 0 || entitlementIsActive(input.Entitlement)
	result := filterRouteCapability(routeCapabilityFilterInput{
		Capability:      input.Capability,
		SnapshotVersion: input.SnapshotVersion,
		ChannelStatus:   input.Channel.Status,
		ChannelType:     input.Channel.Type,
		AbilityEnabled:  input.Ability.Enabled,
		Token:           input.Token,
		RequestModel:    input.RequestModel,
		NormalizedModel: input.NormalizedModel,
		RequestPath:     input.RequestPath,
		EndpointType:    input.EndpointType,
		Entitled:        entitled,
		// Price, quota, and request-body security facts are not available to a
		// configuration-only preview. The response exposes a runtime recheck
		// requirement instead of treating this entry as fully qualified.
		PriceEligibilityKnown:    false,
		SecurityEligibilityKnown: false,
		RequireSnapshot:          true,
		RequireEndpoint:          true,
	})
	return result.Reason
}

func routeTokenUsable(token model.Token) bool {
	return token.Status == common.TokenStatusEnabled &&
		(token.ExpiredTime == -1 || token.ExpiredTime > common.GetTimestamp())
}

func entitlementIsActive(entitlement model.UserChannelEntitlement) bool {
	return entitlement.Status == model.RouteEntitlementStatusEnabled &&
		entitlement.RevokedAt == 0 &&
		(entitlement.ExpiresAt == 0 || entitlement.ExpiresAt > time.Now().Unix())
}
