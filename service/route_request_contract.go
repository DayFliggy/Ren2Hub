package service

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

const (
	RouteFilterSnapshotUnavailable = "snapshot_unavailable"
	RouteFilterSnapshotStale       = "snapshot_stale"
	RouteFilterUnknownCapability   = "unknown_capability"
	RouteFilterUnsupported         = "unsupported_capability"
	RouteFilterChannelDisabled     = "channel_disabled"
	RouteFilterAbilityDisabled     = "ability_disabled"
	RouteFilterTokenForbidden      = "token_model_forbidden"
	RouteFilterPathUnsupported     = "path_unsupported"
	RouteFilterPriceForbidden      = "price_forbidden"
	RouteFilterSecurityForbidden   = "security_forbidden"
	RouteFilterEntitlementRevoked  = "entitlement_revoked"
	RouteFilterMappingConflict     = "mapping_conflict"
)

func stringListContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func tokenAllowsRouteModel(limit map[string]bool, modelName string) bool {
	return limit[modelName] || limit[ratio_setting.FormatMatchingModelName(modelName)]
}

// TokenModelPermissionError applies equally to new requests and stored task reads.
func TokenModelPermissionError(c *gin.Context, modelName string) *types.NewAPIError {
	if !common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		return nil
	}
	value, _ := common.GetContextKey(c, constant.ContextKeyTokenModelLimit)
	limits, _ := value.(map[string]bool)
	if modelName != "" && tokenAllowsRouteModel(limits, modelName) {
		return nil
	}
	message := common.TranslateMessage(c, "distributor.token_model_forbidden", map[string]any{"Model": modelName})
	if message == "" {
		message = "token model permission denied"
	}
	return types.NewErrorWithStatusCode(errors.New(message), types.ErrorCode("token_model_forbidden"), http.StatusForbidden, types.ErrOptionWithSkipRetry())
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
	case strings.HasPrefix(requestPath, "/v1/images/generations"), requestPath == "/v1/images/edits", requestPath == "/v1/edits":
		return string(constant.EndpointTypeImageGeneration)
	case strings.HasPrefix(requestPath, "/v1/embeddings"):
		return string(constant.EndpointTypeEmbeddings)
	case strings.Contains(requestPath, "/mj/"):
		return string(constant.EndpointTypeMidjourney)
	case strings.HasPrefix(requestPath, "/suno/"):
		return string(constant.EndpointTypeSuno)
	case strings.HasPrefix(requestPath, "/v1/videos"), strings.HasPrefix(requestPath, "/v1/video"), strings.HasPrefix(requestPath, "/kling/"), strings.HasPrefix(requestPath, "/jimeng/"):
		return string(constant.EndpointTypeOpenAIVideo)
	case strings.HasPrefix(requestPath, "/v1/audio/"):
		return string(constant.EndpointTypeAudio)
	case strings.HasPrefix(requestPath, "/v1/realtime"):
		return string(constant.EndpointTypeRealtime)
	case requestPath == "/v1/completions", requestPath == "/v1/moderations":
		return string(constant.EndpointTypeOpenAI)
	case strings.HasPrefix(requestPath, "/v1beta/models"), strings.HasPrefix(requestPath, "/v1/models"):
		return string(constant.EndpointTypeGemini)
	default:
		return ""
	}
}
