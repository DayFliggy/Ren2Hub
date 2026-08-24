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
- 发布必须携带读取到的 `expected_active_version`；CAS 竞争失败不插入新能力行。
- 刷新失败记录失败 fingerprint 和时间，但不清空旧 active snapshot；旧 worker 不能把新 active snapshot 标记为失败。
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

## 观测

开启后会异步写入单行 JSON 事件 `route_shadow_decision`。事件只包含请求 ID、用户/Token ID、模型、Lab、版本、渠道 ID、过滤原因和差异原因，不包含 Key、Token 原文、Header Override、请求体或 Authorization。队列满时只丢弃观测事件，并增加丢弃计数，不影响请求。

管理员可通过 `/api/status/test` 查看 `route_shadow_metrics`。重点关注：

- Shadow 决策、差异、unknown、mixed 和未授权候选计数。
- snapshot stale、快照版本冲突和事件丢弃计数。
- 事件 attempted、written、encode failure 和 dropped 计数；日志完整率按 `written / (attempted - encode_failure)` 计算。
- 能力刷新成功/失败计数、扫描 P95 和检测到变更到 active 发布 P95。
- 最近 7 天 Relay 日志为分母的核心模型覆盖率和 Lab 解析率。
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
