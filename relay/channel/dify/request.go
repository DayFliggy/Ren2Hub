package dify

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/sjson"
)

func decodeDifyImage(value string) ([]byte, string, error) {
	mimeType, encoded, err := service.DecodeBase64FileData(value)
	if err != nil {
		return nil, "", fmt.Errorf("invalid dify inline image: %w", err)
	}
	mimeType, _, err = mime.ParseMediaType(mimeType)
	if err != nil || !strings.HasPrefix(mimeType, "image/") {
		return nil, "", fmt.Errorf("dify inline file must be an image")
	}
	if constant.MaxFileDownloadMB > 0 && int64(base64.StdEncoding.DecodedLen(len(encoded))) > int64(constant.MaxFileDownloadMB)*1024*1024 {
		return nil, "", fmt.Errorf("dify inline image exceeds the file size limit")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return nil, "", fmt.Errorf("dify inline image must contain valid base64 data")
	}
	return data, mimeType, nil
}

func (a *Adaptor) sendRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
	}
	var payload DifyChatRequest
	if err := common.Unmarshal(data, &payload); err != nil {
		return nil, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	request, err := channel.BuildApiRequest(a, c, info, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer request.Body.Close()
	// Validate every upload before creating any upstream resource. The final
	// overridden user and file list determine both upload and generation.
	uploads := make(map[int]*http.Request)
	for index, file := range payload.Files {
		if file.TransferMode != "gateway_inline" {
			continue
		}
		image, mimeType, err := decodeDifyImage(file.URL)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
		}
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		if err := writer.WriteField("user", payload.User); err != nil {
			return nil, err
		}
		headers := textproto.MIMEHeader{}
		headers.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": "image." + strings.TrimPrefix(mimeType, "image/")}))
		headers.Set("Content-Type", mimeType)
		part, err := writer.CreatePart(headers)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(image); err != nil {
			return nil, err
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		upload, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, strings.TrimRight(info.ChannelBaseUrl, "/")+"/v1/files/upload", bytes.NewReader(buffer.Bytes()))
		if err != nil {
			return nil, err
		}
		upload.Header, upload.Host = request.Header.Clone(), request.Host
		upload.Header.Set("Content-Type", writer.FormDataContentType())
		upload.Header.Del("Content-Length")
		uploads[index] = upload
	}
	info.UpstreamSideEffecting = true
	if len(uploads) == 0 {
		return channel.DoRequest(c, request, info)
	}
	if err := info.PrepareUpstreamSend(bytes.NewReader(data)); err != nil {
		return nil, err
	}
	for index := range payload.Files {
		upload, found := uploads[index]
		if !found {
			continue
		}
		resp, err := channel.DoRequest(c, upload, info)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return resp, nil
		}
		var result struct {
			ID string `json:"id"`
		}
		err = common.DecodeJson(io.LimitReader(resp.Body, 1024*1024), &result)
		service.CloseResponseBodyGracefully(resp)
		if err != nil || result.ID == "" {
			return nil, types.NewErrorWithStatusCode(fmt.Errorf("dify upload returned no valid file identity"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		file := payload.Files[index]
		file.TransferMode, file.UploadFileId, file.URL = "local_file", result.ID, ""
		encoded, err := common.Marshal(file)
		if err != nil {
			return nil, err
		}
		data, err = sjson.SetRawBytes(data, fmt.Sprintf("files.%d", index), encoded)
		if err != nil {
			return nil, err
		}
	}
	request.Body = io.NopCloser(bytes.NewReader(data))
	request.ContentLength = int64(len(data))
	request.Header.Del("Content-Length")
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	return channel.DoRequest(c, request, info)
}
