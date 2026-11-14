//go:build known_upstream_restored

package router

import (
	"net/http"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRestoredProbeVideoCreateRateLimitPrecedesSharedChannelSelection(t *testing.T) {
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

func TestRestoredProbeLegacyVideoGenerationRateLimitCoversOrdinaryAndPluginRoutes(t *testing.T) {
	p := registerVideoRatePipeline()
	p.request("/v1/video/generations", unclaimedVideoRateBody)
	index := func(fragment string) int {
		for i, name := range p.names {
			if strings.Contains(name, fragment) {
				return i
			}
		}
		return -1
	}
	rateLimit := index(".ModelRequestRateLimit.func")
	pin := index(".PinTaskPluginEndpoint.func")
	assert.GreaterOrEqual(t, rateLimit, 0)
	assert.GreaterOrEqual(t, pin, 0)
	assert.Less(t, rateLimit, pin, "legacy video requests must be limited before ordinary/plugin route selection")
}

func TestRestoredProbeLegacyVideoGenerationRateLimitRejectsSecondRequest(t *testing.T) {
	for _, path := range []string{"/v1/video/generations", "/v1/videos"} {
		t.Run(path, func(t *testing.T) {
			p := newVideoRatePipeline(t)
			// The historical test's 204 came from its artificial terminal handler.
			// This real chain reaches native no-channel validation on the first call.
			assert.Equal(t, http.StatusServiceUnavailable, p.request(path, unclaimedVideoRateBody).Code)
			assert.False(t, p.claimed)
			assert.Equal(t, http.StatusTooManyRequests, p.request(path, unclaimedVideoRateBody).Code,
				"unclaimed video requests historically consumed the shared request limit")
			assert.False(t, p.claimed)
			p.assertNoChargeOrTask(t)
		})
	}
}
