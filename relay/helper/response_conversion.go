package helper

import (
	"net/http"

	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

const ResponseConversionErrorCode types.ErrorCode = "response_conversion_unrepresentable"

func ResponseConversionError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, ResponseConversionErrorCode, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

func ObserveResponseConversion(c *gin.Context, result relayconvert.ResponseResult) {
	if result.Termination.IsTerminal() {
		c.Set("response_termination", result.Termination)
	}
	if len(result.Diagnostics) != 0 {
		previous, _ := c.Get("response_conversion_diagnostics")
		diagnostics, _ := previous.([]types.ConversionDiagnostic)
		c.Set("response_conversion_diagnostics", append(diagnostics, result.Diagnostics...))
	}
}
