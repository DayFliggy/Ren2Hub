package requestplugin

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPluginErrorReportsUseSanitizedCodeAndTaskMessage(t *testing.T) {
	err := types.WithOpenAIError(types.OpenAIError{Message: "CHANNEL_SECRET", Code: "CHANNEL_SECRET"}, 502)
	err.BindSensitiveContext(nil, "CHANNEL_SECRET")
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI} {
		report := newReportError(format, err, nil)
		body, e := json.Marshal(report)
		require.NoError(t, e)
		assert.NotContains(t, string(body), "CHANNEL_SECRET")
	}
	local := types.NewError(err, types.ErrorCodeRequestRejected)
	task := TaskError(local)
	assert.NotContains(t, task.Message, "CHANNEL_SECRET")
	assert.NotContains(t, task.Code, "CHANNEL_SECRET")
}
