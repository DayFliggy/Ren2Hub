package common

import (
	"math"
	"time"
)

const DefaultRelayPhaseTimeout = 300

var RelayResponseHeaderTimeout = DefaultRelayPhaseTimeout
var RelayNonStreamTimeout = DefaultRelayPhaseTimeout
var StreamingFirstOutputTimeout = DefaultRelayPhaseTimeout

// RelayPhaseTimeout keeps a finite boundary even before environment initialization
// or when a deployment supplies zero or a negative value.
func RelayPhaseTimeout(seconds int) time.Duration {
	if seconds <= 0 || int64(seconds) > math.MaxInt64/int64(time.Second) {
		seconds = DefaultRelayPhaseTimeout
	}
	return time.Duration(seconds) * time.Second
}
