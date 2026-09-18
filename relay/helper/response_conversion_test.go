package helper

import (
	"errors"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseConversionErrorRedactsProviderValues(t *testing.T) {
	for _, cause := range []error{
		errors.New("private-provider-value"),
		&types.ConversionLossError{Diagnostics: []types.ConversionDiagnostic{{Code: "citation_source_unrepresentable", Path: "response.output", Message: "private-provider-value", Severity: types.ConversionDiagnosticError}}},
	} {
		apiErr := ResponseConversionError(cause)
		assert.Equal(t, 502, apiErr.StatusCode)
		assert.True(t, types.IsSkipRetryError(apiErr))
		assert.NotContains(t, apiErr.Error(), "private-provider-value")
		var loss *types.ConversionLossError
		require.ErrorAs(t, apiErr, &loss)
		require.Len(t, loss.Diagnostics, 1)
		assert.NotEmpty(t, loss.Diagnostics[0].Code)
		assert.NotEmpty(t, loss.Diagnostics[0].Path)
	}
}

func TestMissingGenerationTerminalIsUpstreamFailure(t *testing.T) {
	jsonErr := ResponseConversionError(relayconvert.ErrResponseTerminalMissing)
	assert.Equal(t, types.ErrorCodeBadResponse, jsonErr.GetErrorCode())
	assert.False(t, types.IsSkipRetryError(jsonErr))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{StreamStatus: relaycommon.NewStreamStatus()}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	state, err := relayconvert.NewResponseStreamState(types.RelayFormatClaude, types.RelayFormatOpenAI, relayconvert.ResponseStreamOptions{})
	require.NoError(t, err)
	apiErr := FinalizeConvertedStream(c, info, state)
	require.NotNil(t, apiErr)
	assert.Equal(t, 502, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeBadResponse, apiErr.GetErrorCode())
	assert.False(t, types.IsSkipRetryError(apiErr), "retry remains subject to acceptance and output barriers")
	assert.False(t, c.Writer.Written())
}
