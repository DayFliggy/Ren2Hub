# 统一路由

更新：2026-09-10。本文描述 `codex/routing-retirement` 当前源码契约和切换边界。B1—B3 是完整切换单元，迁移与隔离数据库矩阵尚未全部验收，中间状态不能发布。生产是否已采用该版本以实际镜像和发布记录为准。

## 路由和权限

`middleware.Distribute` 和协议阶段切换统一调用 `SelectLiveTokenRoute`。自动模式根据模型能力、渠道优先级和健康评分选渠；手动模式使用 Token 的 Profile、活动 Group、Entry 顺序与 Policy。没有 Profile 的已有 Token 使用自动模式；手动 Profile 没有启用的活动组时拒绝请求。

Token 的所有权、启停、到期、额度、IP 和模型限制继续生效。Token 不再存储平台业务分组，用户私有 Profile、活动 Group 和 Entry 是独立的路由配置。账号分类用于充值、订阅和限额解析，不授权模型或决定 Relay 价格。

平台渠道默认可被发现；显式撤销或到期的 UserChannelEntitlement 会阻止使用。能力快照、当前 Channel/Ability 状态、模型限制、协议与 Advanced Custom 路径共同决定候选资格。目录未识别但渠道明确配置的模型归为 custom，不猜测供应商；映射冲突和非法映射会拒绝。

## 并发和计费

- `Channel.capacity_total` 是实际并发容量，同一渠道的所有模型共享一个 Redis 租约池。默认容量为 20。
- `Channel.channel_ratio` 是实际计费乘数，默认 1；UI 的这两个字段是唯一配置来源。
- 每次尝试在预扣和发送前获取租约，复检渠道、能力版本、健康版本、Token、Profile、Entry、使用资格与价格。
- 计价后的倍率或容量变化会阻止发送。Redis 不可用或容量耗尽时不回退、不发送上游、不扣费。
- 重试共享账单，切到更高价格时补充预扣；已交付响应不因之后的租约续期失败而退款。
- 普通、流式和媒体提交都续期并释放租约。媒体任务运行于供应商期间不持续占用提交请求的租约。

请求计费对象与日志使用 `channel_ratio`；任务计费上下文使用明确的 `billing_multiplier` 与 `multiplier_source`。新任务来源为 `channel`，异步结算读取冻结快照，不读取当前渠道价格替代历史事实。表达式计费使用渠道倍率命名，表达式语言、舍入与成本计算保持原语义。Midjourney 继续使用现有钱包账务，不新增订阅计费能力。

异步任务终态与结算意图同事务写入；资金处理使用以任务为唯一身份的持久回执，记录成功、失败退款、待处理及冲突。资金、Token、任务金额和相关计数在主库事务中更新；缓存和日志补偿不会重新执行资金事务。Token 删除不恢复其使用权限。该实现仍需完成多进程、数据库矩阵与故障注入验收。

## 账号分类与限额契约

分类目录包含不可变 `code`、`name`、`default_concurrency`、`default_rpm`、`topup_multiplier` 和 `policy_version`。固定保留 `default`。初始化默认并发 5、RPM 0、充值倍率 1；充值倍率必须为有限正数。

用户使用 `account_class`、`concurrency_override`、`rpm_override`、`policy_version`。覆盖值 `null` 表示继承，`0` 表示不限，正整数表示限制，负数拒绝。并发上限为 1,000,000，RPM 上限为 1,000,000,000。用户和分类分别携带政策版本，更新冲突返回失败。分类及限额更新不批量撤销会话，余额缓存更新保留已领先于数据库的额度。

订阅计划及实例使用 `upgrade_account_class`、`downgrade_account_class`；实例额外保存 `previous_account_class`。恢复仍考虑其他有效订阅、明确降级目标、购买前分类及管理员后来主动修改的分类。分类被用户、订阅计划或订阅快照引用时不能删除。

以上是已实现的数据、管理和限额解析契约。全请求用户并发、分布式 FIFO、用户 RPM 及渠道 RPM/TPM 原子准入尚未完成接入，不能以设置页可编辑或接口返回有效值宣称运行时已强制执行。

## 协议和重试

Chat、Responses、Claude、Gemini、图像、音频、Embedding、Rerank、Realtime、Compact、Midjourney、Suno 和视频均使用同一选渠与准入边界。查询类接口保留自己的任务所有权校验；后续任务操作绑定原任务渠道，并重新检查当前能力和用户资格。

Compact 保留精确模型和基础模型两个阶段。每阶段分别排除已用 Key，能力健康按该阶段实际匹配的能力模型记录，逻辑计费模型保持独立。总尝试次数受系统上限及手动重试策略约束。已输出响应后不得切换上游或拼接第二份响应。

## 接口和运行依赖

- `/api/status.frontend_capabilities` 开放 `api_tokens`、`token_private_routing`。
- `/api/token/*` 返回 `type: auto | manual`，不返回旧分组字段。
- `GET /api/account-classes` 供 Admin 读取分类目录；Root 使用同一路径的 `POST`、`PUT`、`DELETE` 管理目录。更新代码位于 JSON 正文，删除通过查询参数 `code`，分类代码不作为 URL 路径段。
- `/api/next/admin/users` 返回分类、覆盖值、有效并发/RPM 和政策版本。更新要求 `account_class` 与读取到的 `policy_version`，继续使用现有用户管理权限和角色约束。
- `GET /api/user/limits` 返回当前用户的有效并发/RPM、用户政策版本和分类政策版本。
- `/api/routing/*` 管理 Profile、候选目录和静态预览；预览不发送请求、不预扣、不持有租约。
- `/api/channel/` 管理实际容量和渠道倍率；独立 `/route-policy` 接口已移除。
- `/api/pricing` 返回模型基础价格和可用渠道倍率区间，取消旧组过滤和组倍率响应。
- `/api/perf-metrics?model=...` 返回 `model_name`、`series_schema: model-bucket-v2`、汇总指标与 `series`。指标按模型和时间桶聚合，不返回组数组，不接受旧组查询参数；尚未增加渠道归属维度。
- 管理接口对明确退役字段返回 HTTP 400、`code: retired_field`，不静默忽略。模型 SDK 推理入口不采用全局未知字段拒绝。
- Token 管理的列表、检索、读写和批量操作，管理用户 DTO 及账号分类目录均使用同一退役拒绝边界。即使旧字段为 `null`、空数组或 `false`，也不会被当作省略接受；`code`、`name` 等正常字段中的分类值不按字段名称扫描。

不再使用 TOKEN_PRIVATE_ROUTING_ENABLED、ROUTE_LIVE_*、ROUTE_SHADOW_* 或 ROUTE_SCORE_* 灰度开关。能力刷新任务继续使用 ROUTE_CAPABILITY_REFRESH_*；ROUTE_DECISION_EVENT_QUEUE_SIZE 控制有界决策日志队列。

## 数据和验证

启动在自动迁移、后台任务及接流前检查主库和独立日志库的路由结构版本；已有数据库必须完成离线切换。空库可以初始化当前结构。迁移状态版本当前为 `1`，只有 `stage=complete` 才能接流。

离线工具入口为 `cmd/ren2hub-migrate`，提供 `inspect / apply / resume / verify`。主库使用 `--role main`，独立日志库使用 `--role log`；SQLite 可指定 `--sqlite`，网络 DSN 沿用环境配置。`apply` 和 `resume` 要求 `--quiesced`，它表示操作者已经停止全部写入并保留完整副本，工具不会替代停写动作。

迁移记录数据库身份、源结构指纹、工具版本、阶段和游标，以及账务总量、候选摘要、孤儿能力计数和退役索引。已知基线之外的结构、冲突字段、非法 JSON、非法数值以及无法证明等价的参数规则阻断切换。

迁移的历史处理规则：

- 能力按 `(channel_id, canonical_model)` 合并；仅对原先已启用且组名去空白后非空的记录取 OR。优先级、权重取 Channel，不按模型列表重新启用不可达能力。派生快照重新投影。
- 历史任务 `group_ratio` 定点转换为 `billing_multiplier`；缺少来源证据时记录 `historical_snapshot`。完整计费快照存在但旧倍率键省略时保留旧反序列化的显式零；整个快照缺失且仍需结算时阻断。
- 保留未知 JSON 字段，不重算已结算金额，不重复乘历史特殊用户倍率。充值历史零值按旧实际有效行为转换为 1。
- 去掉统计组维度时先合计原始计数和总量，再算均值；旧列、索引、选项及在线 JSON 键只有验证成功后才删除。Redis 不执行全局清空，不删除含未落库额度的 Token 缓存。

ClickHouse 影子表/分区复制与校验当前尚未实现，工具明确拒绝此迁移路径。SQLite 已有人工数据回归；MySQL、PostgreSQL 最低版本及独立日志库故障矩阵仍须按验收报告逐项核验，不能把代码中的方言分支当成通过证据。

物理删除后的回滚只接受迁移前完整数据副本与匹配旧二进制。仅回滚镜像不构成数据恢复。本轮不执行生产数据库升级或恢复。

`router/unified_routing_integration_test.go` 使用隔离 SQLite、Redis 和模拟 HTTP 上游，覆盖 Chat、Compact 阶段切换、Suno、视频、Midjourney、倍率计费、租约释放以及准入失败不发送、不扣费。其他测试覆盖 Profile 版本、资格撤销、Key 健康、表达式计费和退款。主模块与独立 RelayKit 必须分别验证；本地模拟测试不代表真实供应商或生产集群已验收。

逐批实现和验证状态见 [改造进度](../implementation/ROUTING_RETIREMENT_PROGRESS.md)。旧基线测试结果不能证明当前切换版本通过。主模块、独立 RelayKit、Vue 和路由模拟器分别验证；Linux、多实例 Redis、真实数据库和 SDK 检查单列通过、失败或未执行。

全请求三次生成调用预算、安全转换分阶段策略、流式提交缓冲、可补充候选窗口、带 epoch 的半开探针和有状态续接绑定属于 B4—B9 后续实现，本文不将其列为当前能力。生产状态和真实供应商收费语义仍以对应环境证据为准。
