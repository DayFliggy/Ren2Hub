package plugins_test

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/require"
)

func callBuiltinTaskHook(t *testing.T, key, hook string, ctx map[string]any) (map[string]any, error) {
	t.Helper()
	source, err := builtinplugins.Source(key)
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: key})
	require.NoError(t, err)
	value, err := plugin.Engine.Call(t.Context(), hook, ctx)
	if err != nil {
		return nil, err
	}
	return decodePluginValue(t, value), nil
}
