package setting

var ModelRequestRateLimitEnabled = false
var ModelRequestRateLimitDurationMinutes = 1
var ModelRequestRateLimitCount = 0
var ModelRequestRateLimitSuccessCount = 1000

// ModelRequestLimits describes the effective configuration, not a live quota
// counter. Total and successful requests use the same configured time window.
type ModelRequestLimits struct {
	Enabled            bool `json:"enabled"`
	WindowMinutes      int  `json:"window_minutes"`
	TotalRequests      int  `json:"total_requests"`
	SuccessfulRequests int  `json:"successful_requests"`
}

func CurrentModelRequestLimits() ModelRequestLimits {
	return ModelRequestLimits{
		Enabled:            ModelRequestRateLimitEnabled,
		WindowMinutes:      ModelRequestRateLimitDurationMinutes,
		TotalRequests:      ModelRequestRateLimitCount,
		SuccessfulRequests: ModelRequestRateLimitSuccessCount,
	}
}
