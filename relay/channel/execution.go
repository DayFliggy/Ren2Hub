package channel

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/execution"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const executionContextKey = "request_execution"
const BeforeCallContextKey = "request_execution_before_call"
const BeforeCallAdmissionContextKey = "request_execution_before_call_admission"
const BasicValidatedContextKey = "request_execution_basic_validated"

func AfterBasicValidation(c *gin.Context) error {
	if value, ok := c.Get(BasicValidatedContextKey); ok {
		if admit, valid := value.(func() error); valid {
			return admit()
		}
	}
	return nil
}

func RequestExecution(c *gin.Context, class execution.Class) *execution.Request {
	if value, ok := c.Get(executionContextKey); ok {
		if request, valid := value.(*execution.Request); valid {
			return request
		}
	}
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		requestID = common.GetUUID()
		c.Set(common.RequestIdKey, requestID)
	}
	request := execution.New(requestID, class)
	c.Set(executionContextKey, request)
	return request
}

func CheckExecutionCandidate(c *gin.Context, info *relaycommon.RelayInfo) (execution.Candidate, error) {
	if c == nil || info == nil || info.ChannelMeta == nil {
		return execution.Candidate{}, errors.New("request candidate is missing initialized channel metadata")
	}
	modelName := info.RouteCapabilityModel
	if modelName == "" {
		modelName = info.OriginModelName
	}
	return RequestExecution(c, execution.Generation).CheckCandidate(execution.Candidate{
		ChannelID: info.ChannelId,
		Model:     modelName,
		Protocol:  string(info.RelayFormat),
		Stage:     string(info.CompactAttemptStage),
	})
}

// BeginUpstreamCall is shared by HTTP and SDK transports. Callbacks update
// routing diagnostics only after an actual application call obtains budget.
func BeginUpstreamCall(c *gin.Context, info *relaycommon.RelayInfo) (func(*http.Response, error), error) {
	if c == nil || c.Request == nil {
		return nil, errors.New("upstream call is missing request context")
	}
	request := RequestExecution(c, execution.Generation)
	if value, ok := c.Get("route_live_selection"); ok {
		if selection, valid := value.(service.LiveRouteSelection); valid {
			budget := selection.Retry.Budget()
			switch selection.Retry.Mode {
			case model.RoutePolicyRetryNone:
				request.TightenCallLimit(1)
			case model.RoutePolicyRetrySameChannel:
				request.TightenCallLimit(1 + budget.SameChannelAttempts)
			case model.RoutePolicyRetryNextChannel:
				request.TightenCallLimit(1 + budget.FailoverAttempts)
			case model.RoutePolicyRetrySameThenNext:
				request.TightenCallLimit(1 + budget.SameChannelAttempts + budget.FailoverAttempts)
			}
		}
	}
	candidate, err := CheckExecutionCandidate(c, info)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
	}
	if value, ok := c.Get(BeforeCallAdmissionContextKey); ok {
		if admit, valid := value.(func() error); valid {
			if err := admit(); err != nil {
				return nil, types.NewError(err, types.ErrorCodeGetChannelFailed, types.ErrOptionWithSkipRetry())
			}
		}
	}
	call, err := request.BeginCall(c.Request.Context(), candidate.ID)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeDoRequestFailed, types.ErrOptionWithSkipRetry())
	}
	info.RetryIndex = call.Sequence - 1
	if callback, ok := c.Get(BeforeCallContextKey); ok {
		if beforeCall, valid := callback.(func(execution.Call)); valid {
			beforeCall(call)
		}
	}
	return func(response *http.Response, err error) {
		statusCode := 0
		if response != nil {
			statusCode = response.StatusCode
		}
		request.ObserveHeaders(call.ID, statusCode, err != nil)
	}, nil
}
