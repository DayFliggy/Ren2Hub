package service

import (
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// failChannelLimitRead makes the admission-time read of the channel's
// max_concurrency fail while the candidate query keeps working.
func failChannelLimitRead(t *testing.T, db *gorm.DB, channelID int) {
	t.Helper()
	gatewayConcurrency.forget("channel", []int{channelID})
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:channel-limit-read-failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" && slices.Contains(tx.Statement.Selects, "max_concurrency") {
			tx.AddError(errors.New("private limit read failure"))
		}
	}))
}

// An unreadable channel limit is 503 channel_concurrency_unavailable on every
// entry, ends the attempt without trying another candidate, and keeps a
// strict session binding (UNIFIED_ROUTING contract, audit item A3).
func TestChannelLimitReadFailureIsConcurrencyUnavailable(t *testing.T) {
	assertUnavailable := func(t *testing.T, selectErr *ChannelSelectError) {
		t.Helper()
		require.NotNil(t, selectErr)
		require.Equal(t, http.StatusServiceUnavailable, selectErr.StatusCode)
		require.EqualValues(t, "channel_concurrency_unavailable", selectErr.Code)
		require.NotContains(t, selectErr.Text(routeTestContext(t)), "private")
	}
	t.Run("ordinary selection does not try the next candidate", func(t *testing.T) {
		db := setupAffinityChannels(t)
		failChannelLimitRead(t, db, 11)
		selected, selectErr := SelectChannelForRequest(routeTestContext(t), "route-model", nil)
		assertUnavailable(t, selectErr)
		require.Nil(t, selected)
		require.Zero(t, ChannelInFlight(22), "the lower-priority channel is not admitted instead")
	})
	t.Run("prefer affinity returns 503 instead of falling back", func(t *testing.T) {
		db := setupAffinityChannels(t)
		c, key := enableRouteAffinity(t, "prefer", 22)
		failChannelLimitRead(t, db, 22)
		_, selectErr := SelectChannelForRequest(c, "route-model", nil)
		assertUnavailable(t, selectErr)
		_, found := affinityBinding(t, key)
		require.True(t, found)
	})
	t.Run("strict affinity returns 503 and keeps the binding", func(t *testing.T) {
		db := setupAffinityChannels(t)
		c, key := enableRouteAffinity(t, "strict", 22)
		failChannelLimitRead(t, db, 22)
		_, selectErr := SelectChannelForRequest(c, "route-model", nil)
		assertUnavailable(t, selectErr)
		bound, found := affinityBinding(t, key)
		require.True(t, found, "an unreadable limit must not drop a valid session binding")
		require.Equal(t, 22, bound)
	})
	t.Run("pinned and origin-task admission map to 503", func(t *testing.T) {
		db := setupAffinityChannels(t)
		failChannelLimitRead(t, db, 22)
		_, err := AcquirePinnedRouteChannel(routeTestContext(t), 22, "route-model")
		apiErr := PinnedRouteError(err)
		require.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
		require.EqualValues(t, "channel_concurrency_unavailable", apiErr.GetErrorCode())
		require.NotContains(t, apiErr.Error(), "private")
		_, err = AcquireOriginTaskRouteChannel(routeTestContext(t), 22, "route-model")
		taskErr := PinnedTaskRouteError(err)
		require.Equal(t, http.StatusServiceUnavailable, taskErr.StatusCode)
		require.Equal(t, "channel_concurrency_unavailable", taskErr.Code)
	})
	t.Run("candidate query failure stays 500", func(t *testing.T) {
		db := setupAffinityChannels(t)
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:candidate-read-failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "channels" {
				tx.AddError(errors.New("private candidate read failure"))
			}
		}))
		_, selectErr := SelectChannelForRequest(routeTestContext(t), "route-model", nil)
		require.NotNil(t, selectErr)
		require.Equal(t, http.StatusInternalServerError, selectErr.StatusCode)
	})
	t.Run("deleted channel keeps its own mapping", func(t *testing.T) {
		db := setupAffinityChannels(t)
		require.NoError(t, db.Delete(&model.Channel{}, 22).Error)
		gatewayConcurrency.forget("channel", []int{22})
		status, code := ConcurrencyAdmissionError("channel", refreshPendingConcurrencyLimit(routeTestContext(t).Request.Context(), concurrencyKey{"channel", 22}))
		require.Equal(t, http.StatusForbidden, status)
		require.EqualValues(t, "channel_unavailable", code)
	})
}
