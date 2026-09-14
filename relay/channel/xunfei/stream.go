package xunfei

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// The shared scanner owns cancellation and closes this reader to release a
// blocked websocket read. Each provider message becomes one complete JSON line.
type xunfeiMessageReader struct {
	conn    *websocket.Conn
	pending *bytes.Reader
}

func (r *xunfeiMessageReader) Read(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	if r.pending == nil || r.pending.Len() == 0 {
		_, message, err := r.conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		var payload json.RawMessage
		if err := common.Unmarshal(message, &payload); err != nil {
			return 0, err
		}
		line, err := common.Marshal(payload)
		if err != nil {
			return 0, err
		}
		r.pending = bytes.NewReader(append(line, '\n'))
	}
	return r.pending.Read(data)
}

func (r *xunfeiMessageReader) Close() error { return r.conn.Close() }

func xunfeiResponseHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	var usage *dto.Usage
	var content strings.Builder
	var last XunfeiChatResponse
	responseID, created := helper.GetResponseID(c), common.GetTimestamp()
	sentRole := false
	helper.NDJSONStreamScannerHandler(c, resp, info, func(line string, result *helper.StreamResult) {
		var chunk XunfeiChatResponse
		if err := common.Unmarshal([]byte(line), &chunk); err != nil {
			result.Stop(err)
			return
		}
		if chunk.Header.Code != 0 {
			result.Stop(fmt.Errorf("xunfei generation failed: %s", chunk.Header.Message))
			return
		}
		info.UpstreamAccepted = true
		observed := chunk.Payload.Usage.Text
		if err := service.NormalizeProviderUsage(&observed); err != nil {
			result.Stop(err)
			return
		}
		if service.ValidUsage(&observed) {
			usage = &observed
			usage.UsageSource = "provider"
		}
		if info.IsStream {
			response := streamResponseXunfei2OpenAI(&chunk)
			response.Id, response.Created, response.Model = responseID, created, info.UpstreamModelName
			if !sentRole {
				response.Choices[0].Delta.Role = "assistant"
				sentRole = true
			}
			if err := helper.ObjectData(c, response); err != nil {
				result.Stop(err)
				return
			}
		}
		if len(chunk.Payload.Choices.Text) > 0 {
			text := chunk.Payload.Choices.Text[0].Content
			content.WriteString(text)
			if info.IsStream && text != "" {
				result.MarkValidOutput()
			}
		}
		last = chunk
		if chunk.Payload.Choices.Status == 2 {
			result.Done()
		}
	})
	if usage == nil {
		usage = service.ResponseText2Usage(c, content.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	if err := helper.StreamError(info); err != nil {
		return usage, err
	}
	if info.IsStream {
		if err := openai.HandleFinalResponse(c, info, "", responseID, created, info.UpstreamModelName, "", usage, false); err != nil {
			return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
		}
		return usage, nil
	}
	response := responseXunfei2OpenAI(&last)
	response.Id, response.Created, response.Model = responseID, created, info.UpstreamModelName
	response.Choices[0].Message.Content = content.String()
	response.Usage = *usage
	body, err := common.Marshal(response)
	if err != nil {
		return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	c.Header("Content-Type", "application/json")
	if _, err := c.Writer.Write(body); err != nil {
		return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	return usage, nil
}

var _ io.ReadCloser = (*xunfeiMessageReader)(nil)
