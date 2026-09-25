package controller

import (
	"fmt"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"slices"
	"strings"
)

func taskPluginBindingsEqual(left, right []string) bool { return slices.Equal(left, right) }

func validateTaskPluginChannel(channel *model.Channel) error {
	settings := channel.GetSetting()
	bindings := settings.TaskPluginBindings()
	if channel.Type == constant.ChannelTypeTaskPlugin && (settings.TaskPluginKey == "" || len(settings.TaskExtendPluginKeys) > 0) {
		return fmt.Errorf("task plugin channel requires exactly one plugin binding")
	}
	if len(settings.TaskExtendPluginKeys) > 0 && channel.Type != constant.ChannelTypeNewAPI {
		return fmt.Errorf("multiple plugin bindings require a New API channel")
	}
	if len(bindings) > 0 && channel.Type != constant.ChannelTypeNewAPI && channel.Type != constant.ChannelTypeTaskPlugin {
		return fmt.Errorf("task plugin bindings require a task plugin or New API channel")
	}
	if channel.Type == constant.ChannelTypeTaskPlugin && strings.TrimSpace(channel.GetBaseURL()) == "" {
		return fmt.Errorf("task plugin channel base URL is required")
	}
	for _, key := range bindings {
		plugin, ok := jsplugin.DefaultRegistry.Get(key)
		if !ok {
			return fmt.Errorf("task plugin %q is unavailable", key)
		}
		if channel.Type == constant.ChannelTypeNewAPI && !plugin.Meta.SupportsUpstream(jsplugin.UpstreamKindNewAPI) {
			return fmt.Errorf("task plugin %q does not support a New API upstream", key)
		}
	}
	return nil
}
