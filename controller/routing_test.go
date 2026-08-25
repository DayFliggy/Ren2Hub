package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRoutingAPIIsFailClosedByDefault(t *testing.T) {
	t.Setenv("NEXT_TOKEN_PRIVATE_ROUTING_ENABLED", "false")
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("id", 1)

	ListRouteProfiles(c)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Code    string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Equal(t, "feature_disabled", response.Code)
}

func TestFrontendRoutingCapabilityRemainsDisabledUntilSelectorIsLive(t *testing.T) {
	assert.Equal(t, "disabled", routingCapabilityStatus())
}
