package router

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVideoClaimedPluginRateLimitRunsAfterClaim(t *testing.T) {
	p := newVideoRatePipeline(t)
	first := p.request("/v1/videos", claimedInvalidVideoRateBody)
	assert.Equal(t, http.StatusBadRequest, first.Code, first.Body.String())
	assert.True(t, p.claimed, "the production route must claim Sora before decoding")
	second := p.request("/v1/videos", claimedInvalidVideoRateBody)
	assert.Equal(t, http.StatusTooManyRequests, second.Code, second.Body.String())
	assert.True(t, p.claimed, "the production route must claim before rate limiting")
	p.assertNoChargeOrTask(t)
}
func TestVideoUnclaimedRequestsRetainNativeRateLimitScope(t *testing.T) {
	for _, path := range []string{"/v1/video/generations", "/v1/videos"} {
		t.Run(path, func(t *testing.T) {
			p := newVideoRatePipeline(t)
			for i := 0; i < 2; i++ {
				response := p.request(path, unclaimedVideoRateBody)
				assert.Equal(t, http.StatusServiceUnavailable, response.Code, response.Body.String())
				assert.False(t, p.claimed)
			}
			// Sora claims only /v1/videos. Neither unclaimed entry point may consume
			// the later real protocol request's shared user budget.
			first := p.request("/v1/videos", claimedInvalidVideoRateBody)
			assert.Equal(t, http.StatusBadRequest, first.Code, first.Body.String())
			assert.True(t, p.claimed)
			second := p.request("/v1/videos", claimedInvalidVideoRateBody)
			assert.Equal(t, http.StatusTooManyRequests, second.Code, second.Body.String())
			assert.True(t, p.claimed)
			p.assertNoChargeOrTask(t)
		})
	}
}
