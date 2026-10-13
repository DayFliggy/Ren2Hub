package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// Future dates do not add to the historical search span, so choosing today's
// end of day stays valid. Queries retain their supplied end; exports fix it to
// now before opening the cursor to keep a bounded historical interval.
func limitLogTimeRange(start, end int64) (int64, error) {
	if start < 0 || end < 0 {
		return 0, common.NewMessage("Invalid time range")
	}
	now := common.GetTimestamp()
	if end == 0 || end > now {
		end = now
	}
	if start > end {
		return 0, common.NewMessage("Invalid time range")
	}
	limit := operation_setting.GetLogSetting().MaxRangeSeconds()
	if start == 0 {
		return max(0, end-limit), nil
	}
	if start < end-limit {
		return 0, common.NewMessage("The time range cannot exceed {{days}} days", map[string]any{"days": limit / 86400})
	}
	return start, nil
}

func ExportAllLogs(c *gin.Context) { exportLogs(c, true) }
func ExportUserLogs(c *gin.Context) {
	if c.GetInt("role") < common.RoleAdminUser && !operation_setting.GetLogSetting().UserExportEnabled {
		common.ApiErrorT(c, "Log export is disabled")
		return
	}
	exportLogs(c, false)
}

func exportLogs(c *gin.Context, all bool) {
	userID := c.GetInt("id")
	if userID <= 0 {
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	filter := model.LogFilter{ModelName: c.Query("model_name"), TokenName: c.Query("token_name"), Group: c.Query("group"), RequestId: c.Query("request_id"), UpstreamRequestId: c.Query("upstream_request_id"), Keyword: strings.TrimSpace(c.Query("keyword"))}
	var err error
	for key, target := range map[string]*int64{"start_timestamp": &filter.StartTimestamp, "end_timestamp": &filter.EndTimestamp} {
		if value := c.Query(key); value != "" {
			*target, err = strconv.ParseInt(value, 10, 64)
			if err != nil {
				common.ApiErrorT(c, "Invalid time range")
				return
			}
		}
	}
	if len(filter.Keyword) > 128 {
		common.ApiErrorT(c, "Keyword is too long")
		return
	}
	filter.Type, _ = strconv.Atoi(c.Query("type"))
	if all {
		filter.Username = c.Query("username")
		filter.Channel, _ = strconv.Atoi(c.Query("channel"))
	}
	filter.StartTimestamp, err = limitLogTimeRange(filter.StartTimestamp, filter.EndTimestamp)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if filter.EndTimestamp == 0 || filter.EndTimestamp > common.GetTimestamp() {
		filter.EndTimestamp = common.GetTimestamp()
	}
	release, acquired := service.AcquireLogExportLock(userID)
	if !acquired {
		common.ApiErrorT(c, "Another log export is running. Try again after it finishes.")
		return
	}
	defer release()
	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	count := 0
	err = model.StreamLogs(c.Request.Context(), userID, all, filter, func(log *model.Log) error {
		var row any = log
		if !all {
			row = buildUserLogDTOs([]*model.Log{log})[0]
		}
		line, err := common.Marshal(row)
		if err != nil {
			return err
		}
		if _, err = c.Writer.Write(append(line, '\n')); err != nil {
			return err
		}
		count++
		return nil
	})
	if c.Request.Context().Err() != nil {
		return
	}
	if err != nil {
		common.SysError(common.LogText("failed to export logs: %s", err.Error()))
		common.ApiErrorT(c, "Failed to export logs")
		return
	}
	common.ApiSuccess(c, gin.H{"count": count})
}
