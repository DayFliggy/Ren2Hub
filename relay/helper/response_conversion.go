package helper

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

const ResponseConversionErrorCode types.ErrorCode = "response_conversion_unrepresentable"

func ResponseConversionError(err error) *types.NewAPIError {
	if errors.Is(err, relayconvert.ErrResponseTerminalMissing) || errors.Is(err, relayconvert.ErrStreamTerminalMissing) {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	var loss *types.ConversionLossError
	if !errors.As(err, &loss) {
		loss = &types.ConversionLossError{Diagnostics: []types.ConversionDiagnostic{{Code: string(ResponseConversionErrorCode), Path: "response", Severity: types.ConversionDiagnosticError}}}
	}
	// Converter errors can include provider-controlled values. Keep only the
	// declared code/path contract at the HTTP and logging boundary.
	diagnostics := make([]types.ConversionDiagnostic, 0, len(loss.Diagnostics))
	for _, diagnostic := range loss.Diagnostics {
		diagnostic.Message = "target protocol cannot preserve upstream response semantics"
		diagnostics = append(diagnostics, diagnostic)
	}
	return types.NewErrorWithStatusCode(&types.ConversionLossError{Diagnostics: diagnostics}, ResponseConversionErrorCode, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

func ObserveResponseConversion(c *gin.Context, result relayconvert.ResponseResult) {
	if result.Termination.IsTerminal() {
		c.Set("response_termination", result.Termination)
	}
	if len(result.Diagnostics) != 0 {
		previous, _ := c.Get("response_conversion_diagnostics")
		diagnostics, _ := previous.([]types.ConversionDiagnostic)
		for _, diagnostic := range result.Diagnostics {
			found := false
			for _, previous := range diagnostics {
				if previous.Code == diagnostic.Code && previous.Path == diagnostic.Path {
					found = true
					break
				}
			}
			if !found {
				diagnostics = append(diagnostics, diagnostic)
			}
		}
		c.Set("response_conversion_diagnostics", diagnostics)
	}
}

func ObserveResponseStream(c *gin.Context, state *relayconvert.ResponseStreamState) {
	if state == nil {
		return
	}
	ObserveResponseConversion(c, relayconvert.ResponseResult{Termination: state.Termination(), Diagnostics: state.Diagnostics()})
}
