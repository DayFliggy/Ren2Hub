package middleware

import "github.com/gin-gonic/gin"

// The console polls every 10 seconds; 30 per minute leaves room for several
// open tabs while bounding what one account can drive.
const (
	dashboardStatusRateLimitNum      = 30
	dashboardStatusRateLimitDuration = 60
)

// DashboardStatusRateLimit limits the system-status poll per user. It runs
// after UserAuth, which sets the user id the limiter keys on.
func DashboardStatusRateLimit() gin.HandlerFunc {
	return userRateLimitFactory(dashboardStatusRateLimitNum, dashboardStatusRateLimitDuration, "DS:system-status")
}
