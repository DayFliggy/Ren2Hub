package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserLogResponseModelProjection(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   any
		visible bool
	}{
		{"model", "provider/model-2026-09-25", true},
		{"object", map[string]any{"secret": "hidden"}, false},
		{"newline", "model\nsecret", false},
		{"too long", strings.Repeat("m", 513), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := common.Marshal(map[string]any{"response_model_name": test.value, "admin_info": map[string]any{"secret": "hidden"}})
			require.NoError(t, err)
			logs := []*Log{{Other: string(body)}}
			formatUserLogs(logs, 0)
			var actual map[string]any
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &actual))
			_, visible := actual["response_model_name"]
			assert.Equal(t, test.visible, visible)
			assert.NotContains(t, actual, "admin_info")
		})
	}
}
