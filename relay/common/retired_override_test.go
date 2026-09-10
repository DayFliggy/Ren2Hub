package common

import (
	"testing"

	hostcommon "github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestParamOverrideRejectsRetiredContextBeforeChangingRequest(t *testing.T) {
	for _, conditions := range []any{
		map[string]any{"using_group": "vip"},
		[]any{map[string]any{"path": "token_group", "mode": "full", "value": "vip", "pass_missing_key": true}},
	} {
		override := map[string]any{"operations": []map[string]any{{
			"mode": "set", "path": "service_tier", "value": "priority", "conditions": conditions,
		}}}
		output, err := ApplyParamOverride([]byte(`{"model":"gpt-5"}`), override, BuildParamOverrideContext(&RelayInfo{}))
		var retired *hostcommon.RetiredRoutingFieldError
		require.ErrorAs(t, err, &retired)
		require.Nil(t, output)
	}
}

func TestParamOverrideKeepsUnknownInferenceFields(t *testing.T) {
	input := []byte(`{"model":"gpt-5","using_group":"sdk-extension","token_group":0,"feature":false}`)
	override := map[string]any{"operations": []map[string]any{{"mode": "set", "path": "model", "value": "upstream-model"}}}
	output, err := ApplyParamOverride(input, override, nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"upstream-model","using_group":"sdk-extension","token_group":0,"feature":false}`, string(output))
}
