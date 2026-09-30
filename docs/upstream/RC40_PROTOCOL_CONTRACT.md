# rc.40 协议与推理服务合同

基准为固定上游 `d04c118c8803f49e0c9bab74dcf5b5efeab9464a`。本文件说明当前实现；分组复验命令见[网关验证步骤](../routing/GATEWAY_VALIDATION.md#顺序与命令)，逐提交处置从 Git 历史读取。本地测试使用模拟上游，不代表真实供应商或部署容器验收。工作区 `scripts/server/test_ren2hub_ws.py` 用于在服务器验证真实握手、同连接连续报价与结算、重叠拒绝及未知用量保留，执行结果只由所属工作区服务器档案记录。

## Responses WebSocket

原生连接使用 `GET /v1/responses` 升级，要求渠道启用 Responses WebSocket 且能生成最终原生 Responses 目标。同一连接的每个 `response.create` 都重新检查 Token、渠道、模型、状态归属、准入和最终请求预算，并建立该次生成的冻结报价与账务预留。建立连接不等于授权后续所有生成；重叠 `create` 被拒绝，不能重复报价或发送。

最终模型、凭据、请求头、端点、代理及渠道配置参与连接身份摘要。同一 socket 身份变化要求重连；新连接只有身份和用户、Token、渠道、凭据、模型归属均匹配时才能继续旧 `previous_response_id`。更换凭据或端点不能通过重连接管旧状态；拒绝发生在报价和新上游握手之前。摘要不暴露密钥或原始请求头。

明确的供应商拒绝按既有共享重试预算处理。接受状态未知、部分输出、客户端断连或缺少可靠用量时保留预留待核对，不估算出最终费用，也不重放生成。失败的取消操作不能把仍在运行的生成当作已取消。收到不完整的入站帧不产生供应商发送或预留。

排空期间拒绝新生成，允许活动生成在期限内结束；超时关闭连接后仍按未知接受/用量保持账务状态。`TestGatewayResponsesWSDrainLifecycle` 用本地隔离子进程覆盖该生命周期，部署容器的停止信号和宽限期仍须独立验证。普通 HTTP shutdown 结果不能代替 hijacked WebSocket 验收。

## 协议转换与模型诊断

上游增量 `c2b7a9a9e` 的 Claude 消息级 `output_config` 保留后，本地同时维护当前回合计价与转换损失合同。`ClaudeRequest.ResolveEffectiveEffort()` 只读解析顶层配置和消息序列：system 中的 effort 从后续第一个 user 回合开始生效并持续；连续等待中的设置以最后一次为准，末尾没有 user 的配置不影响当前回合。缺省、空对象和配置对象 null 不重置已有值；明确 effort 必须为非空字符串，非法结构或非 system 角色设置 effort 返回带路径的错误，不返回原始值。解析不改变消息、缓存前缀或原始 JSON，不推断模型默认值；`GetEfforts()` 仍只读取顶层。

原生 Claude/AWS/Vertex 消息体继续保留配置，beta 能力由客户端或管理员请求头提供，不自动注入。发送前从最终协议和完成参数覆盖的实际请求冻结 `effort` 属性及消费日志；尚未生效的尾部配置不参与计价，删除和重试不能沿用旧属性。旧 Quote、价格版本和已接受请求的结算不重新解释。Claude 输入跨协议后按真实目标请求计价，不能把已丢失的源配置带入报价。

最终请求为 Claude 格式时，`ApplyParamOverrideWithRelayInfo` 在参数覆盖后重新解析当前回合 effort（`relay/common/override.go`）。覆盖产生的非法 effort 标记为客户端错误，中继入口返回 HTTP 400 `channel:param_override_invalid`，不重试、不发送、不预扣；其他覆盖错误保留既有分类。计价属性按实际协议选择解析方式：`FreezeOutboundBillingAttributes` 依最终发送协议，`BuildBillingExprRequestInputFromRequest` 依 DTO 类型（`relay/helper/billing_expr_request.go`）。只有 Claude 格式使用 `ResolveEffectiveEffort()`，其他协议读取各自的 effort 字段，effort 统一转为小写。显式 `thinking.type` 和 `budget_tokens` 等预算字段进入冻结属性：预算 0 为 `thinking=off`，正数为 `on`，-1 为 `adaptive` 且不保留预算值，显式 `type` 优先于预算推导。没有显式 thinking 时才由 effort 推导，`none` 为 off，其余为 on。

跨协议出现任意消息级 `output_config`（包含空对象、null）时产生 `claude_message_output_config_not_portable`，路径为 `messages[i].output_config`，诊断不含正文或配置值。原生保留；默认 safe 和 strict 拒绝转换，allow 明确记录损失并沿用既有转换，不将消息级配置提升为全局 effort。规划、预览、直接转换及经过其他协议再回到 Claude 的多步路径采用同一损失保护。

Claude、Gemini、OpenAI Chat 和 Responses 的推理、搜索、工具和图片按实际已实现字段转换。共享推理意图保留支持的 effort、预算及显式零值；不支持的语义继续进入损失诊断。工具返回中的图片可提升为后续用户内容，文本和调用关联保持原顺序；不把真实音频或视频改作图片。流完成、usage 和错误事件保留各自含义，成功终态包含输出不等于失败或中断流可以估算结算。

高级自定义渠道的逐路由 `pass_through_body_enabled` 仅在匹配请求路径、模型且该路由支持请求体透传时生效；预览和执行共用匹配逻辑。透传保留未识别扩展字段及 `0`、`false`、`null`，仍执行宿主已有模型映射和最终参数检查。需要改写请求体的转换器不能开启透传；仅转换响应的 SGLang rerank 路由可以保留请求体。

Compact 使用普通模型名和 `/v1/responses/compact` 原生端点。Claude `/v1/messages/count_tokens`、Gemini `/v1/models/:model:countTokens` 和 `/v1beta/models/:model:countTokens` 返回 404，不进入生成、报价或预留。

消费日志的 `other.response_model_name` 来自上游真实 JSON、SSE 或 WS 事件中的模型声明，在协议输出改写之前捕获。`model_name` 仍是请求/计价模型，`other.upstream_model_name` 是真实上游请求模型；响应模型诊断不改变路由或价格。字段在无声明或完全相同且无需映射时可以省略。界面按忽略大小写的前后缀兼容规则派生差异，不存储第二个 mismatch 真值。重试重置观测，第一份实质差异不会被后续普通帧覆盖。用户日志投影限制字段长度并拒绝控制字符。

## vLLM 与 SGLang 状态

`GET /api/channel/:id/vllm/status` 与 `GET /api/channel/:id/sglang/status` 要求管理员身份和 `channel.read`，响应禁止缓存。目标只来自保存的对应类型渠道，查询参数不能提供 URL 或密钥。沿用运维渠道的代理和请求头边界，允许自托管私网；禁止重定向，仅执行四个固定 GET，不启动或停止推理进程。

vLLM 读取 `/health`、`/version`、`/v1/models`、`/metrics`；SGLang 将版本路径改为 `/server_info`。总 context 最长 10 秒，每个正文最多 4 MiB。一个端点失败仍返回其他结果；错误值只包含 `request_failed`、`http_error`、`response_too_large` 或 `invalid_response`，不回显上游错误正文、端点或凭据。SGLang 的服务信息只投影 `version`，不返回启动配置。

成功 envelope 的 `data` 包含：`sampled_at`（毫秒）、`endpoints`（路径到 status/error）、`version`、`models`（id/root/max_model_len）、`metrics`（name/labels/value）以及 `raw_metrics`。指标保留对应引擎前缀与 `process_start_time_seconds`，排除 histogram bucket、负值、NaN 和无穷值；保留 sum/count 和不同 labels 的独立序列。`raw_metrics` 仅向上述授权管理员返回，不写日志。真实推理服务验收需配置 `NEW_API_VLLM_TEST_URL` 和可选 `NEW_API_VLLM_TEST_KEY`；缺少环境变量时的跳过不是通过。
