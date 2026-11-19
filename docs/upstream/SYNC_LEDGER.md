# 上游同步账本

本文只记录现行状态：同步到哪里、本地保留了哪些差异、哪些已知问题没有修、最近一次发布。各批次的经过和历次发布从 Git 读取，改写前的完整流水是 `git show 66c29535f:docs/upstream/SYNC_LEDGER.md`。`NATIVE_B388_REVIEW.json` 里的 `SYNC_LEDGER.md:177` 和 `658–666` 是 `2382fbdd5` 版本的行号，对应下文的六入口批量渠道写合同和原生计数。

## 上游基线与同步范围

NewAPI（`QuantumNous/new-api`）是唯一上游。checkout 的 `origin` 是内部 bare 仓库，不是上游。Sub2API 和 AxonHub 只按具体问题参考，不建远端，不追版本。

冻结同步目标是 `b388140acfd80369ec6243b16869daff1b4051f4`（固定 main，`v1.0.0-rc.51` 之后第 2 个提交）。之后的上游提交没有评估，留给下一批，只有一个例外：`1c63cc8c903630cf16611515d81b9785134daf19`（#8345，请求触发的本地错误不禁用渠道、不换渠重试）已提前吸收，下一个窗口不再重复处置。它改动的 `service/channel.go`、`relay/param_override_error.go`、`relay/image_handler.go` 与该提交的上游原文相同，相对 b388 登记为合同 `upstream-1c63cc8c9`；`relay/common/override.go` 和 `service/request_policy.go` 在本地差异之外套用同一补丁。五个文件的逐段依据在 `NATIVE_B388_REVIEW.json` 的 `current_file_reviews`。

2026-10-17 以 `09109ec31` 重建核心，此后的窗口都已逐提交处置，终点依次是 `206e0fa5c`、`34003e9c2`、`b08ee86fe`、`e449cc700`、`ba5f73677`、`48a6c87f9`（`v1.0.0-rc.49`）、`b20e0566b`、`b388140ac`。最近一个窗口（`b20e0566b → b388140ac`，4 个提交、10 个文件）的逐文件处置在 `WINDOW_B388.json`；更早窗口的处置记录在 Git 历史。上游 `web/` 的改动按[前端对照表](../../frontend/UPSTREAM_MAPPING.md)找 Vue 落点。

同步时不吸收：

- React、Electron 及其专属依赖。
- 站内订阅与套餐、业务分组及分组倍率、多节点管理、旧 Codex/type 57 专用接入、io.net、Playground。
- 模型默认价格和内置计费表达式的增删。
- 上游 `payment_setting.amount_discount`。
- 上游 #8247：本地已在 `InsertWithTx` 的同一事务里写入 OAuth 邀请人。

来源清单 `CORE_SOURCE_DELTA.json` 按文件登记状态、哈希、合同理由和审查依据，各分类的当前文件数见清单的 `counts`。改动核心目录后用 `scripts/refresh-core-source-delta.py --upstream-repo <含 b388 对象的只读 NewAPI 检出>` 重算；`--check-local` 只核对本地哈希，不需要上游检出。

## 现行本地差异

后端对齐固定 b388，只保留用户批准的独有功能、明确改造和 Vue 实际对接所需的最小接点；历史 `verified_fix` 不构成保留理由。346 个生产差异文件的 1,109 段逐段裁定，现为保留 873、恢复原生 169、拆分 67，见 `NATIVE_B388_REVIEW.json`；测试迁移见 `NATIVE_B388_TEST_MIGRATIONS.json`。产品合同摘要见 [AGENTS.md](../../AGENTS.md)，31 项合同的目录在 `CORE_SOURCE_DELTA.json` 的 `contracts`。

| 模块           | 保留的差异                                                                                                                                                                                                                                            | 依据                                                                                                               |
| -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| 路由与并发     | 无业务分组；每 Token 自动或有序手动路由、上游 429 短冷却及逐轮选渠诊断；渠道倍率和 Key 倍率上限；用户与渠道无等待并发；原任务续作固定原渠道；共享端点上普通渠道与插件渠道竞争；六个批量渠道写入口和路由保存按 ID 升序加锁，渠道与能力同事务，零值照写 | [统一路由](../routing/UNIFIED_ROUTING.md) 自动调度采用健康准入、动态质量和默认优先级，缓存复用按可比会话评价，价格只影响末级份额。 |
| 钱包与计价     | 发送前强制估算预扣；模型基础价乘渠道倍率一次；无模型默认价格和内置表达式，工具单价、音频倍率表、Gemini 音频输入固定价沿用上游                                                                                                                         | [实现](../billing/IMPLEMENTATION.md)、[定价](../billing/PRICING.md)                                                |
| 充值与支付     | 充值加赠；备用支付链接；回调和人工补单最多入账一次；GMPay 核验；返利以本金为基数，管理员可按订单回收一次；已使用的兑换码不可修改                                                                                                                      | [充值加赠](../billing/TOPUP_BONUS.md)、[资金 API](../billing/API.md)、下文“充值加赠接点”                           |
| 鉴权缓存与流式 | 上游 #8200 的缓存签名加入本地 `MaxConcurrency`；流式只保留取消时 ClientGone 优先、写入预算和首输出观测                                                                                                                                                | `NATIVE_B388_REVIEW.json`                                                                                          |
| 插件           | 内置任务插件源码与上游一致；通用提供商插件开放；请求处理只有全局、渠道、模型作用域；已退役的 API 前缀在插件分发前返回 404                                                                                                                             | [插件入口](../plugin-api/README.md)                                                                                |
| 原生计数       | Claude 和 Gemini 的 token 计数走独立真实转发，不进入生成或估算回退                                                                                                                                                                                    | 合同 `native-count`                                                                                                |
| 观测与仪表盘   | 个人与全站 summary；请求限制摘要；所有登录用户可读的系统状态卡片；流量读数取宿主网卡或主监听器；渠道上游倍率只读展示                                                                                                                                  | [仪表盘](../console/DASHBOARD.md)、[安装](../installation/DOCKER.md)、[渠道上游倍率](../channel/UPSTREAM_RATIO.md) |
| 登录注册       | 管理员发布用户协议或隐私政策后，登录和注册页要求勾选；只在前端校验，与上游相同                                                                                                                                                                        | [认证](../authentication.md)                                                                                       |

同步上游时要留意的接点：

- `service/route_request.go` 的 `TaskPluginEndpointFilter`、`LookupTaskPluginEndpoint`、`NormalizeTaskPluginSubmitModel`、`taskPluginIdentities` 是从上游 `RouteTaskPluginEndpoint`、`lookupTaskPluginEndpoint`、`PrepareTaskPluginSubmit`、`pinnedTaskPluginIdentities` 迁出的逻辑。上游改这四处时把变化移植到这里。
- 上游 `ada15c050`（保留人工禁用）的守卫在扩展文件 `model/channel_status.go`。
- 定时渠道测试恢复自动停用的 Key（合同 `scheduled-key-recovery`）：`controller/channel-test.go` 只有两个接点，`testChannel` 的可选参数 `keyUnderTest`（存在时不开始路由观测，并把重读的渠道收窄到被测 Key），以及 `runChannelTestTask` 记录本次运行是否由调度发起（载荷 `Mode` 为空），只在调度发起时于渠道测试之后调用 `recoverAutoDisabledKeys`；管理员“测试全部”入队时 `Mode=scheduled_all`，不恢复 Key。资格规则只有一处，`service.RecoverableChannelKeys`，规划、发送前重读的渠道和写回时持锁读到的行三处共用：多 Key、渠道启用或自动禁用、渠道类型不是任务插件（type 61，按持久化类型判断，与插件是否已加载无关；收窄成单 Key 后插件按槽位定位和刷新凭据，可能用别的槽位的凭据证明被测 Key，它们的 Key 状态由插件定时钩子管理）、槽位下标在范围内且仍是自动停用、该槽位文本在渠道内唯一（上游 `handlerMultiKeyUpdate` 按文本更新第一个相等的槽位）。写回时还要求该槽位仍是被测文本；测试在途期间渠道被改成插件类型、被人工禁用或被改出重复文本，都不写回、不发恢复通知。选取和测试在扩展文件 `controller/channel_key_recovery.go`，写入在 `service/channel_key_recovery.go`，经上游的 `UpdateScheduledChannelLocked` 与 `SetScheduledChannelKeyStatus` 只把仍是自动停用的同一 Key 改回启用。不用 `EnableChannel`：它和上游 `UpdateChannelStatus` 一样，渠道已启用时不写库。上游改 `testChannel` 的签名、渠道重读或 `runChannelTestTask` 时对照这两个接点。
- 带 `no-builtin-prices` 理由的文件（如 `setting/ratio_setting/model_ratio.go`、`relay/helper/price.go`、`service/text_quota.go`）逐提交手工合并：丢弃模型默认价格和内置表达式的增删，只吸收计费维度和计算逻辑。`setting/operation_setting/tools.go` 与上游一致，照常吸收。
- 退役前缀在 `router/web-router.go` 的 `retiredAPIPrefixes`，上游在这些前缀下新增路由时不注册。
- `router/web-router.go` 在原生 `ServeFrontendFiles` 之前挂 `ServeFrontendBrotliFiles`（扩展文件 `middleware/frontend_static_brotli.go`）：`scripts/prepare-frontend-embed.mjs` 构建时给 js、mjs、css、svg、json、webmanifest、txt 写入更小的 `<文件>.br`（质量 11）并一同嵌入；请求接受 `br`（合法 q 值大于 0）且该文件存在时由 `http.ServeContent` 原样发送，条件请求、HEAD 和单段 Range 按该文件应答；多段 Range 的 multipart 正文不是 brotli 流，所以忽略 Range、返回完整 `.br`（200），条件请求照常；服务端不压缩，其余请求照原生走 gzip 或原文件。上游改 `ServeFrontendFiles` 的路径判断、响应头或缓存时对照这里。

### 自动选渠诊断接点

合并上游的请求策略、任务计费和共享端点代码时，对照[渠道路由合同](../routing/UNIFIED_ROUTING.md)保留下列接点。

- `service/request_policy.go` 把选渠摘要挂在管理员策略事件中，由路由扩展维护轮次和实际发送前的状态。相对冻结上游的现行逐段依据见 `NATIVE_B388_REVIEW.json` 的 `current_file_reviews`。刷新来源清单时会复核源文件与差异段的指纹。
- HTTP 请求终止及 Responses WebSocket 每次 create 的错误收尾读取待发送的扫描记录。传输层在真正开始发送时清除此记录，预扣等本地拒绝则按请求 ID 输出诊断。提供商插件和讯飞的 WebSocket 各自直接拨号，两个拨号点在握手前调用 `MarkRouteScanDispatched`。
- Responses 与图像任务协议在提交收尾时共用 `reportTaskProtocolSubmission`。客户端断开后，这个接点仍将选中渠道后发生的本地拒绝交给策略记录，然后调用原有请求插件报告。两个分支的回归用例是 `controller.TestDisconnectedTaskProtocolsLogLocalSubmissionRejection`。
- 请求插件在普通中继、任务、Midjourney 和 Responses WebSocket 的本地错误出口调用 `RecordTerminalRelayErrorLog`。它先记录请求终止，再把策略事件连同错误写入日志。
- 普通中继消费日志和 `service/task_billing.go` 的 `LogTaskConsumption` 共用 `service/log_info_generate.go` 的 `appendRequestPolicyAdminInfo`，事件只写在已有日志的管理员字段中。
- `middleware/task_plugin.go` 在共享端点试选时调用 `PreselectTaskPluginEndpointChannel`，保留规范模型名回退与原任务固定渠道的处理顺序。选渠前读取 Token 路由策略失败时，错误在 `service/channel_select.go` 单独记录。

### 充值加赠接点

上游没有对应功能。`NATIVE_B388_REVIEW.json` 只记录 2026-11-14 的折扣版本，受影响文件的审查依据指向本节。逻辑在扩展文件 `model/topup_bonus_*.go`、`service/topup_bonus*.go`、`controller/topup_bonus*.go` 和 `model/redemption_update.go`，原生文件只做接入：

- 四个本地定价渠道经 `QuoteTopUp` 取得不变的实付和加赠，确认字段含 `expected_bonus_quota`；Stripe 和 Creem 在 Checkout Session 和本地订单之前确认加赠。
- `TopUp` 带 `BonusQuota`；`FreezeTopUpQuota` 校验加赠小于到账合计，返利基数为本金。
- `Redeem` 和 `AdjustUserQuotaWithOperation` 接收预载的加赠求值器，分别在兑换 CAS 和操作号重放检查之后决定加赠。
- `model/main.go` 在 AutoMigrate 之后调用一次性迁移，查旧表时直接读系统目录并传播错误；配置加载或迁移失败阻止启动。
- 路由和 Access Token 目录登记 `/api/user/topup-bonus/preview`。
- 加赠窗口只统计同步计费路径：`model/user.go` 的 `UpdateUserUsedQuotaAndRequestCount` 和 `UpdateUserUsedQuota` 保持上游原文；`service/text_quota.go` 的 `PostTextConsumeQuota`、`service/quota.go` 的 `PostWssConsumeQuota` 与 `PostAudioConsumeQuota`、`service/violation_fee.go` 的 `ChargeViolationFeeIfNeeded` 各把一个标识符换成 `service/topup_bonus_usage.go` 的 `updateSyncUsedQuotaAndTopUpBonusWindow`。任务计费路径继续调用原生函数，不计入。上游新增调用这两个原生函数的地方默认不计入，同步时逐个判断它是否属于同步计费路径。

### 备用支付接点

上游只有单个 `TopUpLink`。管理员在 `backup_payment_setting.links` 维护最多 10 个“名称 + http(s) 链接 + 可选图标”，逻辑在扩展文件 `setting/operation_setting/backup_payment_setting.go`，原生文件只做接入：

- `model/option.go` 的 `validateOptionValue` 在保存前调用 `ValidateBackupPaymentLinks`；`handleConfigUpdate` 像上游处理 `ToolPriceOptionKey` 一样把该键交给 `LoadBackupPaymentLinks`，不走通用配置加载器（它原地解码切片会保留被省略的字段，且静默丢弃解码错误）。`LoadBackupPaymentLinks` 用同一套校验整体替换钱包读取的快照，不合规的已存值记错误日志并保留上一份有效列表。
- `controller/topup.go` 的 `GetTopUpInfo` 增加 `backup_payment_links`：`TopUpLink` 不带名称排第一（不是 http/https 链接时不列出），其后是配置项；与在线支付方式一样，支付合规未确认时为空数组。`topup_link` 字段保持上游原样。

## 与上游相同、未在本地修复的已知问题

原生文件保持上游原文；与上游相同的多扣问题按用户决定不在本地修复。恢复原生后重新出现的退步逐项列在[披露](NATIVE_B388_DISCLOSURES.md)（少计费、任务缺渠道不退款、任务 ID 碰撞、统计可见范围、日志脱敏、配置原子性、支付回调核验、两处数据竞争等）。HTTP 流失败后的亲和写回、Remix 与 Midjourney 续作的共有缺陷列在[等待上游](WAITING_UPSTREAM.md)。另有以下几项只登记、不修：

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

2026-11-17 17:32 UTC，从 `96b70e3ab` 普通升级，回退目标也是它。内容：独有功能审计的修复（GMPay 空链上交易号的父订单回调、原生计数脱离路由健康、工单附件预算、加赠窗口只统计同步结算、备用支付链接校验），管理员按订单回收邀请返利，以及删除仪表盘的联系悬浮按钮。没有表结构变化，public 表仍是 46 张。

- 应用提交 `d352118b7c3d938b394d65a1c9ca917a2887d6b8`，根提交 `4be1ec0f1e719c8bda913d722dd9925e8fbc7979`，镜像 `sha256:0632d264f512250319da45e985bc73db120035388bce4e473a733d23b6587ef1`。
- 本地门禁按改动定向运行：registry、Go 点名用例、三库 `TestGmpayAndTicketDatabaseContracts`、`TestAffiliateTopUpReversalConcurrencyDatabases`、`TestFixedPriceBillingDatabaseMatrix`、`TestTaskPollingContractDatabaseMatrix`、`TestDoubaoRelayedImageStreamSettlement`，新增并发路径在 golang:1.26.10 容器里 `-race -count=3`，Python 236 项、Vitest 178 例、前端静态检查、7 个视觉 spec 38 例、镜像构建。没有运行全量档位。
- 范围候选 `--scope auto,billing`（`upgrade`、`billing`、`management`）从正式 `96b70e3ab` 镜像做升级演练，`candidate_success=true`、`deployment_eligible=true`。
- routing 同一镜像单独运行，退出 2：必测 30 项通过，其余是发布豁免清单登记的上游缺陷及其阻断，没有新增项，不计为通过。

现场核验、备份和回退边界见工作区的服务器档案（`docs/server/SERVER_PROFILE.md`）。

## 记录规则

- 只写现行状态。状态变化时改写对应小节，不追加按日期的条目；历史从 Git 读取。
- 上游窗口推进时更新冻结目标，逐文件处置写入新的 `WINDOW_<短 SHA>.json` 并删除上一个窗口文件，刷新 `CORE_SOURCE_DELTA.json`；前端落点有变化时改前端对照表。
- 本地差异只写模块级结论和依据链接；行为细节写进对应的专题文档，逐段裁定写进审查清单。
- 不修的已知问题和等待上游的项各只登记一处，解决后删除；没有在现行源码上核对的条目照实标明。
- 发布记录只保留最近一次，写明提交、镜像、实际运行的门禁范围、候选结果和没有运行的项。
- `CORE_SOURCE_DELTA.json` 用小节锚点（`#现行本地差异`、`#充值加赠接点`、`#备用支付接点`）引用本文，改标题时同步改引用。
