# 网关准入管理 API

所有接口位于 `/api/gateway`。成功返回 `{ "success": true, "message": "", "data": ... }`；错误使用对应 HTTP 状态码，并返回 `success: false`、`message` 和 `code`。这些接口禁止浏览器缓存。

| 方法和路径                      | 权限                      | data                                                                |
| ------------------------------- | ------------------------- | ------------------------------------------------------------------- |
| `GET /self`                     | 登录用户                  | 当前用户配置、有效值及来源                                          |
| `GET /self/runtime`             | 登录用户                  | 当前用户的本地准入运行态                                            |
| `GET /config`                   | Root                      | 全局网关配置                                                        |
| `GET /metrics`                  | Root                      | schema v2：本地准入、重试预算、尝试维护、状态绑定失败及账务恢复积压 |
| `PUT /config`                   | Root                      | 保存后的完整配置，version 加一                                      |
| `GET /users/:id`                | 管理员                    | 指定用户配置、有效值及来源                                          |
| `PUT /users/:id`                | 管理员                    | 保存后的用户覆盖                                                    |
| `GET /runtime/users/:id`        | 管理员                    | 本地准入运行态                                                      |
| `GET /runtime/channels/:id`     | 管理员且具备 channel.read | 本地准入运行态，加 `channel_id`、`rate`、`rate_status`              |
| `GET /channels/:id/credentials` | 管理员且具备 channel.read | 已登记凭据的 `[{ id }]`，id 为稳定 UUID                             |
| `GET /runtime/credentials/:id`  | 管理员且具备 channel.read | 凭据的 rate                                                         |

Root 可以管理所有用户；其他管理员只能查看和修改角色低于自己的用户。`/self` 始终读取登录用户，不接受用户 ID。凭据列表返回已通过可信路径登记的身份，不返回密钥、指纹或可变密钥索引；它不是当前启用密钥列表。

`GET /api/gateway/metrics` 的 schema v2 包含 `route_decision_events_dropped_total`，表示本实例有界路由日志队列的累计丢弃数，进程重启后清零。

## Token 路由预览

`POST /api/routing/tokens/:id/preview` 使用用户认证、Token 所有权检查、敏感接口限频和禁止缓存。请求为 `{ "model": "gpt-4o-mini", "path": "/v1/chat/completions", "request": { "model": "gpt-4o-mini", "messages": [{ "role": "user", "content": "示例" }] } }`；request 可省略，样本最多 1 MiB。正文模型必须匹配 model，Gemini 同时校验 URL 模型及生成操作。Compact 使用 `/v1/responses/compact` 和普通模型名，不支持 `-compact` 模型别名。

沿用统一 success/data 包装。data 包含 Profile 预览字段，以及 `token_id`、`mode`（auto_lab/manual）、服务端生成的 `preview_id`、毫秒时间戳 `generated_at`；`profile_version` 为配置版本，无 Profile 时为 0。每条 entry 包含 `priority`、`score_breakdown`、conversion_plan、diagnostics、filter_reason。自动候选没有 Entry，entry_id 为 0。

`candidate_channel_ids` 按当前资格和实际排序生成，`preferred_channel_id` 仅表示此快照首选。`runtime_recheck_required` 始终为 true，reasons 包括价格、实时额度、安全策略、准入容量、Provider 限制、配置版本、亲和及必要的状态归属。无 Profile 的预览不创建 Profile；不发送请求、不预扣、不持有 Permit、不占用健康探测。

旧 `POST /api/routing/profiles/:id/preview` 保留所有权检查并委托 Token 预览。预览样本只在当前请求内分析，不加入路由日志或持久化历史。

## 配置契约

`PUT /config` 是全量更新并做乐观版本检查。客户端先 GET 并保留所有字段；请求省略 `total_queue_size` 和三个 `retry_budget_*` 字段时保留数据库当前值，显式 null 返回 400。`channel_rates` 按渠道 ID 配置，该渠道的所有模型共享额度。`credential_rates` 按稳定凭据 UUID 配置，同一渠道内使用该凭据的请求共享额度。两个范围原子准入。

```json
{
  "version": 1,
  "user_concurrency": 10,
  "wait_ms": 5000,
  "user_queue_size": 20,
  "channel_queue_size": 20,
  "total_queue_size": 1024,
  "retry_budget_enabled": true,
  "retry_budget_percent": 10,
  "retry_budget_burst": 20,
  "groups": {
    "team": { "concurrency": 5, "wait_ms": null, "queue_size": null }
  },
  "channel_rates": { "12": { "rpm": 60, "tpm": 100000 } },
  "credential_rates": {}
}
```

并发范围为 0 至 1000000；累计等待毫秒为 0 至 5000；每用户队列长度为 0 至 20，渠道队列长度为 0 至 10000，默认 20。RPM、TPM 为 0 至 1000000000，0 表示该限制关闭。配置引用的渠道和凭据必须存在。版本不匹配返回 `409 CONFLICT`，客户端重新加载后再保存。

队列与重试字段：`total_queue_size` 范围 0–100000，0 禁止新增排队但不阻止立即获槽；`retry_budget_percent` 范围 1–100；`retry_budget_burst` 范围 1–10000；`retry_budget_enabled` 为布尔值。数据库缺失字段按上例补齐，仅实际补字段时版本增加一次，已有显式 0/false 保留。

`PUT /users/:id` 的请求和成功 data 为 `{ concurrency: number|null, wait_ms: number|null, queue_size: number|null }`，替换该用户的三个覆盖字段。null 恢复继承；显式 0 保留其含义。该更新允许重复提交相同值。

用户配置查询返回：

```json
{
  "user_id": 3,
  "group": "team",
  "config_version": 2,
  "override": { "concurrency": 0, "wait_ms": null, "queue_size": null },
  "effective": {
    "concurrency": 0,
    "wait_ms": 5000,
    "queue_size": 20,
    "channel_queue_size": 20
  },
  "sources": {
    "concurrency": "user",
    "wait_ms": "system",
    "queue_size": "system",
    "channel_queue_size": "system"
  }
}
```

来源仅有 `user`、`group`、`system`，与实际准入共用逐字段解析。渠道队列长度当前来自系统配置。用户并发 0 表示不设并发上限；仍记录在途占用。等待或队列为 0 时，满载直接拒绝。

## 运行数据

rate 为 `{ rpm_used, tokens_used, tokens_reserved, cooldown_ms }`：RPM 统计最近 60 秒已准入尝试；发送前失败会退回。tokens_used 为最近 60 秒已结算用量，tokens_reserved 为仍有效的在途估算。发送后用量未知时保留估算，实际用量超过估算时按真实用量入账并限制后续请求。cooldown_ms 是当前渠道或渠道内凭据的 Retry-After 冷却剩余毫秒，独立渠道不共享冷却。

用户和渠道并发运行态来自同一进程内 manager，返回 `in_flight`、`queued`、`capacity`、`config_version`、`revision`、`started_at`（Unix 秒）、`draining`。查询不创建池、不占容量。Provider rate 只从 Redis 读取；渠道并发可读而 Provider 不可用时仍返回 200、真实本地计数及 `rate: null`、`rate_status: unavailable`；正常时 `rate_status: available`。独立凭据 rate 读取失败仍返回 503。未配置 Redis 时实际准入改用进程内预留，但这两个接口不暴露内存计数，分别固定返回 `unavailable` 和 503，见[Provider 额度预留](UNIFIED_ROUTING.md#provider-额度预留)。无效输入返回 `400 VALIDATION_ERROR`；资源不存在返回 `404 NOT_FOUND`；无权管理目标用户返回 `403 FORBIDDEN`。

生成入口的用户准入在 OpenAI 错误对象中返回明确 `code`，包括 `gateway_capacity_exhausted`、`gateway_queue_full`、`gateway_wait_timeout`、`gateway_total_queue_full` 和 `gateway_draining`。诊断 message 分别标记 `user admission` 或 `process admission`；渠道尝试错误保留 `channel admission` 原因，路由日志记录对应渠道。客户端已取消或已提交响应时不追加错误。

TPM 要求宿主登记可解析的最终请求格式。仅支持媒体任务、Coze 及未登记旧投影的渠道或凭据不能配置 TPM，保存返回 `400 VALIDATION_ERROR`；RPM 不受此限制。运行时重新检查最终发送体：覆盖后的最大输出上限乘输出数量，加输入 JSON 估算；关闭 CountToken 不关闭准入计数。已登记的 Embedding 使用输入估算。未知扩展、无法估算的媒体/状态输入、非文本输出或缺少有效上限均不能伪装成有限预算。渠道类型变更后残留的无效 TPM 也在发送前拒绝。

预扣发生在转换、最终预算检查和供应商限流通过后。最终输入估算与输出上限同步刷新本次尝试的预扣金额；内部重试共用一个计费会话。`route_decision` 的 `token_budget`、`token_budget_source` 和 `rejection_origin` 记录估算及失败归属，不包含正文或密钥。

## 运维指标

`GET /metrics` 复用 Root 鉴权和关键接口限流，建议低频采集。`counter_scope` 为 `instance`，`started_at` 为进程启动时间；重启后累计计数归零。本系统仅支持一个进程承接业务流量，`/runtime/*` 与指标读取同一个 manager。

- `user_queue`、`channel_queue`：`active` 为本进程等待请求数；`entered_total`、`completed_total`、`failed_total` 为累计结果，`wait_ms_total` 为累计等待毫秒。取消计入失败并释放 active。
- `schema_version` 为 2，不含 `leases`。
- `admission`：活动、等待、池数、总队列上限、授槽/释放累计数、队列满/进程队列满/取消/超时计数。`wait_duration_ms_buckets` 是累计毫秒桶，边界为 1、5、20、100、500、1000、5000、+Inf。守恒关系为 `granted_total - released_total == in_flight`。
- `retry_budget`：enabled、available_hundredths、reserved、first_sends_total、retry_sends_total、rejected_total；余额以百分之一次发送为单位。
- `attempt_maintenance`：active、checks_total、failures_total，仅维护健康探测及 Provider/逻辑频率预留，本地 Permit 不续期。
- `state_binding_failure_total`：输出绑定失败次数，同一请求的失败 latch 不重复计数。
- `recovery_scope` 为 `database`；`billing_recovery` 与恢复任务共用待处理条件。多实例采集不能把共享积压相加。

数据库查询失败返回 503，不把未知积压显示为 0。指标不包含用户、Token、渠道、请求、模型或凭据标识。

## 转换预览

`POST /api/routing/profiles/:id/preview` 仍仅允许访问当前用户的手动路由配置。除 `model`、`path` 外，可传入 `request` JSON 对象作为样本，最大 1 MiB。样本模型须与预览模型一致；Gemini 流式模式从路径识别。

```json
{
  "model": "gpt-4o-mini",
  "path": "/v1/chat/completions",
  "request": {
    "stream": true,
    "messages": [{ "role": "user", "content": "hello" }],
    "parallel_tool_calls": false,
    "top_p": 0
  }
}
```

`request_requirements` 包含协议、操作、流式、已识别特性、工具和原生状态要求；Chat 的 `shape` 仅记录字段存在性、角色和内容类型，不包含正文或参数值。`conversion_policy` 来自 Token。每个 entry 的 `conversion_plan` 表示目标协议、原生与否、转换器和流式支持；宿主专用投影另有 `request_projection`，例如 `aws_nova_text`、`coze_text`。`diagnostics` 给出结构化损失。不可执行或不符合策略的 entry 使用 `conversion_unavailable` 过滤。

四种文本协议对原始 JSON 补充顶层及 typed 嵌套结构分析。DTO 未识别的字段产生 `unknown_request_field`，包括显式 `0`、`false` 和 `null`；`strict` 拒绝丢弃，`safe/allow` 允许并记录字段路径。显式原生透传保留的字段不报告丢弃。JSON Schema、工具参数和 RawMessage/any 内容不作为 DTO 丢弃字段处理；这些开放内容在跨协议转换中的完整损失检查仍有边界。独立调用方应使用 `AnalyzeRequestJSON(request, originalJSON)`，已解码 DTO 无法恢复被丢弃字段。

渠道系统提示词在四种文本入口转换前统一应用，原生透传仍执行模型映射、提示词和参数覆盖。Nova 能保留渠道系统提示词；Coze 无法保留时，预览与执行返回 `channel_system_prompt_unsupported`，Token 的 `allow` 不能绕过。诊断不包含渠道提示词正文。

预览和执行共用 `PlanChannelConversion`。预览不会下载媒体、建立凭据、认领探测、获取 Permit、预扣或发送上游。状态归属、权限、价格、额度、健康和容量仍在真实请求时重检。诊断不包含密钥、内部端点、请求正文或其他用户数据。

`tool_identity` 只记录 `explicit_ids`、`invalid_history`、`requires_ids` 三个事实，不包含调用 ID、参数或结果。Vertex 的无 ID 投影启用时，`tool_ids_projected_to_names` 表示可无歧义改按名称关联；strict 拒绝该损失。`invalid_tool_history` 和 `tool_identity_required` 在所有策略下拒绝。请求多候选答案转换至 Claude/Responses 时返回 `multiple_candidates_unsupported`，allow 也不把多份答案合并成一份。

Claude thinking/effort、Gemini thinkingConfig 与 Chat/Responses reasoning 通过共享推理意图转换，支持的结构按目标协议渲染。具体不支持的字段由转换器报告损失并按 Token 策略拒绝；`allow` 不会使未实现的媒体、命名空间或自由格式工具路径变得可执行。搜索配置按已实现目标结构保留，不能仅凭工具名称推断无损。逐项转换合同见[协议支持矩阵](GATEWAY_SUPPORT_MATRIX.md)。

`request_requirements.media` 只记录媒体种类、MIME、来源类别、字段路径和图片 detail，不含 URL 或媒体内容。`media_conversion_unavailable` 表示当前转换器不能表达该媒体或输入形态，所有策略拒绝，不把音视频当作图片，也不丢掉文档后继续发送文本。例外是 Responses 工具结果里的非供应商来源媒体直转 Gemini：规划阶段不记录诊断，由知道模型版本的转换器决定。转换器把 Gemini 能承载的媒体转为内联数据（Gemini 3 及未知模型放入 `functionResponse.parts`，只接受 PNG、JPEG、WebP、PDF 与纯文本；Gemini 1.x/2.x 作为独立部件，接受 Gemini 支持的类型，含音视频），其余按 NewAPI 省略并给 `unsupported_media_type`，`safe`/`strict` 在发送前以 400 拒绝，`allow` 发送不含该媒体的工具结果。Claude/Gemini 经 Chat 的当前非图片投影属于此类；Chat/Responses 的已实现媒体路径按实际字段结构执行。图片 detail 在 Chat/Responses 之间保留显式 `low/high/auto` 和缺省差异，计价端仍采用原默认规则；其他不能保留 detail 的目标产生损失诊断。

Gemini 图片 `inlineData.data` 转换为 Chat/Responses/Claude 时接受标准和 URL-safe base64，解码校验后使用目标要求的标准编码。非法编码在发送前拒绝；原生 Gemini 保留客户端 SDK 的字节表示。

## Responses WebSocket 与 Compact

标准 OpenAI 渠道支持 Codex 模型、Responses、Compact 和 Responses WebSocket。原生连接使用 `GET /v1/responses` 升级，要求渠道设置 `responses_websocket_enabled` 且能生成最终原生 Responses 目标。同一连接的每个 `response.create` 都重新检查 Token、渠道、模型、状态归属、准入和最终请求预算，并为该次生成建立冻结报价与账务预留；建立连接不等于授权后续所有生成。重叠的 `response.create` 被拒绝，不重复报价或发送。

最终模型、凭据、请求头、端点、代理及渠道配置参与连接身份摘要，摘要不暴露密钥或原始请求头。同一 socket 的身份变化要求重连；新连接只有在身份和用户、Token、渠道、凭据、模型归属均匹配时才能继续旧 `previous_response_id`。更换凭据或端点不能通过重连接管旧状态，拒绝发生在报价和新上游握手之前。

明确的供应商拒绝按共享重试预算处理。部分输出、客户端断连或缺少可靠用量时按观察到的用量估算结算，没有输出的失败生成释放预留，这些情况都不重放生成。失败的取消操作不把仍在运行的生成当作已取消；不完整的入站帧不产生供应商发送或预留。`response.failed`、`response.incomplete` 即使带用量也不代表正常完成，仍按是否交付有效输出决定结算或释放。排空期间拒绝新生成，活动生成可在期限内结束；超时关闭连接后，已交付有效输出的调用按观察用量或冻结估算结算，没有输出的调用释放预留。

服务端结束会话时先向客户端发送关闭帧：Token 被拒或连接身份变化需要重连时为 1008；空闲上游正常关闭时沿用上游的 1000 或 1001；生成被截断、超时或失败时为 1011。客户端关闭时网关向上游发送 1000。每个关闭帧最多等待 1 秒，完成的生成继续复用同一连接。

Compact 使用普通模型名和 `/v1/responses/compact` 原生端点。Claude `/v1/messages/count_tokens`、Gemini `/v1/models/:model:countTokens` 与 `/v1beta/models/:model:countTokens` 没有实现，在鉴权和选路前返回 404，不进入生成、报价或预留。

## Claude 消息级 effort 与计价

`ClaudeRequest.ResolveEffectiveEffort()` 只读解析顶层 `output_config` 和消息序列：system 消息中的 effort 从其后第一个 user 回合开始生效并持续；连续等待中的设置以最后一次为准，末尾没有 user 的配置不影响当前回合。缺省、空对象和配置对象 null 不重置已有值；明确的 effort 必须是非空字符串，非法结构或非 system 角色设置 effort 返回带路径的错误，不回显原始值。解析不改变消息、缓存前缀或原始 JSON，不推断模型默认值；`GetEfforts()` 只读取顶层配置。

原生 Claude、AWS、Vertex 消息体保留配置，beta 能力由客户端或管理员请求头提供，不自动注入。发送前从最终协议和参数覆盖后的实际请求冻结 `effort` 属性并写入消费日志；尚未生效的尾部配置不参与计价，删除和重试不沿用旧属性；旧报价与已接受请求的结算不重新解释。Claude 输入跨协议后按真实目标请求计价，不把已丢失的源配置带入报价。

最终请求为 Claude 格式时，`ApplyParamOverrideWithRelayInfo` 在参数覆盖后重新解析当前回合 effort（`relay/common/override.go`）。覆盖产生的非法 effort 返回 HTTP 400 `channel:param_override_invalid`，不重试、不发送、不预扣；其他覆盖错误保留原有分类。计价属性按实际协议解析：`FreezeOutboundBillingAttributes` 依最终发送协议，`BuildBillingExprRequestInputFromRequest`（`relay/helper/billing_expr_request.go`）依 DTO 类型；只有 Claude 格式使用 `ResolveEffectiveEffort()`，其他协议读取各自的 effort 字段，effort 统一转为小写。显式 `thinking.type` 和 `budget_tokens` 等预算字段进入冻结属性：预算 0 为 `thinking=off`，正数为 `on`，-1 为 `adaptive` 且不保留预算值，显式 `type` 优先于预算推导；没有显式 thinking 时由 effort 推导，`none` 为 off，其余为 on。

跨协议出现任意消息级 `output_config`（包括空对象和 null）时产生 `claude_message_output_config_not_portable`，路径为 `messages[i].output_config`，诊断不含正文或配置值。原生保留；safe 与 strict 拒绝转换，allow 记录损失并按既有转换发送，不把消息级配置提升为全局 effort。规划、预览、直接转换以及经过其他协议再回到 Claude 的多步路径采用同一损失保护。

## 高级自定义渠道透传

高级自定义渠道的逐路由 `pass_through_body_enabled` 只在匹配请求路径、模型且该路由支持请求体透传时生效，预览和执行共用匹配逻辑。透传保留未识别扩展字段及 `0`、`false`、`null`，仍执行宿主模型映射和最终参数检查。需要改写请求体的转换器不能开启透传；只转换响应的 SGLang rerank 路由可以保留请求体。

## 视频与续作

Gemini/Vertex Veo 先生成一份校验后的请求，发送与计费共用其时长、分辨率及单输出数量。时长优先级为 metadata `durationSeconds`、`duration`、`seconds`、默认 8 秒；网关安全上限为 3600 秒，实际模型可接受的时长仍由供应商限制。非法数值、冲突大小写别名、无效 metadata、`sampleCount` 非 1 返回 400，不预扣或发送。任务 DTO 遇到损坏的 duration 和 metadata 时报错，不静默忽略。

Veo 图片仅接收单张 multipart `input_reference`、有效 base64 或 base64 data URI，最多 20 MiB；尚未实现的 URL 图片、读取失败、空或非法图片及冲突来源明确拒绝。解析失败不降级为纯文本视频。

Suno 续作需要当前 Token 的公开 `task_id` 与该成功任务歌曲数组中的 `continue_clip_id`。网关锁定原渠道、稳定凭据及端点，只向上游发送对应的私有任务 ID。未知、跨 Token 或错误歌曲统一拒绝；处理中、旧记录缺少身份、原资源暂不可用返回 503，不改换其他渠道或 Key。视频结果下载复用同一任务凭据，轮询返回显式 ID 时必须与所查询任务一致；错误 ID 不更新状态和费用。

Gemini/Vertex 视频在读取缓存下载地址前复核稳定凭据；不可用返回 `503 task_state_unavailable`。需要补查时，仅接受成功 HTTP、可解析且与原任务 ID 匹配的成功终态，否则返回 502，不从错误正文提取备用 URL。补查及 Vertex OAuth 换取响应客户端取消；这些失败不会发起后续下载或改变任务费用。

## 状态保留

状态只保存归属、摘要和到期时间，默认最长 30 天。原生响应报告有效 `expires_at` 时采用更短期限，后续事件只能缩短，不能延长已有绑定。无效或已过期的供应商期限会阻止绑定输出。

归属查询覆盖 response、conversation、Responses item_reference、encrypted_content、Claude signature/redacted_thinking、Gemini thoughtSignature/cachedContent、容器和文件引用。Gemini 同时接受 proto 字段名，透传会原样转发，因此 `thought_signature`、`cached_content` 与驼峰写法按同一引用查询。未知或不属于当前 Token 的引用拒绝；这不新增缓存、容器或 Files 产品 API。完整客户端历史中的普通工具参数不被当作网关状态。输出绑定失败不会暴露可续用标识或重发模型请求。

Gemini `fileData.fileUri` 中的 Google Files 资源地址及 `gs://` 私有资源归入 `file_uri`，使用同一 Token、渠道、稳定凭据和传输归属规则。原生响应暴露此类引用前建立绑定，未知或升级前未绑定资源要求新会话；普通公开 HTTP(S) 图片不因此增加状态依赖。仅保存 URI 摘要，预览不返回 URI 或媒体字节，不新增文件上传/管理 API。

Chat 的 `content[].file.file_id`、Responses 的 `input_file.file_id` 与 Claude 的 `document.source.file_id` 均归入 `file` 引用校验。原生 Chat 同样准备最终传输身份；已知绑定只允许在匹配的原资源域使用，未知或跨 Token 引用不发送。文件上传与初始文件管理接口仍未实现。

绑定带 `transport_id`，保存最终 URL、Host、账号范围和可能选择租户的自定义覆盖头的哈希，不保存请求头值或密钥。标准追踪头变化不改变归属。Gemini JSON/SSE 路径及 Responses/compact 路径按同一资源域核验；压缩输出的 encrypted_content 可用于原渠道后续 Responses 请求。缺少传输摘要的旧绑定返回 400，要求建立新会话，迁移不会推测旧请求的真实传输身份。

最终请求头中的认证必须与适配器使用的所选渠道凭据一致；静态或动态覆盖为另一凭据返回 400，且不发送、不预扣。`Bearer {api_key}` 等保持身份的覆盖仍可使用。实际密钥应配置在渠道 Key 中，避免绕过凭据级 RPM/TPM 和健康归属。Host、项目或租户变化导致旧状态返回 503；恢复到同一实际资源域后可重新尝试。人工替换 Key 使原绑定失效。

参数覆盖后的最终 JSON 会重新检查状态引用，不能通过覆盖注入未知状态或跨 Token 状态。Responses `prompt` 引用域目前仅登记 OpenAI 类型的原生 Responses 渠道；协议兼容渠道不自动继承该引用域。预览诊断为 `reference_domain_unsupported`，执行中的覆盖注入也会被拒绝。客户端只收到统一状态错误，内部存储故障保留在服务端日志。

参数覆盖的条件 `mode` 支持 `full`、`prefix`、`suffix`、`contains`、`regex`、`gt`、`gte`、`lt`、`lte`。`regex` 使用 Go RE2 语法（线性时间匹配，支持 `(?i)` 等内联标志），模式必须是字符串，非法模式在请求时报错；与其他文本比较一样，取值按字符串匹配，键不存在时遵循 `pass_missing_key` 且不编译模式。

其他协议转 Gemini 时，`gemini.function_call_thought_signature_enabled`（默认开启，与 NewAPI 一致，控制台“模型”设置中可关闭）为历史里每个模型轮次填入一个思考签名绕过值 `context_engineering_is_the_way_to_go`，优先放在第一个函数调用上，没有函数调用时放在第一个文本部件上；已有签名的部件不改写。Gemini 3 对缺少签名的函数调用返回 400，Google 文档允许对来自其他模型的历史使用绕过值，并说明这会降低模型效果。关闭后转换结果不带签名，含函数调用历史的 Gemini 3 请求会被供应商拒绝。绕过值是公开常量而非供应商状态，归属查询不为它和 Google 文档列出的 `skip_thought_signature_validator` 建立或要求绑定，入站与最终请求的状态复核都不拦截这两个值。供应商返回的真实签名仍按原生协议与绑定身份处理；跨协议响应中的签名按 `provider_state_dropped` 丢弃，见支持矩阵。

`gateway_state_cleanup` 系统任务每 5 分钟删除最多 1000 条过期绑定，复用数据库任务租约，单次预算 5 秒。清理延迟不影响权限：查找时直接拒绝过期记录。任务只清理网关状态元数据，不操作账单、异步任务记录或完整会话。

## 准入错误与日志

用户/渠道容量不足、队列满、等待超时继续返回 429；`gateway_total_queue_full` 明确区分进程总等待上限。排空返回 503 `gateway_draining`，策略读取失败返回 503。客户端取消或输出已提交时不写第二份错误。

新日志使用 `admission_state`，读取器兼容历史 `lease_state`。重试额度耗尽在管理日志记录 `retry_budget_exhausted: true`，保留最后一个真实上游错误，不追加供应商健康处罚。

`route_decision.attempts` 按真实发送顺序记录成功状态、稳定错误类别、归一化错误 HTTP 状态、来源、耗时、接受状态、重试许可和响应是否提交；发送前拒绝不生成 attempt。较早的日志可以没有此字段。`latency_ms` 包含本轮发送前准备，`upstream_latency_ms` 才是实际发送至完成的测量值；缺证据时省略，不能把未知表示为零。成功 HTTP 码不作推断。内部观察保留渠道及稳定凭据 ID，但不记录凭据原文、供应商错误正文、请求体或请求头。

所有计价模式在发送前持久化消费保护和冻结估算；最终用量无法计价或写入失败时按冻结估算结算。任务、图片及其他有副作用的生成请求交付有效输出或任务已被接受时按证据结算，没有输出且没有任务 ID 时释放；上传重放仍受限制。状态处理见[结算兜底与恢复](../billing/RECOVERY.md)，数据格式与切换要求见[发布契约](GATEWAY_RELEASE.md)。

消费日志的 `other.response_model_name` 来自上游真实 JSON、SSE 或 WebSocket 事件中的模型声明，在协议输出改写之前捕获；`model_name` 是请求与计价模型，`other.upstream_model_name` 是真实上游请求模型。响应模型只用于诊断，不改变路由或价格；上游没有声明，或声明与请求模型相同且无需映射时可以省略。界面按忽略大小写的前后缀兼容规则派生差异，不存储第二个 mismatch 字段。重试重置观测，第一份实质差异不被后续普通帧覆盖。用户日志投影限制字段长度并拒绝控制字符。

## 模型 manifest 与客户端配置

`GET /api/user/model-manifest?token_id=...` 返回版本为 1 的权限过滤目录。省略 Token 时使用当前用户资格；指定 Token 必须属于当前用户且仍启用、未过期。模型集合由现有 Token 模型权限、渠道状态、手动路由、能力快照和价格资格共同计算，不存第二份目录。`GET /api/user/models` 继续返回名称数组；模型市场和 `/v1/models` 使用同一 manifest 决定集合，后者只投影 OpenAI 兼容字段。

manifest 每项包含 `id`、`visible`、`supported_endpoint_types`、可选 `capabilities`、`price_configured` 和 `price_version`。能力卡的 null、false、空数组含义不同：未知不等于禁止，也不意味着已证明支持。元数据编辑器省略字段不清空；明确清空按模型能力合同执行。能力卡不修改价格、冻结报价或路由能力真相源，不导入默认供应商目录与价格。

模型元数据的 `capabilities` 以 TEXT 保存 JSON，字段为 `reasoning_efforts`、`input_modalities`、`output_modalities`（各最多 32 个不重复的非空值）、`context_limit`、`output_limit`（正整数）、`tool_call`、`source` 和服务端写入的 `updated_at`。null 表示未知，空列表和 false 是明确事实。旧元数据客户端省略能力卡不会清空它；显式空对象清空已知事实。

价格资格在实际端点、协议绑定和渠道执行器确定后计算。原生端点使用空插件作用域；插件端点复用该插件的用量 schema 和报价解析器，不能拿其他插件表达式代替。显式不兼容插件表达式拒绝该端点，不能退回原生价格掩盖。现有报价器允许的全局按次价格后备及显式允许未配置价格模型的设置保留；不额外引入另一套定价规则。协议声明的模型子集与别名使用请求中间件的同一索引。

`GET /api/token/:id/client-config?format=codex|pi` 使用该 Token 的同一 manifest。Codex 导出 Responses TOML，可用 `model` 查询参数选择已授权模型；Pi 导出 Chat Completions JSON。响应包含 `filename`、`media_type`、`content`、`models` 和稳定诊断 `diagnostics`。Token 页提供预览、复制、下载。连接地址仅来自保存的 `ServerAddress`，不使用请求 Host 或转发头；密钥只使用 `REN2HUB_API_KEY` 环境变量引用。未知能力省略，不能移植的能力有诊断，价格一律省略并返回 `prices_omitted`，不虚构零价。PAT 分别要求 `profile:read`、`tokens:read`。

Codex 客户端导出使用标准 OpenAI Responses 协议，不依赖 ChatGPT 订阅渠道、OAuth 或供应商用量窗口；Pi 继续使用标准 Chat Completions。导出只能包含当前 Token 有权使用且可路由的模型，退役 type 57 不会贡献可用端点。

## 渠道生命周期

渠道编号及分类见[渠道接入类型](../channel/TYPES.md)。整数 57 保留为已退役的 ChatGPT Subscription (Codex) 渠道，不会重新分配，也不会自动转成 OpenAI。历史记录仍可由授权管理员读取和显式删除；新建、编辑、复制、测试、模型抓取、同步和路由均拒绝该类型。管理入口沿用原渠道错误 envelope，返回 HTTP 200、`success:false` 和稳定消息 `retired channel type`；生成请求的选路失败继续使用现有网关协议错误格式。列表显示退役状态，隐藏编辑、刷新、测试和用量操作。

Codex 订阅的刷新、用量、额度重置路由及 provider-usage 路由不注册，这些 URL 不能触发 OAuth 或上游请求。钱包、付款和业务订阅接口不受渠道退役影响。迁移方式是管理员删除旧渠道后，用官方 API Key 新建标准 OpenAI 渠道；不能复用旧 OAuth JSON 作为 API Key。

## 渠道状态摘要

管理渠道列表在自动禁用时投影只读 `status_reason` 和 `status_time`（Unix 秒）。原因从现有状态事实派生，服务端去除凭据、账号、URL 和原始响应正文，最多 500 个字符；非自动禁用或无有效事实返回空串与 0。旧接口的 `other_info.status_reason` 同样脱敏，不新增原因存储。界面支持悬停、键盘和移动端点击查看，长文本可换行。

## 请求诊断与关联

生成请求可提供 `X-Ren2Hub-Trace-Id`、`X-Ren2Hub-Thread-Id`：各为 1–128 个 ASCII 字母、数字或 `-_.:`，重复头或非法值在准入前拒绝。关联键按 `(user_id, token_id, external_id)` 隔离；它不是鉴权凭据，也不替代供应商状态归属。

本人读取使用 `GET /api/log/self/requests/:request_id/diagnostics` 和 `GET /api/log/self/request-diagnostics`；管理员使用去掉 `/self` 的对应路径。列表接受 `request_id`、`trace_id`、`thread_id`、`token_id`，按 Trace/Thread 查询必须指定正数 Token ID。自身入口始终绑定当前用户，不能通过查询参数跨用户。Playground 的临时 Token 0 只支持按 request_id 查看时间线，界面隐藏跨请求关联按钮，不跨临时请求聚合。

诊断为独立无正文投影，默认保留 7 天，读取时立即过滤过期数据，后台每批最多清理 1000 行。本人只见脱敏字段；管理员的内部渠道/凭据标识还受当前 `ChannelRead` 和 PAT `channels:read` 交集约束。`ops:read` 本身不会授予这些字段。缺失或已过期的详情返回 `404 REQUEST_DIAGNOSTIC_UNAVAILABLE`；数据库故障返回错误，不伪装为空结果。

`attempt_id` 对应真实发送，未发送候选不形成 attempt；`send_started_at`、`first_output_at`、`finished_at` 为 Unix 毫秒，缺失首个有效输出时不生成 TTFT。`ttft_ms` 从实际发送到首个有效语义输出，不把 HTTP 头、SSE 心跳或空前导帧视作输出。`queue_ms` 与 `user_queue_ms` 来自真实等待队列计时；`wait_ms` 是共享等待预算的消耗，不能解释为纯排队耗时。投影沿用有界观察队列，丢弃与存储失败独立计数，不阻塞发送/结算，不进入资金事务，也不删除消费日志或资金流水。

## vLLM 与 SGLang 状态

`GET /api/channel/:id/vllm/status` 与 `GET /api/channel/:id/sglang/status` 要求管理员身份和 `channel.read`，响应禁止缓存。目标只来自保存的对应类型渠道，查询参数不能提供 URL 或密钥。请求沿用渠道的代理和请求头边界，允许自托管私网；禁止重定向，只执行四个固定 GET，不启动或停止推理进程。

vLLM 读取 `/health`、`/version`、`/v1/models`、`/metrics`；SGLang 把版本路径换为 `/server_info`。整体期限 10 秒，每个正文最多 4 MiB。一个端点失败仍返回其他结果；错误值只有 `request_failed`、`http_error`、`response_too_large`、`invalid_response`，不回显上游错误正文、端点或凭据。SGLang 的服务信息只投影 `version`，不返回启动配置。

成功 envelope 的 `data` 包含 `sampled_at`（毫秒）、`endpoints`（路径到 status/error）、`version`、`models`（id/root/max_model_len）、`metrics`（name/labels/value）和 `raw_metrics`。指标保留对应引擎前缀与 `process_start_time_seconds`，排除 histogram bucket、负值、NaN 和无穷值，保留 sum/count 和不同 labels 的独立序列。`raw_metrics` 只返回给上述授权管理员，不写日志。真实推理服务测试需配置 `NEW_API_VLLM_TEST_URL` 和可选的 `NEW_API_VLLM_TEST_KEY`，缺少环境变量时的跳过不算通过。
