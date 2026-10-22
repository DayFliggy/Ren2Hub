package controller

import (
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/pricesource"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// FetchUpstreamRatios 的本地钩子。请求不带 upstreams 和 channel_ids 时，按
// source_ids 返回 sub2api/axonhub 价格草稿并返回 true；否则恢复请求体并返回
// false，由原生 ratio_sync 流程继续绑定和处理（含 basellm、models.dev、
// OpenRouter 预设）。
func serveFixedPriceSources(c *gin.Context) bool {
	var req struct {
		dto.UpstreamRequest
		SourceIDs []string `json:"source_ids"`
	}
	if err := peekJSONBody(c, &req); err != nil {
		common.SysError(common.LogText("failed to bind upstream request: %s", err.Error()))
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid request parameter format"))
		return true
	}
	if len(req.Upstreams) == 0 && len(req.ChannelIDs) == 0 {
		fetchFixedPriceDrafts(c, req.SourceIDs)
		return true
	}
	if len(req.SourceIDs) != 0 {
		common.ApiErrorStatus(c, http.StatusBadRequest, fmt.Errorf("source_ids cannot be combined with channel sources"))
		return true
	}
	return false
}

// 与原生 ShouldBindJSON 一样不看 Content-Type，按 JSON 解码；解码后把请求体
// 复位，供原生流程再次绑定。
func peekJSONBody(c *gin.Context, v any) error {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return err
	}
	body, err := storage.Bytes()
	if err != nil {
		return err
	}
	if err := common.Unmarshal(body, v); err != nil {
		return err
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return err
	}
	c.Request.Body = io.NopCloser(storage)
	return nil
}

func fetchFixedPriceDrafts(c *gin.Context, ids []string) {
	if _, err := pricesource.Select(ids); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
		return
	}
	sources, err := service.FetchPriceDrafts(c.Request.Context(), ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"sources": sources})
}
