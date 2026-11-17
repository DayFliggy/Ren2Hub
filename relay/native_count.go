package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/gin-gonic/gin"
)

const nativeCountResponseLimit = 1 << 20

// NativeCountTokens forwards the provider's count operation only. Dispatch has
// already applied the ordinary token/model/routing/rate/concurrency policy; no
// generation conversion, prompt estimation or wallet settlement runs here.
func NativeCountTokens(c *gin.Context) {
	provider := relayconstant.NativeCountChannelType(c.Request.URL.Path)
	info := &relaycommon.RelayInfo{OriginModelName: c.GetString("original_model"), RelayMode: relayconstant.RelayModeNativeCount}
	info.InitChannelMeta(c)
	// The count attempt is not bound to route health (see MarkRouteAttemptSent),
	// so finishing it only releases the channel slot and needs no outcome.
	defer service.FinishRouteAttempt(c, info, nil)
	fail := func(status int, code, message string) {
		payload := gin.H{"error": gin.H{"type": code, "message": message}}
		if provider == constant.ChannelTypeAnthropic {
			payload["type"] = "error"
		}
		c.JSON(status, payload)
	}
	if provider == 0 || info.ChannelType != provider {
		fail(http.StatusBadRequest, "unsupported_count_channel", "native token counting is not supported by this channel")
		return
	}
	body, err := nativeCountBody(c, info, provider)
	if err != nil {
		status := http.StatusBadRequest
		if common.IsRequestBodyTooLargeError(err) {
			status = http.StatusRequestEntityTooLarge
		}
		fail(status, "invalid_request_error", err.Error())
		return
	}
	target, err := nativeCountURL(info, provider)
	if err != nil {
		fail(http.StatusInternalServerError, "invalid_channel_configuration", "invalid native count endpoint configuration")
		return
	}
	timeout := 60 * time.Second
	if common.RelayTimeout > 0 {
		timeout = time.Duration(common.RelayTimeout) * time.Second
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		fail(http.StatusInternalServerError, "invalid_channel_configuration", "cannot construct native count request")
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if provider == constant.ChannelTypeAnthropic {
		request.Header.Set("x-api-key", info.ApiKey)
		version := c.GetHeader("anthropic-version")
		if version == "" {
			version = "2023-06-01"
		}
		request.Header.Set("anthropic-version", version)
		if beta := c.GetHeader("anthropic-beta"); beta != "" {
			request.Header.Set("anthropic-beta", beta)
		}
	} else {
		request.Header.Set("x-goog-api-key", info.ApiKey)
	}
	if err := channel.ApplyHeaderOverride(info, c, request); err != nil {
		fail(http.StatusInternalServerError, "invalid_channel_configuration", "invalid native count header configuration")
		return
	}
	response, err := channel.DoRequest(c, request, info)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		fail(status, "upstream_error", "native token count request failed")
		return
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, nativeCountResponseLimit+1))
	if err != nil || len(data) > nativeCountResponseLimit {
		status := http.StatusBadGateway
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		fail(status, "upstream_error", "invalid or oversized native count response")
		return
	}
	if response.StatusCode >= 400 {
		if retryAfter := response.Header.Get("Retry-After"); retryAfter != "" {
			c.Header("Retry-After", retryAfter)
		}
		writeNativeCountUpstreamError(c, provider, response.StatusCode, data)
		return
	}
	if response.StatusCode != http.StatusOK || validateNativeCountResponse(data, provider) != nil {
		fail(http.StatusBadGateway, "upstream_error", "invalid native count response")
		return
	}
	c.Data(http.StatusOK, "application/json", data)
}

// nativeCountUpstreamErrorMessage replaces an upstream error whose body has no
// string error.message.
const nativeCountUpstreamErrorMessage = "native token count upstream returned an error"

// writeNativeCountUpstreamError answers an upstream count error with a fixed
// set of protocol fields only: the provider's error type (Claude) or status
// (Gemini) when it is a plain identifier, the HTTP status, and the upstream
// error.message string. The message is always masked with this channel's
// upstream addresses. It is read directly instead of through the generation
// path's RelayErrorHandler/ToOpenAIError: the former appends error.metadata to
// the message and the latter skips masking when the upstream error.code is
// count_token_failed. Any other upstream field is dropped.
func writeNativeCountUpstreamError(c *gin.Context, provider int, status int, data []byte) {
	var upstream struct {
		Error struct {
			Type    string          `json:"type"`
			Status  string          `json:"status"`
			Message json.RawMessage `json:"message"`
		} `json:"error"`
	}
	// Every field is optional: a body that is not an error object leaves them
	// empty and the defaults below apply.
	_ = common.Unmarshal(data, &upstream)
	var message string
	if common.Unmarshal(upstream.Error.Message, &message) != nil || strings.TrimSpace(message) == "" {
		message = nativeCountUpstreamErrorMessage
	}
	message = common.MaskSensitiveInfo(message, common.UpstreamAddresses(c)...)
	if provider == constant.ChannelTypeAnthropic {
		errorType := "upstream_error"
		if isNativeCountErrorLabel(upstream.Error.Type) {
			errorType = upstream.Error.Type
		}
		c.JSON(status, gin.H{"type": "error", "error": gin.H{"type": errorType, "message": message}})
		return
	}
	body := gin.H{"code": status, "message": message}
	if isNativeCountErrorLabel(upstream.Error.Status) {
		body["status"] = upstream.Error.Status
	}
	c.JSON(status, gin.H{"error": body})
}

// isNativeCountErrorLabel accepts provider error labels such as
// "overloaded_error" or "RESOURCE_EXHAUSTED"; free text is not forwarded.
func isNativeCountErrorLabel(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, r := range value {
		if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

func nativeCountBody(c *gin.Context, info *relaycommon.RelayInfo, provider int) ([]byte, error) {
	if c.ContentType() != "application/json" {
		return nil, errors.New("native token counting requires application/json")
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	data, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if common.Unmarshal(data, &body) != nil || body == nil {
		return nil, errors.New("request must be a JSON object")
	}
	mapped, _, err := relaycommon.ResolveMappedModelName(info.OriginModelName, common.GetContextKeyString(c, constant.ContextKeyChannelModelMapping))
	if err != nil {
		return nil, errors.New("invalid channel model mapping")
	}
	if mapped == "" {
		return nil, errors.New("model is required")
	}
	info.UpstreamModelName = mapped
	if provider == constant.ChannelTypeAnthropic {
		err = validateClaudeCountBody(body, info)
	} else {
		err = validateGeminiCountBody(c, body, info)
	}
	if err != nil {
		return nil, err
	}
	// Provider-owned files/caches need a separately authorized same-channel
	// ownership contract. Inline images/text remain ordinary count inputs.
	if containsPrivateCountReference(body, provider) {
		return nil, errors.New("provider-owned file or cache references are not supported for token counting")
	}
	return common.Marshal(body)
}

func validateClaudeCountBody(body map[string]json.RawMessage, info *relaycommon.RelayInfo) error {
	var modelName string
	if common.Unmarshal(body["model"], &modelName) != nil || modelName != info.OriginModelName {
		return errors.New("invalid count model")
	}
	if err := validateObjectArray(body["messages"], "messages", true); err != nil {
		return err
	}
	var messages []map[string]json.RawMessage
	_ = common.Unmarshal(body["messages"], &messages)
	for _, message := range messages {
		var role string
		if common.Unmarshal(message["role"], &role) != nil || (role != "user" && role != "assistant") {
			return errors.New("messages require a user or assistant role")
		}
		var text *string
		if common.Unmarshal(message["content"], &text) != nil || text == nil {
			if err := validateObjectArray(message["content"], "message content", true); err != nil {
				return err
			}
		}
	}
	for key := range body {
		switch key {
		case "model", "messages", "system", "tools", "tool_choice", "thinking", "output_config":
		default:
			return fmt.Errorf("unsupported count field: %s", key)
		}
	}
	if tools, exists := body["tools"]; exists {
		if err := validateObjectArray(tools, "tools", false); err != nil {
			return err
		}
	}
	body["model"], _ = common.Marshal(info.UpstreamModelName)
	return nil
}

func validateGeminiCountBody(c *gin.Context, body map[string]json.RawMessage, info *relaycommon.RelayInfo) error {
	if !relayconstant.SafeGeminiModelSegment(info.UpstreamModelName) {
		return errors.New("invalid mapped Gemini model path segment")
	}
	if alt := c.Query("alt"); alt != "" && alt != "json" {
		return errors.New("countTokens does not support streaming")
	}
	for key := range body {
		if key != "contents" && key != "generateContentRequest" {
			return fmt.Errorf("unsupported count field: %s", key)
		}
	}
	contents, hasContents := body["contents"]
	generation, hasGeneration := body["generateContentRequest"]
	if hasContents == hasGeneration {
		return errors.New("provide exactly one of contents or generateContentRequest")
	}
	if hasGeneration {
		var nested map[string]json.RawMessage
		if common.Unmarshal(generation, &nested) != nil || nested == nil {
			return errors.New("generateContentRequest must be an object")
		}
		if raw, exists := nested["model"]; exists {
			var modelName string
			if common.Unmarshal(raw, &modelName) != nil || (modelName != info.OriginModelName && modelName != "models/"+info.OriginModelName) {
				return errors.New("generateContentRequest.model must match the URL model")
			}
		}
		nested["model"], _ = common.Marshal("models/" + info.UpstreamModelName)
		contents = nested["contents"]
		encoded, err := common.Marshal(nested)
		if err != nil {
			return err
		}
		body["generateContentRequest"] = encoded
	}
	if err := validateObjectArray(contents, "contents", true); err != nil {
		return err
	}
	var entries []map[string]json.RawMessage
	_ = common.Unmarshal(contents, &entries)
	for _, content := range entries {
		if err := validateObjectArray(content["parts"], "content parts", true); err != nil {
			return err
		}
	}
	return nil
}

func validateObjectArray(raw json.RawMessage, name string, required bool) error {
	var entries []map[string]json.RawMessage
	if common.Unmarshal(raw, &entries) != nil || entries == nil || (required && len(entries) == 0) {
		return fmt.Errorf("%s must be an array of objects with valid items", name)
	}
	for _, entry := range entries {
		if entry == nil {
			return fmt.Errorf("%s must contain objects", name)
		}
	}
	return nil
}

func containsPrivateCountReference(body map[string]json.RawMessage, provider int) bool {
	if provider == constant.ChannelTypeAnthropic {
		var messages []struct {
			Content json.RawMessage `json:"content"`
		}
		_ = common.Unmarshal(body["messages"], &messages)
		for _, message := range messages {
			if claudeCountContentHasPrivateReference(message.Content) {
				return true
			}
		}
		return claudeCountContentHasPrivateReference(body["system"])
	}
	request := body
	if nested, exists := body["generateContentRequest"]; exists {
		request = nil
		_ = common.Unmarshal(nested, &request)
	}
	if _, exists := request["cachedContent"]; exists {
		return true
	}
	if _, exists := request["cached_content"]; exists {
		return true
	}
	var contents []json.RawMessage
	_ = common.Unmarshal(request["contents"], &contents)
	contents = append(contents, request["systemInstruction"], request["system_instruction"])
	for _, content := range contents {
		var entry struct {
			Parts json.RawMessage `json:"parts"`
		}
		if common.Unmarshal(content, &entry) == nil && geminiCountPartsHavePrivateReference(entry.Parts) {
			return true
		}
	}
	return false
}

func claudeCountContentHasPrivateReference(raw json.RawMessage) bool {
	var blocks []map[string]json.RawMessage
	if common.Unmarshal(raw, &blocks) != nil {
		// Text is a valid content value; provider validation handles other shapes.
		return false
	}
	for _, block := range blocks {
		if _, exists := block["file_id"]; exists {
			return true
		}
		var source map[string]json.RawMessage
		if common.Unmarshal(block["source"], &source) == nil {
			if _, exists := source["file_id"]; exists {
				return true
			}
			var sourceType string
			_ = common.Unmarshal(source["type"], &sourceType)
			if sourceType == "file" || claudeCountContentHasPrivateReference(source["content"]) {
				return true
			}
		}
		if claudeCountContentHasPrivateReference(block["content"]) {
			return true
		}
	}
	// Tool input and schemas are arbitrary user JSON, not content blocks.
	return false
}

func geminiCountPartsHavePrivateReference(raw json.RawMessage) bool {
	var parts []map[string]json.RawMessage
	if common.Unmarshal(raw, &parts) != nil {
		return false
	}
	for _, part := range parts {
		if _, exists := part["fileData"]; exists {
			return true
		}
		if _, exists := part["file_data"]; exists {
			return true
		}
		for _, key := range []string{"functionResponse", "function_response"} {
			var response struct {
				Parts json.RawMessage `json:"parts"`
			}
			if common.Unmarshal(part[key], &response) == nil && geminiCountPartsHavePrivateReference(response.Parts) {
				return true
			}
		}
	}
	// Function arguments and response JSON belong to the caller, unlike parts.
	return false
}

func nativeCountURL(info *relaycommon.RelayInfo, provider int) (string, error) {
	base, err := url.Parse(info.ChannelBaseUrl)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return "", errors.New("invalid provider base URL")
	}
	if provider == constant.ChannelTypeAnthropic {
		base.Path = strings.TrimRight(base.Path, "/") + "/v1/messages/count_tokens"
		base.RawQuery = "beta=true"
	} else {
		version := model_setting.GetGeminiVersionSetting(info.UpstreamModelName)
		if version != "v1" && version != "v1beta" {
			return "", errors.New("unsupported Gemini API version")
		}
		base.Path = strings.TrimRight(base.Path, "/") + "/" + version + "/models/" + info.UpstreamModelName + ":countTokens"
	}
	base.RawPath = ""
	return base.String(), nil
}

func validateNativeCountResponse(data []byte, provider int) error {
	var body map[string]json.RawMessage
	if common.Unmarshal(data, &body) != nil || body == nil {
		return errors.New("count response must be an object")
	}
	if _, exists := body["error"]; exists {
		return errors.New("count response contains an error")
	}
	field := "totalTokens"
	if provider == constant.ChannelTypeAnthropic {
		field = "input_tokens"
	}
	value, err := strconv.ParseInt(string(body[field]), 10, 64)
	if err != nil || value < 0 {
		return errors.New("count must be a non-negative integer")
	}
	return nil
}
