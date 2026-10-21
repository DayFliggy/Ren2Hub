package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

// Pinned failures follow NewAPI's pinnedChannelUnavailable: token pins get a
// localized message, origin-task pins a fixed code that task polling matches.
func TestPinnedChannelUnavailableMatchesUpstream(t *testing.T) {
	for _, tc := range []struct {
		name      string
		source    dto.ChannelPinSource
		channelID int
		disable   bool
		status    int
		code      types.ErrorCode
		messageID string
	}{
		{"token pin to missing channel", dto.PinSourceToken, 99, false, http.StatusBadRequest, "", i18n.MsgDistributorInvalidChannelId},
		{"token pin to disabled channel", dto.PinSourceToken, 11, true, http.StatusForbidden, "", i18n.MsgDistributorChannelDisabled},
		{"origin task on missing channel", dto.PinSourceOriginTask, 99, false, http.StatusBadRequest, "origin_task_channel_disabled", ""},
		{"origin task on disabled channel", dto.PinSourceOriginTask, 11, true, http.StatusBadRequest, "origin_task_channel_disabled", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupRouteTest(t)
			createRouteChannel(t, db, 11, "route-model")
			if tc.disable {
				require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("status", common.ChannelStatusManuallyDisabled).Error)
			}
			c := routeTestContext(t)
			GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: tc.channelID, Source: tc.source, RetryMode: dto.PinRetrySingleAttempt})
			_, selectErr := SelectChannelForRequest(c, "route-model", nil)
			require.NotNil(t, selectErr)
			require.Equal(t, tc.status, selectErr.StatusCode)
			require.Equal(t, tc.code, selectErr.Code)
			require.Equal(t, tc.messageID, selectErr.MessageID)
			require.True(t, selectErr.MessageID != "" || selectErr.Message != "", "every pinned failure must explain itself")
		})
	}
}
