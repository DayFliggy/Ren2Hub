package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/relaycapability"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

const GatewayStateContextKey = "gateway_state_binding"
const GatewayStateOutputErrorKey = "gateway_state_output_error"

type gatewayStateReference struct{ kind, value string }

type gatewayStateOutput struct {
	owner      model.GatewayStateBinding
	signatures map[int]string
}

func stateReferences(payload map[string]any, output bool) []gatewayStateReference {
	var references []gatewayStateReference
	add := func(kind string, value any) {
		if object, ok := value.(map[string]any); ok {
			value = object["id"]
		}
		if text, ok := value.(string); ok && text != "" {
			references = append(references, gatewayStateReference{kind, text})
		}
	}
	if output {
		if response, ok := payload["response"].(map[string]any); ok {
			payload = response
		}
		if object, _ := payload["object"].(string); object == "response" || (object == "" && payload["output"] != nil) {
			add("response", payload["id"])
		}
	} else {
		add("response", payload["previous_response_id"])
	}
	add("conversation", payload["conversation"])
	add("cached_content", payload["cachedContent"])
	if payload["container"] != "auto" {
		add("container", payload["container"])
	}
	if tools, ok := payload["tools"].([]any); ok {
		for _, value := range tools {
			if tool, ok := value.(map[string]any); ok && tool["type"] == "code_interpreter" && tool["container"] != "auto" {
				add("container", tool["container"])
			}
		}
	}
	var inspect func(any)
	inspect = func(value any) {
		switch item := value.(type) {
		case []any:
			for _, child := range item {
				inspect(child)
			}
		case map[string]any:
			kind, _ := item["type"].(string)
			if kind == "redacted_thinking" {
				add("redacted_thinking", item["data"])
			}
			if kind == "item_reference" && !output {
				add("response_item", item["id"])
			}
			if output && (kind == "message" || kind == "reasoning" || kind == "function_call" || kind == "custom_tool_call") {
				add("response_item", item["id"])
			}
			add("container", item["container_id"])
			add("file", item["file_id"])
			for _, name := range []string{"fileUri", "file_uri"} {
				if uri, ok := item[name].(string); ok && relayconvert.RequiresMediaOwnership(uri) {
					add("file_uri", uri)
				}
			}
			for _, name := range []string{"encrypted_content", "encrypted_index", "thoughtSignature", "signature"} {
				add(name, item[name])
			}
			for _, name := range []string{"content", "parts", "message", "content_block", "delta", "item", "source", "file", "fileData", "file_data", "citations", "citation", "annotations", "url_citation"} {
				inspect(item[name])
			}
		}
	}
	for _, name := range []string{"input", "output", "content", "messages", "contents", "candidates", "content_block", "delta", "item"} {
		inspect(payload[name])
	}
	return references
}

func ResolveGatewayRequestState(c *gin.Context) (*model.GatewayStateBinding, error) {
	if !strings.Contains(c.Request.Header.Get("Content-Type"), "json") {
		return nil, nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	data, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := common.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	var owner *model.GatewayStateBinding
	for _, reference := range stateReferences(payload, false) {
		binding, err := model.FindGatewayState(c.Request.Context(), c.GetInt("id"), c.GetInt("token_id"), reference.kind, reference.value)
		if err != nil {
			return nil, err
		}
		if binding.TransportID == "" || (owner != nil && !owner.SameOwner(binding)) {
			return nil, model.ErrGatewayStateUnknown
		}
		owner = &binding
	}
	return owner, nil
}

func ValidateGatewayStateOwner(c *gin.Context, channelID int) (string, int, error) {
	if value, exists := c.Get("gateway_origin_midjourney"); exists {
		task, ok := value.(*model.Midjourney)
		if !ok || task == nil || task.UserId != c.GetInt("id") || task.PrivateData.TokenId != c.GetInt("token_id") {
			return "", 0, model.ErrGatewayStateUnknown
		}
		channel, err := model.GetChannelById(channelID, true)
		if err != nil {
			return "", 0, model.ErrGatewayStateUnavailable
		}
		return model.ResolveMidjourneyCredential(c.Request.Context(), channel, task)
	}
	if value, exists := c.Get("gateway_origin_task"); exists {
		task, ok := value.(*model.Task)
		if !ok || task == nil || task.UserId != c.GetInt("id") ||
			(task.PrivateData.TokenId > 0 && task.PrivateData.TokenId != c.GetInt("token_id")) {
			return "", 0, model.ErrGatewayStateUnknown
		}
		channel, err := model.GetChannelById(channelID, true)
		if err != nil {
			return "", 0, model.ErrGatewayStateUnavailable
		}
		return model.ResolveTaskCredential(c.Request.Context(), channel, task)
	}
	value, _ := c.Get(GatewayStateContextKey)
	binding, ok := value.(*model.GatewayStateBinding)
	if !ok || binding == nil {
		return "", 0, nil
	}
	if binding.TransportID == "" || binding.ExpiresAt <= time.Now().Unix() {
		return "", 0, model.ErrGatewayStateUnknown
	}
	if binding.ChannelID != channelID {
		return "", 0, model.ErrGatewayStateUnavailable
	}
	// Admission returns a capacity projection; ownership requires the current
	// endpoint and credential, including changes made while the request waited.
	var channel model.Channel
	if err := model.DB.WithContext(c.Request.Context()).First(&channel, channelID).Error; err != nil {
		return "", 0, model.ErrGatewayStateUnavailable
	}
	if channel.Status != common.ChannelStatusEnabled || binding.EndpointID != channel.GatewayEndpointID() {
		return "", 0, model.ErrGatewayStateUnavailable
	}
	return model.ResolveGatewayCredential(c.Request.Context(), &channel, binding.CredentialID)
}

func PrepareGatewayStateOutput(c *gin.Context, info *relaycommon.RelayInfo, channel *model.Channel) error {
	c.Set("gateway_state_output", nil)
	if info.ConversionPlan != nil && !info.ConversionPlan.Native {
		return nil
	}
	if info.RelayFormat != types.RelayFormatOpenAI && info.RelayFormat != types.RelayFormatOpenAIResponses && info.RelayFormat != types.RelayFormatOpenAIResponsesCompaction && info.RelayFormat != types.RelayFormatClaude && info.RelayFormat != types.RelayFormatGemini {
		return nil
	}
	if info.TokenId <= 0 {
		return nil
	}
	identity, err := model.EnsureGatewayCredential(c.Request.Context(), channel.Id, common.GetContextKeyString(c, constant.ContextKeyChannelKey))
	if err != nil {
		return err
	}
	state := &gatewayStateOutput{
		owner:      model.GatewayStateBinding{UserID: info.UserId, TokenID: info.TokenId, ChannelID: channel.Id, CredentialID: identity.ID, EndpointID: channel.GatewayEndpointID()},
		signatures: make(map[int]string),
	}
	// Bedrock signs with the selected SDK credential; its transport does not
	// apply arbitrary HTTP header overrides. HTTP adaptors bind the final request.
	if channel.Type == constant.ChannelTypeAws {
		state.owner.TransportID = model.GatewayDigest("bedrock:" + state.owner.EndpointID)
	}
	c.Set("gateway_state_output", state)
	return nil
}

// ValidateGatewayOutboundState checks the actual post-override payload. The
// original request cannot authorize references introduced later by a channel.
func ValidateGatewayOutboundState(c *gin.Context, info *relaycommon.RelayInfo, data []byte) error {
	format := info.GetFinalRequestRelayFormat()
	if format != types.RelayFormatOpenAI && format != types.RelayFormatOpenAIResponses && format != types.RelayFormatOpenAIResponsesCompaction && format != types.RelayFormatClaude && format != types.RelayFormatGemini {
		return nil
	}
	var payload map[string]any
	if err := common.Unmarshal(data, &payload); err != nil {
		return types.NewErrorWithStatusCode(errors.New("invalid final request payload"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	if format == types.RelayFormatOpenAIResponses && payload["prompt"] != nil && !relaycapability.SupportsReferenceDomain(info.ChannelType, format, "openai_prompt") {
		return types.NewErrorWithStatusCode(errors.New("channel does not implement the OpenAI prompt reference domain"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	value, _ := c.Get("gateway_state_output")
	state, _ := value.(*gatewayStateOutput)
	for _, reference := range stateReferences(payload, false) {
		binding, err := model.FindGatewayState(c.Request.Context(), info.UserId, info.TokenId, reference.kind, reference.value)
		if err == nil && (binding.TransportID == "" || state == nil) {
			err = model.ErrGatewayStateUnknown
		}
		if err == nil && !state.owner.SameOwner(binding) {
			err = model.ErrGatewayStateUnavailable
		}
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, GatewayStateHTTPStatus(err), types.ErrOptionWithSkipRetry(), types.ErrOptionWithHideErrMsg(GatewayStatePublicMessage(err)))
		}
	}
	if value, exists := c.Get(GatewayStateContextKey); exists && value != nil {
		binding, valid := value.(*model.GatewayStateBinding)
		if !valid || binding == nil || state == nil || !state.owner.SameOwner(*binding) {
			return types.NewErrorWithStatusCode(model.ErrGatewayStateUnavailable, types.ErrorCodeInvalidRequest, http.StatusServiceUnavailable, types.ErrOptionWithSkipRetry())
		}
	}
	return nil
}

// BindGatewayOutput must succeed before any reusable state reaches the client.
func BindGatewayOutput(c *gin.Context, data []byte) error {
	var event struct {
		Type           string `json:"type"`
		SequenceNumber *int   `json:"sequence_number"`
		Error          any    `json:"error"`
	}
	if common.Unmarshal(data, &event) == nil {
		if event.SequenceNumber != nil {
			c.Set("gateway_stream_sequence", *event.SequenceNumber)
		}
		if event.Error != nil || event.Type == "error" || event.Type == "response.failed" || event.Type == "response.incomplete" {
			c.Set("gateway_stream_error_sent", true)
		}
	}
	if value, ok := c.Get(GatewayStateOutputErrorKey); ok {
		if err, valid := value.(error); valid {
			return err
		}
	}
	value, _ := c.Get("gateway_state_output")
	if value == nil {
		return nil
	}
	state, ok := value.(*gatewayStateOutput)
	if !ok || state == nil {
		return gatewayStateOutputFailure(c, errors.New("invalid state binding context"))
	}
	var payload map[string]any
	if err := common.Unmarshal(data, &payload); err != nil {
		return nil
	}
	statePayload := payload
	if response, ok := payload["response"].(map[string]any); ok {
		statePayload = response
	}
	var expiresAt int64
	if expiry, present := statePayload["expires_at"]; present && expiry != nil {
		seconds, valid := expiry.(float64)
		if !valid || math.Trunc(seconds) != seconds || seconds <= float64(time.Now().Unix()) || seconds > 1<<53 {
			return gatewayStateOutputFailure(c, errors.New("invalid provider state expiry"))
		}
		expiresAt = int64(seconds)
	}
	if delta, ok := payload["delta"].(map[string]any); ok && delta["type"] == "signature_delta" {
		part, _ := delta["signature"].(string)
		index, valid := payload["index"].(float64)
		if !valid || math.Trunc(index) != index || index < 0 || index > math.MaxInt32 {
			return gatewayStateOutputFailure(c, errors.New("invalid signature block index"))
		}
		complete := state.signatures[int(index)] + part
		if len(complete) > 256<<10 {
			return gatewayStateOutputFailure(c, errors.New("signature exceeds state binding limit"))
		}
		state.signatures[int(index)] = complete
		delta["signature"] = complete
	}
	for _, reference := range stateReferences(payload, true) {
		if reference.kind == "response" {
			status, _ := statePayload["status"].(string)
			if status == "failed" || status == "cancelled" {
				continue
			}
		}
		binding := state.owner
		if binding.TransportID == "" {
			return gatewayStateOutputFailure(c, errors.New("state transport identity was not established"))
		}
		binding.Kind = reference.kind
		if expiresAt > 0 && (binding.ExpiresAt == 0 || expiresAt < binding.ExpiresAt) {
			binding.ExpiresAt = expiresAt
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
		err := model.BindGatewayState(ctx, binding, reference.value)
		cancel()
		if err != nil {
			return gatewayStateOutputFailure(c, err)
		}
	}
	return nil
}

func gatewayStateOutputFailure(c *gin.Context, err error) error {
	failure := fmt.Errorf("state binding failed: %w", err)
	c.Set(GatewayStateOutputErrorKey, failure)
	gatewayMetrics.stateBindingFailures.Add(1)
	return failure
}

func GatewayStateHTTPStatus(err error) int {
	if errors.Is(err, model.ErrGatewayStateUnknown) {
		return 400
	}
	return 503
}

func GatewayStatePublicMessage(err error) string {
	if errors.Is(err, model.ErrGatewayStateUnknown) {
		return model.ErrGatewayStateUnknown.Error()
	}
	return model.ErrGatewayStateUnavailable.Error()
}
