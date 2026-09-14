package service

import (
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// ValidateGatewayHTTPIdentity runs after headers are resolved, before any
// billing, rate reservation or provider I/O. Credential limits and health must
// use the same credential that actually authenticates the request.
func ValidateGatewayHTTPIdentity(c *gin.Context, info *relaycommon.RelayInfo, request *http.Request, expected http.Header, overrides map[string]string) error {
	credentialHeaders := []string{"Authorization", "Proxy-Authorization", "Api-Key", "X-Api-Key", "X-Goog-Api-Key", "Sec-WebSocket-Protocol"}
	if config := info.ChannelOtherSettings.AdvancedCustom; config != nil {
		if route, ok := config.MatchPathForModel(strings.Split(info.RequestURLPath, "?")[0], info.OriginModelName); ok && route.Auth != nil && route.Auth.Type == dto.AdvancedCustomAuthTypeHeader {
			credentialHeaders = append(credentialHeaders, route.Auth.Name)
		}
	}
	for _, name := range credentialHeaders {
		if request.Header.Get(name) != expected.Get(name) {
			return types.NewErrorWithStatusCode(errors.New("authentication override differs from the selected channel credential; configure the credential in the channel key"), types.ErrorCodeChannelHeaderOverrideInvalid, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	}
	value, _ := c.Get("gateway_state_output")
	state, _ := value.(*gatewayStateOutput)
	if state == nil {
		return nil
	}
	identityHeaders := make(map[string]string)
	for _, name := range []string{"OpenAI-Organization", "OpenAI-Project", "Chatgpt-Account-Id", "X-Goog-User-Project"} {
		if value := request.Header.Get(name); value != "" {
			identityHeaders[strings.ToLower(name)] = value
		}
	}
	for name := range overrides {
		name = strings.ToLower(name)
		switch name {
		case "authorization", "proxy-authorization", "api-key", "x-api-key", "x-goog-api-key", "sec-websocket-protocol", "host",
			"accept", "accept-encoding", "content-type", "content-length", "connection", "user-agent",
			"x-request-id", "x-client-request-id", "traceparent", "tracestate", "baggage",
			"x-stainless-lang", "x-stainless-package-version", "x-stainless-os", "x-stainless-arch",
			"x-stainless-runtime", "x-stainless-runtime-version", "x-stainless-retry-count",
			"x-stainless-timeout", "x-stainless-async", "x-stainless-helper-method", "x-goog-api-client",
			"anthropic-version", "anthropic-beta", "openai-beta":
			continue
		}
		// Unclassified channel headers may select a tenant at a custom endpoint.
		// Bind their resolved values, never the template or client-provided label.
		identityHeaders[name] = request.Header.Get(name)
	}
	endpoint := *request.URL
	endpoint.Path = strings.ReplaceAll(endpoint.Path, ":streamGenerateContent", ":generateContent")
	if info.RelayFormat == types.RelayFormatOpenAIResponsesCompaction {
		endpoint.Path = strings.TrimSuffix(endpoint.Path, "/compact")
	}
	endpoint.RawPath, endpoint.Fragment = "", ""
	query := endpoint.Query()
	if info.GetFinalRequestRelayFormat() == types.RelayFormatGemini {
		query.Del("alt")
	}
	if query.Get("key") == info.ApiKey {
		query.Del("key")
	}
	endpoint.RawQuery = query.Encode()
	encoded, err := common.Marshal(struct {
		URL     string            `json:"url"`
		Host    string            `json:"host"`
		Headers map[string]string `json:"headers"`
	}{endpoint.String(), request.Host, identityHeaders})
	if err != nil {
		return err
	}
	state.owner.TransportID = model.GatewayDigest(string(encoded))
	return nil
}
