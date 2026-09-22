# 网关准入管理 API

所有接口位于 `/api/gateway`。成功返回 `{ "success": true, "message": "", "data": ... }`；错误使用对应 HTTP 状态码，并返回 `success: false`、`message` 和 `code`。这些接口禁止浏览器缓存。

| 方法和路径 | 权限 | data |
|---|---|---|
| `GET /self` | 登录用户 | 当前用户配置、有效值及来源 |
| `GET /self/runtime` | 登录用户 | 当前用户的本地准入运行态 |
| `GET /config` | Root | 全局网关配置 |
| `GET /metrics` | Root | schema v2：本地准入、重试预算、尝试维护、状态绑定失败及账务恢复积压 |
| `PUT /config` | Root | 保存后的完整配置，version 加一 |
| `GET /users/:id` | 管理员 | 指定用户配置、有效值及来源 |
| `PUT /users/:id` | 管理员 | 保存后的用户覆盖 |
| `GET /runtime/users/:id` | 管理员 | 本地准入运行态 |
| `GET /runtime/channels/:id` | 管理员且具备 channel.read | 本地准入运行态，加 `channel_id`、`rate`、`rate_status` |
| `GET /channels/:id/credentials` | 管理员且具备 channel.read | 已登记凭据的 `[{ id }]`，id 为稳定 UUID |
| `GET /runtime/credentials/:id` | 管理员且具备 channel.read | 凭据的 rate |

Root 可以管理所有用户；其他管理员只能查看和修改角色低于自己的用户。`/self` 始终读取登录用户，不接受用户 ID。凭据列表返回已通过可信路径登记的身份，不返回密钥、指纹或可变密钥索引；它不是当前启用密钥列表。

`GET /api/gateway/metrics` 的 schema v2 增加 `route_decision_events_dropped_total`，表示本实例有界路由日志队列的累计丢弃数，进程重启后清零。

## Token 路由预览

`POST /api/routing/tokens/:id/preview` 使用用户认证、Token 所有权检查、敏感接口限频和禁止缓存。请求为 `{ "model": "gpt-4o-mini", "path": "/v1/chat/completions", "request": { "model": "gpt-4o-mini", "messages": [{ "role": "user", "content": "示例" }] } }`；request 可省略，样本最多 1 MiB。正文模型必须匹配 model，Gemini 同时校验 URL 模型及生成操作；Compact 的已登记模型后缀按既有语义比较。

沿用统一 success/data 包装。data 保留旧 Profile 预览字段，新增 `token_id`、`mode`（auto_lab/manual）、服务端生成的 `preview_id`、毫秒时间戳 `generated_at`；`profile_version` 为配置版本，无 Profile 时为 0。每条 entry 增加 `priority`、`score_breakdown`，继续提供 conversion_plan、diagnostics、filter_reason。自动候选没有 Entry，entry_id 为 0。

`candidate_channel_ids` 按当前资格和实际排序生成，`preferred_channel_id` 仅表示此快照首选。`runtime_recheck_required` 始终为 true，reasons 包括价格、实时额度、安全策略、准入容量、Provider 限制、配置版本、亲和及必要的状态归属。无 Profile 的预览不创建 Profile；不发送请求、不预扣、不持有 Permit、不占用健康探测。

旧 `POST /api/routing/profiles/:id/preview` 保留所有权检查并委托 Token 预览。预览样本只在当前请求内分析，不加入路由日志或持久化历史。

## 配置契约

`PUT /config` 保留原有全量更新契约和乐观版本检查。新客户端先 GET 并保留所有字段；旧客户端省略本轮新增的四个字段时保留数据库当前值，显式 null 返回 400。`channel_rates` 按渠道 ID 配置，该渠道的所有模型共享额度。`credential_rates` 按稳定凭据 UUID 配置，同一渠道内使用该凭据的请求共享额度。两个范围原子准入。

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

新增字段：`total_queue_size` 范围 0–100000，0 禁止新增排队但不阻止立即获槽；`retry_budget_percent` 范围 1–100；`retry_budget_burst` 范围 1–10000；`retry_budget_enabled` 为布尔值。数据库缺失字段按上例补齐，仅实际补字段时版本增加一次，已有显式 0/false 保留。

`PUT /users/:id` 的请求和成功 data 为 `{ concurrency: number|null, wait_ms: number|null, queue_size: number|null }`，替换该用户的三个覆盖字段。null 恢复继承；显式 0 保留其含义。该更新允许重复提交相同值。

用户配置查询返回：

```json
{
  "user_id": 3,
  "group": "team",
  "config_version": 2,
  "override": { "concurrency": 0, "wait_ms": null, "queue_size": null },
  "effective": { "concurrency": 0, "wait_ms": 5000, "queue_size": 20, "channel_queue_size": 20 },
  "sources": { "concurrency": "user", "wait_ms": "system", "queue_size": "system", "channel_queue_size": "system" }
}
```

来源仅有 `user`、`group`、`system`，与实际准入共用逐字段解析。渠道队列长度当前来自系统配置。用户并发 0 表示不设并发上限；仍记录在途占用。等待或队列为 0 时，满载直接拒绝。

## 运行数据

rate 为 `{ rpm_used, tokens_used, tokens_reserved, cooldown_ms }`：RPM 统计最近 60 秒已准入尝试；发送前失败会退回。tokens_used 为最近 60 秒已结算用量，tokens_reserved 为仍有效的在途估算。发送后用量未知时保留估算，实际用量超过估算时按真实用量入账并限制后续请求。cooldown_ms 是当前渠道或渠道内凭据的 Retry-After 冷却剩余毫秒，独立渠道不共享冷却。

用户和渠道并发运行态来自同一进程内 manager，返回 `in_flight`、`queued`、`capacity`、`config_version`、`revision`、`started_at`（Unix 秒）、`draining`。查询不创建池、不占容量。Provider rate 继续读取 Redis；渠道并发可读而 Provider 不可用时仍返回 200、真实本地计数及 `rate: null`、`rate_status: unavailable`；正常时 `rate_status: available`。独立凭据 rate 读取失败仍返回 503。无效输入返回 `400 VALIDATION_ERROR`；资源不存在返回 `404 NOT_FOUND`；无权管理目标用户返回 `403 FORBIDDEN`。

生成入口的用户准入在 OpenAI 错误对象中返回明确 `code`，包括 `gateway_capacity_exhausted`、`gateway_queue_full`、`gateway_wait_timeout`、`gateway_total_queue_full` 和 `gateway_draining`。诊断 message 分别标记 `user admission` 或 `process admission`；渠道尝试错误保留 `channel admission` 原因，路由日志记录对应渠道。客户端已取消或已提交响应时不再追加错误。

TPM 要求宿主登记可解析的最终请求格式。仅支持媒体任务、Coze、Codex 及未登记旧投影的渠道或凭据不能配置 TPM，保存返回 `400 VALIDATION_ERROR`；RPM 不受此限制。运行时重新检查最终发送体：覆盖后的最大输出上限乘输出数量，加输入 JSON 估算；关闭 CountToken 不关闭准入计数。已登记的 Embedding 使用输入估算。未知扩展、无法估算的媒体/状态输入、非文本输出或缺少有效上限均不能伪装成有限预算。渠道类型变更后残留的无效 TPM 也在发送前拒绝。

预扣发生在转换、最终预算检查和供应商限流通过后。最终输入估算与输出上限同步刷新本次尝试的预扣金额；内部重试共用一个计费会话。`route_decision` 的 `token_budget`、`token_budget_source` 和 `rejection_origin` 记录估算及失败归属，不包含正文或密钥。

## 运维指标

`GET /metrics` 复用 Root 鉴权和关键接口限流，建议低频采集。`counter_scope` 为 `instance`，`started_at` 为进程启动时间；重启后累计计数归零。本系统仅支持一个进程承接业务流量，`/runtime/*` 与指标读取同一个 manager。

- `user_queue`、`channel_queue`：`active` 为本进程等待请求数；`entered_total`、`completed_total`、`failed_total` 为累计结果，`wait_ms_total` 为累计等待毫秒。取消计入失败并释放 active。
- `schema_version: 2`；移除 `leases`。
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

`reasoning_controls_not_portable` 表示当前转换不能保留供应商推理控制，包含 Claude thinking/effort、Gemini thinkingConfig，以及 Chat/Responses 转 Claude/Gemini 时的 effort 近似或丢失。`safe/strict` 拒绝，`allow` 显式接受并记录损失；Chat 与 Responses 之间可等价复制的 effort 仍可在 strict 下转换，Responses 的 summary/mode/context 不随之宣称可移植。带配置的搜索走 `unsupported_tool_semantics`；搜索上下文大小映射为次数等近似不能标为无损。

`request_requirements.media` 只记录媒体种类、MIME、来源类别、字段路径和图片 detail，不含 URL 或媒体内容。`media_conversion_unavailable` 表示当前转换器不能表达该媒体或输入形态，所有策略拒绝，不把音视频当作图片，也不丢掉文档后继续发送文本。Claude/Gemini 经 Chat 的当前非图片投影属于此类；Chat/Responses 的已实现媒体路径按实际字段结构执行。图片 detail 在 Chat/Responses 之间保留显式 `low/high/auto` 和缺省差异，计价端仍采用原默认规则；其他不能保留 detail 的目标产生损失诊断。

Gemini 图片 `inlineData.data` 转换为 Chat/Responses/Claude 时接受标准和 URL-safe base64，解码校验后使用目标要求的标准编码。非法编码在发送前拒绝；原生 Gemini 保留客户端 SDK 的字节表示。

## 视频与续作

Gemini/Vertex Veo 先生成一份校验后的请求，发送与计费共用其时长、分辨率及单输出数量。时长优先级为 metadata `durationSeconds`、`duration`、`seconds`、默认 8 秒；网关安全上限为 3600 秒，实际模型可接受的时长仍由供应商限制。非法数值、冲突大小写别名、无效 metadata、`sampleCount` 非 1 返回 400，不预扣或发送。任务 DTO 不再静默忽略损坏的 duration 和 metadata。

Veo 图片仅接收单张 multipart `input_reference`、有效 base64 或 base64 data URI，最多 20 MiB；尚未实现的 URL 图片、读取失败、空或非法图片及冲突来源明确拒绝。解析失败不降级为纯文本视频。

Suno 续作需要当前 Token 的公开 `task_id` 与该成功任务歌曲数组中的 `continue_clip_id`。网关锁定原渠道、稳定凭据及端点，只向上游发送对应的私有任务 ID。未知、跨 Token 或错误歌曲统一拒绝；处理中、旧记录缺少身份、原资源暂不可用返回 503，不改换其他渠道或 Key。视频结果下载复用同一任务凭据，轮询返回显式 ID 时必须与所查询任务一致；错误 ID 不更新状态和费用。

Gemini/Vertex 视频在读取缓存下载地址前复核稳定凭据；不可用返回 `503 task_state_unavailable`。需要补查时，仅接受成功 HTTP、可解析且与原任务 ID 匹配的成功终态，否则返回 502，不再从错误正文提取备用 URL。补查及 Vertex OAuth 换取响应客户端取消；这些失败不会发起后续下载或改变任务费用。

## 状态保留

状态只保存归属、摘要和到期时间，默认最长 30 天。原生响应报告有效 `expires_at` 时采用更短期限，后续事件只能缩短，不能延长已有绑定。无效或已过期的供应商期限会阻止绑定输出。

归属查询覆盖 response、conversation、Responses item_reference、encrypted_content、Claude signature/redacted_thinking、Gemini thoughtSignature/cachedContent、容器和文件引用。未知或不属于当前 Token 的引用拒绝；这不新增缓存、容器或 Files 产品 API。完整客户端历史中的普通工具参数不被当作网关状态。输出绑定失败不会暴露可续用标识或重发模型请求。

Gemini `fileData.fileUri` 中的 Google Files 资源地址及 `gs://` 私有资源归入 `file_uri`，使用同一 Token、渠道、稳定凭据和传输归属规则。原生响应暴露此类引用前建立绑定，未知或升级前未绑定资源要求新会话；普通公开 HTTP(S) 图片不因此增加状态依赖。仅保存 URI 摘要，预览不返回 URI 或媒体字节，不新增文件上传/管理 API。

Chat 的 `content[].file.file_id`、Responses 的 `input_file.file_id` 与 Claude 的 `document.source.file_id` 均归入 `file` 引用校验。原生 Chat 同样准备最终传输身份；已知绑定只允许在匹配的原资源域使用，未知或跨 Token 引用不发送。文件上传与初始文件管理接口仍未实现。

绑定新增 `transport_id`，保存最终 URL、Host、账号范围和可能选择租户的自定义覆盖头的哈希，不保存请求头值或密钥。标准追踪头变化不改变归属。Gemini JSON/SSE 路径及 Responses/compact 路径按同一资源域核验；压缩输出的 encrypted_content 可用于原渠道后续 Responses 请求。缺少传输摘要的旧绑定返回 400，要求建立新会话，迁移不会推测旧请求的真实传输身份。

最终请求头中的认证必须与适配器使用的所选渠道凭据一致；静态或动态覆盖为另一凭据返回 400，且不发送、不预扣。`Bearer {api_key}` 等保持身份的覆盖仍可使用。实际密钥应配置在渠道 Key 中，避免绕过凭据级 RPM/TPM 和健康归属。Host、项目或租户变化导致旧状态返回 503；恢复到同一实际资源域后可重新尝试。可信 OAuth 刷新保留凭据 ID，若刷新结果包含不同账号则拒绝写入；人工替换 Key 使原绑定失效。

参数覆盖后的最终 JSON 会重新检查状态引用，不能通过覆盖注入未知状态或跨 Token 状态。Responses `prompt` 引用域目前仅登记 OpenAI 类型的原生 Responses 渠道；协议兼容渠道不自动继承该引用域。预览诊断为 `reference_domain_unsupported`，执行中的覆盖注入也会被拒绝。客户端只收到统一状态错误，内部存储故障保留在服务端日志。

Gemini 转换器不再自动填入思考签名绕过标记，`allow` 也不会合成供应商状态。旧 `gemini.function_call_thought_signature_enabled` 配置仍可读取但不再生效，控制台已移除该开关；RelayKit 同名选项仅保留源码兼容。供应商返回的真实签名仍按原生协议与绑定身份处理。

`gateway_state_cleanup` 系统任务每 5 分钟删除最多 1000 条过期绑定，复用数据库任务租约，单次预算 5 秒。清理延迟不影响权限：查找时直接拒绝过期记录。任务只清理网关状态元数据，不操作账单、异步任务记录或完整会话。

## 准入错误与日志

用户/渠道容量不足、队列满、等待超时继续返回 429；`gateway_total_queue_full` 明确区分进程总等待上限。排空返回 503 `gateway_draining`，策略读取失败返回 503。客户端取消或输出已提交时不写第二份错误。

新日志使用 `admission_state`，读取器兼容历史 `lease_state`。重试额度耗尽在管理日志记录 `retry_budget_exhausted: true`，保留最后一个真实上游错误，不追加供应商健康处罚。

`route_decision.attempts` 按真实发送顺序记录渠道、成功状态、稳定错误类别、归一化错误 HTTP 状态、来源、耗时、接受状态、重试许可和响应是否提交；发送前拒绝不生成 attempt。旧日志可以没有此字段。耗时包含本轮准备，不代表纯上游延迟；成功 HTTP 码和 TTFT 不作推断。记录不包含供应商原始错误、正文、请求头或凭据，并沿用现有有界日志队列。

所有计价模式在发送前持久化待确认消费保护；最终用量写入失败的请求保留预留并计入恢复积压，不自动释放，也不占自动恢复批次。任务、图片及其他有副作用的生成请求已接受或接受不明时同样保留保护；明确未消费才允许释放。文件上传等前置操作失败且生成尚未发送时可释放生成预留，上传重放仍受限制。状态处理见[恢复与对账](../billing/RECONCILIATION.md)，数据格式与切换要求见[发布契约](GATEWAY_RELEASE.md)。
