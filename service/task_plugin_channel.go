package service

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/gin-gonic/gin"
	"slices"
	"strings"
)

func AppendTaskPluginIdentityFilter(c *gin.Context, key string) {
	c.Set("expected_task_plugin_key", key)
}

// TaskPluginCandidateKeys retains the accepted plugins from the request's
// routing generation until a channel chooses one for the actual attempt.
func TaskPluginCandidateKeys(c *gin.Context) []string {
	if c.GetBool("task_plugin_endpoint_channel_bound") {
		return nil
	}
	value, exists := c.Get(jsplugin.ContextKeyPinnedEndpoint)
	pinned, ok := value.(jsplugin.PinnedEndpoint)
	if !exists || !ok {
		return nil
	}
	var keys []string
	for _, candidate := range pinned.Candidates {
		if candidate.Plugin != nil && !slices.Contains(keys, candidate.Plugin.Meta.Key) {
			keys = append(keys, candidate.Plugin.Meta.Key)
		}
	}
	return keys
}

func TaskPluginCandidateChannelEligible(channel *model.Channel, key string, candidates []string) bool {
	if len(candidates) == 0 {
		return TaskPluginChannelEligible(channel, key)
	}
	for _, candidate := range candidates {
		if TaskPluginChannelEligible(channel, candidate) {
			return true
		}
	}
	return false
}

func TaskPluginChannelEligible(channel *model.Channel, key string) bool {
	if key == "" {
		return channel != nil && channel.Type != constant.ChannelTypeTaskPlugin
	}
	if channel == nil {
		return false
	}
	plugin, ok := jsplugin.DefaultRegistry.Get(key)
	if !ok {
		return false
	}
	settings := channel.GetSetting()
	switch channel.Type {
	case constant.ChannelTypeTaskPlugin:
		return settings.TaskPluginKey == key
	case constant.ChannelTypeNewAPI:
		return settings.BindsTaskPlugin(key) && plugin.Meta.SupportsUpstream(jsplugin.UpstreamKindNewAPI)
	default:
		return slices.Contains(plugin.Meta.ChannelTypes, channel.Type)
	}
}

func taskPluginEndpointTypes(channel *model.Channel, models ...string) []constant.EndpointType {
	if channel == nil {
		return nil
	}
	var endpoints []constant.EndpointType
	for _, plugin := range jsplugin.DefaultRegistry.Generation().Plugins() {
		if !TaskPluginChannelEligible(channel, plugin.Meta.Key) {
			continue
		}
		claimed := false
		for _, name := range models {
			for _, declared := range plugin.Meta.Models {
				if strings.EqualFold(name, declared) {
					claimed = true
				}
			}
		}
		if !claimed {
			continue
		}
		endpoints = append(endpoints, constant.EndpointTypeTaskPlugin)
		for _, protocol := range plugin.Meta.Protocols {
			switch protocol.Name {
			case "openai_responses":
				endpoints = append(endpoints, constant.EndpointTypeOpenAIResponse)
			case "openai_image":
				endpoints = append(endpoints, constant.EndpointTypeImageGeneration)
			case "openai_video":
				endpoints = append(endpoints, constant.EndpointTypeOpenAIVideo)
			}
		}
	}
	return endpoints
}

func taskPluginCapabilityFingerprint(channel *model.Channel) string {
	var identities []string
	for _, plugin := range jsplugin.DefaultRegistry.Generation().Plugins() {
		if TaskPluginChannelEligible(channel, plugin.Meta.Key) {
			identities = append(identities, plugin.Meta.Key+"@"+plugin.Meta.Version+":"+plugin.SourceHash)
		}
	}
	slices.Sort(identities)
	return strings.Join(identities, ",")
}
