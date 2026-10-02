package jsplugin

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUsageCompatibilityKeepsDisplayAndExecutionContractsSeparate(t *testing.T) {
	source := routingTestPluginSource("compat-profile", 0, `["image", "default"]`, `
usageSchema:{seconds:{type:"number",unit:"second"}},
usageProfiles:[{models:["image"],schema:{images:{type:"number",unit:"count"}},
compatibilitySchema:{old_images:{type:"number",unit:"count",unitLabel:{en:"image"}},old_mode:{enum:["none","video"]}},
examples:[{label:"one image",facts:{images:1}}]}],`, "")
	registry := NewRegistry()
	plugin, err := registry.Register(source, Options{})
	require.NoError(t, err)
	display, examples := plugin.Meta.UsageForModel("image")
	assert.Len(t, display, 1)
	assert.Contains(t, display, "images")
	require.Len(t, examples, 1)
	assert.Len(t, examples[0].Facts, 1)
	for _, models := range [][]string{{"image"}, {"ep-unknown", "IMAGE"}} {
		facts := plugin.Meta.UsageFactsForModels(models...)
		assert.Len(t, facts, 3)
		assert.Equal(t, "count", facts["old_images"].Unit)
		assert.Equal(t, []string{"none", "video"}, facts["old_mode"].Enum)
		assert.NotContains(t, facts, "seconds")
		facts["old_images"].UnitLabel["en"] = "changed"
		assert.Equal(t, "image", plugin.Meta.UsageFactsForModels(models...)["old_images"].UnitLabel["en"])
	}
	assert.Equal(t, plugin.Meta.UsageSchema, plugin.Meta.UsageFactsForModels("default"))
	snapshot := registry.Snapshot()
	snapshot.Override[0].UsageProfiles[0].CompatibilitySchema["old_mode"].Enum[0] = "changed"
	assert.Equal(t, []string{"none", "video"}, registry.Snapshot().Override[0].UsageFactsForModels("image")["old_mode"].Enum)
}

func TestProtocolRetentionFieldIsRejectedBeforeExecution(t *testing.T) {
	for _, protocol := range []string{"openai_image", "openai_video", "openai_responses"} {
		for _, value := range []string{"true", "false", `"false"`, "null"} {
			t.Run(protocol+"/"+value, func(t *testing.T) {
				source := routingTestPluginSource("bad-retention", 0, `["image"]`,
					fmt.Sprintf(`protocols:[{name:%q,retainResult:%s}],`, protocol, value), "")
				_, err := CompilePlugin(source, Options{})
				require.ErrorContains(t, err, `unknown field "retainResult"`)
			})
		}
	}
}
