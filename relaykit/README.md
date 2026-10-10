# RelayKit

RelayKit 是从 [new-api](https://github.com/QuantumNous/new-api) 中拆分出的独立 Go 模块，提供常用大模型文本协议的 DTO、请求转换、响应转换和流式事件转换。

它只负责协议层的数据建模与语义转换，不包含 HTTP 服务、上游请求发送、渠道调度、鉴权、计费或数据库逻辑。因此可以脱离 new-api 主模块，嵌入其他 Go 网关或代理服务。

## 能力

- 在 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages 和 Gemini `generateContent` 之间转换
- 同时支持请求、非流式响应和增量流式响应
- 自动根据 DTO 类型识别源协议，并选择内置的直接或多跳转换路径
- 返回转换器 ID、质量等级、实际转换步骤和统一 usage，方便审计与调试
- 支持常用的文本、多模态内容、工具调用、推理内容和 usage 映射
- 作为独立 Go module 构建，不依赖 new-api 主模块、Gin、数据库或全局设置

## 支持矩阵

以下四种文本协议登记了双向转换路径；登记表示基础文本转换可用，具体请求与响应仍须通过语义资格和损失策略检查：

| 源格式 \ 目标格式 | OpenAI Chat | OpenAI Responses | Claude Messages | Gemini |
|---|---:|---:|---:|---:|
| OpenAI Chat | — | Good | Fair | Fair |
| OpenAI Responses | Good | — | Fair | Fair |
| Claude Messages | Fair | Fair | — | Discouraged |
| Gemini | Fair | Fair | Discouraged | — |

质量等级表示协议之间的语义匹配程度：

- `Good`：两种协议的核心结构较接近
- `Fair`：主要能力可转换，但部分协议特性可能需要适配或无法完整保留
- `Discouraged`：目前需要经过中间协议转换，语义损失风险更高

请求、非流式响应和流式响应均覆盖上述矩阵。实际采用的路径可从转换结果的 `Steps` 和 `Quality` 字段中读取。

| 专项能力 | 当前边界 |
|---|---|
| 工具结果图片 | Claude ↔ Responses 保留结构化文本、图片顺序和调用 ID；Claude／Responses 转 Chat 时将图片提升到工具结果之后的用户消息，文本留在工具结果中；Claude 转 Gemini 经 Chat 路径；Responses 直转 Gemini 将图片与文档按顺序转为 `inlineData`（Gemini 3 及未知模型放入 `functionResponse.parts`，只接受 PNG、JPEG、WebP、PDF 与纯文本；1.x／2.x 放在同轮函数结果之后）；无法承载的类型、来源或形态省略并给 error 诊断 `unsupported_media_type`，由损失策略决定：safe／strict 拒绝，allow 发送不含该媒体的工具结果，媒体不写入文本；供应商自有来源在所有策略下拒绝 |
| 工具媒体附加语义 | 重复／缺失 ID、无法表达的工具失败状态明确拒绝；除 Responses 直转 Gemini 外，未知内容块明确拒绝；非默认图片 detail 转 Gemini 时只产生诊断；除转 Gemini 内联数据经宿主媒体解析器加载外，不下载或上传媒体 |
| 工具文档与来源 | Claude 工具文档直转 Responses 保留 URL `file_url` 或内联 `file_data`，转 Chat／Gemini 拒绝；Responses 工具文档可直转 Gemini 内联数据；工具媒体均检查类型、来源和目标能力，包含未知块的混合结果不能退化为文本；除 Responses 直转 Gemini 按上行由策略决定外，`allow` 不能绕过 |
| 无工具时的工具控制 | 目标请求没有函数工具时不发送 `tool_choice`／`parallel_tool_calls`，给 warning `tool_choice_without_tools`；strict 拒绝 |
| 引用 | 保留可表达的 URL、标题、范围和顺序；Claude 加密引用／搜索结果需原生路径。普通 URL 注释不能凭生成答案补造来源摘录或加密引用 |
| 托管工具 | 保留可表达的搜索和 MCP 结构；Chat 内部执行轨迹省略会产生诊断，strict 拒绝；转 Chat 时多出的托管搜索定义给 error 诊断 `duplicate_hosted_web_search`，allow 只保留第一个；不能把托管工具伪造成客户端函数 |
| Claude 消息级配置 | 原生保留 `messages[].output_config`；跨协议 safe/strict 拒绝，allow 返回 `claude_message_output_config_not_portable`，不近似映射成全局 effort |
| 多候选 | Chat／Gemini 逐候选判断终态；不能把多份答案合并为 Responses／Claude 单份答案 |
| 暂停与未知终态 | 原生保留；跨协议不能表达时失败，不伪造完成、不自动续跑 |
| 流式状态 | 必须观察到生成终态才能正常结束；重复终态不重复输出，冲突终态报错 |
| 内存边界 | `ResponseStreamOptions.MaxInputBytes` 限制累计输入帧；默认 128 MiB，宿主可采用现有响应限制。缓冲 Responses 使用相同原则，超限仍保留已观测 usage |

Responses 流式和缓冲共用输出项累计器，部分最终快照按身份合并引用和文本偏移。明确的 incomplete 事件不能借用内嵌 completed 状态；观察到终态后拒绝新的非终态事件。原生引用和续跑状态的硬限制不受 allow 放宽，宿主仍负责其 Token、渠道和凭据归属。

生成结果通过 `ResponseResult.Termination` 与 `ResponseStreamState.Termination()` 获取；传输结果继续使用 `StreamOutcome`。`ErrResponseTerminalMissing`／`ErrStreamTerminalMissing` 表示 JSON／流式上游缺少生成终态，不应归类为本地协议表达能力不足。宿主支持范围见[协议矩阵](../docs/routing/GATEWAY_SUPPORT_MATRIX.md)，验收判定见[网关验收边界](../docs/routing/SINGLE_INSTANCE_ACCEPTANCE.md)。

`AnalyzeRequest` 和 `ConversionPlan` 区分旧 OpenAI completion、moderation、edit 操作。这些操作没有跨协议转换器，即使使用 `allow` 也会拒绝转换，不能因复用 `GeneralOpenAIRequest` 而视为 Chat。Responses 历史中的 `function_call.namespace` 与工具声明采用相同的身份语义：原生保留；跨协议在默认 `safe` 和 `strict` 下拒绝，`allow` 接受已登记降级并返回结构化损失诊断，不提供可逆 namespace 映射。

`ClaudeRequest.ResolveEffectiveEffort() (string, bool, error)` 只读计算当前 user 回合生效的 effort：消息级设置等待其后的 user 激活，尾部未激活设置不覆盖当前值，缺省不推断模型默认值；返回值保留原文，调用方负责其属性规范化。`GetEfforts()` 只读取顶层配置。完整边界见[网关 API](../docs/routing/GATEWAY_API.md#claude-消息级-effort-与计价)。

## 安装

RelayKit 要求 Go 1.25.1 或更高版本。

```bash
go get github.com/QuantumNous/new-api/relaykit@latest
```

主要包：

| 包 | 用途 |
|---|---|
| `relaykit/dto` | 各协议的请求、响应、流式事件和 usage DTO |
| `relaykit/types` | 协议格式、错误、文件来源及共享类型 |
| `relaykit/relayconvert` | 请求、响应和流式转换入口 |
| `relaykit/relayconvert/convmeta` | 与宿主实现解耦的转换上下文和选项 |
| `relaykit/reasonmap` | 不同协议之间的结束原因映射 |

## 快速开始

下面将 OpenAI Chat Completions 请求转换为 Claude Messages 请求：

```go
package main

import (
	"context"
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func main() {
	maxTokens := uint(1024)
	request := &dto.GeneralOpenAIRequest{
		Model: "claude-sonnet-4-5",
		Messages: []dto.Message{
			{Role: "user", Content: "Hello!"},
		},
		MaxTokens: &maxTokens,
	}

	meta := &convmeta.Values{
		OriginModelName:     "client-model",
		UpstreamModelName:   request.Model,
		ChannelMetaAttached: true,
	}

	result, err := relayconvert.ConvertRequest(
		context.Background(),
		meta,
		types.RelayFormatClaude,
		request,
	)
	if err != nil {
		panic(err)
	}

	claudeRequest, ok := result.Value.(*dto.ClaudeRequest)
	if !ok {
		panic(fmt.Sprintf("unexpected result type %T", result.Value))
	}

	fmt.Printf("model=%s messages=%d\n", claudeRequest.Model, len(claudeRequest.Messages))
}
```

`ConvertRequest` 根据请求的具体 DTO 类型推断源格式。传入原始 JSON、`map[string]any` 或不受支持的 DTO 会返回错误。

### 非流式响应

响应转换使用相同的目标格式模型：

```go
result, err := relayconvert.ConvertResponse(
	ctx,
	meta,
	types.RelayFormatOpenAI,
	claudeResponse,
)
if err != nil {
	return err
}

openAIResponse := result.Value.(*dto.OpenAITextResponse)
usage := result.Usage
```

支持的响应 DTO：

| 格式 | 非流式响应 | 流式事件 |
|---|---|---|
| OpenAI Chat | `dto.OpenAITextResponse` | `dto.ChatCompletionsStreamResponse` |
| OpenAI Responses | `dto.OpenAIResponsesResponse` | `dto.ResponsesStreamResponse` |
| Claude Messages | `dto.ClaudeResponse` | `dto.ClaudeResponse` |
| Gemini | `dto.GeminiChatResponse` | `dto.GeminiChatResponse` |

### 流式响应

流式转换可能需要跨事件保存工具调用、usage 和结束状态。每条上游流应创建独立的 `ResponseStreamState`，并在上游结束后调用 `FinalizeStreamResponse`：

```go
state, err := relayconvert.NewResponseStreamState(
	types.RelayFormatOpenAI,
	types.RelayFormatOpenAIResponses,
	relayconvert.ResponseStreamOptions{
		ID:           "resp_123",
		Model:        "gpt-4.1",
		IncludeUsage: true,
	},
)
if err != nil {
	return err
}

for _, chunk := range upstreamChunks {
	results, err := relayconvert.ConvertStreamResponseChunk(ctx, meta, state, chunk)
	if err != nil {
		return err
	}
	for _, result := range results {
		emit(result.Value)
	}
}

finalResults, err := relayconvert.FinalizeStreamResponse(ctx, meta, state)
if err != nil {
	return err
}
for _, result := range finalResults {
	emit(result.Value)
}

usage := state.Usage()
```

RelayKit 不负责 SSE 的读取和写入。宿主需要将每个 SSE 事件解析为对应 DTO，并将转换结果重新编码后发送给下游。不要省略 `FinalizeStreamResponse`，部分转换器会在该阶段补发终止事件或最终 usage。

## 转换上下文

大多数基础转换可以传入 `nil` 作为 `convmeta.Meta`。需要模型映射、推理适配、安全设置或流式状态时，应使用 `convmeta.Values`，或在宿主中实现 `convmeta.Meta`。

常用选项通过 `convmeta.Options` 按请求传入：

```go
meta := &convmeta.Values{
	Options: &convmeta.Options{
		Claude: convmeta.ClaudeOptions{
			DefaultMaxTokens: func(model string) int {
				return 4096
			},
		},
		Gemini: convmeta.GeminiOptions{
			ThinkingAdapterEnabled: true,
		},
	},
}
```

需要注意：

- OpenAI Chat 或 OpenAI Responses 转 Claude 时，Claude 请求必须具有 `max_tokens`。源请求未提供时，需要配置 `Claude.DefaultMaxTokens`，否则转换会返回错误。
- RelayKit 不负责选择渠道或映射模型名。调用转换前，应将请求中的 `Model` 设置为目标上游使用的模型名。
- 自定义 `convmeta.Meta` 的指针实现必须保证所有方法对 nil receiver 安全，完整约束见 `convmeta.Meta` 的接口注释。

## 多模态内容

某些跨协议的图片转换需要下载 URL 内容或解析 data URL。宿主应在启动时配置媒体解析器：

```go
relayconvert.SetMediaResolver(relayconvert.MediaResolver{
	GetBase64Data:        getBase64Data,
	DecodeBase64FileData: decodeBase64FileData,
})
```

两个回调的签名由 `relayconvert.MediaResolver` 定义。需要媒体解析而未配置对应回调时，转换会明确返回错误；RelayKit 本身不会发起网络请求。

## 转换结果

请求转换返回 `relayconvert.RequestResult`，响应转换返回 `relayconvert.ResponseResult`。除 `Value` 外，建议关注：

- `From` / `To`：源格式和目标格式
- `Converter`：所选转换器 ID
- `Quality`：转换质量等级
- `Steps`：直接转换或多跳转换的实际路径
- `Usage`：响应转换后的统一 token usage
- `Stream`：结果是否来自流式转换

如果需要固定转换路径，可使用 `ConvertRequestVia`；如果需要按转换器 ID 执行，可使用 `ConvertRequestByID`、`ConvertResponseByID` 和 `NewResponseStreamStateByID`。

## 开发

RelayKit 必须始终保持独立可构建。修改模块后，在 `relaykit` 目录运行：

```bash
GOWORK=off go test ./... -timeout 60s
GOWORK=off go build ./...
```

转换矩阵由 golden tests 覆盖。确认协议输出变化是预期行为后，可更新快照：

```bash
GOWORK=off go test ./relayconvert -run TestGolden -update -timeout 60s
```

## 版本与兼容性

RelayKit 当前使用 `v0.x` 版本。公开 API 和 DTO 仍可能在小版本中调整，升级前请检查发布说明和实际序列化结果。协议之间并非完全同构，建议对业务实际使用的工具调用、多模态、推理和流式场景增加端到端测试。

## 许可证

RelayKit 是 new-api 项目的一部分，遵循项目根目录中的 [GNU Affero General Public License v3.0](../LICENSE)。
