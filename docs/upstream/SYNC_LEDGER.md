# 上游同步账本

本文只记录现行状态：同步到哪里、本地保留了哪些差异、哪些已知问题没有修、最近一次发布。各批次的经过和历次发布从 Git 读取，改写前的完整流水是 `git show 66c29535f:docs/upstream/SYNC_LEDGER.md`。`NATIVE_B388_REVIEW.json` 里的 `SYNC_LEDGER.md:177` 和 `658–666` 是 `2382fbdd5` 版本的行号，对应下文的六入口批量渠道写合同和原生计数。

## 上游基线与同步范围

NewAPI（`QuantumNous/new-api`）是唯一上游。checkout 的 `origin` 是内部 bare 仓库，不是上游。Sub2API 和 AxonHub 只按具体问题参考，不建远端，不追版本。

冻结同步目标是 `b388140acfd80369ec6243b16869daff1b4051f4`（固定 main，`v1.0.0-rc.51` 之后第 2 个提交）。之后的上游提交没有评估，留给下一批。

2026-10-17 以 `09109ec31` 重建核心，此后的窗口都已逐提交处置，终点依次是 `206e0fa5c`、`34003e9c2`、`b08ee86fe`、`e449cc700`、`ba5f73677`、`48a6c87f9`（`v1.0.0-rc.49`）、`b20e0566b`、`b388140ac`。最近一个窗口（`b20e0566b → b388140ac`，4 个提交、10 个文件）的逐文件处置在 `WINDOW_B388.json`；更早窗口的处置记录在 Git 历史。上游 `web/` 的改动按[前端对照表](../../frontend/UPSTREAM_MAPPING.md)找 Vue 落点。

同步时不吸收：

- React、Electron 及其专属依赖。
- 站内订阅与套餐、业务分组及分组倍率、多节点管理、旧 Codex/type 57 专用接入、io.net、Playground。
- 模型默认价格和内置计费表达式的增删。
- 上游 `payment_setting.amount_discount`。
- 上游 #8247：本地已在 `InsertWithTx` 的同一事务里写入 OAuth 邀请人。

来源清单 `CORE_SOURCE_DELTA.json` 按文件登记状态、哈希、合同理由和审查依据，现为 881 `upstream_equal`、281 `adapted`、357 `extension`、45 `removed`。改动核心目录后用 `scripts/refresh-core-source-delta.py --upstream-repo <含 b388 对象的只读 NewAPI 检出>` 重算；`--check-local` 只核对本地哈希，不需要上游检出。

## 现行本地差异

后端对齐固定 b388，只保留用户批准的独有功能、明确改造和 Vue 实际对接所需的最小接点；历史 `verified_fix` 不构成保留理由。346 个生产差异文件的 1,109 段逐段裁定，现为保留 873、恢复原生 169、拆分 67，见 `NATIVE_B388_REVIEW.json`；测试迁移见 `NATIVE_B388_TEST_MIGRATIONS.json`。产品合同摘要见 [AGENTS.md](../../AGENTS.md)，28 项合同的目录在 `CORE_SOURCE_DELTA.json` 的 `contracts`。

| 模块           | 保留的差异                                                                                                                                                                                                             | 依据                                                                                                               |
| -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| 路由与并发     | 无业务分组；每 Token 自动或有序手动路由；渠道倍率和 Key 倍率上限；用户与渠道无等待并发；原任务续作固定原渠道；共享端点上普通渠道与插件渠道竞争；六个批量渠道写入口和路由保存按 ID 升序加锁，渠道与能力同事务，零值照写 | [统一路由](../routing/UNIFIED_ROUTING.md)                                                                          |
| 钱包与计价     | 发送前强制估算预扣；模型基础价乘渠道倍率一次；无模型默认价格和内置表达式，工具单价、音频倍率表、Gemini 音频输入固定价沿用上游                                                                                          | [实现](../billing/IMPLEMENTATION.md)、[定价](../billing/PRICING.md)                                                |
| 充值与支付     | 充值加赠；回调和人工补单最多入账一次；GMPay 核验；返利以本金为基数；已使用的兑换码不可修改                                                                                                                             | [充值加赠](../billing/TOPUP_BONUS.md)、下文“充值加赠接点”                                                          |
| 鉴权缓存与流式 | 上游 #8200 的缓存签名加入本地 `MaxConcurrency`；流式只保留取消时 ClientGone 优先、写入预算和首输出观测                                                                                                                 | `NATIVE_B388_REVIEW.json`                                                                                          |
| 插件           | 内置任务插件源码与上游一致；通用提供商插件开放；请求处理只有全局、渠道、模型作用域；已退役的 API 前缀在插件分发前返回 404                                                                                              | [插件入口](../plugin-api/README.md)                                                                                |
| 原生计数       | Claude 和 Gemini 的 token 计数走独立真实转发，不进入生成或估算回退                                                                                                                                                     | 合同 `native-count`                                                                                                |
| 观测与仪表盘   | 个人与全站 summary；请求限制摘要；所有登录用户可读的系统状态卡片；流量读数取宿主网卡或主监听器；渠道上游倍率只读展示                                                                                                   | [仪表盘](../console/DASHBOARD.md)、[安装](../installation/DOCKER.md)、[渠道上游倍率](../channel/UPSTREAM_RATIO.md) |
| 登录注册       | 管理员发布用户协议或隐私政策后，登录和注册页要求勾选；只在前端校验，与上游相同                                                                                                                                         | [认证](../authentication.md)                                                                                       |

同步上游时要留意的接点：

- `service/route_request.go` 的 `TaskPluginEndpointFilter`、`LookupTaskPluginEndpoint`、`NormalizeTaskPluginSubmitModel`、`taskPluginIdentities` 是从上游 `RouteTaskPluginEndpoint`、`lookupTaskPluginEndpoint`、`PrepareTaskPluginSubmit`、`pinnedTaskPluginIdentities` 迁出的逻辑。上游改这四处时把变化移植到这里。
- 上游 `ada15c050`（保留人工禁用）的守卫在扩展文件 `model/channel_status.go`。
- 带 `no-builtin-prices` 理由的文件（如 `setting/ratio_setting/model_ratio.go`、`relay/helper/price.go`、`service/text_quota.go`）逐提交手工合并：丢弃模型默认价格和内置表达式的增删，只吸收计费维度和计算逻辑。`setting/operation_setting/tools.go` 与上游一致，照常吸收。
- 退役前缀在 `router/web-router.go` 的 `retiredAPIPrefixes`，上游在这些前缀下新增路由时不注册。
- `router/web-router.go` 在原生 `ServeFrontendFiles` 之前挂 `ServeFrontendBrotliFiles`（扩展文件 `middleware/frontend_static_brotli.go`）：`scripts/prepare-frontend-embed.mjs` 构建时给 js、mjs、css、svg、json、webmanifest、txt 写入更小的 `<文件>.br`（质量 11）并一同嵌入；请求接受 `br`（合法 q 值大于 0）且该文件存在时由 `http.ServeContent` 原样发送，条件请求、HEAD 和 Range 按该文件应答，服务端不压缩，其余请求照原生走 gzip 或原文件。上游改 `ServeFrontendFiles` 的路径判断、响应头或缓存时对照这里。

### 充值加赠接点

上游没有对应功能。`NATIVE_B388_REVIEW.json` 只记录 2026-11-14 的折扣版本，受影响文件的审查依据指向本节。逻辑在扩展文件 `model/topup_bonus_*.go`、`service/topup_bonus*.go`、`controller/topup_bonus*.go` 和 `model/redemption_update.go`，原生文件只做接入：

- 四个本地定价渠道经 `QuoteTopUp` 取得不变的实付和加赠，确认字段含 `expected_bonus_quota`；Stripe 和 Creem 在 Checkout Session 和本地订单之前确认加赠。
- `TopUp` 带 `BonusQuota`；`FreezeTopUpQuota` 校验加赠小于到账合计，返利基数为本金。
- `Redeem` 和 `AdjustUserQuotaWithOperation` 接收预载的加赠求值器，分别在兑换 CAS 和操作号重放检查之后决定加赠。
- `model/main.go` 在 AutoMigrate 之后调用一次性迁移，查旧表时直接读系统目录并传播错误；配置加载或迁移失败阻止启动。
- 路由和 Access Token 目录登记 `/api/user/topup-bonus/preview`；`model/user.go` 的两个 used_quota 函数各调用一次 `RecordTopUpBonusUsage`。

## 与上游相同、未在本地修复的已知问题

原生文件保持上游原文；与上游相同的多扣问题按用户决定不在本地修复。恢复原生后重新出现的退步逐项列在[披露](NATIVE_B388_DISCLOSURES.md)（少计费、任务缺渠道不退款、任务 ID 碰撞、统计可见范围、日志脱敏、配置原子性、支付回调核验、两处数据竞争等）。Remix 与 Midjourney 续作的共有缺陷列在[等待上游](WAITING_UPSTREAM.md)。另有以下几项只登记、不修：

- `model/channel_cache.go` 的 `InitChannelCache` 忽略 `DB.Find` 的错误，启用内存缓存时一次失败的同步会把渠道表替换为空。
- Jimeng 非 V3 请求静默使用 5 秒。
- Stripe 在 TOKENS 展示模式下的数量合同不一致，后端保持原生，Vue 停用该入口。
- 上游 #8218 之后旧的 `auth:user:version:<id>` Redis 键成为无 TTL 残留，清理属于生产 Redis 写操作，没有授权。

下面两组来自历次审计的记录，没有在现行源码上重新核对，处理前先复核：

- Midjourney 轮询在两个渠道的 `mj_id` 相同时互相覆盖，渠道缺失时批量判失败且不退款；Jimeng 的 openai_video 校验忽略 `metadata.req_key`；Google 和 Vertex 在 `/v1/videos` 丢失顶层 `resolution`；Doubao 的 `metadata.duration` 为空串时原样发给上游并按最大时长预扣；重试换到更高倍率的渠道后补扣不足返回 403；易支付对已验签但永久无效的回调返回 fail；`common/ssrf_protection.go` 把带 zone 的 IPv6 字面量当作域名；batch 上游返回未请求的任务 ID 时，`updateBatchTasks` 按平台级映射更新另一渠道的任务。
- 在 `206e0fa5c` 基线登记的 Realtime 最终累计结算重复扣费、Waffo 把处理中状态当作失败终态、Uptime Kuma 缺失心跳按 0 统计、日志导出没有行数和字节上限；上游倍率探测在代理一侧不防 DNS 重绑定；签名充值回执的小数解析。

## 等待上游的项

清单、源码位置和上游 issue 见[等待上游](WAITING_UPSTREAM.md)：remix 的 N1、N2、N3、A5、A7，原生与插件的实际平台归属，`draft_task` 原任务声明，MJ 与插件的存储错误分类。这些项未经批准不另造本地修复；重新启动时按最新上游复核。

## 测试与诊断约定

已接受的原生差异冻结成诊断，集合不扩大；各档位的清单和规则只维护在[本地测试方案](../testing/LOCAL_TESTING.md#已接受的原生差异)，数字以 `scripts/testing/` 下的清单为准。这里只记两条与同步直接相关的约定：

- MJ 优先级探针的 4 个本地 manual/full 子例只有本地证据。manual 或并发准入改变检查顺序，或 missing/foreign 的响应出现差异时，这一项豁免不再适用。
- 升级候选对数据逐项比较，只放行五个旧敏感词键的删除和等价过滤实例的追加，以及从折扣版本升级时的充值加赠迁移（`scripts/testing/upgrade_contract.py`）。只切回旧镜像不会恢复已删除的敏感词键和折扣表。

真实供应商、真实支付和真实 OAuth 不在本地验证范围。

## 最近一次正式发布

2026-11-16 08:33 UTC，充值加赠替换充值折扣上线，从 `ff82c3d9d` 普通升级，回退目标也是它。

- 应用提交 `0780f4573afca52ceba4922169f323821439d266`，根提交 `034dac1d9b9cdde5efcc64a8372a546d24672db7`，镜像 `sha256:30bb7eca4998b0d950a211f5a5111fb6583fae0fb9faec781a4bdc59080cb380`。
- 本地门禁在独立克隆的 `37f7257cc` 上运行：`run.py quick` 通过；`run.py affected --base ff82c3d9d` 覆盖 171 个改动文件，Go 单测 6,857 项、三库矩阵 386/386、固定选择 race、Vitest 1,887 项、前端静态检查与构建、ui-contracts 565/565 通过。没有运行 `all`，没有验证真实支付。
- 完整隔离候选从 `ff82c3d9d` 镜像升级，结果为 `candidate_success=false`、退出码 2、`deployment_eligible=true`：已知诊断、阻断、routing（30 通过、1 已知、6 阻断）、原生差异和 race 的结果都与 `ff82c3d9d` 的候选相同，外部请求为零，升级合同实跑通过。
- 生产库原本没有折扣配置，迁移后使用内置版本 0（不发放加赠），public 表由 48 张变为 46 张。

这次发布之后的提交只改了测试脚本和文档，没有重新部署。现场核验、备份和回退边界见工作区的服务器档案（`docs/server/SERVER_PROFILE.md`）。

## 记录规则

- 只写现行状态。状态变化时改写对应小节，不追加按日期的条目；历史从 Git 读取。
- 上游窗口推进时更新冻结目标，逐文件处置写入新的 `WINDOW_<短 SHA>.json` 并删除上一个窗口文件，刷新 `CORE_SOURCE_DELTA.json`；前端落点有变化时改前端对照表。
- 本地差异只写模块级结论和依据链接；行为细节写进对应的专题文档，逐段裁定写进审查清单。
- 不修的已知问题和等待上游的项各只登记一处，解决后删除；没有在现行源码上核对的条目照实标明。
- 发布记录只保留最近一次，写明提交、镜像、实际运行的门禁范围、候选结果和没有运行的项。
- `CORE_SOURCE_DELTA.json` 用小节锚点（`#现行本地差异`、`#充值加赠接点`）引用本文，改标题时同步改引用。
