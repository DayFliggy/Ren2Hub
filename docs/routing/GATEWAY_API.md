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
| `GET /api/token/:id/routing/preview?model=...&path=...&stream=` | 当前配置的只读选择预览，不调用供应商；`stream` 按 `strconv.ParseBool` 解析，非法值 400 `ROUTING_INVALID` |

路由保存 `{mode:"auto"|"manual",version,entries:[{channel_id,enabled}],max_channel_ratio:null|"1.5"}`，手动顺序由 `entries` 的数组顺序决定，请求中的 `position` 不作为输入，保存时按数组下标重写。响应含 Token、模式、版本、带 `position` 的列表及倍率上限。自动模式不保存自选列表；手动空列表保持为空。非法请求400、未授权渠道403、版本冲突409、不可见Token404；数据库错误500，不以空候选假成功。scoped PAT 创建 `type:"manual"` 的 Token 时，PAT 自身也要带 `routing:write`，否则 403 `ROUTING_FORBIDDEN`。删除业务分组，旧组参数明确拒绝：`/api/channel/*`、`POST|PUT /api/token/`、`/api/user/`、`/api/user/self` 和 `*/routing` 的写请求只要请求体非空，不论 Content-Type 都检查，非 JSON 请求体返回 400 `invalid JSON request`。

预览响应回显 `stream`，并给出 `ordering`：手动模式为 `"manual"`，自动模式为 `"weighted_random_per_request"`，表示加权随机按每个请求单独计算，预览只是其中一次抽样。普通用户视图（模型目录与 `routing/channels` 共用 `service.RouteChannel`）只含 `id`、`name`、`type`、`status`、`models`、`supported_endpoints`、`channel_ratio`、`max_concurrency`、`eligible` 和可选的 `filter_reason`；实时 `in_flight` 只出现在管理端渠道列表、搜索、详情和更新响应里。

| 聚合接口 | 合同 |
| --- | --- |
| `GET /api/user/model-catalog?token_id=` | 会话用户或本人 Key 的模型、公开渠道、端点及原生基础价×渠道倍率；不返回地址、代理或凭据 |
| `GET /api/channel/priority/order` | 原生优先级快照与 revision |
| `POST /api/channel/priority/reorder` | NewAPI 版本校验、预览及事务更新；revision 冲突、移动无效或越界时返回 HTTP 200 加 `success:false` |
| `GET /api/channel/labs` | 全平台厂商分类聚合；分类不控制选路 |
| `GET /api/home/metrics` | 消费日志请求计数及 24 个明确时间区间，共享短缓存 |
| `GET /api/data/self/summary` | 本人区间、对比、模型/Token 分布；计数、token、额度和缓存读写来自 `quota_data`，`hourly` 是按小时升序、不缺小时的序列 |
| `GET /api/performance/routing` | Root 读取当前用户/渠道占用与近期真实尝试观察 |
| `GET /api/performance/quality?hours=24` | Root，PAT scope `ops:read`；全站跨模型按小时汇总 `perf_metrics`，`hours` 取 1–720 |
| `GET /api/performance/stats` | 原生字段之外追加 `system_usage`（主机 CPU、内存）和 `db_pool`（`sql.DB.Stats()`） |
| `/api/user/affiliate` | 本人规则、奖励、records、脱敏 invitees 及幂等全额 transfer |
| `/api/admin/affiliate` | 返利记录和个人比例/邀请码管理；全局配置使用原生 options |

渠道 `channel_ratio` 为大于 0、不超过 1000、最多 18 位小数的十进制字符串，用户/渠道 `max_concurrency` 为非负整数，0 表示无限。修改 `channel_ratio` 需要 ChannelSensitiveWrite：只有 ChannelWrite 的管理员提交不同的倍率时返回 HTTP 200、`success:false`，整条请求不生效；提交与当前值等价的写法（如 `"1.0"`）仍然允许。`max_concurrency` 只影响运行，按非敏感字段处理。自动排序与资金接点见[渠道路由](UNIFIED_ROUTING.md)。

`summary` 不含个人以外的数据。`meta.source` 为 `"quota_data"`，`meta.available` 等于 `DataExportEnabled`；为 false 时计数为 0、序列为空数组，表示没有数据来源，不表示用量为 0。耗时和成功率按本人消费日志计算，`meta.coverage.latency`、`meta.coverage.success_rate` 为 false 或区间内没有样本时对应值为 `null`，不拿全站 `perf_metrics` 代替。`quota_data` 每 `meta.flush_interval_minutes` 分钟落库一次，最近的小时可能尚未写入。

`quality` 的窗口与原生 `/api/perf-metrics/summary` 相同，最后一个点是当前未满的小时；窗口内没有请求时 `summary` 为 `null`，没有请求的小时三个比率都为 `null`。`stats.system_usage` 在监控关闭、尚未采样或采样超过 30 秒时为 `null`，不返回 0；`db_pool.log` 为 `null` 且 `log_shared=true` 表示日志库与主库共用连接池。

个人工单 `/api/tickets` 全部要求浏览器会话：GET列表、POST创建、GET `/:id`、POST `/:id/messages`、PATCH `/:id/status`、GET `/attachments/:attachment_id`。创建与回复为multipart，附件字段 `attachments`，最多五张、每张10MiB；详情返回 `{ticket,messages}`，列表返回 `{items,total,page,page_size}`。表单按流式解析，第一次越界就停止读取，返回 HTTP 200、`success:false` 和 `code`：`ATTACHMENT_TOO_MANY`、`ATTACHMENT_TOO_LARGE`、`ATTACHMENT_TYPE_UNSUPPORTED`、`REQUEST_TOO_LARGE`、`VALIDATION_ERROR`。`ATTACHMENT_STORAGE_FAILED` 表示工单或回复已经写库、附件没有存好，客户端应刷新而不是重新提交。附件先写临时文件，事务提交后再改名；下载响应带 `X-Content-Type-Options: nosniff`。

管理员工单前缀 `/api/admin/tickets`：读取列表/详情/summary/附件需要 `ticket:read`，回复需要 `ticket:reply`，agents、status、assignee 需要 `ticket:manage`。指派使用 PATCH `/:id/assignee` 与 `{assignee_id:number|null}`。管理员scope与当前角色权限取交集；个人接口不会因为PAT有ticket scope而开放。

GMPay 个人下单和订单详情同样session-only；Root设置也用浏览器会话。支付完整字段见[GMPay](../billing/GMPAY.md)。其他管理、价格、日志、任务接口沿用原生路由；历史API描述不保证本地仍提供某端点。
