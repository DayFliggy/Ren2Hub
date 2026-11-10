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

const topUpDiscountBodyLimit = 64 << 10

type topUpDiscountConfigView struct {
	Version   int64                     `json:"version"`
	CreatedAt int64                     `json:"created_at"`
	CreatedBy int                       `json:"created_by"`
	Config    model.TopUpDiscountConfig `json:"config"`
}

func newTopUpDiscountConfigView(settings *model.TopUpDiscountSettings) topUpDiscountConfigView {
	return topUpDiscountConfigView{
		Version: settings.Version, CreatedAt: settings.CreatedAt, CreatedBy: settings.CreatedBy, Config: settings.Config,
	}
}

func GetTopUpDiscountConfig(c *gin.Context) {
	settings, err := model.CurrentTopUpDiscountSettings()
	if err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError, err)
		return
	}
	common.ApiSuccess(c, newTopUpDiscountConfigView(settings))
}

func UpdateTopUpDiscountConfig(c *gin.Context) {
	var request struct {
		ExpectedVersion *int64          `json:"expected_version"`
		Config          json.RawMessage `json:"config"`
	}
	if !decodeTopUpDiscountBody(c, &request) {
		return
	}
	if request.ExpectedVersion == nil || len(request.Config) == 0 {
		common.ApiErrorStatus(c, http.StatusBadRequest, errors.New("expected_version and config are required"), gin.H{"code": "topup_discount_invalid_config"})
		return
	}
	settings, err := model.SaveTopUpDiscountConfig(request.Config, *request.ExpectedVersion, c.GetInt("id"), time.Now())
	if err != nil {
		respondTopUpDiscountError(c, err)
		return
	}
	recordManageAudit(c, "topup_discount.config.update", map[string]any{
		"from_version": *request.ExpectedVersion,
		"to_version":   settings.Version,
		"active":       settings.Active(),
		"amount_mode":  settings.Amount.Mode,
		"site_mode":    settings.Site.Mode,
		"day_mode":     settings.Day.Mode,
		"week_mode":    settings.Week.Mode,
		"month_mode":   settings.Month.Mode,
	})
	common.ApiSuccess(c, newTopUpDiscountConfigView(settings))
}

func GetTopUpDiscountStatus(c *gin.Context) {
	status, err := service.GetTopUpDiscountStatus(time.Now())
	if err != nil {
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, err, gin.H{"code": "topup_discount_unavailable"})
		return
	}
	common.ApiSuccess(c, status)
}

func EstimateTopUpDiscount(c *gin.Context) {
	var request struct {
		Config json.RawMessage `json:"config"`
	}
	if !decodeTopUpDiscountBody(c, &request) {
		return
	}
	estimate, err := service.EstimateTopUpDiscount(request.Config, time.Now())
	if err != nil {
		respondTopUpDiscountError(c, err)
		return
	}
	common.ApiSuccess(c, estimate)
}

func GetSelfTopUpDiscount(c *gin.Context) {
	view, err := service.GetTopUpDiscountSelfView(c.GetInt("id"), time.Now())
	if err != nil {
		// The cause may name tables or drivers; users get the stable code only.
		common.SysError(common.LogText("top-up discount self view failed: user_id=%d, error=%v", c.GetInt("id"), err))
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Top-up discount is temporarily unavailable, please try again"), gin.H{"code": service.ErrTopUpDiscountUnavailable.Error()})
		return
	}
	common.ApiSuccess(c, view)
}

// decodeTopUpDiscountBody accepts exactly one JSON object with known fields.
// A body over the limit is rejected even when its first object is complete.
func decodeTopUpDiscountBody(c *gin.Context, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, topUpDiscountBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return rejectTopUpDiscountBody(c, err, "invalid request body")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return rejectTopUpDiscountBody(c, err, "expected a single JSON object")
	}
	return true
}

// rejectTopUpDiscountBody answers 413 when the body hit the limit, else 400.
func rejectTopUpDiscountBody(c *gin.Context, err error, message string) bool {
	status := http.StatusBadRequest
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		status, message = http.StatusRequestEntityTooLarge, "request body is too large"
	}
	common.ApiErrorStatus(c, status, errors.New(message), gin.H{"code": "topup_discount_invalid_config"})
	return false
}

func respondTopUpDiscountError(c *gin.Context, err error) {
	for _, known := range []struct {
		err    error
		status int
	}{
		{model.ErrTopUpDiscountInvalidConfig, http.StatusBadRequest},
		{model.ErrTopUpDiscountSchemaUnsupported, http.StatusBadRequest},
		{model.ErrTopUpDiscountVersionConflict, http.StatusConflict},
	} {
		if errors.Is(err, known.err) {
			common.ApiErrorStatus(c, known.status, err, gin.H{"code": known.err.Error()})
			return
		}
	}
	common.ApiErrorStatus(c, http.StatusInternalServerError, err)
}
