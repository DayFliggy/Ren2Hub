package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxUpstreamRatioIDs = 500

func parseUpstreamRatioIDs(raw string) ([]int, error) {
	parts := strings.Split(raw, ",")
	if raw == "" || len(parts) > maxUpstreamRatioIDs {
		return nil, errors.New("ids must contain 1 to 500 channel ids")
	}
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || id <= 0 {
			return nil, errors.New("invalid channel id")
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// GetChannelUpstreamRatios 返回渠道列表的上游倍率。ETag 按最终响应体计算，
// 表格轮询时内容未变返回 304。with_groups=true 只用于单个渠道的编辑弹窗。
func GetChannelUpstreamRatios(c *gin.Context) {
	ids, err := parseUpstreamRatioIDs(c.Query("ids"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	withGroups := c.Query("with_groups") == "true"
	if withGroups && len(ids) != 1 {
		common.ApiError(c, errors.New("with_groups requires exactly one channel id"))
		return
	}
	views, err := service.GetChannelUpstreamRatioViews(ids, withGroups)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	body, err := common.Marshal(gin.H{"success": true, "message": "", "data": views})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	c.Header("ETag", etag)
	c.Header("Cache-Control", "no-cache")
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

func upstreamRatioChannelID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiError(c, errors.New("invalid channel id"))
		return 0, false
	}
	return id, true
}

func respondUpstreamRatioError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		common.ApiErrorStatus(c, http.StatusNotFound, errors.New("channel not found"))
	case errors.Is(err, model.ErrUpstreamRatioConflict), errors.Is(err, service.ErrUpstreamRatioChannelChanged):
		common.ApiErrorStatus(c, http.StatusConflict, err)
	case errors.Is(err, service.ErrUpstreamRatioInvalid):
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
	default:
		common.ApiError(c, err)
	}
}

// RefreshChannelUpstreamRatio 立即探测一次；主流协议渠道在这里识别上游类型。
func RefreshChannelUpstreamRatio(c *gin.Context) {
	id, ok := upstreamRatioChannelID(c)
	if !ok {
		return
	}
	view, err := service.RefreshChannelUpstreamRatio(c.Request.Context(), id)
	if err != nil {
		respondUpstreamRatioError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// UpdateChannelUpstreamRatio 只接受白名单字段。分组字段叫 group_name：
// RejectRetiredGroups 会拒绝渠道接口请求体里的 group 字段。
func UpdateChannelUpstreamRatio(c *gin.Context) {
	id, ok := upstreamRatioChannelID(c)
	if !ok {
		return
	}
	var update service.UpstreamRatioUpdate
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&update); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, errors.New("invalid request body"))
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		common.ApiErrorStatus(c, http.StatusBadRequest, errors.New("expected a single JSON object"))
		return
	}
	view, err := service.UpdateChannelUpstreamRatio(id, update)
	if err != nil {
		respondUpstreamRatioError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}
