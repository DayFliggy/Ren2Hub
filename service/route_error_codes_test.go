package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

// The routing error code table repeats the names of the native classifier,
// which is upstream text. Every class must agree with the native verdict for
// an upstream error carrying that code: account and server codes fail the
// channel, request codes are ignored.
func TestRouteErrorCodesAgreeWithNativeClassifier(t *testing.T) {
	info := &relaycommon.RelayInfo{OriginModelName: "route-model", StartTime: time.Now()}
	for code, class := range nativeRouteErrorCodes {
		apiErr := types.WithOpenAIError(types.OpenAIError{Message: "m", Type: "upstream_error", Code: code}, http.StatusBadGateway)
		want := perfmetrics.OutcomeFailure
		if class == routeErrorRequest {
			want = perfmetrics.OutcomeIgnored
		}
		require.Equal(t, want, perfmetrics.ClassifyRelayOutcome(context.Background(), info, apiErr), code)
		require.Equal(t, code, routeErrorCode(code))
	}
	require.Equal(t, string(types.ErrorCodeDoRequestFailed), routeErrorCode(string(types.ErrorCodeDoRequestFailed)))
	require.Equal(t, routeUnrecognizedErrorCode, routeErrorCode(errors.New("x").Error()))
}
