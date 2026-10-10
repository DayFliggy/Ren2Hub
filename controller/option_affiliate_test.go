package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateOptionRejectsNonFiniteAffiliateCap(t *testing.T) {
	for _, value := range []string{"NaN", "+Inf", "-Inf"} {
		t.Run(value, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(
				http.MethodPut,
				"/api/option/",
				strings.NewReader(`{"key":"AffiliatePerInviteeCap","value":"`+value+`"}`),
			)

			UpdateOption(context)

			assert.Equal(t, http.StatusOK, response.Code)
			var payload struct {
				Success    bool   `json:"success"`
				Message    string `json:"message"`
				MessageKey string `json:"message_key"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
			assert.False(t, payload.Success)
			assert.Equal(t, "The rebate cap per invitee must be a finite non-negative number", payload.MessageKey)
			assert.Equal(t, payload.MessageKey, payload.Message)
		})
	}
}
