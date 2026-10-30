package controller

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const usageDiscountBodyLimit = 64 << 10

type usageDiscountConfigView struct {
	Version   int64                     `json:"version"`
	CreatedAt int64                     `json:"created_at"`
	CreatedBy int                       `json:"created_by"`
	Config    model.UsageDiscountConfig `json:"config"`
}

func newUsageDiscountConfigView(settings *model.UsageDiscountSettings) usageDiscountConfigView {
	return usageDiscountConfigView{
		Version: settings.Version, CreatedAt: settings.CreatedAt, CreatedBy: settings.CreatedBy, Config: settings.Config,
	}
}

func GetUsageDiscountConfig(c *gin.Context) {
	settings, err := model.CurrentUsageDiscountSettings()
	if err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError, err)
		return
	}
	common.ApiSuccess(c, newUsageDiscountConfigView(settings))
}

func UpdateUsageDiscountConfig(c *gin.Context) {
	var request struct {
		ExpectedVersion *int64          `json:"expected_version"`
		Config          json.RawMessage `json:"config"`
	}
	if !decodeUsageDiscountBody(c, &request) {
		return
	}
	if request.ExpectedVersion == nil || len(request.Config) == 0 {
		common.ApiErrorStatus(c, http.StatusBadRequest, errors.New("expected_version and config are required"), gin.H{"code": "usage_discount_invalid_config"})
		return
	}
	settings, err := model.SaveUsageDiscountConfig(request.Config, *request.ExpectedVersion, c.GetInt("id"), time.Now())
	if err != nil {
		respondUsageDiscountError(c, err)
		return
	}
	recordManageAudit(c, "usage_discount.config.update", map[string]any{
		"from_version": *request.ExpectedVersion,
		"to_version":   settings.Version,
		"active":       settings.Active(),
		"site_mode":    settings.Site.Mode,
		"day_mode":     settings.Day.Mode,
		"week_mode":    settings.Week.Mode,
		"month_mode":   settings.Month.Mode,
	})
	common.ApiSuccess(c, newUsageDiscountConfigView(settings))
}

func GetUsageDiscountStatus(c *gin.Context) {
	status, err := service.GetUsageDiscountStatus(time.Now())
	if err != nil {
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, err, gin.H{"code": "usage_discount_unavailable"})
		return
	}
	common.ApiSuccess(c, status)
}

func EstimateUsageDiscount(c *gin.Context) {
	var request struct {
		Config json.RawMessage `json:"config"`
	}
	if !decodeUsageDiscountBody(c, &request) {
		return
	}
	estimate, err := service.EstimateUsageDiscount(request.Config, time.Now())
	if err != nil {
		respondUsageDiscountError(c, err)
		return
	}
	common.ApiSuccess(c, estimate)
}

func GetSelfUsageDiscount(c *gin.Context) {
	view, err := service.GetUsageDiscountSelfView(c.GetInt("id"), time.Now())
	if err != nil {
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, err, gin.H{"code": "usage_discount_unavailable"})
		return
	}
	common.ApiSuccess(c, view)
}

// decodeUsageDiscountBody accepts exactly one JSON object with known fields.
// A body over the limit is rejected even when its first object is complete.
func decodeUsageDiscountBody(c *gin.Context, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, usageDiscountBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return rejectUsageDiscountBody(c, err, "invalid request body")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return rejectUsageDiscountBody(c, err, "expected a single JSON object")
	}
	return true
}

// rejectUsageDiscountBody answers 413 when the body hit the limit, else 400.
func rejectUsageDiscountBody(c *gin.Context, err error, message string) bool {
	status := http.StatusBadRequest
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		status, message = http.StatusRequestEntityTooLarge, "request body is too large"
	}
	common.ApiErrorStatus(c, status, errors.New(message), gin.H{"code": "usage_discount_invalid_config"})
	return false
}

func respondUsageDiscountError(c *gin.Context, err error) {
	for _, known := range []struct {
		err    error
		status int
	}{
		{model.ErrUsageDiscountInvalidConfig, http.StatusBadRequest},
		{model.ErrUsageDiscountSchemaUnsupported, http.StatusBadRequest},
		{model.ErrUsageDiscountVersionConflict, http.StatusConflict},
	} {
		if errors.Is(err, known.err) {
			common.ApiErrorStatus(c, known.status, err, gin.H{"code": known.err.Error()})
			return
		}
	}
	common.ApiErrorStatus(c, http.StatusInternalServerError, err)
}
