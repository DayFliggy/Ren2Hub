package constant

const (
	ChannelTypeUnknown        = 0
	ChannelTypeOpenAI         = 1
	ChannelTypeMidjourney     = 2
	ChannelTypeAzure          = 3
	ChannelTypeOllama         = 4
	ChannelTypeMidjourneyPlus = 5
	ChannelTypeOpenAIMax      = 6
	ChannelTypeOhMyGPT        = 7
	ChannelTypeCustom         = 8
	ChannelTypeAILS           = 9
	ChannelTypeAIProxy        = 10
	ChannelTypePaLM           = 11
	ChannelTypeAPI2GPT        = 12
	ChannelTypeAIGC2D         = 13
	ChannelTypeAnthropic      = 14
	ChannelTypeBaidu          = 15
	ChannelTypeZhipu          = 16
	ChannelTypeAli            = 17
	ChannelTypeXunfei         = 18
	ChannelType360            = 19
	ChannelTypeOpenRouter     = 20
	ChannelTypeAIProxyLibrary = 21
	ChannelTypeFastGPT        = 22
	ChannelTypeTencent        = 23
	ChannelTypeGemini         = 24
	ChannelTypeMoonshot       = 25
	ChannelTypeZhipu_v4       = 26
	ChannelTypePerplexity     = 27
	ChannelTypeLingYiWanWu    = 31
	ChannelTypeAws            = 33
	ChannelTypeCohere         = 34
	ChannelTypeMiniMax        = 35
	ChannelTypeSunoAPI        = 36
	ChannelTypeDify           = 37
	ChannelTypeJina           = 38
	ChannelCloudflare         = 39
	ChannelTypeSiliconFlow    = 40
	ChannelTypeVertexAi       = 41
	ChannelTypeMistral        = 42
	ChannelTypeDeepSeek       = 43
	ChannelTypeMokaAI         = 44
	ChannelTypeVolcEngine     = 45
	ChannelTypeBaiduV2        = 46
	ChannelTypeXinference     = 47
	ChannelTypeXai            = 48
	ChannelTypeCoze           = 49
	ChannelTypeKling          = 50
	ChannelTypeJimeng         = 51
	ChannelTypeVidu           = 52
	ChannelTypeSubmodel       = 53
	ChannelTypeDoubaoVideo    = 54
	ChannelTypeSora           = 55
	ChannelTypeReplicate      = 56
	ChannelTypeCodex          = 57 // retired; reserved for historical rows, never reuse
	ChannelTypeAdvancedCustom = 58
	ChannelTypeSub2API        = 59
	ChannelTypeNewAPI         = 60
	ChannelTypeTaskPlugin     = 61
	ChannelTypeVLLM           = 62
	ChannelTypeSGLang         = 63
	ChannelTypeDummy          = 64 // this one is only for count, do not add any channel after this

)

// ChannelBaseURLs 保存各渠道类型的内置默认 Base URL。
// 非空值会通过 /api/channel/default_base_urls 下发到前端，作为渠道表单的 API 地址占位提示。
var ChannelBaseURLs = []string{
	"",                                    // 0
	"https://api.openai.com",              // 1
	"https://oa.api2d.net",                // 2
	"",                                    // 3
	"http://localhost:11434",              // 4
	"https://api.openai-sb.com",           // 5
	"https://api.openaimax.com",           // 6
	"https://api.ohmygpt.com",             // 7
	"",                                    // 8
	"https://api.caipacity.com",           // 9
	"https://api.aiproxy.io",              // 10
	"",                                    // 11
	"https://api.api2gpt.com",             // 12
	"https://api.aigc2d.com",              // 13
	"https://api.anthropic.com",           // 14
	"https://aip.baidubce.com",            // 15
	"https://open.bigmodel.cn",            // 16
	"https://dashscope.aliyuncs.com",      // 17
	"",                                    // 18
	"https://api.360.cn",                  // 19
	"https://openrouter.ai/api",           // 20
	"https://api.aiproxy.io",              // 21
	"https://fastgpt.run/api/openapi",     // 22
	"https://hunyuan.tencentcloudapi.com", //23
	"https://generativelanguage.googleapis.com", //24
	"https://api.moonshot.cn",                   //25
	"https://open.bigmodel.cn",                  //26
	"https://api.perplexity.ai",                 //27
	"",                                          //28
	"",                                          //29
	"",                                          //30
	"https://api.lingyiwanwu.com",               //31
	"",                                          //32
	"",                                          //33
	"https://api.cohere.ai",                     //34
	"https://api.minimax.chat",                  //35
	"",                                          //36
	"https://api.dify.ai",                       //37
	"https://api.jina.ai",                       //38
	"https://api.cloudflare.com",                //39
	"https://api.siliconflow.cn",                //40
	"",                                          //41
	"https://api.mistral.ai",                    //42
	"https://api.deepseek.com",                  //43
	"https://api.moka.ai",                       //44
	"https://ark.cn-beijing.volces.com",         //45
	"https://qianfan.baidubce.com",              //46
	"",                                          //47
	"https://api.x.ai",                          //48
	"https://api.coze.cn",                       //49
	"https://api.klingai.com",                   //50
	"https://visual.volcengineapi.com",          //51
	"https://api.vidu.cn",                       //52
	"https://llm.submodel.ai",                   //53
	"https://ark.cn-beijing.volces.com",         //54
	"https://api.openai.com",                    //55
	"https://api.replicate.com",                 //56
	"",                                          //57 retired
	"",                                          //58
	"",                                          //59
	"",                                          //60
	"",                                          //61
	"",                                          //62
	"",                                          //63
}

func GetChannelBaseURL(channelType int) string {
	if channelType < 0 || channelType >= len(ChannelBaseURLs) {
		return ""
	}
	return ChannelBaseURLs[channelType]
}

// ChannelTypes is the channel catalog; numeric identities remain stable across retirement.
var ChannelTypes = map[int]ChannelTypeDefinition{
	ChannelTypeUnknown:        {Name: "Unknown", Category: ChannelCategoryReserved, Lifecycle: ChannelLifecycleReserved},
	ChannelTypeOpenAI:         {Name: "OpenAI", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypeMidjourney:     {Name: "Midjourney", Category: ChannelCategoryMediaTask, Lifecycle: ChannelLifecycleActive},
	ChannelTypeAzure:          {Name: "Azure", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypeOllama:         {Name: "Ollama", Category: ChannelCategoryLocal, Lifecycle: ChannelLifecycleActive},
	ChannelTypeMidjourneyPlus: {Name: "MidjourneyPlus", Category: ChannelCategoryMediaTask, Lifecycle: ChannelLifecycleActive},
	ChannelTypeOpenAIMax:      {Name: "OpenAIMax", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypeOhMyGPT:        {Name: "OhMyGPT", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypeCustom:         {Name: "Custom", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypeAILS:           {Name: "AILS", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypeAIProxy:        {Name: "AIProxy", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypePaLM:           {Name: "PaLM", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeAPI2GPT:        {Name: "API2GPT", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypeAIGC2D:         {Name: "AIGC2D", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypeAnthropic:      {Name: "Anthropic", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeBaidu:          {Name: "Baidu", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeZhipu:          {Name: "Zhipu", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeAli:            {Name: "Ali", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeXunfei:         {Name: "Xunfei", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelType360:            {Name: "360", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypeOpenRouter:     {Name: "OpenRouter", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypeAIProxyLibrary: {Name: "AIProxyLibrary", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypeFastGPT:        {Name: "FastGPT", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypeTencent:        {Name: "Tencent", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeGemini:         {Name: "Gemini", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeMoonshot:       {Name: "Moonshot", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeZhipu_v4:       {Name: "ZhipuV4", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypePerplexity:     {Name: "Perplexity", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeLingYiWanWu:    {Name: "LingYiWanWu", Category: ChannelCategoryOpenAICompatible, Lifecycle: ChannelLifecycleActive},
	ChannelTypeAws:            {Name: "AWS", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeCohere:         {Name: "Cohere", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeMiniMax:        {Name: "MiniMax", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeSunoAPI:        {Name: "SunoAPI", Category: ChannelCategoryMediaTask, Lifecycle: ChannelLifecycleActive},
	ChannelTypeDify:           {Name: "Dify", Category: ChannelCategoryMediaTask, Lifecycle: ChannelLifecycleActive},
	ChannelTypeJina:           {Name: "Jina", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelCloudflare:         {Name: "Cloudflare", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeSiliconFlow:    {Name: "SiliconFlow", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeVertexAi:       {Name: "VertexAI", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeMistral:        {Name: "Mistral", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeDeepSeek:       {Name: "DeepSeek", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeMokaAI:         {Name: "MokaAI", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeVolcEngine:     {Name: "VolcEngine", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeBaiduV2:        {Name: "BaiduV2", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeXinference:     {Name: "Xinference", Category: ChannelCategoryLocal, Lifecycle: ChannelLifecycleActive},
	ChannelTypeXai:            {Name: "xAI", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeCoze:           {Name: "Coze", Category: ChannelCategoryMediaTask, Lifecycle: ChannelLifecycleActive},
	ChannelTypeKling:          {Name: "Kling", Category: ChannelCategoryMediaTask, Lifecycle: ChannelLifecycleActive},
	ChannelTypeJimeng:         {Name: "Jimeng", Category: ChannelCategoryMediaTask, Lifecycle: ChannelLifecycleActive},
	ChannelTypeVidu:           {Name: "Vidu", Category: ChannelCategoryMediaTask, Lifecycle: ChannelLifecycleActive},
	ChannelTypeSubmodel:       {Name: "Submodel", Category: ChannelCategoryNativeProvider, Lifecycle: ChannelLifecycleActive},
	ChannelTypeDoubaoVideo:    {Name: "Doubao", Category: ChannelCategoryMediaTask, Lifecycle: ChannelLifecycleActive},
	ChannelTypeSora:           {Name: "Sora", Category: ChannelCategoryMediaTask, Lifecycle: ChannelLifecycleActive},
	ChannelTypeReplicate:      {Name: "Replicate", Category: ChannelCategoryMediaTask, Lifecycle: ChannelLifecycleActive},
	ChannelTypeCodex:          {Name: "ChatGPT Subscription (Codex, retired)", Category: ChannelCategoryReserved, Lifecycle: ChannelLifecycleRetired},
	ChannelTypeAdvancedCustom: {Name: "Advanced Custom", Category: ChannelCategoryLocal, Lifecycle: ChannelLifecycleActive},
	ChannelTypeSub2API:        {Name: "Sub2API", Category: ChannelCategoryGateway, Lifecycle: ChannelLifecycleActive},
	ChannelTypeNewAPI:         {Name: "New API", Category: ChannelCategoryGateway, Lifecycle: ChannelLifecycleActive},
	ChannelTypeTaskPlugin:     {Name: "Task Plugin", Category: ChannelCategoryMediaTask, Lifecycle: ChannelLifecycleActive},
	ChannelTypeVLLM:           {Name: "vLLM", Category: ChannelCategoryLocal, Lifecycle: ChannelLifecycleActive},
	ChannelTypeSGLang:         {Name: "SGLang", Category: ChannelCategoryLocal, Lifecycle: ChannelLifecycleActive},
}

// ChannelTypeNames preserves the existing name projection for API consumers.
var ChannelTypeNames = func() map[int]string {
	names := make(map[int]string, len(ChannelTypes))
	for channelType, definition := range ChannelTypes {
		names[channelType] = definition.Name
	}
	return names
}()

func GetChannelTypeName(channelType int) string {
	if name, ok := ChannelTypeNames[channelType]; ok {
		return name
	}
	return "Unknown"
}

type ChannelSpecialBase struct {
	ClaudeBaseURL string
	OpenAIBaseURL string
}

var ChannelSpecialBases = map[string]ChannelSpecialBase{
	"glm-coding-plan": {
		ClaudeBaseURL: "https://open.bigmodel.cn/api/anthropic",
		OpenAIBaseURL: "https://open.bigmodel.cn/api/coding/paas/v4",
	},
	"glm-coding-plan-international": {
		ClaudeBaseURL: "https://api.z.ai/api/anthropic",
		OpenAIBaseURL: "https://api.z.ai/api/coding/paas/v4",
	},
	"kimi-coding-plan": {
		ClaudeBaseURL: "https://api.kimi.com/coding",
		OpenAIBaseURL: "https://api.kimi.com/coding/v1",
	},
	"doubao-coding-plan": {
		ClaudeBaseURL: "https://ark.cn-beijing.volces.com/api/coding",
		OpenAIBaseURL: "https://ark.cn-beijing.volces.com/api/coding/v3",
	},
}

// IsAdvancedCustomChannel includes named channels backed by route presets.
func IsAdvancedCustomChannel(channelType int) bool {
	switch channelType {
	case ChannelTypeAdvancedCustom, ChannelTypeVLLM, ChannelTypeSGLang:
		return true
	default:
		return false
	}
}
