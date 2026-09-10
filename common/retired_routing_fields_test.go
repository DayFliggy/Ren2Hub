package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateRoutingRuleJSONRejectsRetiredConditionsAndAffinitySources(t *testing.T) {
	for _, tc := range []struct {
		name string
		json string
		path string
	}{
		{"condition", `{"operations":[{"conditions":[{"path":"using_group","pass_missing_key":true}]}]}`, "$.operations[0].conditions[0].path"},
		{"nested condition", `{"operations":[{"mode":"prune_objects","value":{"conditions":[{"path":"token_group"}]}}]}`, "$.operations[0].value.conditions[0].path"},
		{"condition map", `{"operations":[{"conditions":{"user_group":"vip"}}]}`, "$.operations[0].conditions.user_group"},
		{"prune shorthand", `{"operations":[{"mode":"prune_objects","value":{"where":{"user_group":"vip"}}}]}`, "$.operations[0].value.where.user_group"},
		{"affinity flag", `[{"include_using_group":false}]`, "$[0].include_using_group"},
		{"affinity source", `[{"key_sources":[{"type":"context_string","key":"group"}]}]`, "$[0].key_sources[0].key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRoutingRuleJSON([]byte(tc.json))
			var retired *RetiredRoutingFieldError
			require.ErrorAs(t, err, &retired)
			require.Equal(t, tc.path, retired.Path)
		})
	}
}

func TestValidateRoutingRuleJSONPreservesUnrelatedRequestData(t *testing.T) {
	for _, value := range []string{
		`{"operations":[{"path":"metadata.user_group","value":"using_group"}]}`,
		`{"operations":[{"mode":"set","path":"metadata","value":{"conditions":[{"path":"user_group"}]}}]}`,
		`{"operations":[{"conditions":[{"path":"account_class","value":"vip"}]}]}`,
		`[{"key_sources":[{"type":"request_header","key":"group"},{"type":"gjson","path":"metadata.user_group"}]}]`,
	} {
		require.NoError(t, ValidateRoutingRuleJSON([]byte(value)))
	}
	require.Error(t, ValidateRoutingRuleJSON([]byte(`{"operations":`)))
}
