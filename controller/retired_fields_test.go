package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelManagementRejectsRetiredGroupFields(t *testing.T) {
	for _, test := range []struct {
		name, method, path, body string
		handler                  gin.HandlerFunc
	}{
		{"list", "GET", "/api/channel/?group=", "", GetAllChannels},
		{"search", "GET", "/api/channel/search?group=vip", "", SearchChannels},
		{"create", "POST", "/api/channel/", `{"channel":{"group":"vip"}}`, AddChannel},
		{"update", "PUT", "/api/channel/", `{"id":1,"group":null}`, UpdateChannel},
		{"tag", "PUT", "/api/channel/tag", `{"tag":"a","groups":"vip"}`, EditTagChannels},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			test.handler(ctx)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), `"code":"retired_field"`)
		})
	}
}
