package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

// MidjourneyRequestError preserves the error's origin for health, retry and
// accounting decisions, independently of the proxy's numeric response code.
func MidjourneyRequestError(info *relaycommon.RelayInfo, err error, code types.ErrorCode, status int) *dto.MidjourneyResponse {
	var apiErr *types.NewAPIError
	if !errors.As(err, &apiErr) {
		apiErr = types.NewErrorWithStatusCode(err, code, status)
		if code != types.ErrorCodeBadResponseStatusCode {
			apiErr = types.NewErrorWithStatusCode(err, code, status, types.ErrOptionWithSkipRetry())
		}
	}
	info.LastError = apiErr
	return MidjourneyErrorWrapper(constant.MjRequestError, apiErr.Error())
}

func DoMidjourneyHttpRequest(c *gin.Context, info *relaycommon.RelayInfo, timeout time.Duration, fullRequestURL string, credential *model.TaskPrivateData) (*dto.MidjourneyResponseWithStatusCode, []byte, error) {
	var retryAfter time.Duration
	fail := func(err error, code types.ErrorCode, status int) (*dto.MidjourneyResponseWithStatusCode, []byte, error) {
		response := MidjourneyRequestError(info, err, code, status)
		info.LastError.RetryAfter = retryAfter
		return &dto.MidjourneyResponseWithStatusCode{StatusCode: info.LastError.StatusCode, Response: *response}, nil, info.LastError
	}
	var payload []byte
	if c.Request.Method != http.MethodGet {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return fail(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest)
		}
		data, err := storage.Bytes()
		if err != nil {
			return fail(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest)
		}
		var body map[string]json.RawMessage
		if err := common.Unmarshal(data, &body); err != nil || body == nil {
			return fail(errors.New("invalid Midjourney request object"), types.ErrorCodeInvalidRequest, http.StatusBadRequest)
		}
		if !setting.MjAccountFilterEnabled {
			delete(body, "accountFilter")
		}
		if !setting.MjNotifyEnabled {
			delete(body, "notifyHook")
		}
		if setting.MjModeClearEnabled && len(body["prompt"]) > 0 {
			var prompt string
			if err := common.Unmarshal(body["prompt"], &prompt); err != nil {
				return fail(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest)
			}
			for _, flag := range []string{"--fast", "--relax", "--turbo"} {
				prompt = strings.ReplaceAll(prompt, flag, "")
			}
			body["prompt"], err = common.Marshal(prompt)
			if err != nil {
				return fail(err, types.ErrorCodeJsonMarshalFailed, http.StatusBadRequest)
			}
		}
		payload, err = common.Marshal(body)
		if err != nil {
			return fail(err, types.ErrorCodeJsonMarshalFailed, http.StatusBadRequest)
		}
	}
	// Build and validate the complete outbound request before claiming capacity
	// or charging. Cancellation continues to reach the provider during submission.
	req, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, fullRequestURL, bytes.NewReader(payload))
	if err != nil {
		return fail(err, types.ErrorCodeInvalidRequest, http.StatusBadRequest)
	}
	if info.BeforeUpstream != nil {
		if err := info.BeforeUpstream(); err != nil {
			status := http.StatusServiceUnavailable
			if errors.Is(err, ErrRoutePriceRatioExceeded) || errors.Is(err, ErrLiveRouteCandidateInvalid) {
				status = http.StatusForbidden
			}
			return fail(err, types.ErrorCodeGetChannelFailed, status)
		}
	}
	// The lease may replace the context to propagate renewal failures.
	requestCtx, stop := context.WithTimeout(c.Request.Context(), timeout)
	defer stop()
	req = req.WithContext(requestCtx)
	channel, err := model.GetChannelById(info.ChannelId, true)
	if err != nil {
		return fail(err, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}
	if strings.TrimRight(channel.GetBaseURL(), "/") != strings.TrimRight(info.ChannelBaseUrl, "/") {
		return fail(model.ErrGatewayStateUnavailable, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}
	boundKey, _, err := ValidateGatewayStateOwner(c, channel.Id)
	if err != nil || boundKey != "" && boundKey != info.ApiKey {
		return fail(model.ErrGatewayStateUnavailable, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}
	if err := credential.BindCredential(requestCtx, channel, info.ApiKey); err != nil {
		return fail(err, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}
	client, err := GetHttpClientWithProxy(channel.GetSetting().Proxy)
	if err != nil {
		return fail(err, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("mj-api-secret", info.ApiKey)
	if info.BeforeSend == nil && info.DeferBillingToSend {
		if err := PrepareBillingForSelectedModel(c, info); err != nil {
			return fail(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden)
		}
	}
	if err := info.PrepareUpstreamSend(bytes.NewReader(payload)); err != nil {
		return fail(err, types.ErrorCodeGetChannelFailed, http.StatusServiceUnavailable)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fail(errors.New("Midjourney submission transport failed; acceptance is unknown"), types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}
	defer resp.Body.Close()
	retryAfter, _ = ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024+1))
	if err != nil || len(responseBody) > 16*1024*1024 {
		return fail(errors.New("invalid or interrupted Midjourney response"), types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fail(fmt.Errorf("Midjourney upstream HTTP %d", resp.StatusCode), types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
	}
	var envelope struct {
		Code        *int            `json:"code"`
		Description string          `json:"description"`
		Properties  any             `json:"properties"`
		Result      json.RawMessage `json:"result"`
	}
	if err := common.Unmarshal(responseBody, &envelope); err != nil || envelope.Code == nil {
		return fail(errors.New("invalid Midjourney response envelope"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	response := dto.MidjourneyResponse{Code: *envelope.Code, Description: envelope.Description, Properties: envelope.Properties}
	if response.Code != 1 && response.Code != 21 && response.Code != 22 {
		info.UpstreamRejected = true
		status := http.StatusBadRequest
		if response.Code == 23 || response.Code == 30 {
			status = http.StatusTooManyRequests
		} else if response.Code == 3 {
			status = http.StatusServiceUnavailable
		}
		MidjourneyRequestError(info, fmt.Errorf("Midjourney rejected submission (code=%d): %s", response.Code, response.Description), types.ErrorCodeBadResponseStatusCode, status)
		info.LastError.RetryAfter = retryAfter
		return &dto.MidjourneyResponseWithStatusCode{StatusCode: status, Response: response}, responseBody, info.LastError
	}
	info.UpstreamAccepted = true
	if info.RelayMode == relayconstant.RelayModeMidjourneyUpload {
		var urls []string
		if err := common.Unmarshal(envelope.Result, &urls); err != nil || len(urls) == 0 {
			return fail(errors.New("invalid Midjourney upload result"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		for _, value := range urls {
			if strings.TrimSpace(value) == "" {
				return fail(errors.New("empty Midjourney upload result"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
		}
		response.Result = model.GenerateTaskID()
	} else if err := common.Unmarshal(envelope.Result, &response.Result); err != nil || strings.TrimSpace(response.Result) == "" {
		return fail(errors.New("missing Midjourney result identifier"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	return &dto.MidjourneyResponseWithStatusCode{StatusCode: resp.StatusCode, Response: response}, responseBody, nil
}
