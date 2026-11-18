package service

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// PinnedRouteError keeps authorization failures distinct from local capacity,
// cancellation and storage failures. None of them may replay a pinned request.
func PinnedRouteError(err error) *types.NewAPIError {
	status := http.StatusInternalServerError
	code := types.ErrorCodeGetChannelFailed
	options := []types.NewAPIErrorOptions{types.ErrOptionWithSkipRetry()}
	switch {
	case errors.Is(err, model.ErrTokenRouteForbidden), errors.Is(err, gorm.ErrRecordNotFound):
		status, code = http.StatusForbidden, types.ErrorCodeAccessDenied
	case errors.Is(err, ErrConcurrencyFull):
		status, code = http.StatusTooManyRequests, "channel_concurrency_exceeded"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code = http.StatusRequestTimeout, "request_cancelled"
	case errors.Is(err, errConcurrencyLimitUnavailable):
		status, code = ConcurrencyAdmissionError("channel", err)
		common.SysError(common.LogText("unable to acquire pinned route channel: %v", err))
		options = append(options, types.ErrOptionWithHideErrMsg("Channel concurrency limit is unavailable"))
	default:
		common.SysError(common.LogText("unable to acquire pinned route channel: %v", err))
		options = append(options, types.ErrOptionWithHideErrMsg("Unable to load pinned channel"))
	}
	return types.NewErrorWithStatusCode(err, code, status, options...)
}

// PreferUpstreamError 在重试轮次选不到渠道时保留上一次真实的上游错误；
// 选路失败本身只记日志。首轮没有上游错误，照常返回选路错误。
func PreferUpstreamError[T any](last, selection *T) *T {
	if last != nil {
		return last
	}
	return selection
}

// Text renders the failure for transports without their own localization.
func (e *ChannelSelectError) Text(c *gin.Context) string {
	if e.MessageID != "" {
		return i18n.T(c, e.MessageID, e.Params)
	}
	return e.Message
}

func logRouteLoadFailure(c *gin.Context, err error) {
	logger.LogError(c, common.LogText("load routing candidates failed: %v", err))
}

// routeLoadFailure renders a candidate lookup or admission failure. Capacity is
// the only expected outcome; storage details stay in the server log.
func routeLoadFailure(c *gin.Context, err error) *ChannelSelectError {
	logRouteLoadFailure(c, err)
	return classifyRouteLoadFailure(err)
}

func classifyRouteLoadFailure(err error) *ChannelSelectError {
	if errors.Is(err, ErrConcurrencyFull) {
		return &ChannelSelectError{StatusCode: http.StatusTooManyRequests, Code: "channel_concurrency_exceeded", Message: err.Error()}
	}
	if errors.Is(err, errConcurrencyLimitUnavailable) {
		status, code := ConcurrencyAdmissionError("channel", err)
		return &ChannelSelectError{StatusCode: status, Code: code, Message: "Channel concurrency limit is unavailable"}
	}
	return &ChannelSelectError{StatusCode: http.StatusInternalServerError, Code: types.ErrorCodeGetChannelFailed, Message: "Unable to load token routing"}
}

func PinnedTaskRouteError(err error) *dto.TaskError {
	if status, code, message, matched := originTaskRouteFailure(err); matched {
		taskErr := TaskErrorWrapperLocal(errors.New(message), code, status)
		taskErr.NoRetry = true
		return taskErr
	}
	taskErr := TaskErrorFromAPIError(PinnedRouteError(err))
	taskErr.LocalError = true
	taskErr.NoRetry = true
	return taskErr
}
