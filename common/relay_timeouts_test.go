package common

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRelayPhaseTimeoutRejectsUnboundedConfiguration(t *testing.T) {
	for _, value := range []int{0, -1} {
		assert.Equal(t, 300*time.Second, RelayPhaseTimeout(value))
	}
	assert.Equal(t, 300*time.Second, RelayPhaseTimeout(math.MaxInt))
	assert.Equal(t, 900*time.Second, RelayPhaseTimeout(900))
}
