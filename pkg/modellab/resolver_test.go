package modellab

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestExplicitVendorPrecedesAlias(t *testing.T) {
	require.Equal(t, "google", ResolveModel("gpt-4o", "Google").ID)
	require.Equal(t, "vendor:independent lab", ResolveModel("gpt-4o", "Independent Lab").ID)
	require.Equal(t, "unknown", ResolveModel("my-unknown-model", "").ID)
	require.Equal(t, "openai", ResolveModel("ＧＰＴ-4o", "").ID)
}
func TestChannelMappingClassifiesOnlyAdvertisedModels(t *testing.T) {
	lab, err := ResolveChannel([]string{"public-model"}, `{"public-model":"intermediate","intermediate":"claude-sonnet-4","hidden":"gpt-4o"}`, nil)
	require.NoError(t, err)
	require.Equal(t, "anthropic", lab.ID)
	require.Equal(t, "mapping", lab.Source)
	mixed, err := ResolveChannel([]string{"gpt-4o", "claude-sonnet-4"}, "", nil)
	require.NoError(t, err)
	require.Equal(t, "mixed", mixed.ID)
	_, err = ResolveChannel([]string{"name"}, "invalid", nil)
	require.Error(t, err)
	cyclic, err := ResolveChannel([]string{"a"}, `{"a":"b","b":"a"}`, nil)
	require.NoError(t, err)
	require.Equal(t, "invalid_mapping", cyclic.Source)
}
