package service

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

// B9: a channel whose API type cannot serve /v1/responses/compact is skipped
// during selection instead of being picked and failing with a non-retryable 400.
func TestCompactRequestsSkipChannelsWithoutCompactSupport(t *testing.T) {
	db := setupRouteTest(t)
	createRouteChannel(t, db, 11, "route-model")
	createRouteChannel(t, db, 22, "route-model")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Updates(map[string]any{"type": constant.ChannelTypeAnthropic, "priority": 10}).Error)
	selectFor := func(path string) (*model.Channel, *ChannelSelectError) {
		c := routeTestContext(t)
		GetChannelConstraints(c).AddFilter(dto.ChannelFilter{Kind: dto.FilterRequestPath, RequestPath: path})
		return SelectChannelForRequest(c, "route-model", nil)
	}
	selected, selectErr := selectFor("/v1/responses/compact")
	require.Nil(t, selectErr)
	require.Equal(t, 22, selected.Id, "the higher-priority Anthropic channel cannot compact")
	selected, selectErr = selectFor("/v1/responses")
	require.Nil(t, selectErr)
	require.Equal(t, 11, selected.Id, "only the compact endpoint is narrowed")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 22).Update("type", constant.ChannelTypeAnthropic).Error)
	_, selectErr = selectFor("/v1/responses/compact")
	require.NotNil(t, selectErr)
	require.Equal(t, http.StatusServiceUnavailable, selectErr.StatusCode)
	require.True(t, selectErr.NoAvailableChannel)
	// 与上游一致走 i18n 文案，重试轮次经 Text 输出时也是本地化文本。
	require.Equal(t, i18n.MsgDistributorNoAvailableChannel, selectErr.MessageID)
	require.Equal(t, map[string]any{"Model": "route-model"}, selectErr.Params)
}
