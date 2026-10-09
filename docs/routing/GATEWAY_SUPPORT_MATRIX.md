# 网关协议支持与验证边界

本矩阵维护源码中的协议能力与专项测试边界。能力来自 `pkg/relaycapability`，预览和执行共用 `PlanChannelConversion`，宿主合同为 v12。“原生”表示宿主具备该协议入口，不保证所有上游模型、端点和账号提供相同功能。表内列出源码中的回归入口及其证明范围，不表示全部套件已在最新提交重跑；复验命令见[验证步骤](GATEWAY_VALIDATION.md)，验收边界见[网关验收边界](SINGLE_INSTANCE_ACCEPTANCE.md)，执行结果和实际容器状态只由所属工作区服务器档案维护。

## 文本入口

工具媒体采用直接有序历史转换；缺失／重复调用 ID、不能表达的失败状态或图片 detail 不会因 `allow` 而放行。多候选响应必须逐个结束，不能由首个候选的 stop 证明整个请求完成。

| 语义合同       | 支持范围与拒绝边界                                                                                                                                                                                                                                                                                          |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 工具返回图片   | Claude ↔ Responses 保留结构化文本／图片顺序、调用 ID、URL 或内联来源；Claude／Responses 转 Chat 时将图片提升到工具结果之后的用户消息，文本仍留在工具结果中；Claude 转 Gemini 经 Chat 路径；Responses 直转 Gemini 时文本留在 `functionResponse.response`，图片与文档按原顺序经宿主媒体解析器转为 `inlineData`，Gemini 3 及未知模型放入 `functionResponse.parts`，Gemini 1.x／2.x 放在同轮全部函数结果之后，调用 ID 保留；工具媒体只接受函数结果文档所列的 PNG、JPEG、WebP、PDF 与纯文本，音视频、其他 MIME、单个对象形态、无法加载或归属供应商的来源发送前拒绝；无法保留的附加语义也发送前拒绝，`allow` 不能将媒体字符串化或丢弃 |
| 工具文档与来源 | Claude 工具文档直转 Responses 保留 URL `file_url` 或内联 `file_data`，转 Chat／Gemini 拒绝；Responses 工具文档直转 Gemini 按上条转为 `inlineData`；工具媒体的类型、来源及目标能力均在规划时校验，包含未知块的混合结果不能退化为文本，所有策略均发送前拒绝无法保留的路径                                                                                            |
| 受限终态       | Responses 的 max_output_tokens/content_filter 在 JSON、SSE、SSE 缓冲 JSON 保留正文、usage 及对应原因，不计供应商故障                                                                                                                                                                                        |
| 供应商暂停     | Claude 原生保留 pause_turn；无法表达的跨协议响应明确失败且不重放。跨协议请求 Claude 托管搜索因可预知的暂停风险发送前拒绝                                                                                                                                                                                    |
| 搜索引用       | Gemini grounding、Chat／Responses URL annotations 保留可表达 URL、标题、文本范围和顺序；Claude 带加密状态的引用需原生路径。不能将普通 URL 注释伪造成 Claude 原生引用或来源摘录                                                                                                                              |
| 托管工具       | 可表达搜索及 MCP 结果在多步转换旁传；Claude encrypted_content 搜索结果等续跑必需数据跨协议硬拒绝。Chat 不能表达的纯展示执行轨迹产生诊断，strict 拒绝展示损失                                                                                                                                                |
| 流式合并       | 按块、输出项和工具身份累计；最终快照不重复追加引用或工具结果；不自动续跑                                                                                                                                                                                                                                    |

Responses 缓冲路径也保留独立 annotation、托管项及部分终态快照中的既有字段；无法识别输出身份时明确失败。流式转换和 SSE 缓冲遵守当前宿主响应大小限制，累计超限返回不可重试的转换错误。缺少终态仍属于上游响应异常；客户端取消优先归为取消，不因同时发生转换错误而改变归因。

Responses 流式和缓冲共用同一输出项累计器。部分最终快照按输出身份合并，保留引用偏移；矛盾 incomplete 事件不借用 completed 状态，终态后不继续输出文本。Claude 搜索跨协议规划必须考虑原生引用与暂停能力，不仅检查输入工具是否能改名。原生引用的 encrypted_index 沿用既有状态摘要绑定和所有权复核。

响应语义的无法实现项在所有策略下拒绝。`safe` 允许有诊断的纯展示损失，`strict` 拒绝所有已声明损失；`allow` 仅放宽已有实现声明的降级，不能伪造终态或绕过原生状态归属。文档／文件引用等没有等价目标表示的引用仍明确拒绝，不能由 URL 引用支持推导为所有引用均已支持。

跨协议响应里的推理签名（Claude thinking 的 `signature`、Gemini 的 `thoughtSignature`）无法在目标协议续用，转换结果不含这些值，每个响应或每条流记录一次 warning `provider_state_dropped`：`safe` 与 `allow` 放行，`strict` 拒绝。同协议原生响应原样保留签名。加密的托管工具结果（`encrypted_content`）和引用位置（`encrypted_index`）属于可见答案的一部分，仍在所有策略下拒绝跨协议转换。

Chat/Responses/Claude/Gemini 分别指 OpenAI Chat、OpenAI Responses、Anthropic Messages、generateContent。`转 X` 使用 RelayKit，`投影` 使用供应商专用数据结构，`拒绝` 指预扣和发送前拒绝。实际特性仍受策略、模型映射、权限和配置约束。

| 适配器或模型域                                                             | Chat                   | Responses      | Claude                      | Gemini         |
| -------------------------------------------------------------------------- | ---------------------- | -------------- | --------------------------- | -------------- |
| OpenAI、Azure 及已登记 OpenAI 兼容渠道                                     | 原生                   | 原生           | 转 Chat                     | 转 Chat        |
| OpenRouter、Xinference                                                     | 原生                   | 原生           | 转 Chat                     | 转 Chat        |
| Anthropic、AWS Bedrock Claude                                              | 转 Claude              | 转 Claude      | 原生                        | 转 Claude      |
| AWS Nova                                                                   | 投影，仅 JSON          | 拒绝           | 拒绝                        | 拒绝           |
| Gemini                                                                     | 转 Gemini              | 转 Gemini      | 转 Gemini                   | 原生           |
| Vertex Claude                                                              | 转 Claude              | 转 Claude      | 原生                        | 转 Claude      |
| Vertex Gemini                                                              | 转 Gemini              | 转 Gemini      | 转 Gemini                   | 原生           |
| Vertex llama / -maas，服务账号 JSON                                        | 原生                   | 转 Chat        | 拒绝                        | 转 Chat        |
| Vertex llama / -maas，API Key                                              | 拒绝                   | 拒绝           | 拒绝                        | 拒绝           |
| NewAPI、Sub2API                                                            | 原生                   | 原生           | 原生                        | 原生           |
| DeepSeek                                                                   | 原生                   | 原生           | 原生                        | 拒绝           |
| Ali                                                                        | 原生                   | 原生           | 模型匹配时原生，否则转 Chat | 拒绝           |
| VolcEngine                                                                 | 原生                   | 原生           | 已登记原生端点或转 Chat     | 拒绝           |
| Moonshot、MiniMax                                                          | 原生                   | 拒绝           | 原生                        | 拒绝           |
| SiliconFlow                                                                | 原生                   | 拒绝           | 转 Chat                     | 拒绝           |
| Perplexity                                                                 | 原生                   | 原生           | 转 Chat                     | 拒绝           |
| Ollama                                                                     | 投影                   | 转 Chat 后投影 | 转 Chat 后投影              | 转 Chat 后投影 |
| BaiduV2                                                                    | 投影                   | 拒绝           | 转 Chat 后投影              | 拒绝           |
| ZhipuV4                                                                    | 投影                   | 拒绝           | 原生                        | 拒绝           |
| Cloudflare                                                                 | 投影                   | 原生           | 拒绝                        | 拒绝           |
| xAI                                                                        | 原生                   | 原生           | 拒绝                        | 拒绝           |
| Cohere、Baidu、Zhipu、PaLM、Tencent、Xunfei、Dify、Mistral、Coze、Submodel | 投影或适配器兼容入口   | 拒绝           | 拒绝                        | 拒绝           |
| Jina、MokaAI、Replicate、Jimeng                                            | 拒绝                   | 拒绝           | 拒绝                        | 拒绝           |
| AdvancedCustom                                                             | 按显式路径和转换器配置 | 同左           | 同左                        | 同左           |

ChatGPT Subscription (Codex) type 57 已退役，不再属于协议能力矩阵。标准 OpenAI 渠道的 Codex 模型属于上表 OpenAI 行，不因模型名包含 Codex 而进入订阅后端。生命周期与迁移说明见[渠道接入类型](../channel/TYPES.md)。

除 Nova 外，表内文本入口同时登记 JSON 和流式请求。登记不等于已完成全部响应语义验收。Tencent 纯单段 TokenHub Key 保留原生 Chat，未知或混合凭据采用 TC3 文本交集；AdvancedCustom 必须逐配置验证。

Nova、Coze、Baidu、Cohere、Zhipu、Tencent TC3、Xunfei、PaLM 与 Dify 的投影登记了实际保留字段、消息角色和内容类型。`safe` 拒绝丢失工具定义/历史、未支持媒体、结构约束及未实现的输出上限；已登记的采样和元数据损失产生警告，`strict` 同时拒绝这些警告。`allow` 允许记录过的降级，但不能绕过原生状态、无法执行的消息顺序、输入验证和 TPM 能力限制。执行前会复核同一投影及所选策略。

Cohere 保留历史 user 消息，Baidu 保留所有前导 system，旧 Zhipu 与讯飞不再插入虚构的对话。PaLM 输出其实际供应商 DTO，映射后的模型进入请求 URL。Dify 保留单轮文本和图片，多轮历史拼接会产生损失诊断；内联图片上传在准入和最终认证校验后执行，上传失败或取消不会继续生成，也不会因接受状态未知而重放。

Nova 保留 system/user/assistant 文本、多段文本和已实现的采样上限；system 映射到顶层 `system`。Coze 保留 user/assistant 文本历史，未实现系统消息、工具、媒体及结构输出，也不能可靠执行请求输出上限，因此不支持 TPM。参考 [Nova 请求结构](https://docs.aws.amazon.com/nova/latest/userguide/complete-request-schema.html) 与 [Coze 官方 SDK 的消息和创建接口](https://github.com/coze-dev/coze-py/blob/main/cozepy/chat/__init__.py)，2026-09-12 核对；回归不调用真实供应商。

四种文本入口在转换前统一应用渠道系统提示词。Nova 会收到顶层系统提示；Coze 配置此字段时在预览和预扣前拒绝，`allow` 不放宽渠道要求。原生透传仍应用选定模型、提示词和参数覆盖，并保留原始消息上的扩展属性。AWS 不再重新读取未处理的原始请求。

顶层、typed 嵌套及已登记 RawMessage/any 协议外壳中的未知 JSON 字段记录路径，覆盖 `0/false/null`。`strict` 拒绝已识别的丢弃，`safe/allow` 记录一般扩展警告；工具控制和结构约束的已知损失在 `safe` 下拒绝。检查不把工具参数等业务字典当作协议字段，Schema 损失按实际 Gemini 投影比较，接受 DTO 已实现的 Gemini 别名。独立调用方只有解码后 DTO 时，无法恢复此前已被丢弃的原始字段。

思考签名按供应商状态处理，不再为跨协议历史填入绕过标记，旧配置开关不生效。完整工具历史可按已登记路径转换；需要真实签名的供应商功能必须保留原生签名及其归属，不能把无状态转换称为具有完整推理连续性。Google 对签名的定义参见 [Thinking guide](https://ai.google.dev/gemini-api/docs/thinking)，2026-09-13 核对。

四入口的思考预算、effort 和 summary 按实际转换能力换算，换算后渲染为目标协议的推理字段。只有需要改写的输入（预算低于 -1、预算与思考模式或 effort 互相矛盾、思考类型被强制改写、目标模型不支持关闭思考等）产生 warning 诊断，`strict` 据此拒绝，`safe` 与 `allow` 放行；`RejectConversionLoss` 在 `safe` 下只拒绝 error 级诊断。`TestGatewayReasoningAndSearchPolicyBeforeBilling` 断言 Claude 思考、Gemini 零预算、Chat/Responses effort 跨协议换算在三种策略下都会发送。配置化搜索的已识别语义差异产生诊断，`strict` 下发送前拒绝。11 个独立计划场景和 15 组 HTTP 回归覆盖规划与最终发送边界，不能由基础 SDK 文本路径推导所有搜索或推理特性。

Claude 目标的关闭思考意图按模型能力渲染：Opus 5、Sonnet 5、Opus 4.8 及更早模型发送 `{"type":"disabled"}`；Sonnet 5.5 不接受 `disabled`，改发不带其他字段的 `{"type":"between_tools"}`；Opus 5.5 无法关闭思考，改发 adaptive 并以 `low` effort 运行。两种改写都给出 warning 诊断（`claude_thinking_disable_as_between_tools`、`claude_thinking_disable_unsupported`）。非 Claude 源协议转来的强制 `tool_choice`（`required` 或具名工具）在 Fable 5.1、Mythos 5.1、Opus 5.5、Sonnet 5.5 上，以及渲染结果为手动扩展思考时，改为 `auto` 并给出 `forced_tool_choice_downgraded` warning，因为这些情况下 Claude 会直接返回 400；原生 Claude 请求不改写。依据 Anthropic Messages API 文档，2026-10-08 核对；回归为 `claude_rejected_controls_test.go`。

Responses 的拒答（`type=refusal` 内容块与 `response.refusal.*` 流事件）在非流式、流式和缓冲三条路径上转为 Chat 的 `refusal` 字段，不产生诊断。转 Claude 时拒答文本按其与 `output_text` 的内容顺序输出为 `text` 块，`stop_reason` 为 `refusal`，优先于 `tool_use`、`end_turn` 和 `max_tokens`；Claude 没有拒答内容块，因此产生 `responses_refusal_reduced` warning 诊断（非流式路径为 `output[i].content[j].refusal`，流式统一为 `refusal` 并去重），`safe` 与 `allow` 放行，`strict` 拒绝。两种目标在同时出现增量与终态快照时都只交付一次。回归为 RelayKit 的 `TestResponsesRefusalReachesChatRefusalField` 与 `TestResponsesRefusalReachesClaudeUnderConversionPolicy`。转换生成的 Claude `thinking` 块始终带 `signature` 键，未知签名时为空串，不伪造供应商签名。Chat 历史中的旧版 `function_call` 与紧随其后的同名 `role:"function"` 结果配对为工具调用和工具结果：转 Claude、Responses 时生成按消息顺序稳定、避开客户端已有 ID 的 `call_legacy_N`，转 Gemini 时按名称匹配不生成 ID；无法配对的调用或结果给出 error 级诊断（`legacy_function_call_unpaired`、`legacy_function_result_unpaired`），`safe` 与 `strict` 发送前拒绝，`allow` 省略未应答的调用。缺少 `name` 或 JSON 无效的旧版调用在请求分析阶段返回 400，原生 Chat 转发也不例外。

Gemini `functionDeclarations[].parameters` 使用 OpenAPI 子集；转给 Chat、Claude、Responses 时由共享的工具编码步骤改写为 JSON Schema（类型小写、`nullable` 转为 null 类型、`example` 转为 `examples`、int64 边界转为数字），没有等价写法的关键字（如 `propertyOrdering`）丢弃并给出 `gemini_schema_keyword_dropped` warning，`strict` 拒绝。转 Gemini 的方向保持原样。

Vertex Gemini 启用现有 `RemoveFunctionResponseIdEnabled` 时，四种入站协议共用同一工具历史投影。`safe/allow` 仅在移除调用和结果 ID 后仍能按名称唯一关联时放行，并记录 `tool_ids_projected_to_names`；`strict` 拒绝这项可识别损失。无效历史或必须依赖 ID 才能消歧的同名并行调用在所有策略下拒绝，`allow` 也不能伪造关联。关闭该设置时不追加无 ID 投影或对应诊断。预览与执行共用不含原始提示词、参数或结果的 `ToolIdentityRequirements`；12 条 HTTP 回归确认四协议 × 三策略的歧义历史均在发送和预扣前拒绝。

OpenAI `n > 1` 或 Gemini `generationConfig.candidateCount > 1`（含既有 `candidate_count` 别名）产生 `multiple_candidates` 要求。目标 Claude 或 Responses 不能表达多个独立回答，所有策略均在规划阶段以 `multiple_candidates_unsupported` 拒绝；原生 Chat/Gemini 保留各自的多候选能力。依据为 RelayKit 的 `TestMultipleCandidatesRequireRepresentableTarget`，不以截取首个回答代替正确转换。

Vertex llama / `-maas` 的开源模型入口要求服务账号 JSON，API Key 配置在能力快照、预览、执行和 TPM 配置中统一拒绝；认证模式修改会重建能力快照。服务账号缓存按渠道和凭据摘要区分，Key 调序保留原账号缓存，人工替换不复用旧账号 Token。输出上限通过同一适配器入口投影为 `max_tokens`，保留 `0/missing`；请求形式参考 [Vertex Llama 请求文档](https://docs.cloud.google.com/vertex-ai/generative-ai/docs/partner-models/llama/use-llama)，2026-09-14 核对。

Responses `prompt` 只允许 OpenAI 类型原生渠道的已登记引用域。状态摘要增加实际传输身份，覆盖 Host、项目与自定义租户头；认证覆盖不得改换所选 Key，变化的追踪 ID 和已登记 SDK 遥测头不影响续轮。稳定凭据调序、手动替换、压缩后的 Responses 续轮及覆盖后状态注入由隔离 HTTP 回归保护。

Google Files URI 与 `gs://` 引用要求已知、同 Token 的媒体归属绑定。原生续轮会保持已绑定 URI，未知或跨 Token 引用在发送前拒绝；两类引用的 8 条 HTTP 请求同时校验实际出站 URI。归属绑定不构成 Files 产品 API。

TPM 只对登记了最终请求体解码器的操作开放。HTTP 和 AWS SDK 都在发送前读取最终请求体的独立副本，计算输入 JSON 估算和有效输出总上限；渠道提示词、参数覆盖、输出数量及协议切换均在此之前完成。Coze、未登记的旧投影或媒体格式不能配置有效 TPM；即使渠道支持文本 TPM，带未知扩展、无法估算的媒体/状态输入、缺少输出上限或生成非文本的请求仍会被拒绝。此估算不是供应商 tokenizer 的精确认证，结算使用可信 usage 修正。

## 专项覆盖与限制

| 验证层                       | 已覆盖                                                                                                                                                                                                                                                                                | 尚未证明                                                                                                                                                |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 宿主请求矩阵                 | `TestGatewayAdaptorRequestMatrix`：所有非自定义已登记渠道类型，Vertex 三模型域、AWS 两模型域，四协议及流式开关，共 400 场景；支持路径实际转换并序列化，其他路径规划拒绝                                                                                                               | 该层不发送请求，不证明响应或供应商认证；工具、媒体与状态按专项回归判断                                                                                  |
| 宿主响应与旧协议             | `TestGatewayStandardAdaptorResponseMatrix` 的 226 条标准协议实际适配器 JSON/SSE 路径；Baidu/Cohere/Tencent TC3/Zhipu/Dify 共 30 个正常和异常响应场景；讯飞 8 个 WebSocket 场景；PaLM 缓冲流 4 场景                                                                                    | 标准矩阵使用基础文本，工具和媒体按专项证据判断；非标准认证及真实供应商未认证                                                                            |
| 官方 Python / TypeScript SDK | 每语言 32 条文本 JSON/SSE、16 轮工具往返、16 条中断流、Responses 状态、未知/跨 Token 拒绝、前导失败切换                                                                                                                                                                               | OpenAI、Anthropic、Gemini、Ollama 四类模拟上游，非全部适配器或真实供应商认证                                                                            |
| 官方 SDK 语义                | 两语言各 63 场景，每语言 39 条成功和 24 条拒绝；四入口图片 JSON/SSE、结构输出、配置型搜索、plain/grammar custom tools、签名和私有文件；逐请求核对预扣会话、上游、钱包、Token、日志，拒绝全部零变化                                                                                    | 使用标准协议本地上游，不推导所有适配器和模型支持这些特性；没有真实搜索、工具执行或文件上传，供应商专用签名仅验证归属与保留                              |
| 云适配器 SDK                 | Azure、AWS Claude Bearer/SigV4、Vertex Claude/Gemini 五配置：两语言共 80 条基础 JSON/SSE 路径、40 轮单工具往返、40 条已交付中断、40 轮双工具往返；实际适配器、AWS 签名/事件流、模型映射、零值及终止事件                                                                               | 这三套 Vertex 场景使用 API Key；搜索/custom tools、媒体和供应商专用推理签名未纳入这些 SDK 场景；真实供应商未认证                                        |
| Vertex 开源模型 SDK          | `TestGatewayOfficialVertexOpenSourceSDK`：llama 与非 llama `-maas` 两域，服务账号/API Key 四配置；两语言共 64 场景，24 条 JSON/SSE 成功、40 条发送前拒绝。实际适配器及本地 RS256 JWT 换取校验 URL、账号头、模型和输出上限；另有 API Key 零预扣/换取/发送及四次凭据调序/替换 HTTP 回归 | 本套基础文本未验证工具、媒体、专用推理或真实服务账号授权；Anthropic 入口及 API Key 开源模型均明确拒绝                                                   |
| 兼容适配器 SDK               | `TestGatewayOfficialCompatibleSDK`：28 配置 × 每语言 224 场景均通过；合计 340 条 JSON/SSE 成功路径和 108 条发送前拒绝。验证实际适配器的 URL、模型映射、请求体、认证头、流式输出和 usage                                                                                               | 本套覆盖基础文本，工具、异常流、媒体及供应商专有状态不由此推导；真实供应商未认证                                                                        |
| 旧协议 SDK                   | Baidu、Cohere、Tencent TC3、旧 Zhipu、Dify、PaLM，两语言共 108 场景：24 条 Chat JSON/SSE、72 条其他协议发送前拒绝、10 条已交付中断及 2 条 PaLM 缓冲失败                                                                                                                               | 这六类的工具和媒体没有通过本套 SDK 验收；未声明的跨协议入口继续拒绝；真实供应商未认证                                                                   |
| 特殊文本 SDK                 | `TestGatewayOfficialSpecialSDK`：Nova、Coze、讯飞，两语言共 54 场景；10 条成功、38 条发送前拒绝、4 条已交付中断、2 条 Coze 创建已接受后查询失败不重放                                                                                                                                 | Nova 流式明确返回 400，其他不支持协议返回 503；本套未验证工具/媒体、真实地区、OAuth 或真实供应商                                                        |
| 标准 OpenAI Codex SDK | `TestGatewayOfficialCodexSDK`：两语言共 24 场景；Chat/Responses JSON/SSE、未知/跨 Token 拒绝、凭据调序续轮、停用/替换拒绝、上游错误及已交付中断；标准 API Key、`/v1/responses` 与参数透传由实际 OpenAI 适配器验证 | 本地模拟上游，不是供应商认证；type 57 已退役；Compact/WebSocket 见各自专项回归，本套不计入 |
| AdvancedCustom SDK           | 每语言 22 条成功路径及 10 条发送前拒绝；四入站与四上游协议、映射/路径/流式查询参数、Bearer/header/query/none 认证                                                                                                                                                                     | 全部自定义配置、工具/媒体特性、真实供应商                                                                                                               |
| AWS                          | 事件流、取消、header 覆盖；Bearer/AKSK 各只发送一次；Nova 系统消息、文本段、零值、无输出、负 usage；投影策略回归                                                                                                                                                                      | 实际地区/端点、全部响应终止原因及完整特性                                                                                                               |
| Ollama                       | 四协议 JSON/SSE、官方 SDK 工具往返与中断；共享投影/策略、角色、工具历史、大整数参数、结构输出校验、工具 ID、完成/截断/损坏/前导边界及 usage                                                                                                                                           | 真实版本与模型、全部扩展特性；未登记可靠输入预算，不支持 TPM                                                                                            |
| Coze                         | 文本历史、消息 ID 查询编码、轮询取消、多答案和 usage；SSE 明确终止；真实 HTTP 验证创建接受不明及轮询 404/503 均不重放；拒绝无效 TPM                                                                                                                                                   | 真实供应商、工具/媒体实现及非文本响应                                                                                                                   |
| 共享 DTO 与请求准备          | 16 条提示词路径及 8 条未知字段拒绝；typed/RawMessage/any 协议外壳、开放字典、Gemini 别名和 Schema 约束；四协议最终体 TPM；公共输出上限 20 个零值/优先级场景、seed=0、Ollama num_predict=0；旧投影及智谱 v4 字段保留                                                                   | Ollama 特性约束及完整媒体估算                                                                                                                           |
| 异步提交与终态               | 10 个任务适配器的 200/201/202 响应只返回数据；Sora 本地 HTTP 覆盖提交事务、恢复、原凭据续作、接受不明、RPM 和明确拒绝重试；提交与凭据读写三库通过；Suno/视频终态恢复三库 24 场景及后台恢复通过                                                                                        | 所有供应商完整提交生命周期与幂等确认、真实供应商                                                                                                        |
| Suno 续作                    | `TestGatewaySunoContinuation*` 五组 HTTP 回归：候选前验证同 Token 原任务和歌曲 ID；公开 task_id 改写私有上游 ID；Key 调序保持原凭据；未知/跨 Token/缺参数/错误歌曲、权限收紧，以及渠道停用/忙碌/凭据或端点变化均不越权发送或新增扣费                                                  | 仅允许已完成且有稳定归属绑定的 MUSIC 原任务；处理中或原资源不可用返回 503，未知/跨 Token/错误歌曲返回 400；旧无绑定任务不支持续作，真实供应商续作未认证 |
| Midjourney                   | 提交 1/21/22、上传数组、候选推进及冷却、取消、Token 归属、稳定凭据、轮询隔离和账务故障本地 HTTP；提交/终态恢复三库 33 场景，完整迁移两次；v2 图片签名隔离                                                                                                                             | 未知接受结果的供应商查询确认、旧无绑定任务归属及真实供应商                                                                                              |
| OpenAI 图像                  | JSON/SSE、JSON 转 SSE；空/畸形数据、usage 负值/溢出/明细、大首帧、异常终止、按交付数量结算；12 个实际本地 HTTP 场景及官方 Python/TypeScript SDK；最终覆盖数量预扣、模型映射、扩展保留                                                                                                 | 真实图像模型、供应商扩展事件                                                                                                                            |
| 豆包插件 Images              | Seedream Flash、逐模型 profile、generate JSON、edit JSON/multipart 参考图、URL/Base64 输出；`TestGatewayDoubao*` 覆盖非法字段/计数、取消、下载失败、升级恢复、冻结报价、唯一结算和结果丢弃 404                                                                                        | 不支持 stream/mask；真实供应商模型可用性和价格不由模拟上游证明；不导入默认价格                                                                          |
| xAI 插件视频 | `grok-imagine-video`、`-1.5`、`-1.5-lite` 经 `/xai/v1/videos` 的生成、编辑、延长与查询，`/v1/videos` 和 `/v1/responses`；`TestXAI*` 覆盖请求校验、逐模型用量、原生状态往返和产物代理；`TestGatewayXAIVideoSettlesChargedModerationAndRefundsFailures` 在 xAI 渠道上验证审核拦截和报告费用的失败按 SUCCESS 结算、其他失败退款 | 完成结果只报告时长和审核事实；价格读取的分辨率、输入图片等未报告事实时按提交时冻结估算结算（编辑预留 9 秒，延长预留 15 秒输入视频）；未调用真实 xAI；不导入默认价格 |
| Playground 模型身份          | `TestGatewayPlaygroundModelIdentity`：无持久 Token 的浏览器请求、跨渠道失败后模型重映射、逻辑报价与真实响应模型分离、未授权别名零发送                                                                                                                                                 | Token 0 诊断只按请求 ID 查看，不作跨请求 Trace/Thread 聚合                                                                                              |
| 转换后的图片                 | 8 类 × JSON/转 SSE × 正常/空输出共 32 场景；Replicate 5 个 HTTP 上传/生成场景；Ali 轮询取消/超时/状态/身份/下载失败及 5 个 HTTP 接受边界回归                                                                                                                                          | 各供应商全部流式事件、幂等查询确认与真实端点                                                                                                            |
| OpenAI 音频                  | TTS 二进制/SSE、STT JSON/文本/SSE；协议终止、空体、损坏数据、负 usage、取消、估算来源；11 个 HTTP 场景与官方 Python/TypeScript SDK 的 TTS/STT                                                                                                                                         | 供应商扩展事件及真实供应商                                                                                                                              |
| 专有音频                     | Cloudflare 非流式转写 JSON/text；MiniMax 完整 hex 音频；火山 HTTP/WS、binary/转 SSE；73 个适配器场景、9 个火山网关场景、4 个最终输入计价 HTTP 场景；官方 OpenAI SDK 两语言共 16 个正常/拒绝/截断场景                                                                                  | Cloudflare translation/TTS、MiniMax 原生增量流/URL 输出明确拒绝；专有音频全部扩展与真实供应商未认证                                                     |
| Embedding / Rerank           | 26 条适配器/协议路径，100 个响应场景、2 个客户端断开及 1 个 Gemini 模型映射分发回归；9 个 HTTP 场景覆盖映射、预扣、退款、空输入和不可执行能力；OpenAI SDK float/base64                                                                                                                | 厂商扩展向量格式及真实端点；Cloudflare Rerank 已在规划前拒绝                                                                                            |
| Realtime                     | 本地 WS 正常/失败/取消/截断、并发响应隔离/重复完成/无 ID 歧义、终态输出估算、固定价格和账务恢复；官方 Python/TypeScript SDK 共 8 场景及正常关闭码                                                                                                                                     | 完整音频工具会话、全部供应商事件和真实供应商                                                                                                            |
| 状态归属                     | 原生 Responses JSON/SSE 调序、停用、端点变化、过期；六类不透明状态 HTTP 续轮与未知/跨 Token 拒绝；JSON/SSE 绑定失败不暴露 ID、不重发且退款；签名分片按尝试隔离；Host/项目/自定义租户、认证覆盖、稳定凭据调序与替换、prompt 引用域、compact 续轮通过                               | 各供应商非标准引用域、全部自定义认证配置和真实供应商有效期                                                                                              |
| 控制台                       | 预览 API/视图 19 条回归；API fixture 四组、真实准入四组和真实样本预览四组 Playwright；普通用户指标 403、跨用户预览 404、凭据及端点不外露                                                                                                                                              | 全部供应商投影的诊断展示矩阵                                                                                                                            |

SDK 版本锁文件位于 `tests/gateway-sdk`：Python OpenAI 3.13.0、Anthropic 1.5.0、google-genai 2.23.0；TypeScript OpenAI 7.15.0、Anthropic 0.125.0、Google 2.22.0。

云验收分为 `TestGatewayOfficialCloudSDK`、`TestGatewayOfficialCloudToolAndInterruptionSDK` 和 `TestGatewayOfficialCloudParallelToolSDK`；其他套件与锁定依赖统一从[SDK 测试说明](../../tests/gateway-sdk/README.md)运行。源码中的正式测试与脚本是复现入口，执行结果只由所属工作区服务器档案记录。

双工具回归通过 OpenAI/Anthropic SDK 内置流累积器和 Google SDK 内容事件验证：首帧同时出现两个调用、后续分片组成参数 `0` 与 `false`、工具结果逆序回传，检查 ID 或经验证的 Vertex 名称关联、最终值、usage 和唯一终止事件。Google 返回的完整函数参数来自网关转换和官方 SDK 解析，没有手写 SSE 累积器。云工具场景中的“往返”包括流式工具请求和提交结果后的最终响应，不等同于额外两个独立能力路径。

新增 `verify_media.py` / `verify_media.mjs` 各调用官方 SDK 验证 7 条正常媒体路径和 1 条图片中断路径：Embedding 浮点/base64、图片 JSON/SSE、TTS PCM、STT JSON 与 multipart `stream=true`。中断必须由 SDK 抛错；没有使用手写 SSE 累积器替代 SDK。音频参数依据本地固定版本类型及 [OpenAI 转写文档](https://developers.openai.com/api/docs/guides/speech-to-text)、[语音生成参数](https://developers.openai.com/api/reference/cli/resources/audio/subresources/speech/methods/create) 核对。

图片最终预算读取适配器与渠道覆盖后的 `n`、`parameters.n`、`sampleCount`、`batch_size` 或 `num_outputs`；multipart 也按实际发送字段处理。`FinalImagePriceMeta`（`relay/helper/image_budget.go`）同时用该最终请求体冻结 `service_tier`、`quality`、归一化尺寸与数量作为计价属性，渠道覆盖写入的档位不会在报价中丢失。该预算只证明可计费数量有界，不支持图片 TPM。JSON 转 SSE 表示网关包装已完成图片，不代表供应商原生提供渐进生成。Imagen 的既有 token 规则明确记为估算。Replicate 在上传前完成最终数量校验、准入和预扣，上传使用已验证的最终传输头；上传已接受或生成结果未知时不重放。

TTS 发送前读取最终 `input`、MiniMax `text` 或火山 `request.text`，按已有字符/token 规则预扣；音频输出仍无可靠有界预算，不开放 TPM。MiniMax/火山仅开放 speech，Cloudflare 仅开放非流式 transcriptions；预览和执行读取相同路径/流式登记。火山二进制输出中断会中止 HTTP 传输，已交付部分按估算结算并保存 `usage_source` 和终止状态。官方 Realtime SDK 使用 Python 类型化迭代器和 TypeScript 事件接口，未将测试汇总逻辑称为 SDK 内置累积器。

基础文本矩阵不扩张专项验收的结论：供应商专有搜索、签名及自定义引用域按已列明的具体路径验证，基础 SDK 成功不代表未测特性也已通过。已明确拒绝的协议或特性不记作待实现支持。音频、图像、Embedding、Rerank、Realtime、异步任务不计入上述 400 个文本请求场景，应使用各自专项证据。本矩阵描述本地隔离合同验收，不是生产兼容认证。
