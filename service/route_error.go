package service

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
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
	default:
		common.SysError(common.LogText("unable to acquire pinned route channel: %v", err))
		options = append(options, types.ErrOptionWithHideErrMsg("Unable to load pinned channel"))
	}
	return types.NewErrorWithStatusCode(err, code, status, options...)
}

func PinnedTaskRouteError(err error) *dto.TaskError {
	taskErr := TaskErrorFromAPIError(PinnedRouteError(err))
	taskErr.LocalError = true
	taskErr.NoRetry = true
	return taskErr
}
