//go:build upstream_probe

package middleware

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The upstream preparation stage currently conflates storage faults with a
// disabled origin channel. Keep the desired classification as a red diagnostic.
func TestUpstreamPluginOriginChannelStorageFailureIsPrivate500(t *testing.T) {
	setupOriginTaskDB(t)
	previous := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previous })
	channel := insertOriginTaskChannel(t, common.ChannelStatusEnabled)
	insertOriginOwnedTask(t, "storage-probe", 7, channel.Id, "origin-plugin")
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("test:origin-channel-storage-probe", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" {
			tx.AddError(errors.New("private storage diagnostic"))
		}
	}))
	err := applyOriginTaskIntent(originTaskTestContext(7), map[string]any{"originTaskIds": []any{"storage-probe"}}, jsplugin.Meta{Key: "origin-plugin"})
	require.NotNil(t, err)
	require.NotContains(t, err.Message, "private storage diagnostic")
	require.Equal(t, http.StatusInternalServerError, err.StatusCode)
	require.Equal(t, "get_origin_task_channel_failed", err.Code)
}
