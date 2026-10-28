package router

import (
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestHostProtocolRegistryDrivesProtocolRoutesOnce(t *testing.T) {
	engine := gin.New()
	SetTaskPluginProtocolRouter(engine)

	expected := []string{
		"POST /v1/responses",
		"GET /v1/responses/:response_id",
		"POST /v1/videos",
		"GET /v1/videos/:task_id",
		"GET /v1/videos/:task_id/content",
		"HEAD /v1/videos/:task_id/content",
		"POST /v1/images/generations",
		"POST /v1/images/edits",
	}
	actual := make([]string, 0, len(engine.Routes()))
	for _, route := range engine.Routes() {
		actual = append(actual, fmt.Sprintf("%s %s", route.Method, route.Path))
	}
	sort.Strings(expected)
	sort.Strings(actual)
	assert.Equal(t, expected, actual)
}

func TestVideoCreateRateLimitPrecedesSharedChannelSelection(t *testing.T) {
	handlers, err := taskPluginProtocolHandlers("openai_video", "create")
	if !assert.NoError(t, err) {
		return
	}

	index := func(fragment string) int {
		for i, handler := range handlers {
			name := runtime.FuncForPC(reflect.ValueOf(handler).Pointer()).Name()
			if strings.Contains(name, fragment) {
				return i
			}
		}
		return -1
	}
	rateLimit := index(".ModelRequestRateLimit.func")
	selection := index(".RouteTaskPluginEndpoint.func")
	assert.GreaterOrEqual(t, rateLimit, 0)
	assert.GreaterOrEqual(t, selection, 0)
	assert.Less(t, rateLimit, selection, "shared video requests must be limited before ordinary/plugin routing")
}
