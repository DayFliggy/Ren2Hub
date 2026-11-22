package router

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestChannelManualStatusRoutesUseDurableState(t *testing.T) {
	for _, route := range []string{"single", "batch", "tag"} {
		for _, target := range []int{common.ChannelStatusEnabled, common.ChannelStatusManuallyDisabled} {
			t.Run(fmt.Sprintf("%s/%d", route, target), func(t *testing.T) {
				engine, db, root, _, channel := channelSecurityFixture(t)
				stored := common.ChannelStatusEnabled
				if target == stored {
					stored = common.ChannelStatusManuallyDisabled
				}
				require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{"status": stored, "tag": "manual-source"}).Error)
				require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channel.Id).Update("enabled", stored == common.ChannelStatusEnabled).Error)
				common.MemoryCacheEnabled = true
				require.NoError(t, model.ReloadChannelCache())
				model.CacheUpdateChannelStatus(channel.Id, target)
				path := fmt.Sprintf("/api/channel/%d/status", channel.Id)
				body := fmt.Sprintf(`{"status":%d}`, target)
				if route == "batch" {
					path = "/api/channel/status/batch"
					body = fmt.Sprintf(`{"ids":[%d],"status":%d}`, channel.Id, target)
				}
				if route == "tag" {
					path = "/api/channel/tag/disabled"
					if target == common.ChannelStatusEnabled {
						path = "/api/channel/tag/enabled"
					}
					body = `{"tag":"manual-source"}`
				}
				response := channelSecurityRequest(t, engine, root, http.MethodPost, path, body)
				require.Equal(t, 200, response.Code, response.Body.String())
				require.Contains(t, response.Body.String(), `"success":true`)
				var actual model.Channel
				require.NoError(t, db.First(&actual, channel.Id).Error)
				require.Equal(t, target, actual.Status, "manual %s must repair stale cache disagreement", route)
				var abilities []model.Ability
				require.NoError(t, db.Where("channel_id = ?", channel.Id).Find(&abilities).Error)
				require.NotEmpty(t, abilities)
				for _, ability := range abilities {
					require.Equal(t, target == common.ChannelStatusEnabled, ability.Enabled)
				}
				response = channelSecurityRequest(t, engine, root, http.MethodPost, path, body)
				require.Equal(t, 200, response.Code)
				require.Contains(t, response.Body.String(), `"success":true`)
			})
		}
	}
}
