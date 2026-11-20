# 网关与最小扩展 API

现行路由由 `router/` 注册，管理权限采用 NewAPI authz 与 PAT scope 目录。Vue 使用同一合同；不存在 `/api/next` 兼容层。模型调用 API Key、浏览器会话和管理 PAT 是不同凭据。

| 路由 | 用途与边界 |
| --- | --- |
| `/v1/models`、`/v1beta/models` | 鉴权后的原生模型目录 |
| `/v1/chat/completions`、`/v1/messages`、`/v1/responses` | Chat、Claude、Responses；流式和转换取决于适配器 |
| `/v1/responses/compact`、GET `/v1/responses` | 原生 Compact 与 Responses WebSocket |
| `/v1/realtime`、音频、图片、视频及插件路由 | 以 relay/task/video router 真实注册和所选模型能力为准 |
| `GET/PUT /api/token/:id/routing` | 自有Token偏好；原生 `routing:read/write` 权限 |
| `GET /api/token/:id/routing/channels` | 当前可选渠道，不含完整渠道密钥和 `in_flight` |
| `GET /api/token/:id/routing/preview?model=...&path=...&stream=...&transport=...` | 当前配置的静态资格预览，不调用供应商；transport 为 http（默认）或 websocket，WS 仅 /v1/responses；非法 transport/stream 为 400 ROUTING_INVALID，不支持静态预览的路径为 400 preview_path_unsupported |

路由保存 `{mode:"auto"|"ordered",selected_only,version,entries:[{channel_id,enabled}],max_channel_ratio:null|"1.5"}`。全平台配置为 `auto`、`selected_only=false`、零条目。选定渠道时两种模式均要求 1–100 条互不重复的条目。保存时按数组顺序重写 `position`，顺序模式按此选路。管理员删除最后一条后 GET 仍返回 `selected_only=true`，令牌拒绝普通调用直到重新配置。非法请求返回 400，未授权渠道返回 403，版本冲突返回 409，不可见令牌返回 404。创建令牌时省略 `type` 或传 `auto`，其他值返回 400。列表和详情的 `type`、`selected_only` 和 `route_channel_count` 表示选路方式、范围和条目总数。删除业务分组，旧组参数明确拒绝：`/api/channel/*`、`POST|PUT /api/token/`、`/api/user/`、`/api/user/self` 和 `*/routing` 的写请求只要请求体非空，不论 Content-Type 都检查，非 JSON 请求体返回 400 `invalid JSON request`。

预览响应回显解析后的 `model`、`transport` 和 `stream`（WS 为 true），并给出 `ordering`：顺序模式为 `"ordered"`，自动模式为 `"weighted_random_per_request"`，表示加权随机按每个请求单独计算，预览只是其中一次抽样。条目为 `{channel_id,name?,eligible,reason,channel_ratio}`。自动模式只列出候选集合内服务所查模型、且可路由的渠道，被禁用的渠道与模型目录一样不出现；顺序模式按列表顺序列出全部条目，指向被禁用或已删除渠道的条目保留 `reason`（如 `channel_unavailable`），但不返回 `name`，`channel_ratio` 为空串。普通用户视图（模型目录与 `routing/channels` 共用 `service.RouteChannel`）的共享字段为 `id`、`name`、`type`、`status`、`models`、`supported_endpoints`、`channel_ratio`、`max_concurrency`、`eligible` 和可选的 `filter_reason`。模型目录的渠道项另含 `lab`（`id`、`name`、`source`），用于厂商分类展示，不参与路由或计费；`routing/channels` 不因共用字段而增加该项。实时 `in_flight` 只出现在管理端渠道列表、搜索、详情和更新响应里。

| 聚合接口 | 合同 |
| --- | --- |
| `GET /api/user/model-catalog?token_id=` | 会话用户或本人 Key 的模型、公开渠道、端点及原生基础价×渠道倍率；不返回地址、代理或凭据 |
| `GET /api/channel/priority/order` | 原生优先级快照与 revision |
| `POST /api/channel/priority/reorder` | NewAPI 版本校验、预览及事务更新；revision 冲突、移动无效或越界时返回 HTTP 200 加 `success:false` |
| `GET /api/channel/labs` | 全平台厂商分类聚合；分类不控制选路 |
| `GET /api/home/metrics` | 消费日志请求计数及 24 个明确时间区间，共享短缓存 |
| `GET /api/pricing/count` | `{models}` 为原生 `/api/pricing` 列出的模型数，供游客首页显示；门控与 `/api/pricing` 相同（`HeaderNavModuleAuth("pricing")`），PAT 规则同为不限 scope |
| `GET /api/data/self/summary` | 本人区间、对比、模型/Token 分布；计数、token、额度和缓存读写来自 `quota_data`，`hourly` 是按小时升序、不缺小时的序列 |
| `GET /api/performance/routing` | Root 读取当前用户/渠道占用与近期真实尝试观察 |
| `GET /api/performance/quality?hours=24` | Root，PAT scope `ops:read`；全站跨模型按小时汇总 `perf_metrics`，`hours` 取 1–720 |
| `GET /api/performance/stats` | 原生字段之外追加 `system_usage`（主机 CPU、内存）和 `db_pool`（`sql.DB.Stats()`） |
| `/api/user/affiliate` | 本人规则、奖励、records、脱敏 invitees 及幂等全额 transfer |
| `/api/admin/affiliate` | 返利记录和个人比例/邀请码管理；按订单回收返利在 `/api/user/topup/admin/affiliate-reversal`，见 [API](../billing/API.md)；全局配置使用原生 options |

厂商分类还用于原生定价目录 `supplementVendorID` 的缺失厂商补全；只补展示归属，不改变已有厂商、模型价格、路由或计费。

渠道 `channel_ratio` 为大于 0、不超过 1000、最多 18 位小数、十进制指数在 -18 到 18 之间的十进制字符串，用户/渠道 `max_concurrency` 为非负整数，0 表示无限。修改 `channel_ratio` 需要 ChannelSensitiveWrite：只有 ChannelWrite 的管理员提交不同的倍率时返回 HTTP 200、`success:false`，整条请求不生效；提交与当前值等价的写法（如 `"1.0"`）仍然允许。`max_concurrency` 只影响运行，按非敏感字段处理。自动排序与资金接点见[渠道路由](UNIFIED_ROUTING.md)。

`summary` 不含个人以外的数据。`meta.source` 为 `"quota_data"`，`meta.available` 等于 `DataExportEnabled`；为 false 时计数为 0、序列为空数组，表示没有数据来源，不表示用量为 0。耗时和成功率按本人消费日志计算，`meta.coverage.latency`、`meta.coverage.success_rate` 为 false 或区间内没有样本时对应值为 `null`，不拿全站 `perf_metrics` 代替。`quota_data` 每 `meta.flush_interval_minutes` 分钟落库一次，最近的小时可能尚未写入。

`summary` 的区间最长 366 天。实际读取的跨度从窗口起点算到 `min(现在, 窗口终点)`；`include_comparison` 默认 `true`，此时起点前移一个窗口长度。跨度超过 `/api/status` 的 `log_max_range_days` 时返回 HTTP 200、`success: false` 和 `The time range cannot exceed {{days}} days`。前端按同一公式预判：本窗口自身超限时不发请求并提示缩短区间；只有加上上一窗口才超限时传 `include_comparison=false`，KPI 标注“无法对比”；钱包本月统计超限时显示“—”；热图只查询 `min(364, log_max_range_days)` 天，范围外的日期标为未覆盖，不计入合计和均值。

`quality` 的窗口与原生 `/api/perf-metrics/summary` 相同，最后一个点是当前未满的小时；窗口内没有请求时 `summary` 为 `null`，没有请求的小时三个比率都为 `null`。`stats.system_usage` 在监控关闭、尚未采样或采样超过 30 秒时为 `null`，不返回 0；`db_pool.log` 为 `null` 且 `log_shared=true` 表示日志库与主库共用连接池。

个人工单 `/api/tickets` 全部要求浏览器会话：GET列表、POST创建、GET `/:id`、POST `/:id/messages`、PATCH `/:id/status`、GET `/attachments/:attachment_id`。创建与回复为multipart，附件字段 `attachments`，最多五张、每张10MiB；详情返回 `{ticket,messages}`，列表返回 `{items,total,page,page_size}`。表单按流式解析，第一次越界就停止读取，返回 HTTP 200、`success:false` 和 `code`：`ATTACHMENT_TOO_MANY`、`ATTACHMENT_TOO_LARGE`、`ATTACHMENT_TYPE_UNSUPPORTED`、`ATTACHMENT_QUOTA_EXCEEDED`、`ATTACHMENT_STORAGE_FULL`、`REQUEST_TOO_LARGE`、`VALIDATION_ERROR`。

每个附件开始写入前，在进程内按单张上限 10 MiB 预留，写完收缩到实际大小，请求以任何方式结束时释放；不使用 `Content-Length`，没有附件的文字回复不经过这两项检查。个人预算：每个用户滚动 24 小时内作为作者在用户消息里写入的附件（按 `ticket_attachments.message_id → ticket_messages.author_id` 统计）加上该用户在途预留和本次预留不超过 200 MiB，否则返回 `ATTACHMENT_QUOTA_EXCEEDED`，剩余不足 10 MiB 时不再接受新附件。预算查询在登记本次预留之后进行，不持有预留锁，带请求上下文和 3 秒超时；查询失败、超时或请求取消时释放本次预留并按普通错误返回。这是写入速率预算，不是累计占用上限，附件不删除。管理员回复入口不受个人预算约束。空间下限：附件目录所在文件系统的 `Bavail` 减去全站在途预留和本次预留后不足 5 GiB 时，用户和管理员都返回 `ATTACHMENT_STORAGE_FULL`，附件停止接收直到空间恢复；读取可用空间失败按普通错误返回。下限只约束本应用的附件写入，不能阻止同一分区上的其他写入。非 Linux 的开发构建不做空间检查，产品只交付 Linux 镜像。用户的创建与回复按 IP 限速，管理员回复按管理员账号限速（`UserCriticalRateLimit("ticket-admin")`）。附件先写临时文件，在数据库事务开始前改名到最终位置；改名失败时清理已暂存或改名的文件，不创建工单或回复记录，并返回普通错误响应。数据库事务失败也会清理这些文件；成功提交后才保留文件。当前后端不发送 `ATTACHMENT_STORAGE_FAILED`，不能把旧的前端兼容分支解释为现行接口的部分成功保证。下载响应带 `X-Content-Type-Options: nosniff`。

管理员工单前缀 `/api/admin/tickets`：读取列表/详情/summary/附件需要 `ticket:read`，回复需要 `ticket:reply`，agents、status、assignee 需要 `ticket:manage`。指派使用 PATCH `/:id/assignee` 与 `{assignee_id:number|null}`。管理员scope与当前角色权限取交集；个人接口不会因为PAT有ticket scope而开放。

状态修改（用户与管理员的 PATCH `/:id/status`）只接受 `open` 和 `closed`，按流转处理：`open`/`replied` → `closed` 写入关闭时间，`closed` → `open` 清空关闭时间。请求当前状态，或对 `replied` 的工单请求 `open`，不写库，返回工单现状，`updated_at` 和列表排序不变；管理端只在状态真的变化时写审计。

工单通知（新工单、回复、指派）的类型都是 `ticket_update`，走原生通知器按“接收人 + 类型”的限额（默认 10 分钟 2 条，`NOTIFY_LIMIT_COUNT`、`NOTIFICATION_LIMIT_DURATION_MINUTE`）。同一个接收人短时间内超出的通知不发送，其中可能有别的工单的通知；通知是尽力而为，处理工单以管理员工单列表和用户的工单列表为准。

提交用户被删除后，工单仍出现在管理员列表、summary 计数和关键字搜索中，详情、回复与状态修改照常可用。列表和详情的每个工单都带 `user_deleted:boolean`；软删除的用户保留用户名和显示名，硬删除只保留 `user.id`（取自工单的 `user_id`），用户名、显示名为空串，详情不返回 `email`。回复不会给已删除用户发送通知。

GMPay 个人下单和订单详情同样session-only；Root设置也用浏览器会话。支付完整字段见[GMPay](../billing/GMPAY.md)。其他管理、价格、日志、任务接口沿用原生路由；历史API描述不保证本地仍提供某端点。
