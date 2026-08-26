package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestLiveRouteRequestSupportedKeepsNonUnifiedProtocolsOnLegacyRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name                    string
		path                    string
		compact                 bool
		requiresNativeResponses bool
		want                    bool
	}{
		{name: "chat", path: "/v1/chat/completions", want: true},
		{name: "task submit", path: "/suno/submit/generate", want: false},
		{name: "video submit", path: "/v1/videos", want: false},
		{name: "responses compact", path: "/v1/responses/compact", compact: true, want: false},
		{name: "native responses", path: "/v1/responses", requiresNativeResponses: true, want: false},
		{name: "midjourney submit", path: "/mj/submit/imagine", want: false},
		{name: "prefixed midjourney submit", path: "/fast/mj/submit/imagine", want: false},
		{name: "video remix", path: "/v1/videos/task_123/remix", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, test.path, nil)

			assert.Equal(t, test.want, liveRouteRequestSupported(c, test.compact, test.requiresNativeResponses))
		})
	}
}
