> 历史归档：2026-09-16 单实例改造前的工作树材料。文中 Redis 并发、多实例和旧测试结果只适用于所记版本，不是当前实现的验收证据。现行契约见 [统一路由](../UNIFIED_ROUTING.md)。

# 网关改造迁移与发布准备

本说明对应 2026-09-14 的本地产品提交 `638cfcc46e2890e8fedc9332a95edb257a9114f0`，后端提交为 `02b0a180a73b9318ebf9f4eac38e78d81735df21`，开发基线为 `d1a69b1d5fe30dddbac2dca4a0d42cc8e05ba8b7`。源码尚未发布，镜像身份及生产基线尚未核验，因此这是一份本地准备记录，不是发布或回滚授权。

按工作区[应用更新技能](../../../../skills/update-ren2hub-production/SKILL.md)，本次暂按**不兼容迁移发布**处理。新增结构可被旧程序忽略，但旧程序不会执行新的用户限额、成功次数预留、稳定状态归属及部分输出结算，不能据此证明回退旧镜像正确。发布前必须相对届时已验证的生产 SHA 重新检查本清单。

## 持久化变更

| 数据 | 本次变化 | 升级与回退边界 |
|---|---|---|
| `tokens` | 新增 `conversion_policy`，默认 `safe` | 仅空值补为 `safe`；保留管理员设置的 `allow/strict`。旧程序忽略策略，不能作为等价回退 |
| `users` | 新增可空 `gateway_concurrency`、`gateway_wait_ms`、`gateway_queue_size` | `null` 继承，`0` 关闭相应限制；既有用户默认通过系统配置继承 10 / 5000 ms / 20 |
| `options` | 新增 `gateway_runtime_v2` 与迁移时间记录 | 首次插入默认值；后续启动不覆盖管理员配置。CAS 版本用于配置更新，不用于清空在途请求 |
| `channels` | 新增 `gateway_identity_version` | 人工凭据或端点身份变化使旧绑定失效；Key 调序和可信 OAuth 刷新保留相应身份 |
| `channel_healths` | 新增探测 owner、到期时间，复用 health epoch | 不清空旧健康数据；旧结果不能清除新冷却。历史 `open` 且无有效冷却按恢复规则处理 |
| `gateway_credentials` | 新表，稳定 UUID、渠道及凭据摘要 | 不保存明文 Key；不以数组下标作为新状态身份 |
| `gateway_state_bindings` | 新表，用户、Token、状态摘要、渠道、凭据、端点/传输身份、有效期 | 默认 30 天，保留更短供应商期限；不保存完整会话。升级前未知归属或缺少传输身份的续轮要求新会话 |
| `billing_recoveries` | 新增消费快照、日志/统计执行状态与错误 | 快照不含提示词或密钥；结算、统计、日志分别幂等恢复。已存在历史账单不改写 |
| `tasks.private_data` | 既有 JSON 增加凭据、端点及计费元数据 | 原任务表继续承担所有权；不另建会话或任务真相来源。历史兼容仅限原代码已保存的身份，不能补造未知绑定 |
| `midjourneys.private_data` | 新增 JSON 字段，使用相同稳定凭据与账务元数据 | 旧无绑定任务可保留查看，但后台或续作不能猜测 Key；新图片链接采用独立签名域 |
| 能力快照 | 宿主合同 v11 改变能力指纹并重建投影 | 相同内容发布幂等，首次并发发布有 CAS；不改写价格快照。不可执行路径可能从旧预览中消失 |

`ChannelInfo`、任务 `Properties/PrivateData` 写 SQL 时返回 JSON 字符串，读取支持驱动返回的 string、bytes 和 NULL；这解决 PostgreSQL simple protocol 的 bytea 类型问题，不改变已有 JSON 的业务含义。模型迁移入口为 `model/main.go` 和 `model/gateway_config.go`，迁移失败必须阻止该实例开始服务。

## Redis 与请求排空

渠道和用户租约使用 `route:lease:v2`，供应商速率状态使用 `gateway:provider:v1`。用户逻辑请求和渠道尝试使用不同占用身份，TTL 为 120 秒，30 秒续期；热更新、启动和恢复都不清理其他实例的租约。Redis 不可用返回明确的准入错误，不以内存放行。

新旧准入版本不得同时接收流量。发布前停止新请求并等待用户、渠道、排队及 Realtime 占用结束；WebSocket 是升级后的连接，不能仅凭 `http.Server.Shutdown` 返回推断已排空。排空期间持续观察恢复积压；不要用清空 Redis 代替排空，也不要清除仍可能存在的任务或账务恢复记录。

当前 HTTP 优雅关闭默认 120 秒，生产 Compose 必须保留 `SIGTERM` 及大于该值的宽限期。仍有后台任务时，新版本继续从既有任务表恢复；任务是否可查询由保存的稳定身份决定，不切换任意 Key。

## 本地证据

已执行的具体日志、命令及 SDK 锁版本见[验证说明](../GATEWAY_VALIDATION.md)和[支持矩阵](../GATEWAY_SUPPORT_MATRIX.md)。本轮隔离数据库为 SQLite、MySQL 5.7.19、PostgreSQL 16；未将 PostgreSQL 16 的结果称为生产 PostgreSQL 15 实测。

| 检查 | 已有证据 | 不能由此推导 |
|---|---|---|
| 升级与重复启动 | `TestGatewayStartupMigrations` 在三库创建旧字段夹具、执行完整迁移两次并关闭重连；验证历史金额、默认策略和管理员覆盖 | 完整生产数据快照升级或旧镜像回退 |
| 账务恢复 | 三库 36 场景、任务终态 24 场景、Midjourney 33 场景；覆盖事务失败、并发、重复结算、日志和统计恢复 | 真实供应商幂等查询或接受状态不明时可重放 |
| 能力发布 | 三库首次并发和相同内容重复发布 | 尚未构建的发布镜像身份 |
| 多实例 | 同 MySQL/Redis 两进程准入及两真实应用；取消、进程退出、配置热更新和 Redis 故障 | 新旧准入版本可以共同接流 |
| 本地构建 | 产品提交后的 Windows 二进制与 Linux amd64/CGO=0/GOWORK=off 编译，最终 Windows 二进制通过双应用验收 | Docker 冷构建、Linux 运行结果、OCI revision 或发布镜像身份 |

## 最终发布步骤

1. 核对[实施记录](../GATEWAY_IMPROVEMENT_STATUS.md)中的本地提交、F01–F13 回归及[支持矩阵](../GATEWAY_SUPPORT_MATRIX.md)，记录发布所用源码 SHA 和根部署资产 SHA。按[同步流程](../../../../docs/workflows/SYNC_WORKFLOW.md)检查子模块提交可获取性，保留其他任务的未提交修改；当前本地提交尚未证明可从发布远端取得。
2. 在最终提交的干净隔离检出中重做发布构建和本清单差异检查。继承现有不可变 SHA 镜像方式，验证 OCI revision、版本及实际镜像身份；本轮尚未执行 Docker 冷构建。
3. 生产操作获得对应授权后，按[容器替换手册](../../../../skills/update-ren2hub-production/references/container-replacement-runbook.md)执行预检、可读数据库备份、排空和仅替换应用容器；不重建 PostgreSQL/Redis。
4. 验证状态、Vue 入口和深链接、哈希资源、登录、JSON/SSE 中继、准入及恢复积压。真实供应商验证与本地模拟 SDK 分开记录；付费调用需单独授权。
5. 全部上线验证完成后提升成功版本记录。失败时保留证据，按已批准的维护/恢复方案处理；未证明兼容时禁止自动只回退镜像。数据库恢复是独立授权操作。

本地二进制、健康检查或已通过的单元测试均不能替代最终提交、不可变镜像、备份及上线验收证据。
