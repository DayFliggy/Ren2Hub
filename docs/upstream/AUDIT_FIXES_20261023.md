# 2026-10-23 全量审计修复与发布

本批修复从 `a0a1f8dd1b7909089aed0339bd714406a5ccb66b` 整合。实施 checkout 为 `/home/work/ren2hub-audit-merged-20261023`；逐项检查和原始日志位于 `/home/work/ren2hub-audit-repair-evidence-20261023/`。本文件描述实际修复和验证；源码、发布与运行身份由根工作区服务器档案记录。

## 来源和处置

| 来源 | 固定版本 | 本批用途 |
| --- | --- | --- |
| NewAPI，唯一上游 | `206e0fa5c600127a314a97f88592c067f784f4a6` | 保留原生钱包、预扣、权限和协议；恢复运行时额度单位、认证状态和邀请入口合同 |
| Sub2API，定点参考 | `a34f373973bf547ccec301c7405eb45194e1f28a` | 借鉴来源可追踪的返利账本、操作幂等、OAuth 邀请归属；没有复制另一套钱包 |
| AxonHub，定点参考 | `cbdf9a6d4a3dab1e4bfe5168ce5d80205c805913` | 核对替代机制；没有直接可移植的同构修复 |

Sub2API 的管理员扣减没有对称冲销。本地按本次明确批准增加可追溯管理员返利的净额冲销、已转出负债和后续抵债，不宣称这是 Sub2API 原有行为。上游共同缺陷仍按[同步账本](SYNC_LEDGER.md)保留，不因为本批检查通过而标成已修复。

## 实现结果

| 范围 | 唯一维护点 | 行为和兼容边界 |
| --- | --- | --- |
| 额度换算 | `frontend/src/utils/quota.ts`、`stores/app.ts` | `/api/status.quota_per_unit` 驱动展示和提交，支持正有限小数；用 BigInt 有理数运算拒绝非整数额度及溢出。令牌只改名称时保留原始预算，避免显示舍入改变数值 |
| 返利门控和账本 | `model/affiliate.go`、`affiliate_query.go` | 合规未确认不发放返利，记录 `compliance_unconfirmed`；转入在业务边界拒绝；新返利先抵债，再增加可转/冻结额度 |
| 管理员额度幂等 | `model/user_quota_adjustment.go`、`controller/user_quota.go` | add/subtract/override 都记录操作；同操作 ID 同参数返回原 before/after，不重复额度、账本或审计；变更参数拒绝；覆盖按实际差额处理 |
| 邀请归属 | `SignUpView.vue`、`model/affiliate.go`、`controller/setup.go`、`model/main.go` | 注册入口保存邀请归属并交给现有 OAuth flow；新 root 初始化邀请码，实际 affiliate API 对旧空邀请码安全回填 |
| 返利页面 | `useInvite.ts`、`InviteRecordsCard.vue`、`InviteRewardCard.vue` | 显示冲销、实际入账、抵债和待抵扣金额；服务端 `transfer_enabled` 与余额分离，未合规时禁用转入；内部 allocation 不对外展示 |
| 模型目录 | `model/catalog_prices.go`、`pkg/billingexpr/reference.go`、`ModelPriceDisplay.vue` | 保留精确十进制价格，展示缓存、图像、音频和表达式支持的维度；报价不可静态确定时返回稳定原因，未知内部原因不直接展示成错误码或零价 |
| 工单附件 | `controller/tickets_attachment.go` 及创建/回复入口 | 先暂存、提升文件，再进入数据库事务；失败删除临时和已提升文件，成功才保留；按数据库约束校验文件名最多 255 个字符 |
| 控制台 JSON | `setting/console_setting/config.go`、`model/option.go`、`useSystemSettings.ts` | 四项默认 `[]`；旧空串/纯空白读取时规范化，非空非法 JSON 仍明确报错，不静默掩盖损坏数据 |
| TrustQuotaUSD | `setting/retired_business_options.go`、`quota_setting.go` | 拒绝新写入，忽略旧持久化配置并从有效设置导出中移除；保持强制预扣，不恢复隐式信任额度 |
| 渠道状态提交 | `model/channel_status.go`、`controller/channel.go` | 区分数据库 Changed 与 PublicationPending；发布失败仍刷新缓存、关闭现有连接、记录真实审计，原有自动启停通知沿用 committed 布尔值 |
| 用户更新收尾 | `controller/user_publication.go` | 提交后先记录审计，所有收尾动作都尝试；缓存/并发发布失败返回 success=true 和 publication_pending，前端非阻断提示 |
| Dashboard | `service/dashboard_summary.go`、`model/dashboard_summary.go`、`useDashboard.ts`、`useUsageDistribution.ts` | 额度 DTO 使用十进制字符串；跨窗口重试按请求归属计成功；导出关闭时保留账户 KPI，依赖导出的图表显示未启用；份额只在展示层舍入 |
| 日志 | `LogsView.vue`、`api/logExport.ts`、`httpTransport.ts` | 筛选失败清空旧结果；增量解析 NDJSON 和序列化导出，验证成功终态/记录数量，64 MiB 输入与输出、10 万行限制，超限取消下载；不改服务器无界导出合同 |
| 认证与界面 | `oauth/linuxdo.go`、`SignUpView.vue`、`accountSecurity.ts` 等 | Linux DO 使用公开 ServerAddress 的 `/oauth/linuxdo` 回调；按注册状态展示密码/OAuth；保留 2FA 锁定；移动端兑换码启停、充值错误态、用户表单顺序和焦点修复 |
| 消息 | token routing、redemption、affiliate 错误入口和 zh-CN catalog | 可预期业务错误带稳定 message_key；补充中英文展示、工单 Model/Request 标签及缺失验证文案；保留旧 relay 键名兼容上游，但不再显示已退役的分组/升级账户提示 |

本批把提交后用户逻辑、渠道提交结果和兑换码安全排序分别收拢到 `controller/user_publication.go`、`model/channel_status.go`、`model/redemption_sort.go`。原入口调用这些扩展，不另建业务实现；来源清单的 adapted 理由和文件哈希同步更新。

## 数据迁移和事务

- 扩展现有 `affiliate_records` 追加账本，不新增通用钱包账本。新增 `related_record_id`、`reversible`、`credited_quota`、`debt_offset_quota`、`debt_added_quota`；关联冲销、转出 allocation 和原始来源，保留费率快照。
- `users.aff_offset_debt_quota` 默认 0；资料更新不覆盖该财务字段。`admin_quota_operations.mode` 默认空串，旧操作按原有 add 语义重放。既有唯一键继续约束来源和操作 ID。
- AutoMigrate 只增加列，不推算历史账目。旧记录 `reversible=false`；迁移前可转、冻结、累计额度不变，旧转出也不会误计为新返利转出。只追溯迁移后明确记录的管理员返利。
- 扣减按受邀用户的新管理员返利 FIFO 冲销，部分冲销采用累计比例以避免重复舍入误差。可用/冻结部分直接扣除；已转出部分形成负债，后续返利先抵债。超过可归属返利的扣减不额外制造负债。
- 覆盖按用户额度的实际差额处理。锁定参与用户并在同一数据库事务写用户余额、返利汇总、管理员幂等记录及追加账本；失败全部回滚。MySQL REPEATABLE READ 下账本聚合使用锁定当前读，避免等待钱包锁后读到旧快照。
- 历史累计奖励继续表示累计产生奖励；净调整由冲销记录、当前可转/冻结余额和负债分别表达。内部 `transfer_allocation` 不包含在公开记录列表中。

## 提交后错误语义

`publication_pending` 只用于有现成恢复路径的缓存或并发发布：准入重试 pending limit，用户认证版本 fence 和缓存回读从数据库重新发布。渠道布尔结果继续表示数据库是否已更改，批量接口保留 count，并额外返回 pending count。

权限策略重载和会话清理没有可靠的通用重试队列，因此失败仍显式返回错误，同时附 `committed:true`，并继续尝试其余收尾动作。这类错误不伪装成“只等重试即可”。已更新的认证版本仍使旧会话失效；注入失败测试同时覆盖这一安全边界。

## 验证记录

逐项命令和原始输出位于 `/home/work/ren2hub-audit-repair-evidence-20261023/`。外部数据库仅使用本次新建的本地隔离 MySQL / PostgreSQL 容器，不连接生产库。

- `affiliate-matrix.log`：SQLite / MySQL / PostgreSQL 实际迁移、旧账保留、三模式幂等、同 ID 并发去重、不同 ID 并发冲销、已转出部分重复冲销和未来返利抵债，三库均执行，无 skip。
- `settings-matrix.log`、`metrics-matrix.log`：三库预扣配置与退役选项、跨窗口日志统计均通过。初次 PostgreSQL DSN 格式及复用测试库的隔离保护失败已修正测试启动参数，未更改产品逻辑以绕过保护。
- 工单回归注入文件 rename、数据库提交失败及过长文件名；渠道回归在内存缓存开/关两种模式下注入发布失败，验证提交结果、缓存、连接关闭和审计。用户收尾注入并发、认证缓存、策略及会话失败。
- 浏览器原始记录在 `visual/README.md`：8 类页面 × 桌面/移动 × 浅/深，32 个唯一用例通过；本轮抽查截图，不以截图张数代替逐页视觉结论。使用 native API fixtures，不能代替真实 OAuth 供应商或支付网关联调。已实际操作空 JSON 保存、移动兑换码启停及返利禁转；原夹具失配失败日志保留。
- 前端完整 Vitest 为 250 个文件、1660 项通过；前端类型、lint、生产构建、Go 构建、受影响包 vet、模型/service/controller 定向测试、Linux 附件清理测试、三库返利账本/系统任务矩阵和轮询 race 均通过。Go 单测每次最多 60 秒，controller 按用例集合分批；MySQL 权限不足导致的既有 controller database-matrix 用例未验证，未计为通过。

## 保留项

Realtime 累计重复扣费、Waffo 非终态误判、Uptime Kuma 缺失心跳按零统计、服务端无界日志导出均保留为 NewAPI 共同债务。只有上游修复或用户另行批准本地偏离才重开，证据版本与触发条件见同步账本。

本批没有生产数据迁移、真实供应商 OAuth/支付联调或正式环境负载测试。正式部署结果及限制只在下方实际执行后填写；模拟服务与浏览器 fixtures 不等于真实支付或 OAuth 联调。

## 最终验收

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| 前端完整单测 | 250 文件、1660 项通过 | `tests/frontend-full-repair-final.log` |
| 前端类型、ESLint、生产构建 | 通过；现有图表 chunk 有非阻断体积提示 | `tests/frontend-typecheck-repair-final.log`、`tests/frontend-lint-repair-final.log`、`tests/frontend-build-embedded-final.log` |
| 后端定向测试、go vet、build | 轮询、返利、OAuth、附件清理、目录报价和设置合同通过；后端 build/vet 通过 | `tests/*-final.log`、`tests/go-vet-final.log`、`tests/go-build-embedded-final.log` |
| 数据库矩阵与 race | 返利账本及系统任务 SQLite/MySQL/PostgreSQL 矩阵通过；模型轮询 race 通过。其他 controller MySQL 数据矩阵因测试账号权限不足未验证 | `tests/affiliate-ledger-matrix-final.status.json`、`tests/system-task-db-matrix-final.status.json`、`tests/routing-race-final.log`、`tests/controller-targeted-final.log` |
| 隔离启动 | 最终二进制 SQLite 启动成功；status、注册页和 18 个嵌入资产返回 HTTP 200 | `runtime/smoke-final.json` |
| 浏览器专项 | 32/32 通过；桌面/移动、明/暗主题，使用受控业务 API fixtures | `tests/visual-final.log`、`visual/screenshots/` |

Go embed 验收使用本次前端生产产物。构建后恢复了版本控制的 embed index；最终镜像将从已发布提交重新构建并验证，不复用本地二进制作为部署镜像。

Go embed 验收使用本次前端生产产物；部署镜像从实际发布 SHA 重新构建。正式更新身份和验收结果由工作区服务器档案记录。


## 复核收尾

多 Key 轮询游标按渠道锁保护，并发缓存重建场景纳入 race 回归；邀请转出先处理相同操作 ID 的已完成重放，再检查新操作合规状态。残留或失效的邀请码可由用户确认、修改或清除，不再阻断 OAuth 首次注册。附件暂存目录按保留时长回收孤儿文件，支持关闭自动删除。返利累计上限按历史累计奖励计算，冲正不重新开放上限，避免重复充值/冲正绕过生命周期限制。渠道状态更新以数据库提交结果表达 `changed`，缓存/并发发布失败由独立待重试信息表示；这与冻结 NewAPI `206e0fa5` 的返回语义不同。
