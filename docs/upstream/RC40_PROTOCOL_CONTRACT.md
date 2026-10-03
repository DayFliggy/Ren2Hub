# 上游协议与推理服务合同

最初基准为 rc.40 `d04c118c8803f49e0c9bab74dcf5b5efeab9464a`，2026-10-02 的手工适配锁定 NewAPI `main` 的 `1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5`；吸收、延期和参考实现差异见[同步账本](SYNC_LEDGER.md)。本文件说明当前实现；分组复验命令见[网关验证步骤](../routing/GATEWAY_VALIDATION.md#顺序与命令)。本地测试使用模拟上游，不代表真实供应商或部署容器验收。工作区 `scripts/server/test_ren2hub_ws.py` 用于在服务器验证真实握手、同连接连续报价与结算、重叠拒绝及未知用量保留，执行结果只由所属工作区服务器档案记录。

## Responses WebSocket

标准 OpenAI 渠道继续支持 Codex 模型、Responses、Compact 和符合以下约束的 WebSocket 请求。ChatGPT Subscription (Codex) 渠道 type 57 已退役，其 OAuth、ChatGPT 后端及专用用量链路不再提供；这不改变标准 OpenAI 请求的参数、模型名、价格、路由和账务合同。Codex/Pi 客户端导出仍从统一 manifest 生成。退役编号与迁移边界见[渠道接入类型](../channel/TYPES.md)。

原生连接使用 `GET /v1/responses` 升级，要求渠道启用 Responses WebSocket 且能生成最终原生 Responses 目标。同一连接的每个 `response.create` 都重新检查 Token、渠道、模型、状态归属、准入和最终请求预算，并建立该次生成的冻结报价与账务预留。建立连接不等于授权后续所有生成；重叠 `create` 被拒绝，不能重复报价或发送。

最终模型、凭据、请求头、端点、代理及渠道配置参与连接身份摘要。同一 socket 身份变化要求重连；新连接只有身份和用户、Token、渠道、凭据、模型归属均匹配时才能继续旧 `previous_response_id`。更换凭据或端点不能通过重连接管旧状态；拒绝发生在报价和新上游握手之前。摘要不暴露密钥或原始请求头。

明确的供应商拒绝按既有共享重试预算处理。部分输出、客户端断连或缺少可靠用量时按观察到的用量估算结算，没有输出的失败请求释放预留；这些情况都不重放生成。失败的取消操作不能把仍在运行的生成当作已取消。收到不完整的入站帧不产生供应商发送或预留。

排空期间拒绝新生成，允许活动生成在期限内结束；超时关闭连接后，已交付有效输出的调用按观察用量或冻结估算结算，没有输出的失败调用释放预留。`response.failed` / `response.incomplete` 即使包含用量也不代表正常完成，仍以是否交付有效输出决定结算或释放。`TestGatewayResponsesWSDrainLifecycle` 与 `TestGatewayResponsesWSUnsuccessfulTerminalChargesOnlyDeliveredOutput` 覆盖这些边界；部署容器的停止信号和宽限期仍须独立验证，普通 HTTP shutdown 结果不能代替 hijacked WebSocket 验收。

## 协议转换与模型诊断

Responses custom/freeform 工具可在 Chat、Claude、Gemini 间以单一字符串 `input` 参数包装并还原，历史调用和结果保留关联，工具元数据按真实尝试重置。带 grammar 的输入格式不能仅凭描述提示视为等价约束：产生 `custom_tool_grammar_not_portable`，safe/strict 拒绝，allow 显式记录损失。无名称调用、非法包装和不完整终态不得伪造成成功工具输出。

Claude 普通工具 `content_block_start.input` 为后备参数；有真正 `input_json_delta` 时使用 delta 累计值，无 delta 时在工具结束前发出后备，避免丢参或拼接两份 JSON。原生 `safeguards` 保留；跨协议产生 `claude_safeguards_not_portable`，沿用统一 safe/allow 边界。共享 OpenAI 能力表覆盖 Sol/Luna 及快照名的完成 token 与采样规则，不另建适配器专属模型表。

转换为 Responses SSE 的文本输出遵守完整内容块生命周期：先发 `response.content_part.added`，再发文本 delta，结束时发完整 text/part done 与一致的最终响应，允许官方 SDK 累积器构造 `finalResponse()`。Responses 适配器中的转换损失错误使用统一错误映射，返回 HTTP 400 并跳过重试，不发送上游或输出伪成功；不能把合法的客户端拒绝映射为 HTTP 500。

上游增量 `c2b7a9a9e` 的 Claude 消息级 `output_config` 保留后，本地同时维护当前回合计价与转换损失合同。`ClaudeRequest.ResolveEffectiveEffort()` 只读解析顶层配置和消息序列：system 中的 effort 从后续第一个 user 回合开始生效并持续；连续等待中的设置以最后一次为准，末尾没有 user 的配置不影响当前回合。缺省、空对象和配置对象 null 不重置已有值；明确 effort 必须为非空字符串，非法结构或非 system 角色设置 effort 返回带路径的错误，不返回原始值。解析不改变消息、缓存前缀或原始 JSON，不推断模型默认值；`GetEfforts()` 仍只读取顶层。

原生 Claude/AWS/Vertex 消息体继续保留配置，beta 能力由客户端或管理员请求头提供，不自动注入。发送前从最终协议和完成参数覆盖的实际请求冻结 `effort` 属性及消费日志；尚未生效的尾部配置不参与计价，删除和重试不能沿用旧属性。旧 Quote、价格版本和已接受请求的结算不重新解释。Claude 输入跨协议后按真实目标请求计价，不能把已丢失的源配置带入报价。

最终请求为 Claude 格式时，`ApplyParamOverrideWithRelayInfo` 在参数覆盖后重新解析当前回合 effort（`relay/common/override.go`）。覆盖产生的非法 effort 标记为客户端错误，中继入口返回 HTTP 400 `channel:param_override_invalid`，不重试、不发送、不预扣；其他覆盖错误保留既有分类。计价属性按实际协议选择解析方式：`FreezeOutboundBillingAttributes` 依最终发送协议，`BuildBillingExprRequestInputFromRequest` 依 DTO 类型（`relay/helper/billing_expr_request.go`）。只有 Claude 格式使用 `ResolveEffectiveEffort()`，其他协议读取各自的 effort 字段，effort 统一转为小写。显式 `thinking.type` 和 `budget_tokens` 等预算字段进入冻结属性：预算 0 为 `thinking=off`，正数为 `on`，-1 为 `adaptive` 且不保留预算值，显式 `type` 优先于预算推导。没有显式 thinking 时才由 effort 推导，`none` 为 off，其余为 on。

跨协议出现任意消息级 `output_config`（包含空对象、null）时产生 `claude_message_output_config_not_portable`，路径为 `messages[i].output_config`，诊断不含正文或配置值。原生保留；默认 safe 和 strict 拒绝转换，allow 明确记录损失并沿用既有转换，不将消息级配置提升为全局 effort。规划、预览、直接转换及经过其他协议再回到 Claude 的多步路径采用同一损失保护。

Claude、Gemini、OpenAI Chat 和 Responses 的推理、搜索、工具和图片按实际已实现字段转换。共享推理意图保留支持的 effort、预算及显式零值；不支持的语义继续进入损失诊断。工具返回中的图片可提升为后续用户内容，文本和调用关联保持原顺序；不把真实音频或视频改作图片。流完成、usage 和错误事件保留各自含义，成功终态包含输出不等于失败或中断流可以估算结算。

高级自定义渠道的逐路由 `pass_through_body_enabled` 仅在匹配请求路径、模型且该路由支持请求体透传时生效；预览和执行共用匹配逻辑。透传保留未识别扩展字段及 `0`、`false`、`null`，仍执行宿主已有模型映射和最终参数检查。需要改写请求体的转换器不能开启透传；仅转换响应的 SGLang rerank 路由可以保留请求体。

Compact 使用普通模型名和 `/v1/responses/compact` 原生端点。Claude `/v1/messages/count_tokens`、Gemini `/v1/models/:model:countTokens` 和 `/v1beta/models/:model:countTokens` 返回 404，不进入生成、报价或预留。

消费日志的 `other.response_model_name` 来自上游真实 JSON、SSE 或 WS 事件中的模型声明，在协议输出改写之前捕获。`model_name` 仍是请求/计价模型，`other.upstream_model_name` 是真实上游请求模型；响应模型诊断不改变路由或价格。字段在无声明或完全相同且无需映射时可以省略。界面按忽略大小写的前后缀兼容规则派生差异，不存储第二个 mismatch 真值。重试重置观测，第一份实质差异不会被后续普通帧覆盖。用户日志投影限制字段长度并拒绝控制字符。

AxonHub `2bf8e2c9` 的 Playground 模型初始化问题按本地合同核查：浏览器无持久模型 Token 的请求仍以逻辑模型冻结价格，跨渠道重试分别映射上游请求模型，真实响应模型只用于诊断。已有等价实现由 `TestGatewayPlaygroundModelIdentity` 保护，包括首渠道失败后重映射、不同供应商模型价格不替换逻辑报价，以及未授权别名零发送；不引入第二个模型状态。

## 豆包 Images 与插件用量

豆包插件 1.2.1 保留 Seedream Flash、逐模型 profile 和 OpenAI Images generate/edit，并对齐 NewAPI `1a4166d8` 的结果保留边界，复用原有宿主路由、上传限制、任务状态和资金恢复。generate 接收 JSON；edit 接收 JSON/multipart 并要求参考图。不支持的 stream、mask、重复标量字段和非法计数在发送前拒绝，URL/Base64 结果由原宿主管线交付。

`UsageProfile.compatibilitySchema` 仅声明明确兼容的历史事实；展示使用当前 schema，计量/价格资格/冻结计算统一使用当前字段与兼容字段的派生集合。拒绝重复字段、非法单位和未声明事实，不自动补零；旧图片和 `video_input` 保持其既有含义。单档图片模型按成功交付数量结算；非法或缺失事实依现有冻结估算结算，不重解释在途任务锁定的插件源码与报价。

`retainResult` 仅允许出现在原生 submit/dynamic 路由声明；false 仅作用于 immediate 终态，异步结果继续保留并记录警告。协议声明出现该字段时加载失败，包括显式 true；不保留未发布扩展的兼容别名。OpenAI Images 的 immediate 结果由宿主自动一次性交付，数据库不保存结果正文；异步 Images 与 Responses 的 JSON、SSE、background 结果保留。已丢弃任务的后续结果及 artifact 检索返回 404，首次内存交付仍可用。

immediate 任务创建、终态 CAS、完成用量和结算恢复意图共用一个事务，提交后才交付资金事件。事务失败不留下可被超时退款的非终态任务；已发送且确认结果的请求使用原冻结估算恢复，不重发。证据无法验证时保持积压，不猜测费用。取消、下载失败、重复终态及插件升级不触发第二次生成或重复资金事件。详细 ABI 见[插件 API](../plugin-api/v1.md)。

Chat → Responses 的无名工具调用在 JSON 和 SSE 转换中返回明确协议错误，不能删掉调用后伪装为 completed。此项刻意保留 Ren2Hub 的严格失败合同，与 NewAPI 当前跳过无名调用的行为不同；已接受请求不重放，结算继续依据实际输出和冻结证据。

## vLLM 与 SGLang 状态

`GET /api/channel/:id/vllm/status` 与 `GET /api/channel/:id/sglang/status` 要求管理员身份和 `channel.read`，响应禁止缓存。目标只来自保存的对应类型渠道，查询参数不能提供 URL 或密钥。沿用运维渠道的代理和请求头边界，允许自托管私网；禁止重定向，仅执行四个固定 GET，不启动或停止推理进程。

vLLM 读取 `/health`、`/version`、`/v1/models`、`/metrics`；SGLang 将版本路径改为 `/server_info`。总 context 最长 10 秒，每个正文最多 4 MiB。一个端点失败仍返回其他结果；错误值只包含 `request_failed`、`http_error`、`response_too_large` 或 `invalid_response`，不回显上游错误正文、端点或凭据。SGLang 的服务信息只投影 `version`，不返回启动配置。

成功 envelope 的 `data` 包含：`sampled_at`（毫秒）、`endpoints`（路径到 status/error）、`version`、`models`（id/root/max_model_len）、`metrics`（name/labels/value）以及 `raw_metrics`。指标保留对应引擎前缀与 `process_start_time_seconds`，排除 histogram bucket、负值、NaN 和无穷值；保留 sum/count 和不同 labels 的独立序列。`raw_metrics` 仅向上述授权管理员返回，不写日志。真实推理服务验收需配置 `NEW_API_VLLM_TEST_URL` 和可选 `NEW_API_VLLM_TEST_KEY`；缺少环境变量时的跳过不是通过。
