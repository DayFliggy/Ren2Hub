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

const topUpBonusBodyLimit = 64 << 10

type topUpBonusConfigView struct {
	Version   int64                  `json:"version"`
	CreatedAt int64                  `json:"created_at"`
	CreatedBy int                    `json:"created_by"`
	Config    model.TopUpBonusConfig `json:"config"`
}

func newTopUpBonusConfigView(settings *model.TopUpBonusSettings) topUpBonusConfigView {
	return topUpBonusConfigView{
		Version: settings.Version, CreatedAt: settings.CreatedAt, CreatedBy: settings.CreatedBy, Config: settings.Config,
	}
}

func GetTopUpBonusConfig(c *gin.Context) {
	settings, err := model.CurrentTopUpBonusSettings()
	if err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError, err)
		return
	}
	common.ApiSuccess(c, newTopUpBonusConfigView(settings))
}

func UpdateTopUpBonusConfig(c *gin.Context) {
	var request struct {
		ExpectedVersion *int64          `json:"expected_version"`
		Config          json.RawMessage `json:"config"`
	}
	if !decodeTopUpBonusBody(c, &request) {
		return
	}
	if request.ExpectedVersion == nil || len(request.Config) == 0 {
		common.ApiErrorStatus(c, http.StatusBadRequest, errors.New("expected_version and config are required"), gin.H{"code": "topup_bonus_invalid_config"})
		return
	}
	settings, err := model.SaveTopUpBonusConfig(request.Config, *request.ExpectedVersion, c.GetInt("id"), time.Now())
	if err != nil {
		respondTopUpBonusError(c, err)
		return
	}
	recordManageAudit(c, "topup_bonus.config.update", map[string]any{
		"from_version": *request.ExpectedVersion,
		"to_version":   settings.Version,
		"active":       settings.Active(),
		"amount_mode":  settings.Amount.Mode,
		"site_mode":    settings.Site.Mode,
		"day_mode":     settings.Day.Mode,
		"week_mode":    settings.Week.Mode,
		"month_mode":   settings.Month.Mode,
	})
	common.ApiSuccess(c, newTopUpBonusConfigView(settings))
}

func GetTopUpBonusStatus(c *gin.Context) {
	status, err := service.GetTopUpBonusStatus(time.Now())
	if err != nil {
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, err, gin.H{"code": "topup_bonus_unavailable"})
		return
	}
	common.ApiSuccess(c, status)
}

func EstimateTopUpBonus(c *gin.Context) {
	var request struct {
		Config json.RawMessage `json:"config"`
	}
	if !decodeTopUpBonusBody(c, &request) {
		return
	}
	estimate, err := service.EstimateTopUpBonus(request.Config, time.Now())
	if err != nil {
		respondTopUpBonusError(c, err)
		return
	}
	common.ApiSuccess(c, estimate)
}

func GetSelfTopUpBonus(c *gin.Context) {
	view, err := service.GetTopUpBonusSelfView(c.GetInt("id"), time.Now())
	if err != nil {
		respondSelfTopUpBonusError(c, err)
		return
	}
	common.ApiSuccess(c, view)
}

// respondSelfTopUpBonusError hides the cause, which may name tables or
// drivers; users get the stable code only.
func respondSelfTopUpBonusError(c *gin.Context, err error) {
	common.SysError(common.LogText("top-up bonus view failed: user_id=%d, error=%v", c.GetInt("id"), err))
	common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Top-up bonus is temporarily unavailable, please try again"), gin.H{"code": service.ErrTopUpBonusUnavailable.Error()})
}

// decodeTopUpBonusBody accepts exactly one JSON object with known fields.
// A body over the limit is rejected even when its first object is complete.
func decodeTopUpBonusBody(c *gin.Context, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, topUpBonusBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return rejectTopUpBonusBody(c, err, "invalid request body")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return rejectTopUpBonusBody(c, err, "expected a single JSON object")
	}
	return true
}

// rejectTopUpBonusBody answers 413 when the body hit the limit, else 400.
func rejectTopUpBonusBody(c *gin.Context, err error, message string) bool {
	status := http.StatusBadRequest
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		status, message = http.StatusRequestEntityTooLarge, "request body is too large"
	}
	common.ApiErrorStatus(c, status, errors.New(message), gin.H{"code": "topup_bonus_invalid_config"})
	return false
}

func respondTopUpBonusError(c *gin.Context, err error) {
	for _, known := range []struct {
		err    error
		status int
	}{
		{model.ErrTopUpBonusInvalidConfig, http.StatusBadRequest},
		{model.ErrTopUpBonusSchemaUnsupported, http.StatusBadRequest},
		{model.ErrTopUpBonusVersionConflict, http.StatusConflict},
	} {
		if errors.Is(err, known.err) {
			common.ApiErrorStatus(c, known.status, err, gin.H{"code": known.err.Error()})
			return
		}
	}
	common.ApiErrorStatus(c, http.StatusInternalServerError, err)
}
