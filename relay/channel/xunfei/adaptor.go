package xunfei

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type Adaptor struct {
	requestURL string
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertClaudeRequest(*gin.Context, *relaycommon.RelayInfo, *dto.ClaudeRequest) (any, error) {
	//TODO implement me
	panic("implement me")
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return a.requestURL, nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	if err := service.ValidateRequestProjection(info, constant.APITypeXunfei, request); err != nil {
		return nil, err
	}
	keys := strings.Split(info.ApiKey, "|")
	if len(keys) != 3 {
		return nil, errors.New("invalid xunfei credential")
	}
	domain, endpoint, err := getXunfeiAuthUrl(c, keys[2], keys[1], request.Model)
	if err != nil {
		return nil, err
	}
	if info.ChannelBaseUrl != "" && info.ChannelBaseUrl != constant.ChannelBaseURLs[constant.ChannelTypeXunfei] {
		base, err := url.Parse(info.ChannelBaseUrl)
		if err != nil || base.Host == "" {
			return nil, errors.New("invalid xunfei endpoint")
		}
		switch base.Scheme {
		case "http":
			base.Scheme = "ws"
		case "https":
			base.Scheme = "wss"
		case "ws", "wss":
		default:
			return nil, errors.New("invalid xunfei endpoint scheme")
		}
		if base.Path == "" || base.Path == "/" {
			base.Path = "/" + getAPIVersion(c, request.Model) + "/chat"
		}
		endpoint, err = buildXunfeiAuthUrl(base.String(), keys[2], keys[1])
		if err != nil {
			return nil, err
		}
	}
	a.requestURL = endpoint
	return requestOpenAI2Xunfei(*request, keys[0], domain), nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	// TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	body, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	conn, err := channel.DoWssRequest(a, c, info, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	stopCancellation := context.AfterFunc(c.Request.Context(), func() { _ = conn.Close() })
	defer stopCancellation()
	if err := conn.WriteMessage(websocket.TextMessage, body); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/x-ndjson"}}, Body: &xunfeiMessageReader{conn: conn}}, nil
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	return xunfeiResponseHandler(c, info, resp)
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
