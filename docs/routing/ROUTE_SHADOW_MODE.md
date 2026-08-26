# 自动 Lab 路由 Shadow Mode

## 目的

本阶段只验证能力索引和自动 Lab 候选，不改变线上实际渠道。实际渠道仍由 legacy selector 选择，旧 Token 的 `Group/AutoGroups`、重试、affinity、计费和退款链路保持不变。

## 开关

```dotenv
ROUTE_SHADOW_ENABLED=false
ROUTE_CAPABILITY_REFRESH_TASK_ENABLED=true
ROUTE_CAPABILITY_REFRESH_INTERVAL_SECONDS=30
ROUTE_CAPABILITY_REFRESH_TIMEOUT_SECONDS=300
ROUTE_SHADOW_EVENT_QUEUE_SIZE=1024
ROUTE_SHADOW_OBSERVATION_QUEUE_SIZE=2048
ROUTE_SCORE_SHADOW_ENABLED=false
ROUTE_SCORE_LIVE_ENABLED=false
TOKEN_PRIVATE_ROUTING_ENABLED=false
ROUTE_LIVE_ENABLED=false
ROUTE_LIVE_USER_IDS=
ROUTE_LIVE_TOKEN_IDS=
ROUTE_LIVE_MODELS=
ROUTE_LIVE_INSTANCES=
ROUTE_DECISION_EVENT_QUEUE_SIZE=1024
```

Shadow 默认关闭。开启后可以用以下变量限制灰度范围，多个值使用英文逗号分隔：

```dotenv
ROUTE_SHADOW_USER_IDS=1001,1002
ROUTE_SHADOW_TOKEN_IDS=2001
ROUTE_SHADOW_MODELS=gpt-5,claude-opus-5
```

所有灰度条件都满足时才记录 Shadow 决策。关闭 Shadow 时不会读取能力索引、查询 legacy 轨迹、写入决策日志或增加 Shadow 计数。

## 能力刷新

- 服务启动时重建所有渠道能力快照。
- 渠道、Ability 或 Advanced Custom 配置变更时触发单渠道刷新。
- `route_capability_refresh` SystemTask 按 fingerprint 扫描遗漏的外部变更。
- 每个渠道用 CAS 发布 active snapshot；数据库保留当前版本和最近两个旧版本。
- 发布和失败标记必须携带读取到的 `active_version`、`source_hash`、`catalog_version`；CAS 竞争失败不插入新能力行或污染 active 快照。
- 刷新失败记录失败 fingerprint 和时间，但不清空旧 active snapshot；旧 worker 不能把新 active snapshot 标记为失败。首次构建失败会留下 failed pointer，供刷新任务重试和诊断。
- 删除渠道后会从内存索引移除，但数据库快照保留用于诊断。

## Shadow 决策

决策只按以下顺序执行硬过滤：

1. 规范化请求模型和 active snapshot。
2. Lab、能力状态和实际模型映射。
3. `Channel.Status`。
4. `Ability.enabled`、有效分组和 Token 模型权限。
5. endpoint/path 能力。
6. 价格资格和系统安全策略。

自动 Shadow 只按 `Channel.priority` 降序，同 priority 按 `channel_id` 升序。`weight` 仅记录，不参与选择；用户 position、health score、capacity 和并发租约不在本阶段生效。`unknown`、映射冲突和不支持的能力只能作为被过滤项，不能成为 Shadow 首选渠道。

价格、额度和请求正文安全检查依赖实际请求期的计费与安全链路，Shadow 与配置 Preview 不会伪造其通过结果。决策会标记 `runtime_recheck_required` 及对应原因；只有调用方提供了明确资格结果时，静态过滤器才会以 `price_forbidden` 或 `security_forbidden` 排除候选。Guarded Live 在真实选择后仍执行完整计费和安全复检。

## 观测

开启后会异步写入单行 JSON 事件 `route_shadow_decision`。事件只包含请求 ID、用户/Token ID、模型、Lab、版本、渠道 ID、过滤原因和差异原因，不包含 Key、Token 原文、Header Override、请求体或 Authorization。队列满时只丢弃观测事件，并增加丢弃计数，不影响请求。

验收指标同时写入主库的小时聚合表。聚合只保留 boot UUID、小时、规范化模型和计数/延迟桶，不保存完整决策、渠道、用户、Token、请求体或凭据。Shadow 开启后，每个实例会为当前小时写入一个零值全局心跳，用于区分空闲小时和缺失证据；实例重启后会封存前序实例的已结束小时。日志队列和聚合队列均为有界异步队列：日志队列积压会降低事件完整率，聚合队列积压、聚合写入失败或晚到数据会将该小时标记为可能缺失。未封存或可能缺失的小时不能作为验收证据。已封存数据保留 14 天，诊断只读取最近 7 天的完整小时窗口，不依赖 Redis 或进程内计数。

动态评分有独立的 `ROUTE_SCORE_SHADOW_ENABLED` 开关。开启后仅在自动 Lab 候选的同一 priority 层内计算 Weight、Error、Latency、TTFT、RateLimit、Quota、CircuitBreaker 和 Sticky 的 breakdown；静态首选和实际 legacy 选择不变。只有显式开启 `ROUTE_SCORE_LIVE_ENABLED` 且满足 Live 灰度条件时，评分结果才会参与新路由选择，两个开关默认均为关闭。

管理员可通过 `/api/status/test` 查看 `route_shadow_metrics`。重点关注：

- Shadow 决策、差异、unknown、mixed 和未授权候选计数。
- snapshot stale、快照版本冲突和事件丢弃计数。
- `metrics` 是当前进程观测；`durable` 是跨实例、跨重启的验收真相。事件完整率按 `event_submitted / (event_attempted - event_encode_failed)` 计算，提交失败和 dropped 均会降低完整率。
- `durable.refresh_lag_p95_ms` 使用 fingerprint 发现到 active snapshot 发布的持久化直方图；无样本时显示为不可用，不能按 0ms 解释。
- 核心模型由最近 7 个完整日的 Relay 聚合选取；`core_model_coverage` 表示核心模型占 Relay 流量的比例，`core_model_shadow_coverage` 表示这些模型的 Shadow 初始决策数与 Relay 请求数之比。
- `core_model_lab_resolution` 按 `resolved / (resolved + unresolved + conflict)` 计算，解析在权限、路径等后续过滤之前记账。未封存、缺失或可能丢失的任何核心模型数据都会使 Shadow 诊断不可用。
- `difference_reasons` 是否都能解释新旧选择差异。

## 决策重放

内部 `ReplayRouteShadowDecision` 只接受已脱敏的 Shadow decision JSON。事件必须包含 request ID、请求模型、请求路径、用户组、legacy 轨迹和 snapshot version；重放只读取事件引用的历史 snapshot，不使用当前 active 内存索引，不执行上游请求，不创建账单，也不修改请求上下文。若快照早于当前能力投影版本或缺少静态渠道状态，重放会显式拒绝，不会以事件字段或实时渠道配置补全。

## 灰度检查

建议按以下顺序执行：

1. 固定 fixture 验证 canonical、alias、mapping、unknown、mixed 和 Advanced Custom path。
2. 单实例开启 Shadow，确认 `/api/status/test` 指标增长且线上渠道不变。
3. 双实例同时刷新同一渠道，确认只有一个 snapshot 版本生效。
4. 注入数据库刷新失败、Ability 禁用、渠道禁用、删除渠道和 catalog 变化。
5. 覆盖流式、异步、多 Key、affinity、重试和无候选请求。
6. 确认事件完整率、刷新延迟、未授权候选和副作用满足阶段退出门槛。

任何异常都可以关闭 `ROUTE_SHADOW_ENABLED`。legacy selector 仍独立运行，不需要回滚数据库快照。

## 受控 Live 入口

`TOKEN_PRIVATE_ROUTING_ENABLED` 和 `ROUTE_LIVE_ENABLED` 必须同时开启，默认均为关闭。开启前必须为每个参与渠道和规范化模型配置启用的 `ChannelRoutePolicy`，否则新路由会 fail-closed，不会读取 `capacity_total/capacity_used` 作为并发上限，也不会静默回退 legacy。

live 路由可以使用 `ROUTE_LIVE_USER_IDS`、`ROUTE_LIVE_TOKEN_IDS`、`ROUTE_LIVE_MODELS` 和 `ROUTE_LIVE_INSTANCES` 缩小灰度范围；每个非空 allowlist 都必须匹配。它们与 Shadow 的 allowlist 完全独立，未配置时不额外限制已显式启用的 live 路由。

当前 Live 只覆盖由统一 Relay 或新任务提交控制器完整管理的请求。Midjourney、`/v1/responses/compact`、需要原生 Responses 压缩能力的请求，以及绑定原任务渠道的 `/v1/videos/:id/remix` 始终使用 legacy selector，不能写入 Live 决策或租约状态。多 Key 中已过冷却期的 Key 通过数据库 CAS 只放行一个 half-open 探针；其他并发请求会继续排除该 Key，直到探针成功关闭或失败重新冷却。

评分中的 RateLimit 和 Quota 仅在运行时 telemetry 可用时参与计算；当前没有该 telemetry 时会明确标记为 unknown 并使用中性分，不得将缺失数据解释为空闲容量或配额。

管理员策略接口：

- `GET /api/channel/:id/route-policy?model=<model>`
- `PUT /api/channel/:id/route-policy?model=<model>`

策略更新使用版本校验；Redis 租约抢槽成功后会再次检查渠道状态、健康 epoch 和能力快照版本。每个上游尝试独立获取和释放租约，流式请求使用 TTL 续租。live route 失败只影响明确开启该开关的请求；默认关闭时 legacy 选择、重试、affinity 和计费保持原行为。
