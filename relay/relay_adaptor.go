package relay

import (
	"strconv"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/pkg/relaycapability"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/advancedcustom"
	"github.com/QuantumNous/new-api/relay/channel/ali"
	"github.com/QuantumNous/new-api/relay/channel/aws"
	"github.com/QuantumNous/new-api/relay/channel/baidu"
	"github.com/QuantumNous/new-api/relay/channel/baidu_v2"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/cloudflare"
	"github.com/QuantumNous/new-api/relay/channel/codex"
	"github.com/QuantumNous/new-api/relay/channel/cohere"
	"github.com/QuantumNous/new-api/relay/channel/coze"
	"github.com/QuantumNous/new-api/relay/channel/deepseek"
	"github.com/QuantumNous/new-api/relay/channel/dify"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/jimeng"
	"github.com/QuantumNous/new-api/relay/channel/jina"
	"github.com/QuantumNous/new-api/relay/channel/minimax"
	"github.com/QuantumNous/new-api/relay/channel/mistral"
	"github.com/QuantumNous/new-api/relay/channel/mokaai"
	"github.com/QuantumNous/new-api/relay/channel/moonshot"
	"github.com/QuantumNous/new-api/relay/channel/newapi"
	"github.com/QuantumNous/new-api/relay/channel/ollama"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	"github.com/QuantumNous/new-api/relay/channel/palm"
	"github.com/QuantumNous/new-api/relay/channel/perplexity"
	"github.com/QuantumNous/new-api/relay/channel/replicate"
	"github.com/QuantumNous/new-api/relay/channel/siliconflow"
	"github.com/QuantumNous/new-api/relay/channel/sub2api"
	"github.com/QuantumNous/new-api/relay/channel/submodel"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	"github.com/QuantumNous/new-api/relay/channel/tencent"
	"github.com/QuantumNous/new-api/relay/channel/vertex"
	"github.com/QuantumNous/new-api/relay/channel/volcengine"
	"github.com/QuantumNous/new-api/relay/channel/xai"
	"github.com/QuantumNous/new-api/relay/channel/xunfei"
	"github.com/QuantumNous/new-api/relay/channel/zhipu"
	"github.com/QuantumNous/new-api/relay/channel/zhipu_4v"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type adaptorRegistration struct {
	new func() channel.Adaptor
}

var adaptorRegistry = map[int]adaptorRegistration{
	constant.APITypeAli:            {new: func() channel.Adaptor { return &ali.Adaptor{} }},
	constant.APITypeAnthropic:      {new: func() channel.Adaptor { return &claude.Adaptor{} }},
	constant.APITypeBaidu:          {new: func() channel.Adaptor { return &baidu.Adaptor{} }},
	constant.APITypeGemini:         {new: func() channel.Adaptor { return &gemini.Adaptor{} }},
	constant.APITypeOpenAI:         {new: func() channel.Adaptor { return &openai.Adaptor{} }},
	constant.APITypePaLM:           {new: func() channel.Adaptor { return &palm.Adaptor{} }},
	constant.APITypeTencent:        {new: func() channel.Adaptor { return &tencent.DispatchAdaptor{} }},
	constant.APITypeXunfei:         {new: func() channel.Adaptor { return &xunfei.Adaptor{} }},
	constant.APITypeZhipu:          {new: func() channel.Adaptor { return &zhipu.Adaptor{} }},
	constant.APITypeZhipuV4:        {new: func() channel.Adaptor { return &zhipu_4v.Adaptor{} }},
	constant.APITypeOllama:         {new: func() channel.Adaptor { return &ollama.Adaptor{} }},
	constant.APITypePerplexity:     {new: func() channel.Adaptor { return &perplexity.Adaptor{} }},
	constant.APITypeAws:            {new: func() channel.Adaptor { return &aws.Adaptor{} }},
	constant.APITypeCohere:         {new: func() channel.Adaptor { return &cohere.Adaptor{} }},
	constant.APITypeDify:           {new: func() channel.Adaptor { return &dify.Adaptor{} }},
	constant.APITypeJina:           {new: func() channel.Adaptor { return &jina.Adaptor{} }},
	constant.APITypeCloudflare:     {new: func() channel.Adaptor { return &cloudflare.Adaptor{} }},
	constant.APITypeSiliconFlow:    {new: func() channel.Adaptor { return &siliconflow.Adaptor{} }},
	constant.APITypeVertexAi:       {new: func() channel.Adaptor { return &vertex.Adaptor{} }},
	constant.APITypeMistral:        {new: func() channel.Adaptor { return &mistral.Adaptor{} }},
	constant.APITypeDeepSeek:       {new: func() channel.Adaptor { return &deepseek.Adaptor{} }},
	constant.APITypeMokaAI:         {new: func() channel.Adaptor { return &mokaai.Adaptor{} }},
	constant.APITypeVolcEngine:     {new: func() channel.Adaptor { return &volcengine.Adaptor{} }},
	constant.APITypeBaiduV2:        {new: func() channel.Adaptor { return &baidu_v2.Adaptor{} }},
	constant.APITypeOpenRouter:     {new: func() channel.Adaptor { return &openai.Adaptor{} }},
	constant.APITypeXinference:     {new: func() channel.Adaptor { return &openai.Adaptor{} }},
	constant.APITypeXai:            {new: func() channel.Adaptor { return &xai.Adaptor{} }},
	constant.APITypeCoze:           {new: func() channel.Adaptor { return &coze.Adaptor{} }},
	constant.APITypeJimeng:         {new: func() channel.Adaptor { return &jimeng.Adaptor{} }},
	constant.APITypeMoonshot:       {new: func() channel.Adaptor { return &moonshot.Adaptor{} }},
	constant.APITypeSubmodel:       {new: func() channel.Adaptor { return &submodel.Adaptor{} }},
	constant.APITypeMiniMax:        {new: func() channel.Adaptor { return &minimax.Adaptor{} }},
	constant.APITypeReplicate:      {new: func() channel.Adaptor { return &replicate.Adaptor{} }},
	constant.APITypeCodex:          {new: func() channel.Adaptor { return &codex.Adaptor{} }},
	constant.APITypeAdvancedCustom: {new: func() channel.Adaptor { return &advancedcustom.Adaptor{} }},
	constant.APITypeSub2API:        {new: func() channel.Adaptor { return &sub2api.Adaptor{} }},
	constant.APITypeNewAPI:         {new: func() channel.Adaptor { return &newapi.Adaptor{} }},
}

func GetAdaptor(apiType int) channel.Adaptor {
	registration, found := adaptorRegistry[apiType]
	if _, declared := relaycapability.Describe(apiType, ""); !found || !declared {
		return nil
	}
	return registration.new()
}

func GetTaskPlatform(c *gin.Context) constant.TaskPlatform {
	if key := c.GetString("expected_task_plugin_key"); key != "" {
		return constant.TaskPlatform(key)
	}
	channelType := c.GetInt("channel_type")
	if channelType > 0 {
		return constant.TaskPlatform(strconv.Itoa(channelType))
	}
	return constant.TaskPlatform(c.GetString("platform"))
}

func GetTaskAdaptor(platform constant.TaskPlatform) channel.TaskAdaptor {
	if plugin, ok := pluginruntime.DefaultRegistry.Get(string(platform)); ok {
		return taskplugin.New(plugin)
	}
	if platform == constant.TaskPlatformSuno {
		if plugin, ok := pluginruntime.DefaultRegistry.Get("sunoapi"); ok {
			return taskplugin.New(plugin)
		}
		return nil
	}
	if kind, err := strconv.Atoi(string(platform)); err == nil {
		if plugin, ok := pluginruntime.DefaultRegistry.GetByChannelType(kind); ok {
			return taskplugin.New(plugin)
		}
		switch kind {
		case constant.ChannelTypeAli, constant.ChannelTypeKling, constant.ChannelTypeJimeng, constant.ChannelTypeVertexAi, constant.ChannelTypeVidu, constant.ChannelTypeDoubaoVideo, constant.ChannelTypeVolcEngine, constant.ChannelTypeSora, constant.ChannelTypeOpenAI, constant.ChannelTypeGemini, constant.ChannelTypeMiniMax, constant.ChannelTypeSunoAPI:
			return nil
		}
	}
	return nil
}

func ResolveTaskPluginForPlatform(generation *pluginruntime.RoutingGeneration, platform constant.TaskPlatform) (*pluginruntime.LoadedPlugin, bool) {
	if generation == nil {
		return nil, false
	}
	return generation.Get(string(platform))
}

func GetTaskAdaptorForTask(task *model.Task) (channel.TaskAdaptor, error) {
	plugin, err := service.LoadTaskPluginForTask(task)
	if err != nil {
		return nil, err
	}
	return taskplugin.New(plugin), nil
}

func init() {
	service.GetPinnedTaskAdaptorFunc = func(task *model.Task) (service.TaskPollingAdaptor, error) { return GetTaskAdaptorForTask(task) }
}
