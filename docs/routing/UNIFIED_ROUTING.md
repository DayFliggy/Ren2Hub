# 统一路由

更新：2026-09-18。本文描述单实例网关的源码契约。只允许一个网关进程承接业务流量；新旧版本必须顺序排空切换。实现与验证状态见 [验收记录](SINGLE_INSTANCE_ACCEPTANCE.md)；最新源码修复与当前运行容器分别记录。

## 路由和权限

`middleware.Distribute` 和协议阶段切换统一调用 `SelectLiveTokenRoute`。自动模式根据模型能力、渠道优先级和健康评分选渠；手动模式使用 Token 的 Profile、活动 Group、Entry 顺序与 Policy。没有 Profile 的已有 Token 使用自动模式；手动 Profile 没有启用的活动组时拒绝请求。

Token 的所有权、启停、到期、额度、IP 和模型限制继续生效。旧 `Group`、`AutoGroups`、`CrossGroupRetry` 字段、业务组权限、组间价格和旧随机选择器退出运行路径。账号分类仍可用于充值、订阅和账号限流，但不授权模型或决定 Relay 价格。

平台渠道默认可被发现；显式撤销或到期的 UserChannelEntitlement 会阻止使用。能力快照、当前 Channel/Ability 状态、模型限制、协议与 Advanced Custom 路径共同决定候选资格。目录未识别但渠道明确配置的模型归为 custom，不猜测供应商；映射冲突和非法映射会拒绝。

## 本地准入与资源生命周期

鉴权、操作分类和逻辑频率限制之后，先取得用户 Permit，再选渠取得渠道 Permit，复核资格与转换、健康和 Provider 额度，最后进行发送前账务及真实发送。用户池以用户 ID 标识，覆盖整个逻辑请求，包括所有 Token、协议、重试和转换降级。渠道池以渠道 ID 标识，跨路由分组共享，覆盖一次上游尝试。

用户和渠道共用 `service/local_admission.go` 的稳定 FIFO manager。新请求不能绕过旧 waiter；释放直接授槽，不轮询 Redis。容量和队列配置原位更新：扩容唤醒、缩容等待已有工作完成；队列缩小不驱逐既有 waiter。用户容量 0 为无限且仍计数；渠道原始容量 0 保留解析为默认 20 的规则。总等待上限仅登记队列人数，不创建第三条队列。

状态使用短临界区统一保护资源、总队列及回收，锁内不做外部 I/O。Permit 通过一次性释放保护所有权，活动和等待均为零时回收；快照不创建池。配置提交后先一次性标记全部受影响池失效，再进行策略刷新或缓存 I/O，revision 防止迟到结果覆盖。失效期间停止授槽；刷新失败拒绝等待者。容量由 manager 按当前策略授槽，扩容唤醒后不要求容量等于入队前的值；状态、价格、能力版本和健康 epoch 仍须复核。默认值与继承见 [管理 API](GATEWAY_API.md)。

用户队列与一次渠道队列共享 `GatewayWaitBudget`。预算只扣等待和队列策略读取时间，上游使用逻辑请求 context。实际入队的策略读取与等待共用剩余预算子 context；路由扫描可刷新 Provider 与渠道队列配置，但不能延长原请求的等待预算。等待、取消和授槽在同一同步边界处理。取消先停止上游，再由生命周期拥有者释放；SSE/Realtime 持有到实际结束。视频 Remix、Suno 续作及 Midjourney 提交即使绑定原渠道也参与用户准入；查询、通知和未实现入口不占生成槽。后台任务不持续持有原 HTTP 槽。

渠道尝试清理完健康探测和 Provider 预留后释放 Permit，再执行 retry backoff。内部重试共用同一计费会话；更高价格补充预扣，输出已经交付时不能因后续维护错误退款。用户/渠道本地占用不包含 Redis key、TTL 或续期；健康探测和 Provider/逻辑频率预留继续使用各自既有一致性机制。

## 候选排序、排队和故障转移

请求正文的语义需求在选渠前分析并保存在当前请求内，文本、Compact、JSON 媒体／Embedding／Rerank／legacy 及 multipart 音频／图像复用已有解析边界。`SelectLiveTokenRoute` 先按当前能力和权限筛选，再执行双向转换规划，最后排序并截断为最多 64 个合格渠道。已确定不兼容的渠道不占调度上限。该计划不取得槽位、不初始化凭据、不下载媒体、不预扣；不能替代发送前复核。

手动 `none` 与 `same_channel` 从首次真实发送开始生效。此前可以在活动组启用的 Entry 内跳过不兼容、超价或无法准入的候选；此后 `none` 不再发送，`same_channel` 只重试实际首次发送的渠道。保留 position、总发送预算、共享重试储备及最多一次渠道排队。排队后还会核对当前 Token 转换策略；策略变化以 `conversion_policy_changed` 拒绝旧计划。Token 缓存保存转换策略，旧缓存缺失该字段时从数据库读取。

权限、Token 模型权限、状态绑定、原任务渠道、模型能力、转换策略与价格资格始终先于负载优化。自动动态排序保持 `priority → 10% 负载桶 → 软亲和 → RequestID 稳定加权顺序`；关闭动态排序时保持静态顺序。手动路由 position 是硬边界；负载均衡仅在首个合格 position 层加权选择，不跨层重排。

先立即尝试允许候选，全部繁忙后最多进入一个渠道队列。扫描期间变为空闲的候选可重新立即获取；池已回收且快照容量为零不代表不可用，最终仍由 manager 授槽。排队只比较仍合格、未冷却、队列未满的繁忙候选：自动动态模式在相同 priority 和负载桶内、手动负载均衡在相同 position 内按 `queued / effective_capacity` 较小者优先，平局保留原顺序。静态和非负载均衡手动模式保留原顺序。

已知冷却或已确认没有 Provider 发送额度的渠道提前跳过；未知状态不伪装成充足，发送前原子预留仍是最终判定。拿槽后再次复核当前资格、健康与额度，不合格则释放并继续已有候选扫描；每次发送使用本次健康 epoch，旧观察结果不能清除新冷却。排队后和发送前重新读取数据库中的渠道端点、启用状态与所选凭据；Key 或端点身份改变时拒绝旧请求，不在队列中局部替换 Authorization。

文本、Midjourney 和视频的发送前拒绝使用同一处理：可扫描独立备用渠道，候选用尽后仍可使用此前繁忙渠道尚未使用的唯一排队机会，末候选超价或不能转换也不能丢掉该机会；已排过队或绑定原任务的请求继续遵守各自边界。最多扫描 64 个不同渠道，最多 3 次真实发送；扫描、满载及发送前拒绝不消耗发送次数，也不处罚供应商健康。

方案 B 的明确边界：排到 A 后，B 随后空闲不会自动迁移该 waiter。本轮不做多渠道匹配、自适应容量、EWMA、P2C、恢复慢启动或独立 WebSocket ingress 限制。

路由决策日志在逻辑请求结束时记录最后一次真实进入上游发送边界的渠道和重试序号。后续候选资格筛选、发送前额度拒绝或重试预算耗尽可能清空当前候选，但不会覆盖这份最终发送证据；没有真实发送时渠道字段保持缺省。这样日志中的 `selected_channel_id` 与实际上游发送及账务检查保持一致。

Retry-After 只作用于所属渠道或已有渠道内 credential/模型健康范围。切换独立渠道只使用本地指数 full jitter；再次访问受限范围必须满足完整冷却下界，不能截短后发送。健康 epoch、探测所有权和受限探测保持生效。已输出、接受副作用或接受状态未知时禁止不安全重放。

进程共享重试额度使用固定百分之一精度，初始/最大储备默认 20 次；每次首次真实发送补 0.1 次，额外发送消耗 1 次。发送前可撤销预留，进入发送边界后提交；结果未知不退款，重试本身不补额度。预留计入 burst，热更新不补满，禁用期间不积累。额度不足立即停止重试、保留最后真实上游错误，并记录 `retry_budget_exhausted`。

新计费对象与日志使用 `channel_ratio`。历史 TaskBillingContext 的 `group_ratio` JSON 字段保留作为已接受的价格快照，异步结算不会重新读取分组配置。Midjourney 继续使用已有钱包账务和退款记录，不新增订阅计费能力。

## 协议和重试

生成终态由 RelayKit 的 `ResponseTermination` 表达，与 `StreamOutcome` 的传输结果分开。正常限长／内容过滤保留内容及用量，作为受限结束，不重试、不处罚健康；供应商暂停在原生协议保留，跨协议无法表达时返回 `response_conversion_unrepresentable`。未知 incomplete 不猜测 completed 或可重试。转换错误归为 `local_convert`，禁止替代请求；已提交输出只按目标协议报错，不追加另一份成功响应。无终态 EOF 仍为失败。

Responses SSE 缓冲到 JSON 复用完整 SSE 事件解码器，支持多行 data、CRLF/LF/CR、BOM、注释、事件名及 EOF 最后完整事件，遵守整事件大小上限，不向 JSON 客户端写心跳。已观测用量继续传入既有账务流程，终态、引用与诊断不写入账务用量字段。

Chat、Responses、Claude、Gemini、图像、音频、Embedding、Rerank、Realtime、Compact、Midjourney、Suno 和视频均使用同一选渠与准入边界。查询类接口保留自己的任务所有权校验；后续任务操作绑定原任务渠道，并重新检查当前能力和用户资格。

Compact 保留精确模型和基础模型两个阶段。每阶段分别排除已用 Key，能力健康按该阶段实际匹配的能力模型记录，逻辑计费模型保持独立。总尝试次数受系统上限及手动重试策略约束。已输出响应后不得切换上游或拼接第二份响应。

## 阶段期限与账务保护

`RELAY_RESPONSE_HEADER_TIMEOUT`、`RELAY_NON_STREAM_TIMEOUT`、`STREAMING_FIRST_OUTPUT_TIMEOUT` 分别限制响应头、非流式完成和首个有效输出，单位为秒，默认 300；非正数或溢出值使用有限默认值。普通 HTTP、代理、AWS 和受保护下载使用对应期限。流式入口收到 JSON 或错误响应时仍受非流式期限约束；心跳和元数据不能延长首输出期限。

`STREAMING_TIMEOUT` 限制有效输出之后的上游空闲时间。`RELAY_TIMEOUT` 的正值设置额外 HTTP 总期限，默认 0 不取消阶段期限。正常持续输出的长流不受非流式期限截断。退出时关闭上游并释放尝试资源。

SSE 按事件空行组装多行 `data:`，支持 CRLF/LF/CR、注释、事件名和 BOM，整事件受大小上限约束；NDJSON 使用独立解码路径。正常 EOF 可处理最后一个未以空行结束的完整事件，读取失败或取消不补交未完成事件。已验证的大图片帧可越过前导缓冲限制；畸形帧或状态绑定失败不得暴露前导内容。

所有计价模式在发送前持久化消费保护，最终证据不足时保留预扣，不凭缺少记录推定可退款。实际用量使表达式计算失败时，保存 `reconciliation_required`、冻结价格与规范化用量，不生成成功消费日志；不保存正文、请求头或原始表达式错误。修正价格配置不会自动解锁旧记录，须按该记录明确对账。待确认或待对账记录计入积压但不占自动恢复批次；任务接受不明与前置上传的退款边界见 [账务状态与故障边界](GATEWAY_REVIEW_FIXES_2026_09_18.md#账务状态与故障边界)。

## 接口和运行依赖

- `/api/status.frontend_capabilities` 开放 `api_tokens`、`token_private_routing`。
- `/api/token/*` 返回 `type: auto | manual`，不返回旧分组字段。
- `/api/routing/*` 管理 Profile、候选目录和静态预览；预览不发送请求、不预扣、不持有 Permit。
- `POST /api/routing/tokens/:id/preview` 覆盖自动、手动及无 Profile 的旧 Token，旧 Profile 预览委托同一入口。预览复用实际静态资格及排序，输出快照标识、时间、配置版本和动态复核提示；加权顺序不是下一次请求承诺。
- `/api/channel/` 管理实际容量和渠道倍率；独立 `/route-policy` 接口已移除。
- `/api/pricing` 返回模型基础价格和可用渠道倍率区间，取消旧组过滤和组倍率响应。

不再使用 TOKEN_PRIVATE_ROUTING_ENABLED、ROUTE_LIVE_*、ROUTE_SHADOW_* 或 ROUTE_SCORE_* 灰度开关。能力刷新任务继续使用 ROUTE_CAPABILITY_REFRESH_*；ROUTE_DECISION_EVENT_QUEUE_SIZE 控制有界决策日志队列。

`route_decision.attempts` 增加 `termination` 和 `response_diagnostics`（仅稳定 code/path）。网关指标增加 `route_decision_events_dropped_total`。这些诊断不记录请求正文、引用文本、凭据或完整上游错误。自动路由运营页面的分数仅用于运营比较，实际选择继续遵守后端排序。

## 数据和验证

代码清理不自动删除历史数据库列、账单、任务或审计数据。旧组配置即使仍在数据库中也不会重新进入请求权限和定价。该版本改变权限与计费语义，发布前需备份并按部署技能评估回滚，不能仅凭 AutoMigrate 成功认定可只回滚镜像。

`router/unified_routing_integration_test.go` 使用隔离 SQLite、Redis 和模拟 HTTP 上游，覆盖 Chat、Compact 阶段切换、Suno、视频、Midjourney、倍率计费、Permit 释放以及准入失败不发送、不扣费。其他测试覆盖 Profile 版本、资格撤销、Key 健康、表达式计费和退款。主模块与独立 RelayKit 必须分别验证；本地模拟测试不代表真实供应商或生产集群已验收。

工作区 `reference-projects/` 提供三个参考项目的固定源码；已吸收机制和未采用方案见 [参考设计取舍](GATEWAY_REVIEW_FIXES_2026_09_18.md#三个参考项目的吸收与取舍)。旧 Redis 用户并发和配置变化重建活动池不属于当前方案。
