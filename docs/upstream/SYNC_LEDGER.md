# 上游同步账本

本文只记录现行状态：同步到哪里、本地保留了哪些差异、哪些已知问题没有修、最近一次发布。各批次的经过和历次发布从 Git 读取，改写前的完整流水是 `git show 66c29535f:docs/upstream/SYNC_LEDGER.md`。`NATIVE_B388_REVIEW.json` 里的 `SYNC_LEDGER.md:177` 和 `658–666` 是 `2382fbdd5` 版本的行号，对应下文的六入口批量渠道写合同和原生计数。

## 上游基线与同步范围

NewAPI（`QuantumNous/new-api`）是唯一上游。checkout 的 `origin` 是内部 bare 仓库，不是上游。Sub2API 和 AxonHub 只按具体问题参考，不建远端，不追版本。

冻结同步目标是 `6627f0e3f37c002564b630a73e5afb0d28a92d3b`（固定 main，`v1.0.0-rc.51` 之后第 31 个提交）。之后的上游提交没有评估，留给下一批。

2026-10-17 以 `09109ec31` 重建核心，此后的窗口都已逐提交处置，终点依次是 `206e0fa5c`、`34003e9c2`、`b08ee86fe`、`e449cc700`、`ba5f73677`、`48a6c87f9`（`v1.0.0-rc.49`）、`b20e0566b`、`b388140ac`、`3a407cc9b`、`48f9df96e`、`6627f0e3f`。最近一个窗口（`48f9df96e → 6627f0e3f`，10 个提交、47 个文件）的逐文件处置见[窗口清单](WINDOW_6627.json)；更早窗口的处置记录在 Git 历史。上游 `web/` 的改动按[前端对照表](../../frontend/UPSTREAM_MAPPING.md)找 Vue 落点。

同步时不吸收：

- React、Electron 及其专属依赖。
- 站内订阅与套餐、业务分组及分组倍率、多节点管理、旧 Codex/type 57 专用接入、io.net、Playground。
- 模型默认价格和内置计费表达式的增删。
- 上游 `payment_setting.amount_discount`。
- 上游 #8247：本地已在 `InsertWithTx` 的同一事务里写入 OAuth 邀请人。
- 单渠道多 Key：多 Key 模式、Key 选择、按 Key 停用与 Key 级状态，见“单 Key 渠道接点”。

来源清单 `CORE_SOURCE_DELTA.json` 按文件登记状态、哈希、合同理由和审查依据，各分类的当前文件数见清单的 `counts`。改动核心目录后用 `scripts/refresh-core-source-delta.py --upstream-repo <含冻结目标对象的只读 NewAPI 检出>` 重算；`--check-local` 只核对本地哈希，不需要上游检出。

## 现行本地差异

后端对齐冻结目标 `6627f0e3f`，只保留用户批准的独有功能、明确改造和 Vue 实际对接所需的最小接点；历史 `verified_fix` 不构成保留理由。`NATIVE_B388_REVIEW.json` 保存对齐 b388 时对 346 个生产差异文件 1,109 段的逐段裁定（保留 873、恢复原生 169、拆分 67），这是当时的历史裁定，没有按新目标重新逐段复核；之后的窗口里仍有本地差异的文件，按新目标逐段登记在同一文件的 `current_file_reviews`。测试迁移见 `NATIVE_B388_TEST_MIGRATIONS.json`。产品合同摘要见 [AGENTS.md](../../AGENTS.md)，合同目录在 `CORE_SOURCE_DELTA.json` 的 `contracts`。

上游管理通过独立的 model、service、controller 与权限资源接入 Sub2API 和 NewAPI 两个远端账号协议。迁移和系统任务注册保留最小接点。渠道状态复用行锁更新，倍率写回只发布渠道缓存同步；托管倍率的表单只读与字段省略在前端执行，原生 `controller/channel.go` 保持不变，直接调原生 API 改写的倍率在下一轮上游同步时校正。倍率数据源与绑定语义见[渠道上游倍率](../channel/UPSTREAM_RATIO.md)。

后台更新参考 Sub2API `328db26f450edbdf9f8d4344a3ea903ac6396842` 的 update service 与 admin system handler。Ren2Hub 使用独立的 common、service、controller 扩展文件与 router/extensions.go，沿用 RootAuth，只有 release 构建可替换程序。归档与校验清单的初始地址须使用 HTTPS，主机为 `github.com` 或 `objects.githubusercontent.com`，且不含用户信息。HTTP 客户端跟随 CDN 重定向。20 分钟进程内缓存、强制 SHA-256 清单、linux/amd64 单资产及进程内互斥对应单节点部署。检查更新时 GitHub 对 `releases/latest` 返回 404（仓库尚未发布 Release）按 200 返回空 `latest_version`，并清掉已缓存的 Release，面板显示“尚未发布版本”；其他读取失败有缓存时附 `warning` 返回缓存，没有缓存时返回 502，不像 Sub2API 那样把所有失败都当作“已是最新”。Release 工作流仅发布 semver 二进制归档，移除 docker-build、docker-image-branch、sync-release-to-gitcode 工作流。候选镜像保留独立的源码与二进制身份检查。

| 模块           | 保留的差异                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | 依据                                                                                                                                                          |
| -------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 路由与并发     | 无业务分组；每 Token 可在所选渠道内自动或顺序选路（所选条目全部停用保留列表并退回全平台自动路由），全平台使用自动选路、上游 429 短冷却及逐轮选渠诊断；静态路由预览与独立路由子页已退役，由令牌页独立路由弹窗配置（`POST /api/token/` 返回 `data.id`，`GET /api/token/routing/channels` 提供按草稿倍率与模型限制过滤的候选渠道）；渠道倍率和 Key 倍率上限；用户与渠道无等待并发；原任务续作固定原渠道；共享端点上普通渠道与插件渠道竞争；六个批量渠道写入口和路由保存按 ID 升序加锁，渠道与能力同事务，零值照写 | [统一路由](../routing/UNIFIED_ROUTING.md) 自动调度采用健康准入、动态质量和默认优先级，缓存复用按可比会话评价，价格只影响末级份额。                            |
| 钱包与计价     | 发送前强制估算预扣；模型基础价乘渠道倍率一次；无模型默认价格和内置表达式，工具单价、音频倍率表、Gemini 音频输入固定价沿用上游                                                                                                                                                                                                                                                                                                                                                                                  | [实现](../billing/IMPLEMENTATION.md)、[定价](../billing/PRICING.md)                                                                                           |
| 充值与支付     | 充值加赠；备用支付链接；回调和人工补单最多入账一次；GMPay 核验；返利以本金为基数，管理员可按订单回收一次；已使用的兑换码不可修改                                                                                                                                                                                                                                                                                                                                                                               | [充值加赠](../billing/TOPUP_BONUS.md)、[资金 API](../billing/API.md)、下文“充值加赠接点”                                                                      |
| 鉴权缓存与流式 | 上游 #8200 的缓存签名加入本地 `MaxConcurrency`；流式只保留取消时 ClientGone 优先和首输出观测                                                                                                                                                                                                                                                                                                                                                                                                                   | `NATIVE_B388_REVIEW.json`                                                                                                                                     |
| 插件           | 内置任务插件源码与上游一致；通用提供商插件开放；请求处理有全局、渠道、模型作用域和插件声明的作用范围（`plugin_scope`），没有业务分组；已退役的 API 前缀在插件分发前返回 404                                                                                                                                                                                                                                                                                                                                    | [插件入口](../plugin-api/README.md)                                                                                                                           |
| 原生计数       | Claude 和 Gemini 的 token 计数走独立真实转发，不进入生成或估算回退                                                                                                                                                                                                                                                                                                                                                                                                                                             | 合同 `native-count`                                                                                                                                           |
| 观测与仪表盘   | 个人与全站 summary；请求限制摘要；所有登录用户可读的系统状态卡片；流量读数取宿主网卡或主监听器；渠道上游倍率只读展示；模型目录 `GET /api/user/model-catalog` 提供进程内无锁投影的用户侧渠道健康与质量摘要（仅覆盖已授权报价，严禁泄露实时占用、映射目标名与 Root 诊断细节）                                                                                                                                                                                                                                    | [仪表盘](../console/DASHBOARD.md)、[统一路由](../routing/UNIFIED_ROUTING.md)、[安装](../installation/DOCKER.md)、[渠道上游倍率](../channel/UPSTREAM_RATIO.md) |
| 登录注册       | 管理员发布用户协议或隐私政策后，登录和注册页要求勾选；只在前端校验，与上游相同                                                                                                                                                                                                                                                                                                                                                                                                                                 | [认证](../authentication.md)                                                                                                                                  |
| 公告与通知     | 独立表 `announcements` 与 `announcement_reads` 提供类型、状态、置顶、定时窗口、弹窗/静默推送及用户级已读追踪；管理接口挂载在 `/api/admin/announcements`（受 `announcement:read`/`announcement:write` 权限与 PAT scope 保护），用户接口挂载在 `/api/announcements`（仅会话可访问）；原生 `console_setting.announcements` 与 `/api/notice` 保持不动                                                                                                                                                              | `model/announcement.go`、`controller/announcement.go`                                                                                                         |

请求字段一致性（`request-field-consistency`）：HTTP 中继在 Token 鉴权后、限流和分发前共用 `middleware.RejectConflictingRequestFields`，读取现有有界且可重放的 BodyStorage。JSON 任意深度重复成员、结构字段大小写变体、multipart 标量重复及 JSON/multipart 双重解读均以 400 `duplicate_request_field` 拒绝，不占请求并发、不预扣、不生成路由尝试。自由键空间（schema 属性、工具参数等）允许 `id` 与 `ID`，精确重复仍拒绝；Claude 工具输入和 Gemini 函数结果按所在协议位置放宽，包装的 Responses 输入仍检查大小写。multipart 列表沿用已知多图及数组字段语义。Content-Type 不区分大小写地含 `multipart/form-data` 时，媒体类型必须精确匹配且 boundary 可解析、非空；否则同样在入口返回 400，并说明声明歧义。

`pkg/requestfield` 来源为 Sub2API `328db26f4` 的 LGPL-3.0 扫描器，本地收窄协议位置的自由键判断。`router/relay-router.go`、`task-router.go`、`video-router.go`、`task-plugin-protocol-router.go` 和 `plugin-router.go` 只增加共用中间件接点。Responses WebSocket 每个 `response.create` 帧和 Realtime 每个客户端帧在解码前调用同一扫描器；拒绝后以各自协议的错误事件回复，冲突帧不转发，会话继续。对应最小接点位于 `relay/responses_websocket.go` 和 `relay/channel/openai/relay_realtime.go`，错误构造放在扩展文件。

已知局限：OpenAI `metadata` 等未列入自由键空间的位置，大小写不同的同名键（如 `userId` 与 `userid`）会被拒绝；`x-multipart` 等非标准类型不按 multipart 扫描，目前下游会闭合失败。

同步上游时要留意的接点：

- `service/route_request.go` 的 `TaskPluginEndpointFilter`、`LookupTaskPluginEndpoint`、`NormalizeTaskPluginSubmitModel`、`taskPluginIdentities` 是从上游 `RouteTaskPluginEndpoint`、`lookupTaskPluginEndpoint`、`PrepareTaskPluginSubmit`、`pinnedTaskPluginIdentities` 迁出的逻辑。上游改这四处时把变化移植到这里。
- 上游 `ada15c050`（保留人工禁用）的守卫在扩展文件 `model/channel_status.go`。
- `relay/responses_websocket.go` 的 `buildResponsesWSCreatePayload` 在得到最终出站 `response.create` 事件后交给 `service.ObserveRouteGenerate`，读取参数覆盖与出站插件之后实际发给上游的 `generate`；两条发送路径和每次重试都经过它，早于 `MarkRouteAttemptSent`。每个 create 在独立的 gin 上下文里运行。这个文件按 `current_file_reviews` 逐段登记，上游改出站事件的构造或 `generate` 的处理时对照这个接点。
- `service/route_error_codes.go` 的 `nativeRouteErrorCodes` 抄录原生 `pkg/perf_metrics/outcome.go` 中 `classifyFailure` 上游分支点名的错误码和类型，并在原生的失败一组里另分账号类与服务器类。合并上游对 `classifyFailure` 的改动时复核这张表：`TestRouteErrorCodesListEveryNativeName` 从原生源码读出名单和判定，新增、删除或改判的名称会让它失败；`TestRouteErrorCodesAgreeWithNativeClassifier` 对每个名称分别作为 code 和 type 经真实错误处理器核对原生判定，并固定服务器类只有五个名称。这张表有两处用途：熔断快照只保存表内的码，表外记 `unrecognized`；上下文超长忽略判定（`requestContextOverflow`）把账号类当作不可被消息覆盖的信号。漏抄的上游新账号码在快照里只损失诊断，在超长判定里会让带超长措辞的 5xx 被记为 ignored，因此复核时要给新名称归类。
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

### 单 Key 渠道接点

一条渠道只保存一把 Key，提供商插件渠道只保存一份凭据；多个 Key 或多个账号各建一条渠道（合同 `single-key-channel`）。上游的多 Key 模式、Key 选择、按 Key 停用和 Key 级状态不吸收。原生文件里的接点：

- 写入口：`POST /api/channel/` 拒绝 `multi_to_single` 模式和 `multi_key_mode`、`key_mode` 字段，`PUT /api/channel/` 拒绝 `key_mode` 与 `multi_key_mode`，没有 `/api/channel/multi_key/manage`；创建（批量创建先按行拆成各渠道）、改 Key 和改变 Key 的读法（类型、插件或 Vertex `vertex_key_type`，对保留的 Key）都经 `validateSingleKey` 拒绝多行 Key，只有 Vertex 服务账号和声明 `oauth2_jwt` 认证的任务插件凭据这两种 JSON 对象可以跨行；批量创建（每行一条渠道）照旧。
- 运行时：`middleware/distributor.go` 直接取渠道 Key；`ChannelMeta`、上下文、`admin_info` 和请求策略事件都没有多 Key 字段；`types.NewChannelError` 不带多 Key 标记；Responses WebSocket 锁定连接只比较 Key 文本；Vertex 令牌缓存按渠道 ID；原任务续作、上游模型拉取、上游倍率识别和推理服务状态都用渠道 Key。
- 插件凭据：登录替换渠道里的凭据，已存凭据属于本插件时把它交给 `login` 并要求同一账号；请求不再带 `add` 和 `key_index`（被拒绝），`plugin_credentials` 不带 `index`。刷新、`relogin` 停用、`scheduled` 和余额都按渠道进行；一个账号在同一插件只登录在一条渠道上，任务轮询仍按提交任务的账号分批。
- 存储与状态：`model.Channel` 没有 `channel_info` 和 `Keys`，新安装不建 `channel_info` 列；`UpdateChannelStatusDetailed` 只写渠道状态，`DisableChannelByTag` 直接按标签停用，渠道缓存不预解析 Key。没有每渠道轮询锁，渠道行锁是每渠道的写入边界：`UpdateChannelStatusDetailed` 的状态写入，以及 Key 与凭据写入的 `UpdateChannelLocked`、`UpdateTaskPluginChannelLocked`、`UpdateScheduledChannelLocked`，都在锁住渠道行的事务里同时写渠道和能力表（SQLite 上四处共用每渠道 Key 锁），这些写入方在各自事务内保证渠道状态和 `abilities.enabled` 一致；普通编辑独立重建能力表的并发覆盖风险见[等待上游](WAITING_UPSTREAM.md) ROUTE-1；其他 `other_info` 写入方经 `UpdateChannelOtherInfo` 比较写入，遇到改动重读。
- Vue：渠道表单只有单密钥和批量密钥，编辑时一个字段替换 Key；插件凭据面板只有登录和重新登录。

同步上游时，上游对多 Key 代码的改动（`channel_info`、`GetNextEnabledKey`、`handlerMultiKeyUpdate`、`multi_key/manage`、`key_index`、`MultiKeyMode` 及 Key 级状态）按“不吸收”在窗口文件里逐项写明；同一文件里与多 Key 无关的改动照常吸收，并保持本节的接点。

上游倍率探测的代理请求由 `service/protected_fetch_pinned_proxy.go` 固定本地解析结果，防护开启时逐个校验所有 IP，任一不允许即拒绝；HTTP 保留原 Host，HTTPS CONNECT 使用 IP、证书按原主机名校验，HTTPS 代理自身证书单独校验。通用受保护抓取同样复用此扩展；仅防护开启且目标为 IP 字面量或启用域名 IP 过滤时固定解析，防护关闭或关闭域名 IP 过滤时沿原生代理解析。防护开启仍检查域名和端口，每一跳重定向重新执行适用校验，管理员配置的代理本身可为本地地址。共享目标校验拒绝带 zone 的 IPv6。

B1 已批准的本地安全边界（`audit-local-safety`）：

- 批量额度增减在同一预扣分片锁内同步修改缓存与批量记录，缓存重建不能观察半完成状态。命中缓存不持锁，未命中在锁内二查后水合；Redis 故障回退前落库待处理增量，落库失败合并保留。批量锁只用于摘取和恢复条目，不在锁内写库；软删除余额照常落库，硬删除条目记录一次错误并移除。非批量增减及提交后的授信同样使用预扣分片锁；先按既有规则落库待批量预扣，再读取当前数据库余额同步缓存，不叠加迟到增量，保持认证元数据。缓存恢复后重新同步余额，避免重复消费。
- Stripe、Creem、Waffo、Pancake 的已验签回调核验成功终态、冻结金额币种及商户身份；与 Epay/GMPay 共用精确十进制结算。永久不符不入账并返回 2xx、记录拒绝审计；Stripe 临时入账失败 ACK、Waffo 非成功状态、Pancake 临时查单失败 ACK 仍保留，具体字段和官方依据见[资金 API](../billing/API.md)。
- Realtime 使用现有请求体与流扫描限制，双向写有期限；事件状态同锁处理，关闭双方连接并等待读协程退出后结算。逐轮与会话的重复扣费仍按 BILL-1 只登记。
- Compact 与 OpenAI 用量识别保留 `cache_creation_input_tokens`，仅缓存创建也计费；日志聚合包含 12 小时拆分，避免总量与拆分重复累计。
- 异步轮询以渠道和供应商任务 ID 建立独立索引，普通与批量结果只能更新所属渠道任务；成功结算和失败退款仍沿用原有 CAS 赢家路径。渠道之间独立派发并在本轮返回前等待完成，批量模式也可跨渠道并行；同渠道重复供应商 ID 仍属于原生边界。

B2 已批准的权限与输出边界：

- 本人日志统计按用户 ID 限制额度与 RPM/TPM，改名不丢历史；管理员显式搜索保留 LIKE。用户列表只投影已知的非秘密设置，通知目标 URL 与凭据不返回。日志中心四分类在保持零表结构迁移与原有单参数兼容的前提下，于 `model/log_filter_types.go` 及最小接点扩展 `/api/log(/self)`（含导出）的 `types` 多类型筛选、`/api/audit(/self)` 的 `categories` 多类别筛选与 `/api/mj(/self)` 的 `status` 状态筛选，非法参数显式失败。
- 公共分页归一到正数及最多 100 行，页码限制防止 offset 溢出。AUTH-7 日志导出无总行数、字节数、时间跨度或总时长上限；每个用户同时只能运行一个导出，不设全局并发上限。使用 500 行键集短查询，每行写入与刷新前续期 60 秒写截止且结束复位。AUD-EXP-1/3：两个导出路由排除 gzip，审计 writer 通过 Unwrap 保留截止能力；不支持截止在流响应头前显式失败，取消/写失败跳过最终刷新。查询列表和统计仍保留原时间范围限制。完整合同和 ClickHouse 例外见[网关 API](../routing/GATEWAY_API.md#日志导出)。
- OAuth 登录的每个 flow 绑定独立 HttpOnly 浏览器 Cookie，见[认证](../authentication.md)。渠道读取按 Root 与有效写权限投影秘密，header_override、param_override与带身份的setting、含高级认证或插件配置的settings按整块投影；完整脱敏占位或省略字段不会写回，新增及标签编辑拒绝占位；敏感更新同时要求账户权限与 PAT scope。`header_override`、`param_override` 显式空字符串清空，省略保留；省略 `setting` 时读取已存的插件绑定配置完成校验，不清空。更新响应重新读取数据库后投影，读取失败以500和updated:true明确已保存；接口见[扩展接口](../routing/GATEWAY_API.md)。
- 状态更新失败返回明确错误；不存在、未变与持久化失败分开，批量列出失败项。缓存只在事务提交后发布，快照不原地修改；刷新读取失败保留原缓存并报告。仅显式的自动停用/恢复调用在缓存已处于目标状态或人工停用挡住自动停用时走读取快路径，不等待状态锁或查库；管理员单个、批量与标签启停始终以数据库事务为依据，不因陈旧缓存值相同而跳过。普通编辑的能力事务边界仍属 ROUTE-1。
- 上游响应复制过滤凭据、Cookie 和逐跳头（含 Connection 声明项）；通配请求头不转发 WebSocket 认证子协议。错误各字符串字段、嵌套 metadata 与前置日志共用净化，内部重试码及 cause 保留；错误体读取失败仍关闭响应体。

用户批准吸收的协议兼容与边界修复（`audit-local-safety`）：

- 流内协议失败摘要：`relay/common/stream_status.go` 新增仅输出到服务端 `Summary()` 的 `failureMessage`（保留首条非空文本，上限 500 rune 并经 `common.MaskSensitiveInfo` 脱敏，不进入 `StreamOutcome` 与用户可见日志），`service/responses_usage.go` 的 `ObserveResponsesOutcome` 在失败分支写入该文本；`relay/helper/stream_scanner.go` 的 `StreamScannerHandler` 函数体受 `known_upstream_logger_race.json` 冻结约束，暂保持原文不改其日志级别行。
- 上游伪心跳过滤：`setting/operation_setting/general_setting.go` 新增 `DropUpstreamKeepAliveEnabled`（`general_setting.drop_upstream_keepalive_enabled`，默认 `true`），扩展文件 `relay/helper/stream_scanner_keepalive.go` 实现 `IsUpstreamKeepAliveEvent` 与行级流式 `keepAliveFilterReader`，在 `relay/helper/stream_scanner.go` 的 `NewStreamScanner` 接入最小接点，于首响应计数前丢弃空 `SSE-Keep-Alive` 事件且超长行原样透传。
- Responses 缺失 `text.format` 补全：扩展文件 `relay/channel/openai/relay_responses_text_format.go` 在目标 `text`/`response.text` 为 JSON 对象且缺失 `format` 时补入 `{"type":"text"}`，`relay/channel/openai/relay_responses.go` 在流式与非流式两处接入单行调用。
- 用户名大小写查重：`model/user.go` 的 `CheckUserExistOrDeleted` 按 `LOWER(username) = ?` 查重（覆盖活跃与软删除用户），避免 PostgreSQL/SQLite 下的大小写变体绕过；不向 `users` 表插入保留名占位记录。

## 与上游相同、未在本地修复的已知问题

原生文件保持上游原文；与上游相同的多扣问题按用户决定不在本地修复。恢复原生后重新出现的退步逐项列在[披露](NATIVE_B388_DISCLOSURES.md)（任务缺渠道不退款、管理员列表可见范围、调试日志、配置原子性、支付回调确认、定价与日志计数竞争等）。HTTP 流失败后的亲和写回、Remix 与 Midjourney 续作的共有缺陷列在[等待上游](WAITING_UPSTREAM.md)。另有以下几项只登记、不修：

- 非自用模式且用户启用 `AcceptUnsetRatioModel` 时，未设模型倍率、走 token 重算的按量任务按预扣定额结算，多扣或少扣取决于用量（`relay/helper/price.go:212,231`、`setting/ratio_setting/model_ratio.go:103`、`service/task_billing.go:381`、`service/task_polling.go:815`）。自用模式下会按默认倍率重算；是否跳过重算的条件与冻结上游相同，本地只以渠道倍率替代分组倍率。
- Stripe 在 TOKENS 展示模式下的数量合同不一致，后端保持原生，Vue 停用该入口。

以下计费与任务问题已对照当前源码和冻结上游 `6627f0e3f` 核实，相关行为相同，只登记、不修：

- BILL-1（P2，上游同款多计费，按用户决定只登记）：Realtime 每轮 `preConsumeUsage` 经 `service/quota.go:128` 直扣，用量同时累加；会话结束后又按累计用量结算，存在重复扣费（`relay/channel/openai/relay_realtime.go:195,216,295`、`relay/websocket.go:59`、`service/quota.go:217`）。
- FUND-3（P2，上游同款多计费，按用户决定只登记）：Waffo 将所有非 `PAY_SUCCESS` 状态（包括处理中状态）写成失败终态，后续成功通知会因订单不再 pending 而不能入账（`controller/topup_waffo.go:383`、`model/topup_settlement.go:177`）。网关是否实际发送处理中通知尚无运行验证。
- BILL-4（P2，上游同款多计费，按用户决定只登记）：Midjourney 轮询读不到渠道时，批量将任务标为失败而不退款（`controller/midjourney.go:91`）；原生行为边界见[披露](NATIVE_B388_DISCLOSURES.md)。
- FUND-6（P3）：易支付对已验签但永久无效的回调仍返回 `fail`，GMPay 的永久核验或结算拒绝仍返回 200 `fail`，可能产生重复回调噪声。Creem、Waffo 成功通知、Pancake 已解析订单的永久结算拒绝现返回 2xx 并审计；Stripe 保留原有永久确认并补核验与审计。未验签请求不属于可信永久拒绝；各渠道当前分类见[资金 API](../billing/API.md)。

以下历次审计记录尚未在现行源码上重新核对，处理前先复核：

- Midjourney 轮询在两个渠道的 `mj_id` 相同时互相覆盖。
- Uptime Kuma 缺失心跳按 0 统计。

插件的凭据日志、迟到失败、作用范围重复项和 Seedream、Doubao、Jimeng、Google/Vertex 参数问题已在[等待上游](WAITING_UPSTREAM.md)核对登记；插件系统跟随上游，不做本地补丁。

## 等待上游的项

共有问题、上线前须复核的安全项及待核实的协议行为统一登记在[等待上游](WAITING_UPSTREAM.md)。这些项未经批准不另造本地修复；重新启动时按最新上游复核。

## 测试与诊断约定

已接受的原生差异冻结成诊断，集合不扩大；各档位的清单和规则只维护在[本地测试方案](../testing/LOCAL_TESTING.md#已接受的原生差异)，数字以 `scripts/testing/` 下的清单为准。同步还需保留下列探针边界。

- MJ 优先级探针的 4 个本地 manual/full 子例只有本地证据。manual 或并发准入改变检查顺序，或 missing/foreign 的响应出现差异时，这一项豁免不再适用。

真实供应商、真实支付和真实 OAuth 不在本地验证范围。

## 最近一次正式发布

2026-11-23 首次安装 Ren2Hub `4de3817f1771945b7bd1d4af1a9dc7bd9043f88d`，部署根提交 `0768a973b7bd6def2875206ca7713d85bfbc9afe`，镜像 `sha256:93bb8cfbde25cddfaba6b5c57ac9654897ddc33631da8541c3cbedfc731bf72c`。生产实例运行镜像内程序，等待首次接管。正式 HTTPS 的版本、前端资源和两个日志导出接口均已核验；真实供应商、支付和 OAuth 未调用。当前无前一版正式回退目标。运行身份、证据与恢复边界见工作区 `docs/server/SERVER_PROFILE.md`。

## 记录规则

- 只写现行状态。状态变化时改写对应小节，不追加按日期的条目；历史从 Git 读取。
- 上游窗口推进时更新冻结目标，逐文件处置写入新的 `WINDOW_<短 SHA>.json` 并删除上一个窗口文件，刷新 `CORE_SOURCE_DELTA.json`；前端落点有变化时改前端对照表。
- 本地差异只写模块级结论和依据链接；行为细节写进对应的专题文档，逐段裁定写进审查清单。
- 不修的已知问题和等待上游的项各只登记一处，解决后删除；没有在现行源码上核对的条目照实标明。
- 发布记录只保留最近一次，写明正式版本、源码提交、运行形态、已执行的验收和恢复目标。
- `CORE_SOURCE_DELTA.json` 用小节锚点（`#现行本地差异`、`#充值加赠接点`、`#备用支付接点`）引用本文，改标题时同步改引用。
