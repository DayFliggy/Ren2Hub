//go:build known_upstream_restored

package middleware

import (
	"fmt"
	"net/http"

	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRestoredProbeApplyOriginTaskIntentReadsOriginChannelThroughCache(t *testing.T) {
	for _, cache := range []bool{false, true} {
		t.Run(fmt.Sprint("cache=", cache), func(t *testing.T) {
			setupOriginTaskDB(t)
			previousCache := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = cache
			t.Cleanup(func() { common.MemoryCacheEnabled = previousCache })
			model.InitChannelCache()
			channel := insertOriginTaskChannel(t, common.ChannelStatusEnabled)
			insertOriginOwnedTask(t, "task-own", 7, channel.Id, "origin-plugin")
			meta := jsplugin.Meta{Key: "origin-plugin"}
			intent := map[string]any{"originTaskIds": []any{"task-own"}}

			c := originTaskTestContext(7)
			require.Nil(t, applyOriginTaskIntent(c, intent, meta), "a channel created after the cache was built is read from the database")
			pinnedID, _ := resolvedOriginPin(c)
			require.Equal(t, channel.Id, pinnedID)

			require.NoError(t, model.DB.Delete(&model.Channel{}, channel.Id).Error)
			intentErr := applyOriginTaskIntent(originTaskTestContext(7), intent, meta)
			require.NotNil(t, intentErr)
			assert.Equal(t, "origin_task_channel_disabled", intentErr.Code)

			require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("test:origin-intent-channel-storage", func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" {
					tx.AddError(fmt.Errorf("private origin channel storage failure"))
				}
			}))
			intentErr = applyOriginTaskIntent(originTaskTestContext(7), intent, meta)
			require.NotNil(t, intentErr)
			assert.Equal(t, http.StatusBadRequest, intentErr.StatusCode)
			assert.Equal(t, "origin_task_channel_disabled", intentErr.Code)
			assert.NotContains(t, intentErr.Message, "private")
		})
	}
}
