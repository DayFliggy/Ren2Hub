# 上游同步账本

NewAPI（`QuantumNous/new-api`）是 Ren2Hub 唯一上游；项目发布所用内部 bare `origin` 不是上游。Ren2Hub checkout 保留 `upstream` 指向 NewAPI；Sub2API（`Wei-Shaw/sub2api`）与 AxonHub（`looplj/axonhub`）仅作具体问题参考，不作为 Git 远端或并列上游。

本轮源码冻结目标为 `ba5f7367797566175a6cd97a748d430af3602a01`，于 2026-11-04 完成窗口核对；原已适配基线为 `e449cc700eb569cdfc2199cb73e97de46944f43c`。这不表示目标 SHA 仍是上游当前 HEAD，源码接入与运行验收状态分别记账。2026-10-23/24 的专项报告已于 2026-10-29 从工作树清理；可在 Ren2Hub 仓库用 `git show 85a34bc6bb94e69a323f80a137c3f09effb7e474:docs/upstream/<路径>` 读取清理前版本。旧条目中的“待上游”、`candidate_sha=null` 和缺陷状态只描述当时输入与工作阶段；不得作为当前待办，重新启动时须按最新上游复核。

## 2026-11-10 充值折扣合并

按用户要求把上游 `payment_setting.amount_discount` 与 2026-10-30 的用量折扣合并为充值折扣，全部作用在充值实付上，用量阶梯作为单笔充值阶梯的自动补充。行为见 `docs/billing/TOPUP_DISCOUNT.md`。该功能此前没有真正上线，按全新部署处理：不做 schema 迁移，不保留旧 `/usage-discount` 跳转、历史日志渲染或回滚兼容层。用户确认的政策为：单笔充值阶梯按阈值匹配，各分量相加后统一封顶，上限初值 5%，Stripe 与 Creem 不打折，接受囤积与并发同档，返利基数保持足额。

调用计价回到上游。`relay/helper/price.go` 的 `HandleChannelRatio(ctx)` 恢复上游签名和返回值；`types/price_data.go`、`relay/common/relay_info.go`、`model/task.go`、`controller/relay.go`、`relay/relay_task.go`、`service/log_info_generate.go`、`service/task_billing.go`、`service/violation_fee.go` 中的折扣代码块按 `4f5ab2596` 的逆补丁删除。这些文件去掉 `usage-discount` 理由，其余已批准差异不变。

充值报价放在扩展文件 `service/topup_discount_quote.go` 中，四个渠道共用的入口是 `controller/topup_discount_pay.go`。原生充值文件的接入如下：

- `getPayMoney`、Waffo、Pancake 去掉上游 `AmountDiscount`，每个请求调用一次 `QuoteTopUp`，pay 请求在建单和网关调用前核对 `expected_pay_amount`/`expected_currency`。
- Stripe 报价只删除 `AmountDiscount` 代码块。
- `TopUp.Insert` 与 GMPay `CreateGmpayOrder` 在同一事务里写 `topup_discounts` 审计记录。
- `topup/info` 删除 `discount` 键。
- `PaymentSetting.AmountDiscount` 字段删除，`payment_setting.amount_discount` 加入退役键。

小时表、配置表、服务与接口统一改名为 `topup_discount*`，`/api/option/topup_discount` 和 `/api/user/topup-discount`。来源清单用 `scripts/refresh-core-source-delta.py --upstream-repo /home/work/reference-projects-live-20261104/new-api` 重算，计数为 797 `upstream_equal`、280 `adapted`、276 `extension`、45 `removed`，合同 `usage-discount` 替换为 `topup-discount`，`candidate_sha` 为空。

## 2026-11-04 同步审计修复

本批从 `b9ffeda2fa185b52cf4d8ce44c4a3c665a7271a2` 开始，冻结上游仍为 `ba5f7367797566175a6cd97a748d430af3602a01`。审计修复提交 `e1dfd5f9e19e6e3e644c6fc9c412fc179b12afb1` 于 2026-11-04 13:18 UTC 发布到生产，跨版本候选 14 阶段通过，Opus 交叉审计无阻塞问题。本节首批源码验证与候选证据保存在 `/home/work/ren2hub-audit-fixes-20261104/delivery.json`；交叉审计后的 P3 修复仅本地提交，未推送、未部署，也未连接生产环境，其证据位于 `/home/work/ren2hub-p3-fixes-20261104/delivery.json`。

- Doubao：新增 `requestedVideoDuration`，校验/预扣与提交共用上游的 `seconds || duration || metadata.duration` 取值。前置 `null`、空字符串和数值 `0` 回退；`-1` 为真值，继续发送自动时长并预扣模型最大值。JavaScript 的末项 `metadata.duration=0` 仍返回 `0`，字符串 `"0"` 也是有效候选，两者由既有正整数校验拒绝。只给 frames 仍按帧数预扣，完全未指定时长仍预扣最大值。回归通过实际 Responses 解码覆盖空值加超长 metadata、duration/frames 冲突，并验证提交参数与 usage 一致；新回归在修复前已实际失败。
- Vue 模型定价：未进入全量快照的模型通过原生 `?model=<name>` 查询补齐 `plugin_variants` 与 `usage_schema`；使用 AbortSignal 取消旧请求和卸载请求，迟到响应不能覆盖当前模型，失败显示错误并允许重试。该补充读取不替换整页保存基线。确认 tools 标签页与 JSON 模式点击问题项确有入口缺陷；切回模型可视化编辑前提交当前编辑器并解析 JSON 草稿，非法 JSON 显示错误且不切换，工具及模型草稿继续保留。
- Vertex 渠道表单：打开时只接受缺省或空字符串、`v1`、`v1beta1`；非法存量值设置现有 `settingsError` 并阻止保存，删除 submit 中无法呈现给用户的 throw。
- Hailuo：非 H3 路径的 `validateHailuoCombo` 已按模型限定 6 秒或 6/10 秒，必然拒绝非正数、NaN 和 Infinity；删除相邻重复检查，保留该唯一校验及模型错误文案，补充行为回归。
- Sora：冻结上游没有 `relay/channel/task/sora`，实际实现为 `plugins/tasks/sora/plugin.js`。上游未提供本地 `requestValues` 的冲突校验；两者 Responses decoder 均通过 if/else 仅保留 seconds，因此 Responses 上的冲突检查不会执行，按用户要求只标记，不改行为。multipart 会先复制全部 fields，duration 并未被 if/else 删除；本地 JSON 和 multipart 均在 build/usage 拒绝不一致时长，已用实际 decode → build/extract 调用验证。保留 Sora JS 原样，新增回归防止误删有效校验。上下游对照在 `plugins/sora-upstream-comparison.json`。

以下行为已核对冻结上游，按本批指令仅登记，不追加本地修复，也不声称上游已经解决：

| 行为 | 上游依据与边界 |
| --- | --- |
| Jimeng 非 V3 静默使用 5 秒 | `outboundSeconds` 在 V3 校验前直接返回 5；本地沿用此分支。 |
| Jimeng openai_video 忽略 `metadata.req_key` | decoder 校验时从上下文/请求 model 推导 req_key；提交阶段才读取 metadata。 |
| Google / Vertex 在 `/v1/videos` 丢失顶层 resolution | decoder 接受顶层 resolution，但 Veo 出站/usage 读取 metadata 或 size，未将顶层值归一到 metadata；Omni 不在此结论内。 |
| 重试切换更高倍率后补扣不足返回 403 | 本地渠道倍率替代上游分组倍率；两者选定重试渠道后的 billing error 均直接终止，预扣不足沿用 403，不改写成上一轮上游错误。 |

验证按定向 Go/Vitest、静态检查、构建、ModelPricingView 浏览器、统一 unit/ui-contracts、已提交镜像候选的顺序执行。数据库 schema、钱包事务和并发实现未改，本批不重复独立 databases/race 层；候选仍执行隔离 PostgreSQL/Redis 的完整 14 阶段普通升级合同。真实供应商、真实支付与生产部署不属于本地测试。`CORE_SOURCE_DELTA.json` 重新计算核心文件指纹并更新每项保留差异依据；Vue 是现有独立扩展，其本批接口及交互边界在本节登记。

## 2026-11-04 交叉审计 P3 修复

本批从已部署提交 `e1dfd5f9e19e6e3e644c6fc9c412fc179b12afb1` 继续，冻结上游保持 `ba5f7367797566175a6cd97a748d430af3602a01`。前端/插件生产修复及测试证据、指纹、提交和从生产镜像版本进行的隔离候选结果见 `/home/work/ren2hub-p3-fixes-20261104/delivery.json`。本批只在本地提交，不推送或部署。

- Vertex 表单：对齐上游：冻结 React `web/src/features/channels/lib/channel-form.ts:577-578` 的加载默认化和 `:729-736` 的按类型保存语义。Vue 保留现有空字符串默认项，非法非字符串或未知版本加载为 `''`，不显示版本错误、不阻止直接保存，type 41 保存时修正为该默认值；`null` 同样在 type 41 保存为 `''`；缺省、`''`、`v1`、`v1beta1` 打开后直接保存不改变存量值。删除上一轮打开时设置版本 `settingsError` 的分支，其他存量 JSON 解析错误保持显式阻塞。后端 `controller/channel.go:666` 仅在 type 41 校验版本，因此非 Vertex 沿用 Vue 的增量保存，不像 React 删除旧键，避免无关类型切换改写存量设置；只有 type 41 写入版本。此默认化按用户最新明确要求吸收上游。
- Doubao 时长类型：对齐上游：冻结 `plugins/tasks/doubao/plugin.js:929-930` 的 number/string 类型识别。host 路径共享类型判断，选中非 number/string 值（`true`、`false`、`[6]`、对象、`null` 等）视为未知时长，出站删除 duration，统一预扣 `profile.maxDuration`，即使带 frames 也不用帧数估算；完全未给时长且只给 frames 仍按原规则估算。保留上游 `||` 对非空 metadata 时长的回退；`||` 跳过 `null`、`false` 等假值后若无候选回退，仍按已声明的非标量键处理，避免混同完全缺省后采用 frames。此最大预扣是本地明确要求的保守适配；上游 `:932-933` 会继续尝试 frames，上游提交 `:843-844` 的 parseInt 又可能将 `[6]` 发为 6 秒而按最大值预扣，本地不照搬这处提交/预扣不一致。正常数字、数字字符串、`-1` 和 native `vendorBody` 行为保持原样。decoder 的 Number() 检查不改：Responses 真实 decoder 覆盖全部非标量及带 frames 用例；openai_video 的对象、`false`、`null` seconds 仍在原生范围检查时拒绝，其余可解码值验证无 duration 和最大预扣。
- Doubao null alias 测试：改用真实 Responses decoder 输入 `seconds:null, duration:6`。该 decoder 只传递 `seconds`，因此与上游一致，提交 body 不添加 duration，预扣最大时长；测试仅记录现行行为，不将不实际可达的直构造 host 状态当成 decoder 合同。
- Doubao metadata 空字符串：核对冻结上游 `Object.assign` 与出站构造后，`metadata.duration:""` 会原样进入 Ark 请求；Usage 将它作为未知时长预扣最大值（同时带 frames 时按 frames 估算，与上游 `:929-933` 相同），因此没有少扣。本批不修，只登记该与上游一致的行为。
- 定价编辑器：只修复 dirty 残留。新建编辑器提交出名称后，`updateDraft` 清除 editorDirty；草稿已经写入页面，编辑器之后以该名称从草稿显示，没有未提交的修改。本批曾加入命名时跳过单模型查询的优化，GPT 交叉审计先后发现两个 P2：连续点「新增模型」后跳过标记残留，以及原地命名（tools、JSON、复制当前定价）后从告警链接或列表重新打开时 watcher 不触发，两种情况下插件模型都缺少 `plugin_variants` 和 `usage_schema`。该优化已撤回，获取行为恢复为 `e1dfd5f9e` 的 watcher 驱动：命名不在快照中的模型时按需读取一次 schema，与生产一致；列表打开、初始深链接和切换模型照旧，迟到的旧请求被中止且不覆盖当前编辑器。

上游来源对照：模型定价的快照与单模型查询接口来自冻结 NewAPI；Vertex 默认化与按类型保存对齐 React，默认项和非 Vertex 增量保存按 Vue 现有合同适配；Doubao native/decoder 和 metadata 透传对照冻结 `plugins/tasks/doubao/plugin.js`。CORE_SOURCE_DELTA 在所有 Go 测试文件修改后重新生成。

## 2026-11-04 请求头与 Kling 按后续要求回归上游

用户在全量同步验收后明确要求“将这两项也回归 NewAPI 上游实现”，覆盖此前保留的任务请求头白名单和 Kling 出站参数/计费一致性修复。基于本地 `7e4f517fd5f2c33e6b891dbf1cc651a7d1ce10db`，仍以冻结上游 `ba5f7367797566175a6cd97a748d430af3602a01` 为准，不扩大版本窗口。

- `ClientRequestHeaders` 完整恢复上游：排除六类网关凭据头，其他头按规范名称取首值，始终提供 `Content-Type` 和 `Accept`（未发送时为空）；原生/协议 decoder 与 driver 继续共用该入口。移除本地白名单合同，恢复上游对应回归和插件 API 文档。
- `plugins/tasks/kling/plugin.js` 整文件恢复上游，撤销 metadata 合并后的本地 `model` 删除、`outboundMode` 强制复用及额外有限尺寸检查。`kling_responses_test.go` 恢复上游原文，删除只约束已撤销 Kling 本地行为的回归；其他插件的计费验证不变。
- 此次按明确指令接受上游语义：自定义非黑名单头可进入插件，Kling metadata 的模式覆盖与 usage 取值沿上游各自规则。此前复现报告作为历史证据保留，不再作为这两项当前保留理由；也不宣称上游已修复此前差异。

验证：受影响的 plugins、adaptor、插件 runtime、middleware 和 controller 共 880 个 Go 顶层用例通过，另有 2 个嵌入资源顶层用例（10 个含子用例事件）通过；零意外跳过，`GOWORK=off` 且每批超时 60 秒。根模块 `go vet`、后端构建和 diff/格式检查通过。恢复 middleware 的上游头部夹具后仅重跑该包及尚未完成的 controller，前三包未改源码/测试的已通过结果复用；早先失败记录单独保留。前端、数据库 schema、钱包/结算及并发状态实现未改，原对应源码验证作为未变部分的证据；新提交仍须重新构建并运行完整跨版本候选。

验证、源码身份和候选状态统一记录在 `/home/work/ren2hub-sync-20261104-ja5AWnjV/upstream-followup-headers-kling/delivery.json`。此前全链与 `deployment_eligible=true` 候选只对应 `7e4f517fd`，不得用于本次修改后的源码；本次不发布 Git、不切换生产。

## 2026-11-04 全量回归上游

本轮本地起点为 `a276b48eaa60ae2ff1e761c21f5ac5fb526a541c`，原适配基线为 `e449cc700eb569cdfc2199cb73e97de46944f43c`，冻结目标为 `ba5f7367797566175a6cd97a748d430af3602a01`。只处理该窗口的 33 个提交、188 个变更文件；此后上游提交另行处理。实施在独立 checkout 和专用上游缓存中完成，原工作树保留。正式 Git 发布和生产容器切换不属于本轮授权。

上游实现为默认；残余例外只来自已批准的产品合同或目标替换后可复现的必要修复。全量复核原有 277 个 adapted、261 个 extension、45 个 removed 文件，包含本轮补丁未触及的历史差异。文件哈希、每个非测试差异的依据及合同目录统一维护在 `CORE_SOURCE_DELTA.json`，不把旧 `extension-hook` / `upstream-defect` 标签当永久保留依据。生成器对新增、修改、删除生产文件一视同仁：缺合同、缺冻结目标审查或缺依据直接报错且不写清单。

处置表中的“接入”表示源码及对应定向回归已完成；最终全链门槛和候选身份另见本节验证记录，不能仅凭补丁可应用或文件哈希相同认定验收完成。

| # | 完整来源 SHA | 行为 | 处置 |
| --- | --- | --- | --- |
| 1 | `a2defd2a8256abe039e5295bd583aeb2d02f111f` | URL 脱敏尾部标点/括号 | 接入；保留必要的已批准合同接点 |
| 2 | `9eda50a32fedb9122531d0c7ca0112dd8efdb9a0` | VChart tooltip 与依赖 | 不吸收：退役 React/Electron 或 Vue 未使用的依赖 |
| 3 | `829f0828a5a9a099f24068d21b99411750ff8037` | 退出卸载受保护布局 | Vue 已有等价行为，保留现有实现并复验 |
| 4 | `f3660175e06eb05e609d56f1627c160a5a52172e` | Zstd 窗口/内存/提前读请求体限制 | 接入；保留必要的已批准合同接点 |
| 5 | `4193734a2abf2764375f29c26be8f540e9b9d4db` | 日志轮转后的动态 writer | 接入；保留必要的已批准合同接点 |
| 6 | `a3851c29e1640d275b47af9ec141d64ae7325d65` | Turnstile 密钥先保存后启用 | Vue 已有等价行为，保留现有实现并复验 |
| 7 | `3150dfea9cbe7244914291f8724a45c71bc02b85` | go-jose/v4 4.1.4 → 4.1.6 | 接入；保留必要的已批准合同接点 |
| 8 | `837b8145f875ec2060ede30afa97fa0814367837` | Electron undici 更新 | 不吸收：退役 React/Electron 或 Vue 未使用的依赖 |
| 9 | `ee0f0ac78b5395099a8ebb9fa95e2d4bdb8038bb` | Electron 版本更新 | 不吸收：退役 React/Electron 或 Vue 未使用的依赖 |
| 10 | `7fa815edb29257d14fe9d4cf7d8672d5e8173d27` | Electron fast-uri 更新 | 不吸收：退役 React/Electron 或 Vue 未使用的依赖 |
| 11 | `1695171fa2b14bdafe91bec3b9b6bed1ed367c11` | React tailwind-merge 更新 | 不吸收：退役 React/Electron 或 Vue 未使用的依赖 |
| 12 | `cef78205e86b1d17cfe79b532ee04cc3aeea93cb` | React knip 更新 | 不吸收：退役 React/Electron 或 Vue 未使用的依赖 |
| 13 | `9fc70bb83451a8c3ffcfcb17c71bb6706c4b52a6` | React Query Devtools 更新 | 不吸收：退役 React/Electron 或 Vue 未使用的依赖 |
| 14 | `6e78cb3288bb07d2c3972d83776c8aeeaf6358eb` | marked 升级到 18.1.2 | Vue 升级 marked 18.1.2，保留文档隔离与链接过滤 |
| 15 | `1e8c707acae93377336a5cc24f167a3e116fb13b` | React CodeMirror language 更新 | 不吸收：退役 React/Electron 或 Vue 未使用的依赖 |
| 16 | `883eeb87d864a0d657b39ff3703e057ebfaa523b` | 插件计费警告 | 接入；保留必要的已批准合同接点 |
| 17 | `77436271f4792f99a293b87ed7ab8d2606a411b3` | DOMPurify 升级 | 不吸收：退役 React/Electron 或 Vue 未使用的依赖 |
| 18 | `37d23dcc86b8eef4972a867582859a85d770c56c` | 个人侧栏设置服从管理员可见性 | 复验 Vue 共享可见性过滤；不新增 React 个人侧栏开关 |
| 19 | `c59f6fd553921702cb47dacadc9731fa01548e96` | 厂商原生路径、共享候选、原生 SSE 和 artifact | 接入；保留必要的已批准合同接点 |
| 20 | `8b253de42d2820d2772be8cab3fbd31c31cfecf3` | Kling 4.0 时长和 size 映射 | 接入；保留必要的已批准合同接点 |
| 21 | `1424ffdd4aef28812a0e9309a465453cbdd034e4` | 显式插件前缀声明去重 | 接入；保留必要的已批准合同接点 |
| 22 | `fe57cba425b51e113d26333185a2266e448c7651` | Kling 4.0 采用 list_price 扣量 | 接入；保留必要的已批准合同接点 |
| 23 | `ea343c4a81151bfd537d33b66527c25fa21a1112` | 已转发 SSE 的 Reserve 和 Insert 失败仍计费 | 接入；保留必要的已批准合同接点 |
| 24 | `d0abbf6e0d33552a41c402d9b1fadbaff27cf40d` | Vertex Gemini Omni Interactions 视频 | 与第 27 项成组接入，只保留最终 Token 计量 |
| 25 | `70e6af6429824f99de98e96e65254c8c2ef7ee0b` | 引入 github.com/MoeForge/moego v0.1.0-alpha.1；同一 ojson.Doc 完成请求编辑，保留未触及成员顺序/数字拼写；处理受控重复键；媒体与插件 base64 使用 ValidateString/EncodeDataURL/零拷贝读取 | 接入；保留必要的已批准合同接点 |
| 26 | `651d5a213fc81e7447e88cd35364695ee664bf09` | 把 prompt_tokens_details 和 completion_tokens_details 传入宿主结算，保留 cached_tokens 与推理等细分统计 | 接入；保留必要的已批准合同接点 |
| 27 | `c046559bbd8b154e88e69905f60e71a5f4f1e5aa` | Omni 计费事实改为 Token | 接入；保留必要的已批准合同接点 |
| 28 | `45746eee4de25ab1eb75d4b7fbb8de2199483ea3` | 空闲未收帧时发 PING；未应答连接关闭；健康慢请求可继续，已交付数据不自动重放 | 接入；保留必要的已批准合同接点 |
| 29 | `9b473a3fe4fed8d18980fd8d3c6a8908847a5f25` | 2FA 锁定过期后重置失败窗口 | 接入；保留必要的已批准合同接点 |
| 30 | `52c173038e3551618ef7c62f2e4db62ca43a8f69` | 支持 continuationToken、mediaProcessing 及 snake_case；CONTINUATION → OpenAI length；Argon 默认 medium、cyber-permissive 默认 high，并支持 thinking-level | 接入；保留必要的已批准合同接点 |
| 31 | `83b18fe6cc3fcf489fb64772445c8587d71251f1` | 渠道创建返回 ID/输入映射及 panic 回滚修复 | 接入；保留必要的已批准合同接点 |
| 32 | `94b521664599d416fead8e57d8318717c0e29058` | 渠道 settings.vertex_api_version 只接受空/v1/v1beta1；只改变 Google Gemini/Imagen 请求版本，Claude/OpenSource 路由不变 | 接入；保留必要的已批准合同接点 |
| 33 | `ba5f7367797566175a6cd97a748d430af3602a01` | 新增 embedding usage DTO；promptTokenCount > 0 时从上游统计与 promptTokenDetails 构造 usage，否则保留估算 | 接入；保留必要的已批准合同接点 |

保留 Vue、品牌、单节点、工单、GMPay、返利、模型目录、统计与运维观测，以及原生计数、请求限制摘要和上游倍率只读展示。每 Token 自动/有序手动路由、渠道倍率及上限、用户/渠道无等待并发仍复用统一入口；钱包、预扣和结算维持原生唯一实现；调用计价与上游一致，充值折扣只在充值下单时报价一次（2026-11-10 起）。模型默认价格和内置表达式不恢复，工具单价、音频倍率与 Gemini 音频固定价照常吸收。业务分组、站内订阅/套餐、供应商订阅账号、type 57、io.net、Playground、React/Electron、旧 `/api/next` 和独立账本继续退役。

渠道创建增加 `data.ids`、输入索引映射 `data.results` 和单结果 `data.id`；插件管理同步 pricing issues、skipped routes、原生路径/凭据/SSE/媒体合同，Vue 同批接入；Vertex 增加空值/v1/v1beta1 版本选择，HTTP/2 健康检查默认关闭。moego 最后成组进入请求编辑和 base64 路径，保留数字/未触及原文、受控重复键和缓冲区生命周期，独立模块不反向依赖 host，工具链保持 Go 1.26.10 / Bun 1.4.0。

实际收敛包括删除单调用的 `checkinToday` 文件、恢复直接的缓存切分调用和无必要类型别名、回归多处纯注释/格式漂移、删除 Vidu 旧宽泛校验而采用目标端点约束。真实客户端取消对照证明上游插件 adaptor 的 detached context 已能完成已交付 SSE 的结算，因此删除本轮尝试加入的三处取消 guard 和重复 `WithoutCancel`，仅留下行为回归。F 已吸收上游 schema/JSON 保真逻辑，不再为该部分保留独立本地例外。

必要修复证据位于 `/home/work/ren2hub-sync-20261104-ja5AWnjV/`：`core/RESULTS.json` 关联缓存创建 usage、Compact、双向 prompt_cache_options、multipart 预算和正则缓存淘汰对照；`plugins/verification-summary.json` 关联请求头白名单、Kling 等出站/计费同源、CAS 退款/重复上游 ID、URL 脱敏和流式生命周期；`parent-proofs/currency-proof.json` 关联有限正汇率/额度校验；`vue/counterfactuals/` 关联配置原子性、定价快照并发、支付回调与用户/Root 明细隔离，`vue/usage-flow-result.json` 补充独立 flow 查询对照。每项以目标实现的实际行为断言失败和当前实现通过为依据，编译失败、缺依赖和零用例不能作为缺陷证据。

普通更新验收扩展现有 `candidate --previous-image`：旧镜像生成合成配置、余额、充值、工单附件和在途任务，在同一隔离 PG/Redis/附件目标上替换新镜像；旧 `preparing` 状态由新版本恢复，供应商提交不能重复，完成只结算/退款一次，旧产物与旧工单附件仍可按原权限读取，重复启动的数据/迁移保持一致。`seconds` 与自定义 `u("p")` 数量分别验证；后者不是 Omni 的输入/输出 Token 断言。模拟供应商阶段间计划停服时显式固定 `TASK_POLL_MAX_FAILURES=0` 并报告，失败阈值另由原生测试保护，不改变生产默认值。

验证状态：修改前 Go 2,556 个顶层用例、Vue 2,087 项基线通过，旧 a276 镜像由固定源码构建。最终源码验证位于 `/home/work/ren2hub-sync-20261104-ja5AWnjV/final-source-validation-v3/`：Go 2,631 个顶层用例、Vue 2,100 项（287 文件）通过；68 项数据库合同共 120 组执行通过，覆盖 SQLite、MySQL 8.4.11、PostgreSQL 15.19，无跳过或清理错误；类型、lint、格式、三模块 vet、前后端构建、4 组 race、4 组真实隔离后端浏览器及 629 项 UI 合同（63 文件）全部通过。新增管理行为另有桌面/手机、明/暗 16 张截图的人工检查；UI 合同不等于全站像素对比。

数据库阶段补入 `TestTaskPollingRefundDatabaseMatrix`，复用五项现有退款/重复任务断言验证三种数据库及实际退款日志。此前已通过的生产代码和现有测试均未改动，因此保留 unit 和原 119 组数据库结果，用同一 runner 定向补跑新增合同；对应文件指纹和复用边界见 `validation-reuse.json`。首次文档写入导致指纹变化的运行，以及一次驱动收到 SIGTERM 而未形成完整报告的运行，都保留原始记录，不作为放行证据。

首轮跨版本候选的前 12 阶段通过，管理阶段因验收器错误读取用户工单 DTO 不返回的 `user_id` 而失败。验收器改为从同一受验证的隔离数据库核对 canonical 工单归属，并保留登录身份、附件内容/映射、跨用户拒绝及会话清理断言；25 个 runner 测试通过。生产 Go/Vue 代码及现有测试未变，已通过源码门槛继续有效，完整候选按修正后的新提交和镜像重新执行；失败记录保留，不算作通过。

本地应用提交、根 gitlink、镜像、跨版本候选和正式发布状态统一记录在 `/home/work/ren2hub-sync-20261104-ja5AWnjV/delivery.json`；详细候选报告位于同目录 `candidate-final/`。候选必须与真实提交和镜像绑定，14 个阶段全部成功并满足 `deployment_eligible=true`；不把本文件在构建前的记录当作候选通过证据。正式 Git 发布和生产切换不在本轮范围。

## 2026-11-02 折扣卡片响应式与标准计费空态

按用户后续要求，保留日夜光学毛玻璃视觉并优化字号与容器布局。辅助文字最低 12px，主倍率和窗口名随卡片宽度增长；真实金额与规则说明完整换行，不用省略号隐藏。窄卡改为分行，宽卡同步放大窗口列宽，设置热区至少 44px。概览网格增加可用宽度约束，200% 文字放大时不会因为桌面视口断点而强行保持三列。

无原始／实际优惠、无个人窗口且非统计降级时，显示独立“标准计费”面板与服务端倍率，移除全零台账、空水位尺及误导封顶线。有可达阶梯时继续展示真实进度；仅全局优惠、上限压顶和统计不可用分别保留来源、封顶提示与告警。金额转换、服务端倍率和赢家、后端计价、权限与数据结构不变。

OpenCode `hjm-g/gemini-4-argon` 与 `cn-m/kimi-k3` 完成交叉设计和最终源码／截图审计。Gemini 提出的宽卡英文周期列宽和中间宽度说明折行已修正；Kimi 确认状态边界、金额精度、Root 入口和测试约束无阻断项。按本轮设计重新建立四张零容差快照（有优惠／标准计费 × 日／夜），保留对比度与无溢出断言，新增多容器宽度、长金额和 200% 文字测试。证据在 `/home/work/ren2hub-discount-responsive-20261102/`，正式发布身份与运行验证另记工作区服务器档案。

本地验证通过：前端全量 283 文件／2084 项单测，最终折扣视觉回归 42 项（含四张零容差快照）、相邻仪表盘布局 10 项、真实隔离后端四组日夜桌面／移动冒烟、类型检查、ESLint、格式检查、Vite 构建和两个嵌入资源检查。概览网格的最终改动另以 DashboardView 11 项单测复核。真实后端验证在仅回环网络 namespace 与独立空库运行。

## 2026-11-02 系统折扣编辑整合

用量折扣统一进入「系统设置 → 计费 → 系统折扣」`/system-settings/billing/usage-discount`，旧地址保留 query/hash 后跳转，删除重复侧栏入口。独立页面改为系统设置内的专用面板，移除运行状态、健康计数及其自动请求；诊断 API 合同保留。编辑区按固定优惠与上限、全站阶梯、个人窗口组织，档位使用紧凑原生表格，日/周/月切换保留全量草稿并显示隐藏窗口错误，排除用户与让利估算按需展开。

继续使用原有版本化 API、精确金额转换与全量原子保存；复用系统设置统一离开保护，避免重复确认。保存冲突保留草稿，估算跟踪草稿变化并提示过期。后端计价、权限、数据库结构及充值折扣均未改动。

OpenCode `hjm-g/gemini-4-argon` 负责布局设计及最终视觉审计，`cn-m/kimi-k3` 负责交互约束与接口、状态复审，两者最终均无阻断项。浏览器检出的吸底栏遮挡已通过改为流内保存区修正，无遮挡断言保留。前端全量 2079 项 Vitest、类型、ESLint、改动源码格式、Vue 构建、Go 嵌入资源测试及二进制构建通过；37 项 UI 用例覆盖桌面/390px/320px、中英文、日夜、冲突与角色边界，仪表盘原附件零容差基线继续通过。四组隔离真实后端浏览器验证新页面保存、服务端精确值和刷新保持通过。证据目录为 `/home/work/ren2hub-system-discounts-20261102/`；正式发布身份和结果另记工作区服务器档案。

## 2026-11-02 折扣卡片附件定稿

按用户提供的日间、夜间附件发布折扣卡片，保留附件对应的光学毛玻璃水位尺与三行窗口布局。`useDashboard` 直接传递 `GET /api/user/usage-discount` 的完整响应，保留服务端倍率字符串、固定/全站分量、个人最高窗口与封顶结果；设置入口沿用 Root 权限及原有配置页。本轮不改后端计价、接口或数据库结构。

OpenCode `hjm-g/gemini-4-argon` 对附件、组件、请求失效控制和后端权限/计价合同作只读交叉审计，两轮均通过，最终结论为可发布、无阻塞问题。390px 中文日夜截图与附件的解码像素完全一致；附件原图作为 Playwright 零容差基线提交。另在隔离真实后端保存配置，验证普通用户权限、全局 3%、个人 4%、最终 0.95 倍、5% 封顶、刷新保持和 Root 配置入口；截图金额取隔离实例的真实响应，不硬编码附件示例金额。

验证证据位于 `/home/work/ren2hub-discount-release-20261102/`：前端全量 2076 项 Vitest 通过；Go service/controller 折扣定向测试（60 秒超时）、类型检查、ESLint、改动文件格式、Vue 构建、Go 嵌入资源测试及二进制构建通过；桌面/手机 × 日/夜四组真实浏览器冒烟通过；64 项浏览器 UI 用例通过（首轮 60 项通过后收到终止信号，剩余 4 项另轮通过）。源码、候选和正式部署身份以工作区服务器档案及本轮发布证据为准。

## 2026-11-02 仪表盘范围切换与可见性修复

`3c506e38e` 于 2026-11-02 03:47 UTC 通过普通更新执行器发布，根提交为 `fb8fb9ec8273338b6020c3219fe4b4ccecd48139`。镜像为 `ren2hub-test:3c506e38e321bac3f8264130ba26985992749547-v1`，ID `sha256:a970a9ddb80085a6b8bd03f17e094e68dbcf7c3c0fae6b50aa75d84c9481f0c2`，用 go1.26.10 构建。按用户明确要求，自动路由页签向全部登录角色开放；管理员和 Root 在标题右侧通过“个人 / 全站”切换概览与统计数据。设计和数据边界见[仪表盘说明](../console/DASHBOARD.md)。本轮使用 OpenCode `hjm-g/gemini-4-argon` 交叉设计及最终 diff 审计，并完成全角色桌面/手机、明暗主题视觉审查。

冻结上游仍为 `e449cc700eb569cdfc2199cb73e97de46944f43c`，本地修改基于 `b3a1e01673dfee0002aa5dc61b4047b6dfe32bda`；本轮不升级上游、不 fetch。

- `controller/dashboard_summary.go`、`service/dashboard_summary.go` 与 `model/dashboard_summary.go` 共用显式的个人/全站范围。新 `/api/data/summary` 使用 AdminAuth 和 PAT `log:read`；个人入口固定鉴权 ID，使用 `usage:read`。两者禁用 HTTP 缓存。用量来自 quota_data，耗时/成功率来自日志，成功率按 user_id + request_id 去重；全站包含 Root 和删除账号历史，不返回身份字段。
- `controller/log.go` 与 `model/log.go` 修复个人统计使用 username LIKE 的越权：`%`、`_`、改名、用户名复用不再影响身份范围。额度和 60 秒 RPM/TPM 共用按 ID 的过滤，原有时间语义保留；列表、导出、统计复用同一日志过滤。
- `model/user.go` 的查询范围在 Count 和分页之前限制管理员用户列表/search，只返回下级账号；详情和修改沿用同一角色判断。管理员使用日志、对应统计，以及旧 `/api/data/users`、`/api/data/flow` 和指定 username 的 `/api/data/` 排除 Root 与软删除 Root。通过主库 ID 查询后过滤日志库，查询失败返回错误；不带身份的全站聚合仍保留 Root。Gemini4 指出的旧用量接口旁路已纳入同一规则。
- Vue 复用 PageHero actions 与 SegmentedToggle，范围只存本页内存。切换和身份变化取消旧请求、清空隐藏统计页签的数据；降权恢复个人视图。全站隐藏余额/折扣，不以个人累计请求数兜底，概览的 Token 趋势与模型分布卡片在全站视图下扩展为三列满宽；系统状态继续使用现有快照。新增中英文文案，用户管理筛选只提供可见角色。

必要原生差异：`controller/usedata.go`、`controller/log_export_test.go` 从 upstream_equal 转为 adapted；原有 adapted 的 controller/log.go、controller/user.go、model/log.go、model/user.go、model/usedata.go、model/usedata_flow.go、router/api-router.go、middleware/access_token_routes.go 更新。权限修复理由为 `usage-visibility`，仪表盘路由理由为 `dashboard-scope`；没有 schema 迁移。

发布前验证：

- 定向 Go controller/model/service/router 回归通过（单包 `-timeout=60s`），包括实际三角色路由、PAT scope、旧会话/PAT 降权、伪造查询、分离日志库、个人统计 ID 隔离、Root 明细排除和用户分页。
- 隔离三库 `TestChannelMetricsDatabases` 与 `TestLogExportDatabaseMatrix` 在 SQLite、MySQL 8.4.11、PostgreSQL 15.19 全部通过；零跳过、失败或清理错误。
- 前端全量 283 个文件、2070 项 Vitest 全通过；三角色 × 明暗 × 桌面/手机的 Playwright UI 合同 12 项全部通过；仅回环网络 namespace 的真实浏览器冒烟四组通过。
- 独立组合克隆 `3c506e38e` 的 `static`（`run-zlcym8b7`）与 `build`（`run-d04w7jfx`，产物 SHA256 `cdfa45cceff31e5d9f0d18691d722c5e760258fa085e2cbbb11a51d1d1725c59`）通过。
- 候选 `run-b8spy8er` 在根 `fb8fb9e`、应用 `3c506e38e` 的组合克隆中运行，使用同一镜像和 `--traffic-host-interface eth0`。七组全部通过，`deployment_eligible=true`，外部请求为 0，数据库为 fresh tmpfs。

切换结果：切换前后任务、Midjourney、充值、GMPay、系统任务和数据库会话都为 0，表数仍为 45。`REN2HUB_VERIFY_OK` 报告 `traffic=host_interface:eth0`，本地和 `https://next.renai.uno` 的只读核验都通过。

来源清单已用 `scripts/refresh-core-source-delta.py --upstream-repo /home/work/reference-projects/new-api` 重算，计数为 798 `upstream_equal`、277 `adapted`、261 `extension`、45 `removed`。

## 2026-11-01 首页审计修正与第二次正式发布

`e3c0b98dc` 于 2026-11-01 19:39 UTC 通过普通更新执行器发布，根提交为 `763ee06a50f3658863c506ab9273d12a7494369d`。镜像为 `ren2hub-test:e3c0b98dcbd743bf1aa9bd9f308872239cb91954-v1`，ID `sha256:0b10be030209b53ff8180f674a7fa443732a32f23a297ccb0af007cab5316fbf`，用 go1.26.10 构建。相对 `63e537495`，这次只改首页前端和一组后端测试，没有数据库或接口变化：

- `fb59d3ef6`：首页第二轮修复。沉浸模式按 `[data-hero-map-immersive-focus]` 重新定位地图，请求跟随实时枢纽；画布 resize 监听在 `scene.init()` 前注册；沉浸开关在滑动、断点和辅助技术退出时交还焦点；`LanguageSelector` 跟随外部语言切换；信号控制台和延迟计文本本地化；删除 `HOME_LAUNCHED_AT` 和无引用的文案键。
- `7b46c0a6e`：修正 Codex 首页审计的三项问题。轨迹抽屉加 `tabindex="0"`，focusout 改为检查整个 inspector，焦点进入抽屉不再关闭抽屉。飞线两端引用实时枢纽和模型标记，弧线控制点每帧按当前位置计算，重排后不再飞向旧位置。`running` 只表示宿主可见性闸门，rAF 只在可见且未减弱动态时调度；加载时开启“减弱动态效果”、之后关闭，动画会恢复，`WorldMap` 和 `CharRain` 同步该偏好。
- `e3c0b98dc`：复核发现的飞线问题。飞线引用实时标记后，拖动地图使标记离屏并跨过经度接缝时，飞线头会横跳约 430 像素。飞线现在和常驻连线一样随标记的 `visible`、`edgeAlpha` 隐藏和淡出；隐藏期间照常推进并触发到达。
- `489d98f8a`、`8fe6bf735`：`TestDashboardSuccessRateCancelledWaiterReturnsContextError` 在 unit 门禁中超时 60 秒。原因是上一个测试留下的 singleflight 调用会被下一个测试的 `DoChan` 加入，新测试自己的查询函数不运行，cleanup 一直等它完成；`-count=400` 可以稳定复现。复核又指出只调用 `Forget` 不够：旧 worker 在查询函数返回后才写缓存，这次写入可能落到下一个测试。测试辅助函数 `resetDashboardSuccessRate(t)` 现在先用同键的空操作 `DoChan` 等在途调用结束（上限 5 秒），再清缓存；cleanup 先重置再恢复替身。生产代码只多了 `dashboardSuccessRateRefreshKey` 常量。

发布前验证：

- 在 `e3c0b98dc` 的独立 clone 中串行运行 unit、databases、static、build、race、browser、ui-contracts（526 项全过）和 `test_runner.py`（17 项），全部通过。
- 前端 Vitest 全量 282 个文件、2061 项通过，vue-tsc、eslint、prettier 通过。新增回归测试换回修正前的代码都会失败。仪表盘测试组 `-count=1000` 和 `-race -count=50` 通过。
- Codex `gpt-6-astra` xhigh 共做三轮：首页审计发现上述三项；第一轮复核发现飞线接缝和测试写回两项；第二轮复核没有发现问题。
- 候选 `run-k385kprv` 在根 `763ee06`、应用 `e3c0b98dc` 的组合 clone 中运行，使用同一镜像和 `--traffic-host-interface eth0`。七组全部通过，`deployment_eligible=true`，外部请求为 0，数据库为 fresh tmpfs。

切换结果：切换前后任务、Midjourney、充值、GMPay、系统任务和数据库会话都为 0，表数仍为 45。`REN2HUB_VERIFY_OK` 报告 `traffic=host_interface:eth0`，本地和 `https://next.renai.uno` 的只读核验都通过。

来源清单补算了 `service/dashboard_system_status_test.go` 的哈希（`8fe6bf735` 改动时漏算），计数不变：800 `upstream_equal`、275 `adapted`、257 `extension`、45 `removed`。

## 2026-11-01 正式发布

`63e537495` 于 2026-11-01 14:28 UTC 通过普通更新执行器发布。该提交把本地 main 的首页交互修复与任务渠道缺失退款分支合在一起，根提交为 `299f6581f7e325b1135aa682fa3492369f9384a7`。镜像为 `ren2hub-test:63e5374954346808759e5443a472a9faf60cbf97-v1`，ID `sha256:0e97c34650d0bc5085d0ba4693f31d2dfe4f45dcab5df142b93812dd75b4b4a1`，用 go1.26.10 构建。

发布前验证：

- 在独立 clone 中串行运行 unit、databases、static、build、race、browser 和 `test_runner.py`，全部通过。
- ui-contracts 525 项中 524 项通过。失败的是首页展示的 light mobile 一项：运行时宿主机 CPU 被另一组 Playwright 占用，画布缩放的等待超时。单独重复运行 28 次均通过。
- 候选 `run-6zryym8e` 使用同一镜像和 `--traffic-host-interface eth0`，七组全部通过，`deployment_eligible=true`。外部请求为 0，数据库为 fresh tmpfs，`traffic` 为 `host_interface/eth0`。

切换结果：

- 切换前后，任务、Midjourney、充值、GMPay、系统任务和数据库会话都为 0。
- AutoMigrate 新增三张表，表数从 42 变为 45。
- `REN2HUB_VERIFY_OK` 报告 `traffic=host_interface:eth0`，本地和 `https://next.renai.uno` 的只读核验都通过。

Codex 第二轮复核还发现两项问题，本次没有改：

- batch 上游响应里有未请求的任务 ID 时，`updateBatchTasks` 会按平台级 `taskM` 更新另一渠道的任务。
- Midjourney 轮询（`controller/midjourney.go`，以及 `model/midjourney.go` 的 `MjBulkUpdate`）遇到两个渠道的 `mj_id` 相同时，两条任务会互相覆盖。

这两处代码与上游 `e449cc700` 相同，按“原生文件保持上游”的约定保留。

## 2026-11-01 发布前审计修正

发布前用 Codex 对待发布源码做后端和发布交叉审计，复现的问题已修正，每项都有回归测试，修正前失败、修正后通过。任务渠道缺失退款的两处问题记在 2026-10-30 条目。

倍率校验先比较大小、后限制指数。`decimal.Cmp` 比较前会把两边换算到较小的指数，`1e2147483647` 这类短输入因此会构造巨大的整数，修正前 `1e10000000` 一次校验约耗时 2.5 秒、分配 66 MB。新增 `model.ParseBoundedRatio`，在任何比较之前限定长度不超过 64 字符、十进制指数在 -18 到 18 之间。`NormalizeChannelRatio`（渠道倍率和每 Key 的 `max_channel_ratio`，后者普通用户默认可写）和手填上游倍率都改用它。合法取值不变，只有 `0e100000` 这类指数超界的零现在会被拒绝。

上游倍率探测走代理时，受保护拨号器只检查代理地址，目标 URL 没有校验。探测客户端的 `Proxy` 函数现在先调用 `ValidateSSRFProtectedFetchURL`，被禁止的目标不再经代理发出，与共享受保护客户端的做法一致。直连探测不加这一步，仍由拨号器在探测的 context 内解析并检查 IP，已取消的探测不发起 DNS 查询（`TestUpstreamRatioDirectProbeResolvesWithinItsContext`）。代理路径的预检与共享客户端一样不受 context 约束，代理远端的二次解析也无法核对。

前端定价编辑器改任务单价后点“生成任务计价草稿”，原先整条表达式被替换成新的基础价，请求规则和已应用的任务条件倍率（`cond ? m : 1` 形式的乘数）随之丢失，保存后匹配的请求按基础价少收。`repriceTaskExpression` 只接受能无损重建的形状：顶层乘积中恰好一个单阶梯、无常数项的 `tier()` 价格因子，其余因子都是 `cond ? 数字 : 1` 乘数；空白或纯数字表达式直接替换。重建时保留这些乘数和 `v1:` 前缀。其他形状（`image_count`、除法、多阶梯、乘数分支内再运算、前端无法编译的表达式等）一律拒绝，提示直接编辑表达式，原表达式不变。`readTaskPrices` 按同一规则回填单价。另外，`lib/billing-expr.ts` 的 `combineBillingExpr` 原样移植自上游，会把 `v1:` 前缀包进括号；带前缀的表达式再编辑可视请求规则时生成无法编译的字符串，现在把前缀放回最前面。

`TestModelPriceHelperHasNoBuiltInPrices` 单独运行时因 i18n 未初始化而 panic，已在测试内补 `i18n.Init()`。生产启动本来就会初始化。

来源清单已用 `scripts/refresh-core-source-delta.py --upstream-repo /home/work/reference-projects/new-api` 重算，计数为 800 `upstream_equal`、275 `adapted`、257 `extension`、45 `removed`，`candidate_sha` 为空。

## 2026-10-31 流量读数改为宿主网卡或主监听器

流量读数不再统计 HTTP 正文。启动时按 `TRAFFIC_HOST_INTERFACE`、`TRAFFIC_HOST_NET_DEV` 判定一次来源（`common/traffic_host.go`），进程内不切换：两个变量都为空（空串等同未设置）时使用主监听器计数，scope 为 `application_listener`；任一非空即为宿主模式，scope 为 `host_interface`，路径缺省为 `/host/proc/net/dev`。宿主模式每 10 秒重新完整读取 `net/dev`，只取配置的那块网卡的 rx/tx 字节，不汇总，也不改读其他网卡。网卡名或路径不合法、文件打不开、内容格式不对、找不到该网卡时，分别报 `host_config_invalid`、`host_counters_unreadable`、`host_counters_invalid`、`host_interface_missing`，两个方向都为 null，并且不改用监听器。reason 只在进入、切换和恢复时各写一次日志，observer 启动时记录一次所选来源。监听器模式由 `common/traffic.go` 包装主监听器接受的连接：Read 计入入站，Write 计入出站，包含 HTTP 头、正文、WebSocket 帧和健康检查，不含应用主动连接上游的流量和 pprof `:8005`。连接包装内嵌 `net.Conn` 接口值，只覆盖 Read、Write、CloseWrite，不提升 `ReadFrom`、`WriteTo` 等绕过计数的方法；`n>0` 时即使同时返回错误也累计，错误原样返回。

方向统一按服务器视角：`inbound_mbps` 为宿主 rx 或连接 Read，`outbound_mbps` 为宿主 tx 或连接 Write，Mbps = Δ字节 × 8 / 秒 / 1e6。前端两页按此显示 ↑ 出站、↓ 入站，看板原先的 ↑ 请求随之翻转。`observability.traffic` 和 `series.points[]` 的 `request_mbps`、`response_mbps` 改名为 `inbound_mbps`、`outbound_mbps`，`application_http_body` 退役，不保留别名，已打开的旧页面需刷新一次。Root 的 `/api/performance/stats` 另带 `interface`（宿主模式为配置的网卡名，监听器模式为空串）和 `reason`；看板 `/api/user/system-status` 的 `traffic` 逐字段投影 `scope`、两个速率、`bucket_seconds` 和 `points`，不带这两项。基线在首次采样、计数下降、时钟回拨、间隔超过 30 秒或本次带 reason 时重置，恢复后的第一份正常计数只建立基线。快照过期时 `stale` 仍清空两个速率。

原生文件：`main.go` 把 `srv.ListenAndServe()` 换成 `net.Listen` 加 `srv.Serve(common.CountingListener(ln))`，与 go1.26.10 的 `ListenAndServe` 等价，失败文案不变；附带变化是端口被占用时在启动成功日志之前退出。`main.go` 原已 adapted，reasons 增加 `ops-metrics`。`router/main.go` 删除 `middleware.RecordApplicationTraffic()` 后与 `e449cc700` 一致，回到 `upstream_equal`。`middleware/application_traffic.go`、`middleware/application_traffic_test.go` 和 `common/application_traffic.go` 删除，SSE、WebSocket、partial I/O 合同迁到 `common/traffic_test.go` 的真实连接测试。`controller/relay.go`、`controller/responses_websocket.go`、CPU/内存/磁盘采样和过载判断未改。`.gitignore` 增加 `docker-compose.override.yml`：安装文档让部署方在其中启用宿主网卡计数，它是本地部署文件，不应提交（`.gitignore` 不在来源清单范围内）。

已知限制：宿主读数是整块网卡的收发，中转请求的客户端和上游两段都计入；同名网卡在两次采样之间被重建且新计数已超过旧值时无法识别，会出一个错误点；非 Linux 上显式配置宿主模式会报 `host_counters_unreadable`。计数文件以非阻塞方式打开，只读普通文件且最多 1 MiB：FIFO、设备等报 `host_counters_unreadable`，超过上限报 `host_counters_invalid`，路径配错不会阻塞或拖垮采样（CPU、内存、存储观察共用同一采样循环）。宿主配置无效时启动日志不再打印“traffic from host interface”，由首次采样记录一次原因。observer 早于监听器启动，监听器模式下这段时间计为真实 0。2026-10-24、2026-10-26 条目中的“HTTP body 吞吐”“应用 HTTP 正文流量”描述当时状态，不改写。

来源清单已用 `scripts/refresh-core-source-delta.py --upstream-repo /home/work/reference-projects/new-api` 重算，计数为 800 `upstream_equal`、275 `adapted`、255 `extension`、45 `removed`，`candidate_sha` 为空。

## 2026-10-31 Dashboard 系统状态卡片口径统一

系统状态卡片的格子颜色和标题胶囊改用同一套阈值判定。`service/dashboard_system_status.go` 把 `observationHealth` 对 CPU、内存、磁盘的检查结果写入投影的资源 `state`：已测量的资源超过阈值时为 `warning`，阈值关闭时仍为 `ok`，读数保留。前端只按占比显示空闲、正常、关注三档，警告和危险两档只来自 `state`，不再按固定的 75% / 90% 告警。资源检查目前只产出 `warning`，所以资源格子最高到警告档，以前超过 90% 显示的红色不再出现。内存和磁盘格子改用后端的 `percent`，不再由字节数自行换算。`service/performance_observation_health.go` 的资源阈值比较改为浮点比较，90.6% 在 90% 阈值下判为 warning。原生 `common/system_monitor.go` 的过载熔断仍沿用上游的整数比较，从 91% 起才拒绝请求，该文件未修改。

投影新增 `storage_status` 字段，取主库、独立日志库和已启用 Redis 探测中最差的结果，取值为 ok、critical、stale、unknown；主库没有检查结果时为 unknown，不报 ok。这样能说明卡片上没有对应格子的严重状态，胶囊显示“存储异常”。探测名称、原因、延迟和阈值数值仍只在 Root 的 `/api/performance/*` 返回。宿主机负载格最高只到关注档，浮层和读屏文案标明“宿主机平均负载”。超过核数仍在浮层内用危险文字色标出。

视觉审计后的修正：阈值关闭的资源投影为 `unchecked`，读数保留但按中性色显示，替代原先的 `ok`；资源监控关闭（`disabled`）的格子显示“未启用”。成功率不参与标题胶囊判定，维持原设计，胶囊只反映本实例的健康检查。标题区的胶囊和成功率不再换行，窄卡片截断标题，各状态标题区高度一致。TiB 读数在 10 以下保留两位小数，930 / 1000 GiB 显示为 0.91 / 0.98 TiB，与 93% 一致。其余修正：过期胶囊的悬停与读屏文本说明哪些读数过期；加载和读取失败时成功率显示“--”；英文 Loading 改为首字母大写；代理错误、网络错误和响应格式错误改用界面语言文案，服务端业务错误原样显示；成功率查询失败用警告文字色；读不到核数时负载格按未知样式显示；缺字节数时内存和磁盘标题显示百分比；四个格子的占位字号统一。

配色复核后的修正：CPU、内存、磁盘的档位颜色由 `frontend/src/utils/loadTone.ts` 一处计算，仪表盘格子和 Root 运维页资源仪表共用。Root 仪表原先正常一律用 `--glow`、告警用全局状态色，现在按同一占比色阶和阈值判定上色，过期和未知不再借用告警色。标题胶囊的正常、告警、严重和读取失败改用对应档位的 `--load-*` 颜色，与格子同色。日间阈值关闭的读数改用新增的冷石板灰 `--load-unchecked`，原先的暖灰离正常档太近（ΔE 4.7）。告警和严重格子的悬停与读屏文字带判定（如“容器，超过告警阈值”），档位不只靠颜色表达。内存没有读数时十段画成虚线，不再和 0% 一样。Root 资源趋势线改用色阶以外的 `--trend-{cpu,memory,disk}`。Root 状态区的整体状态标签和检查项圆点仍用全局状态色，统一需要改全局 `--status-*`，本次未做。

## 2026-10-31 删除内置模型价格

按用户要求删除内置模型价格：模型倍率、按次价、补全、缓存、缓存写入、图像、音频的默认表，DeepSeek 峰谷、`gpt-6-astra`、`gpt-image-2` 等内置计费表达式。工具调用的内置单价按用户要求保留上游原文，数值照搬，智谱、阿里搜索档位的人民币标价同样不换算。模型价格只来自管理员保存的十一项配置，删除或清空某个模型即变为未定价，请求被拒绝。`POST /api/option/rest_model_ratio` 与 `CompletionRatioMeta` 选项随之删除。前端同时修正：清空确认文案说明保存后模型变为未定价；公开倍率提示改为实际路由 `/api/ratio_config`；模式筛选的"全部"不再复用条件树的"全部满足"。

计费行为变化：未配置的补全倍率为 1，管理员保存的 `CompletionRatio` 总是生效，旧的模型锁定规则删除；未配置的缓存写入倍率由 1.25 改为 1；音频计费见本节末尾，合并时已恢复为上游；OpenRouter Claude 不再由 `usage.Cost` 反推缓存写入 Token。保留自用模式和 `AcceptUnsetRatioModel` 的 37.5 兜底、Claude 1h 与 OpenAI 12h 缓存写入的固定乘数、DALL-E 尺寸规则。完整规则见 `docs/billing/PRICING.md`。

原生文件：`setting/ratio_setting/model_ratio.go`、`cache_ratio.go`、`setting/billing_setting/tiered_billing.go`、`setting/operation_setting/tools.go`（只删除末尾 Gemini 音频固定价，工具单价与上游一致）由 `upstream_equal` 变为 `adapted`，`builtin_billing.go` 及其测试删除；`controller/option.go`、`controller/pricing.go`、`main.go`、`router/api-router.go`、`middleware/audit.go`、`middleware/access_token_routes.go`、`relay/helper/price.go`、`service/quota.go`、`service/text_quota.go`、`model/model_pricing_config.go`、`model/model_pricing_conversion.go` 原已 adapted，均新增原因 `no-builtin-prices`。

同步成本：截至 `e449cc700`，近 6 个月（2026-04-28 起）上游提交数为 `model_ratio.go` 13、`relay/helper/price.go` 17、`service/text_quota.go` 23、`service/quota.go` 19、`model/model_pricing_config.go` 8、`tiered_billing.go` 7、`tools.go` 6、`cache_ratio.go` 5、`builtin_billing.go` 4。其中多数提交为新模型添加默认价格或新计费维度（GPT-5.6、Claude Opus 4.8、DeepSeek V4.1、Gemini 搜索计费、OpenAI 12h 缓存写入）。以后同步这些文件须逐提交手工合并：丢弃模型默认价格表与内置表达式的增删，只吸收计费维度和计算逻辑；`tools.go` 的工具单价照常吸收；不得整文件复制上游。

来源清单已用 `scripts/refresh-core-source-delta.py --upstream-repo /home/work/reference-projects/new-api` 重算，计数为 798 `upstream_equal`、277 `adapted`、238 `extension`、45 `removed`，`candidate_sha` 为空；参考仓库未 fetch，HEAD 仍为 `206e0fa5c`。

合并到 main 时按用户要求恢复上游音频计费：`AudioRatio`、`AudioCompletionRatio` 的内置表及其 `InitRatioSettings`、首次写入 `defaultPricingMaps`，`tools.go` 末尾的 Gemini 音频输入固定价，`service/text_quota.go` 的原生 Gemini 音频单独计费与日志，`model/model_pricing_conversion.go` 的 Gemini/OpenAI 音频冲突判断，均恢复为上游原文；本地扩展 `model/catalog_prices.go` 的目录投影恢复为 main 中对该冲突判断的接入；`tools.go` 回到 `upstream_equal`。理由：OpenAI 兼容接口只在存在音频倍率时走音频结算，删除内置表后音频模型会按文本价少扣。OpenRouter Claude 反推缓存写入仍删除，它只对内置模型倍率生效。上述文件的音频部分以后照常吸收上游。前端定价页按基础价格判断“未定价”，仅有内置音频倍率的模型仍列为未定价；保存时，从未有美元单价的已存倍率在单价留空时原样保留；已显示单价被清空的启用项拒绝保存，不再静默删除倍率。恢复后来源清单重算为 799 `upstream_equal`、276 `adapted`、253 `extension`、45 `removed`（含用量折扣与系统状态的合并结果），`tools.go` 不再列为本地差异。

## 2026-10-30 任务渠道缺失时退还预扣额度

`service/task_polling.go` 的批量任务与视频任务轮询在 `model.CacheGetChannel` 失败时，上游用 `TaskBulkUpdateByID` 直接把这批任务标为 FAILURE，不退还预扣额度。本地由 `failTasksWithoutChannel` 逐个任务处理，写库错误合并返回。该分支于 2026-10-31 按用户“将这些分支合并到 main 上”的要求合入。

2026-11-01 用户要求核对上游是否已有方案并吸收。上游 main `d0abbf6e0d33552a41c402d9b1fadbaff27cf40d` 比 `e449cc700` 多 24 个提交，`task_polling.go` 只增加视频响应脱敏，两条渠道缺失路径仍不退款；相关请求 QuantumNous/new-api#7738 仍为 open，没有合入的修复（#7736 以 not_planned 关闭）。因此改为照搬同一文件 `sweepTimedOutTasks` 的写法：按原状态 CAS 改为 FAILURE 并写 `FinishTime`；`SubmitTime` 早于 `model.TaskRefundLegacyCutoff` 的旧任务随 CAS 清零 quota、不退款；只有赢得 CAS 的调用方在 quota 非零时执行 `RefundTaskQuota`，CAS 失败与已被推进的任务分别记日志。回归测试 `TestUpdateVideoTasksRefundsPreConsumedWhenChannelMissing` 与 `TestUpdateBatchTasksMissingChannelRefundsOnceAndKeepsLegacyRule` 覆盖两条路径、只退一次、旧任务不退款、`FinishTime` 以及并发轮询持有的旧副本不重复退款。这是与上游相同的多扣缺陷的本地修复，用户确认保留。本次只核对这一项，上述 24 个提交的其余内容未吸收。

发布前审计又复现两处资金问题，已修正。第一，`CacheGetChannel` 的任何错误都会触发判失败和退款：关闭内存缓存时一次瞬时的渠道查询失败，或内存缓存漏掉了库中仍存在的渠道，都会把仍在执行的任务判失败并退款。现在 `failTasksWithoutChannel` 先用 `model.GetChannelById` 查库，只有返回 `gorm.ErrRecordNotFound` 才判失败；渠道仍存在或查询出错时任务不变，函数返回错误，下轮轮询重试。第二，`RunTaskPollingOnce` 的 `taskM` 按上游任务 ID 建表，不区分渠道，与上游相同。两个任务的上游 ID 相同时，后读到的任务覆盖前者，而两者的 ID 都留在各自渠道的列表里：删除渠道的路径会对另一个渠道的任务做 CAS 并退款；正常轮询里，一个渠道返回的失败或结果会写到另一个渠道（或同渠道另一行）的任务上；同一列表出现两次相同 ID 时，CAS 失败后留在内存里的 FAILURE 状态还能让第二次 CAS 成功并重复退款。原生建表循环现在只收第一个出现的上游 ID（任务按 id 升序读取，即最早的任务），其余同 ID 任务本轮不轮询，等前一个结束后的下一轮，或由 72 小时超时清理按同样规则判失败并退款。这样每个上游 ID 每轮只对应一个任务和一个渠道，`failTasksWithoutChannel` 和正常轮询都不再需要单独检查渠道归属。函数移到扩展文件 `service/task_missing_channel.go`，原生 `task_polling.go` 只保留两处调用和建表循环中的三行去重。两条原有回归测试一并移入 `service/task_missing_channel_test.go`，新增的 `TestUpdateBatchTasksKeepsTasksWhenChannelReadFails`、`TestUpdateVideoTasksKeepsTasksWhenOnlyTheCacheMissesTheChannel`、`TestRunTaskPollingOnceHandlesOneTaskPerUpstreamID`（渠道已删除和渠道返回 404 两种情况）在修正前都失败。

已知限制：`controller/midjourney.go` 的渠道缺失路径未改，仍按上游 `MjBulkUpdate` 批量判失败、不退款。`model/channel_cache.go` 的 `InitChannelCache` 与上游一样忽略 `DB.Find` 的错误；启用内存缓存时，一次失败的同步会把渠道表替换为空，下次同步前上游会把在途任务判为渠道缺失；本地查库确认渠道仍存在，任务保持不变。渠道确实被删除时，上游可能已执行并计费，本地照样退款，这部分损失由运营方承担。`RefundTaskQuota` 的返回值与上游超时清理一样不检查。原生文件 `service/task_polling.go` 原已 adapted，reasons 为 `channel-ratio`、`concurrency`、`upstream-defect`。

## 2026-10-30 用量折扣

新增用量折扣：固定折扣、全站阶梯和个人日/周/月阶梯合成一个乘在渠道倍率上的系数，行为见 `docs/billing/USAGE_DISCOUNT.md`。这是 Ren2Hub 扩展，NewAPI 没有对应功能。

原生文件只做接入。`relay/helper/price.go` 的 `HandleChannelRatio` 增加 RelayInfo 参数并乘 factor，这是唯一一处乘法，三个调用点（`price.go` 两处、`relay/relay_task.go`）传入 info。`types/price_data.go` 的 `ChannelRatioInfo` 增加基础倍率和决策；`relay/common/relay_info.go`、`model/task.go` 保存决策，`controller/relay.go` 写入任务上下文。`model/user.go` 两个 used_quota 函数开头各加一行小时增量。`service/log_info_generate.go`、`service/task_billing.go` 追加日志对象，`service/violation_fee.go` 改用基础倍率。`model/main.go` 注册两张表，`main.go` 启动时加载配置，`router/api-router.go` 与 `middleware/access_token_routes.go` 登记五条路由。这 14 个文件原本就是 adapted，reasons 增加 `usage-discount`。逻辑放在 `types/usage_discount.go`、`model/usage_discount*.go`、`service/usage_discount*.go` 和 `controller/usage_discount.go`。

来源清单已用 `scripts/refresh-core-source-delta.py --upstream-repo /home/work/reference-projects/new-api` 重算，计数为 804 `upstream_equal`、273 `adapted`、252 `extension`、43 `removed`，`candidate_sha` 为空。

## 2026-10-29 渠道上游倍率

在 Ren2Hub 新增渠道上游倍率功能，只展示上游声明的分组倍率，不写 `channel_ratio`，不参与路由或计费。详细行为见 `docs/channel/UPSTREAM_RATIO.md`。

原生文件的改动：`model/main.go` 注册新表，`model/system_task.go` 新增任务类型，`controller/system_task_handlers.go` 注册每分钟运行的调度任务，`router/channel-router.go` 新增三条渠道路由。这四个文件在来源清单中已标为 adapted。新功能代码放在 `model/channel_upstream_ratio*.go`、`service/channel_upstream_ratio*.go`、`controller/channel_upstream_ratio.go` 与前端对应文件中。

参考来源：Sub2API（`Wei-Shaw/sub2api`）的上游声明倍率解析、高峰计算、退避与探测地址处理，作为参考实现移植，未建立 Git 同步关系。NewAPI 的 `/api/usage/token/` 与 `/api/pricing` 响应形状以本地 `calciumion/new-api:latest` 实例的实际返回为依据。

共享的 `common/ssrf_protection.go` 把带 zone 的 IPv6 字面量（如 `fe80::1%eth0`）当作域名，未开启域名 IP 过滤时放行，与上游 NewAPI 相同。本功能只在自己的探测拨号器里拒绝这类主机，共享文件保持上游原文。

来源清单已用 `scripts/refresh-core-source-delta.py --upstream-repo /home/work/reference-projects/new-api` 重算。为取得冻结核心对象 `e449cc700eb569cdfc2199cb73e97de46944f43c`，已在该只读参考仓库执行 `git fetch origin <sha>`，未 checkout、未 clean；参考仓库 HEAD 仍为 `206e0fa5c`。重算后计数为 804 `upstream_equal`、273 `adapted`、238 `extension`、43 `removed`，`candidate_sha` 为空。

## 2026-10-28 最终审计与验证修正

本轮以工作树当前内容重新核对 NewAPI `e449cc700eb569cdfc2199cb73e97de46944f43c`，并对 `eb169e7`、`8802113`、`fcc241cdf` 做独立只读交叉审计。Git 历史确认 `6cd0248` 将旧 PowerShell 采集器迁移为 Python 版本，并非误删；技能入口已补充从工作区根目录运行的明确路径。

验证器修正：数据库测试发现器现在纳入未跟踪且未被 `.gitignore` 排除的测试文件；共享端点测试的 MySQL 表名前缀缩短到数据库标识符限制以内。此前数据库报告因缺少未跟踪测试而失败，路由矩阵因 MySQL 标识符过长而失败，均不作为成功证据。Codex `gpt-6-astra`（xhigh）复审确认三项 P2：插件准备阶段替换模型名后会丢失原始 `Stream`/`StreamFormat`，导致重选使用非流式观测；参数覆盖正则缓存按管理员输入无限累积；来源清单的哈希及分类未反映最新工作树。现已在共享端点选择上下文中保留完整 `ModelRequest`，以 256 条、256 KiB、单条 4 KiB 上限的并发安全 FIFO 缓存替代无界 `sync.Map`，并重新生成来源清单；各项均有测试或生成器核验。

来源清单已在源码冻结前重生成：NewAPI `e449cc700eb569cdfc2199cb73e97de46944f43c`，计数 804 `upstream_equal`、273 `adapted`、229 `extension`、43 `removed`；清单哈希覆盖当前工作树，`candidate_sha` 在提交前仍为空。

源码冻结后完成全套本地验证与隔离浏览器验收；应用提交 `b39843c6977a21d0309858ec6c10d09d615675d4` 已推送内部 bare、构建候选并完成正式普通更新。

## 2026-10-28 延期项与 main 新增 2 提交吸收

本节吸收上一批延期的 `eb169e7`、`8802113`，以及 NewAPI main 在 `b08ee86` 之后新增的 `fcc241cdf`、`e449cc700`。来源清单基线随之改为 `e449cc700eb569cdfc2199cb73e97de46944f43c`。本节验证与发布结果只记录 Ren2Hub 本地事实，不向 NewAPI 报告。

| NewAPI 完整 SHA                            | 处置           | Ren2Hub 落点、证据与边界                                                                                                                                                                                                                                                                                                                                                                                                 |
| ------------------------------------------ | -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `e449cc700eb569cdfc2199cb73e97de46944f43c` | adapted        | `pkg/perf_metrics/metrics.go` 新增 `RecordImmediateTaskResult`，去掉上游 `Group` 维度；`controller/relay.go` 在结算成功后调用。即时成功的任务不进入轮询终态，原先没有性能样本。新增 `controller/relay_task_metrics_test.go`：已结算的即时成功恰好 1 个样本，即时失败、异步提交和结算失败为 0。                                                                                                                           |
| `fcc241cdf8ae0a7ba54107a256b5b0167d5a4fdc` | not applicable | 上游把重试次数当作优先级档位，亲和占用一次后跳过最高档。本地亲和只在第 0 次尝试生效（`service/channel_select.go`），重试时 `routeCandidates` 每次按优先级重排并排除已用渠道，不读重试次数，该缺陷不存在。                                                                                                                                                                                                                |
| `eb169e74d772afdb281970d0cbe0b62fbf9fa994` | adapted        | 后端 `controller/ratio_sync.go` 加 `include_unchanged`，表达式优先改按本地或上游 `billing_mode == tiered_expr` 判断，无上游价格的行不返回；本地固定价来源钩子先读请求体再复位，`include_unchanged` 不影响其分派。`ratio_sync_test.go` 与上游一致。Vue `PriceFetchDialog.vue` 每行可选保持、除以或乘以站点汇率，并可批量设置；换算放在 `features/pricing/priceAdjustment.ts`（移植上游 `upstream-price-adjustment.ts`）。 |
| `8802113263b8a94bf8957a1231b4ec04705d7dd9` | adapted        | 普通渠道与插件渠道在共享端点（Responses、images、videos）竞争；插件自有原生路由仍由插件独占。同时带入价格守卫、单插件定价展示、按张图片可视化定价、普通价配置和插件 API 文档段落。                                                                                                                                                                                                                                       |

汇率调整只换算以站点币种报价的来源，保存值始终是 USD 基础价，计费时照常再乘 `ChannelRatioInfo`，不会重复计价。旧价格只改 `ModelPrice`，没有时改 `ModelRatio`；`CompletionRatio`、缓存和音频倍率等相对倍率不变。表达式只改 `tier()` 价格叶和 `fixed()`，阈值、零价、版本前缀、`image_count` 和请求条件倍率保持原文。条件里调用 `tier`/`fixed`、非线性运算或无法识别的结构一律拒绝，非法汇率、溢出或下溢也拒绝；任一选中行出错时禁止填入。表格显示的“最终价格”就是 `fill()` 写入草稿的值，仍须走现有保存流程。

共享端点路由按本地合同改写，Token 自动路由与有序手动路由保持完整：

- `RouteTaskPluginEndpoint` 用 `SelectChannelForRequest` 预选：普通渠道胜出时原始请求体直达普通 relay，不跑插件解码、模式检查或模型改写，并加 relay-only 过滤，重试不会落到插件渠道；插件渠道胜出时沿用插件路径，重试限于其接受的候选。手动路由按列表顺序决定两类渠道，先于优先级。
- 共享端点先完成用户并发准入，再进行渠道预选；预选拿到的渠道许可立即释放。`Distribute` 复用已完成的用户准入，并通过 `ReacquireSelectedRouteChannel` 在当前过滤条件下重新校验和取得渠道许可；渠道已满或不再符合时丢弃预选的亲和记录（`forgetChannelAffinityUse`），然后正常重选。有 pin 时不复用预选结果，与上游相同。
- 预选失败且不是“无可用渠道”时（429 或 5xx，例如渠道全忙、strict 亲和或加载失败），直接返回该错误，不再让插件解码错误掩盖它。“无可用渠道”仍回落到插件路径，与上游相同。

价格守卫 `relay/helper/price.go` 逐字吸收：普通请求遇到含 `u()` 的表达式，或表达式仍被该端点的任务插件继承，在预扣前返回 400 `model_price_error`。这关闭了一条与上游 `b08ee86` 相同的少扣路径，即条件回退式任务表达式被当作每百万 token 价格。行为变化：非任务模型配置含 `u()` 的表达式时，普通请求和渠道测试都返回 400。`model/model_pricing_config.go` 改为只有一个插件时也展示插件变体。

Vue 编辑器：`visual.ts` 识别 `<pricing> * image_count`，可视化编辑保留数量乘数，加条件时用括号包住整个分支；按张计价时 `PricingTier.vue` 显示“/ 张”“每张图片价格”。`ModelPricingEditor.vue` 的“配置普通 OpenAI 价格”先把当前表达式写入每个仍在继承的插件覆盖，再把模型默认值换成普通价：模型或插件 schema 有 `image_count` 计数时为 `tier("image", fixed(0)) * image_count`，否则为 `tier("base", p * 0 + c * 0)`。占位 0 与上游相同；本地另加保存拦截：表达式仍是该占位时提交报 `ordinaryPriceRequired`，需免费须显式改写。上游 React 可直接保存该 0 价，本地为关闭少扣而偏离。默认表达式含 `u()` 且所有插件已有独立价格时也显示该按钮，对应上游 `usesTaskModelPricing` 对 `u()` 的判断。

统一本地验证已完成（各报告绑定源码指纹 `e2a6af9b7d146b05183ddfaddbce7ac04d08d1e2e98ce72ac731ce80fc78a3d1`；原始报告包已于 2026-10-29 清理）：Go 单测 167 批、6,422 个事件通过，0 skip；Vue 268 个文件、1,869 项通过；静态检查含 typecheck、ESLint、格式检查和 `go vet` 通过；嵌入构建 10 项通过，二进制指纹为 `d488de0f300117522f514bb33ce43a6c136366a0cc3a9d3217efa22c3959bcc9`；三库矩阵 72/72 suite 通过、1,221 个方言用例通过、0 skip/fail；race 3 suite、30 项通过；UI 合同 489 项通过；独立浏览器 4 个隔离环境（desktop/mobile × light/dark）均通过。候选验收与正式普通更新已完成：候选七阶段全通过且 `deployment_eligible=true`；运行身份、备份与 HTTPS 核验记录在本节末。

- 回归边界：共享端点同时遇到用户并发已满和所有候选渠道已满时，必须返回 `user_concurrency_exceeded`，且用户与渠道许可都只释放一次；Token 自动/手动路由、普通/插件竞争、价格守卫、即时任务采样和汇率转换均保留既有测试。
- 来源清单以 `e449cc700` 刷新，计数为 804 `upstream_equal`、273 `adapted`、229 `extension`、43 `removed`；当前共享端点路由文件因 Token 路由和并发适配记为 `adapted`，清单逐文件哈希已重算。两个延期标签已删除；`i18n/keys.go`、`pkg/jsplugin/routing.go`、`controller/ratio_sync_test.go` 回到与上游一致。

交叉审计：Codex CLI `gpt-6-astra`（`model_reasoning_effort=xhigh`，只读沙箱）审阅了本节本地 Go/Vue 差异、新测试和四个上游补丁，共报告 4 项，处置如下。

- 中，已确认：“配置普通 OpenAI 价格”后直接保存会存成 0 价。已按上段加保存拦截并补测试。
- 中，推测：共享端点预选在插件解码之前，普通渠道胜出时不会建立 origin-task pin。上游 `8802113` 的 `RouteTaskPluginEndpoint` 顺序相同，其“origin task still pins”用例同样只覆盖 `pluginOnly`；普通 relay 不读取插件任务 ID，这条路径不涉及计费。按上游一致保留，不修改。
- 低，已确认：所有插件都有独立价格时，按钮被隐藏。默认表达式含 `u()` 时已改为显示，与上游 `usesTaskModelPricing` 一致。其余情况仍要求至少一个插件在继承，因为此时模型默认值不被任何插件使用，可直接按普通价编辑。
- 低，推测：`image_count` 只识别根级乘法。上游 `imageCountPricingRoot` 同样只识别根级，嵌套形式退回文本编辑，不会改写错误。保留，不修改。

本轮 xhigh 复审发现的三个 P2 已处理：共享端点上下文新增完整 `ModelRequest`，插件模型解析后仍沿用原始流式维度；`relay/common/override.go` 改为并发安全的有界 FIFO 正则缓存，并覆盖条目、总字节和超长规则边界；来源清单重新生成，逐文件哈希与当前源码一致。路由流式回归、缓存上限回归和上述全量验收均已通过。最终 Codex `gpt-6-astra` xhigh 复审未发现新的 P0/P1/P2；仅建议补充 `Stream=false, StreamFormat=sse` 与总字节压力/并发 race 两项 P3 覆盖，当前不影响发布，留作后续测试增强。

Codex 确认价格换算、对话框显示值与 `fill()` 一致、ratio sync 优先级、价格守卫和即时任务采样无问题。审计环境没有 Go，未运行测试，以上 Go 验证由本机完成。

正式发布结果：Ren2Hub `b39843c6977a21d0309858ec6c10d09d615675d4`，部署时根源码 `087c58472c6135b3b1583c7412e7b90da8cfa60d`；候选 `run-_bjz7e91` 七阶段及 HTTPS 只读报告通过，镜像 `sha256:bb1de3083df0305d0c82822cd85ee3fafe2f63e9e0c12f13fcc6b3e963584712`，2026-10-28 22:42 UTC 更新正式应用。备份为 `/srv/ren2hub-preview/backups/upstream-sync-b39843c6-20261028T224200Z/`。本段发布结果在部署后以文档提交补记，未改动运行源码或已部署镜像。

## 2026-10-28 同步窗口完整审计

重新拉取 NewAPI 后 main 仍为 `b08ee86fe2a899dcf9060d8cde7858c93c2b770c`，latest 仍为 `v1.0.0-rc.47`。对 `206e0fa5c600127a314a97f88592c067f784f4a6..b08ee86fe2a899dcf9060d8cde7858c93c2b770c` 共 39 个提交按流式、计费、任务插件、Vue 四个区域逐提交复核，并逐文件比对上游累计差异。无 P0/P1。与上游 `b08ee86` 行为相同的项目不做本地偏离，下列只记录本地问题及修复。修复未提交、发布或部署，`candidate_sha=null`。

| 级别 | 问题                                                                                                                                                                                                            | 修复                                                                                                                                                                                                                                              |
| ---- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P2   | `relay/channel/openai/relay_responses.go` 在完成事件前已有写错误时不调用 `sr.Done()`（本地多出 `!info.StreamStatus.HasErrors()`），上游未 EOF 时该流以超时结束；本地测试夹具缺上游的取消/清理同步，掩盖了差异。 | 文件恢复为上游原文；`relay_responses_billing_test.go` 移植上游 `waitForCleanup` 同步，"完成事件前协议错误"用例改为不含上游 EOF，旧实现下得到 `timeout` 而失败。                                                                                   |
| P2   | Vue 日志模型核对读取不存在的 `other.response_model_name`，后端写入的是 `other.response_model` 对象（`requested_model`/`upstream_model`/`returned_model`），返回模型不一致提示从不出现。                         | `frontend/src/utils/logModel.ts` 读取 `response_model`；单测与视觉夹具改用后端结构，旧实现下单测失败 2 项。                                                                                                                                       |
| P2   | 模型广场未映射后端价格目录的 `cache_create_12h`，12h 缓存写入价格显示原始键名；可视化定价层级的变量下拉缺 `cc12h`。                                                                                             | `modelMarket.ts` 增加 `cacheWrite12h` 维度与中英文文案；`PricingTier.vue` 下拉改由 `TOKEN_VARIABLES` 去掉 `len` 生成；补 ModelCard 与 ModelPricingEditor 用例，后者旧组件下失败。                                                                 |
| P2   | `docs/plugin-api/v1.md`、`v1.d.ts` 仍停在 `206e0fa`，未随 `556517fa5`、`dbe97beb9`、`db80ac8e0` 更新，仍描述已删除的宿主递归 duration 校验。                                                                    | 按本地实现写入 multipart 原生路由、插件自校验与宿主 usage facts 上限、`duration-auto@1` 无效果；请求头按本地 allowlist 描述，未发送的头省略。`8802113` 的共享端点段落当时随延期未带入，后已补入（见"2026-10-28 延期项与 main 新增 2 提交吸收"）。 |
| P3   | `.github/workflows/release.yml` 漏改 `7d8b91268` 的 `go-version-file: go.mod`。                                                                                                                                 | 三处改为 `go-version-file: go.mod`。                                                                                                                                                                                                              |
| P3   | `service/tiered_settle.go`、`model/model_pricing_conversion.go`、`pkg/billingexpr/types.go` 的注释与上游措辞漂移，逻辑一致。                                                                                    | 恢复上游注释原文。                                                                                                                                                                                                                                |

验证记录（Go `go1.26.10 linux/amd64`，`GOTOOLCHAIN=local GOWORK=off`，后端均 `-timeout 60s`）：

- `./relay/channel/openai`、`./model`、`./pkg/billingexpr` 全包与 `./service -run 'Tiered|TextQuota'` 通过；根模块 `go build -p=2 ./...`、`go vet -p=2 ./...` 通过。Responses 生命周期用例 `-count=20` 通过；用 `-overlay` 换回 `0837a03` 的 `relay_responses.go` 时新用例失败。
- `golang:1.26.10-bookworm` 容器（断网、源码只读）中 `-race`：`./relay/channel/openai -run Responses` `-count=1` 通过，生命周期用例 `-count=20` 通过。`-count=3` 下 `TestResponsesToGeminiStreamObservesFirstOutput` 因进程级计数累积失败，与上一批记录相同。
- Vue：logModel、ModelCard、ModelPricingEditor、modelMarket API 与 `src/features/pricing` 共 8 文件/86 项通过；typecheck、改动文件 ESLint/Prettier 与生产构建通过。`tests/visual/log-response-model.spec.ts` 为 Playwright 用例，按主机资源限制未运行。
- 来源清单以 `b08ee86` 刷新：`relay/channel/openai/relay_responses.go` 由 adapted 转为 upstream_equal，计数为 804 upstream_equal、273 adapted、227 extension、43 removed。

## 2026-10-28 NewAPI main 7 提交吸收

以 Ren2Hub `0837a03c77ea0d5b7618588d4c9e77781f656590`（上一批 32 提交的源码提交）为基线，对照 NewAPI `34003e9c2eed2c20e617f583fdfe54bf1bf42237..b08ee86fe2a899dcf9060d8cde7858c93c2b770c` 的 7 个提交。7 项都在 rc.47 之后，官方 latest 仍为 `v1.0.0-rc.47`。上游 `web/` 的 React 文件不适用，由 Vue 对应实现。本批源码未提交、发布或部署，`candidate_sha=null`。

| #   | NewAPI 完整 SHA                            | 处置     | Ren2Hub 落点、证据与边界                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| --- | ------------------------------------------ | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `01da3a55f838cfa21010b05c7451c0a42cf5a173` | adapted  | 上游按模式文本共享编译结果，规则来自渠道参数覆盖的 `from` 和条件 `value`，不取自请求体。此前无界 `sync.Map` 会随管理员输入的不同模式永久累积；本地保留复用行为并改为 256 条、256 KiB、单模式 4 KiB 上限的并发安全 FIFO，超长模式不缓存。`TestParamOverrideRegexCacheIsBounded` 与超长模式用例保护上限。W1、W3。                                                                                                                                                                                                                                                                                          |
| 2   | `ad561128202c0a1f0a547df6c283d1efeb803692` | absorbed | RelayKit `GetOpenAIChatCapabilities`：`gpt-6.1-sol` 不保留采样参数，`gpt-6.1-terra` 仅显式 `none` 时保留，两者改用 `max_completion_tokens`。本批不增加 6.1 默认价格。W1、W2。                                                                                                                                                                                                                                                                                                                                                                                                                            |
| 3   | `54788117d6574edcabbff2aa61d838f4d85616eb` | absorbed | `pkg/perf_metrics/outcome.go` 原文吸收。分类结果同时进入本地自动路由熔断和观测：非流式响应写完后连接被关闭，现计成功样本。Cloudflare、Coze、Ollama、Zhipu、Cohere、Tencent 的流式处理器不设置 `StreamStatus`，客户端取消且处理器无错误时也会计成功，压低错误率。全局性能指标会为这类成功样本记录截至取消时的 `LatencyMs`（已有首个输出时含 TTFT），使延迟偏低；自动路由观测只在观测到首个输出时记录延迟，Zhipu、Cohere 会观测输出，其余四类不产生路由延迟样本。预扣、结算和日志写入不受分类影响。按批准保持上游口径，不另做本地偏离。新增 `service/route_attempt_test.go` 回归，旧分类器下失败。W1、W3。 |
| 4   | `49271d2d8b5fd85c6d69e3947594ac79804ba31f` | absorbed | `relay/channel/aws/dto.go` 保留 `safeguards`；无该字段的旧请求出站字节不变。W1、W3。                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| 5   | `55515dbae24179e7dd9ba332adfa43c2961521f5` | absorbed | 公共流扫描器仅在内容精确为 `[DONE]` 时结束：无前缀 `[DONE]` 现结束流，`data: [DONE]extra` 现作为数据交给处理器。本地写入预算、输出观测和读取错误分类不变；各渠道自带的扫描逻辑不在本提交范围，例如 `relay/channel/openai/chat_via_responses.go` 仍只认 `data:` 前缀行，与上游 `b08ee86` 相同，该路径靠 `response.completed` 等终止事件结束。W1、W3。                                                                                                                                                                                                                                                     |
| 6   | `26c488c4476aa7d8f7e8afc33421b70ee8e9f617` | adapted  | 后端 `FilterResponsesWebSocket` 接受 Azure，默认仍关闭；Vue `ChannelFormModal.vue` 的 Responses WebSocket 开关加入类型 3，补 Azure 开启与非支持类型隐藏用例。Vue 没有 Azure Responses API 版本输入，WebSocket 与 HTTP 一样使用默认 `preview` 或渠道 API 版本。W1、W3、W4。                                                                                                                                                                                                                                                                                                                               |
| 7   | `b08ee86fe2a899dcf9060d8cde7858c93c2b770c` | absorbed | Cloudflare embeddings 改走 `openai.OpenaiHandler`，保留向量与额外字段。扣费结果变化：改按上游 `prompt_tokens` 计费，上游无 usage 时才回退输入估算。W1。                                                                                                                                                                                                                                                                                                                                                                                                                                                  |

验证记录（Go `go1.26.10 linux/amd64`，`GOTOOLCHAIN=local GOWORK=off`，后端均 `-timeout 60s`）：

- W1：`./relay/common ./pkg/perf_metrics ./relay/channel/aws ./relay/helper ./relay/channel/cloudflare ./model ./service ./relay` 通过；controller 定向 GPT-6.1、采样参数和 Cloudflare embedding 4 个用例通过；上游新增的正则、分类、safeguards、`[DONE]`、Azure 选择与握手测试均实际执行。新增路由回归用 `-overlay` 换回旧 `outcome.go` 时失败，换回后通过。
- W2：RelayKit `go test ./...` 与 `go build ./...` 通过。
- W3：根模块 `go build -p=2 ./...`、`go vet -p=2 ./...` 通过；`./relay/... ./pkg/... ./service/... ./model/... ./middleware/... ./common/... ./setting/... ./plugins/...` 全部通过。`golang:1.26.10-bookworm` 容器（断网、源码只读）中 `-race -count=1`：`relay/helper`（`-parallel=1`）、`relay/common`、`relay/channel/aws`、OpenAI Responses 定向、service/relay 路由与 WebSocket 定向、controller 两个 WebSocket 结算用例通过。`relay/helper` 默认并行下的 `logger.logHelper` 竞争及 `TestPerformanceAggregationAndFlush` 的测试写入竞争，在基线 `0837a03` 导出快照上同样出现，不由本批引入，未计通过；`-count=3` 下 `TestSSEHelpersMeasureEffectiveOutputInsteadOfControlFrames`、`TestResponsesToGeminiStreamObservesFirstOutput` 因进程级计数累积失败，`-count=1` 通过。
- W4：Vue Vitest 全量 267 文件/1856 项、typecheck、目标 ESLint/Prettier 与生产构建通过。
- 来源清单以 `b08ee86fe2a899dcf9060d8cde7858c93c2b770c` 刷新，计数仍为 803 upstream_equal、274 adapted、227 extension、43 removed；本批文件两侧同步移动，原有适配理由仍适用。完整审计后计数见上一节。
- 交叉审计：Codex CLI 0.165.0 `gpt-6-astra` 只读审计工作树差异、上游补丁和 7 个提交，无 P0/P1。其提出的正则缓存无界增长已在 2026-10-28 最终审计中按上游基础实现作适配修复（见上文），不再作为“账本表述不准”保留；`#3` 延迟样本的时序与重复计数担忧经本地调用链核对后记为无重复。Responses 缓冲扫描器不认裸 `[DONE]`、正则结果用例不能区分缓存前后两项与上游原文一致，不做本地偏离。审计沙箱无法写 Go 构建缓存，未独立运行测试；本批无代码改动，W1–W4 结果沿用。

## 2026-10-27 NewAPI main 32 提交吸收

2026-10-28 源码修复、适用验证和最终独立复审已完成；源码验收通过，已提交为 Ren2Hub `0837a03c77ea0d5b7618588d4c9e77781f656590`，未推送、发布或部署。以下各段中的"未提交"描述的是提交前的状态。

随后一次交叉审计发现日志页未读取后端 `cache_tokens`，且 OpenAI 缓存明细被重复计入总量；现已修复字段映射，并按后端显式输入总量及 Anthropic 用量语义计算总量和命中率。同步清单补记任务插件请求头 allowlist 与流式输出观测两项适配理由。针对性 UI 测试 12/12、typecheck、ESLint、Prettier、前端生产构建和 `git diff --check` 均通过；改动仍未提交、发布或部署。

按用户批准的计划，从 Ren2Hub 工作树基线 `ae9be9d32116e7511633dcf3c7e6814cd5fc0737` 对照 NewAPI `206e0fa5c600127a314a97f88592c067f784f4a6..34003e9c2eed2c20e617f583fdfe54bf1bf42237` 的 32 个提交。rc.47 分界是 `e835b1bfc43b565d625537830dbfb19328925134`；其前 26 项至该 Axios 提交，后 6 项为缓存计费、协议及缓存测试更新。没有合并上游分支；钱包预扣/结算机制、渠道倍率/并发和扩展模块合同保持，缓存计费结果的变化见下文。本轮源码尚未提交、发布或部署。

| #   | NewAPI 完整 SHA                            | 处置            | Ren2Hub 落点、证据与边界                                                                                                                                                                                                                                                                                                       |
| --- | ------------------------------------------ | --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | `eea05e010c7f212e1a617638fe27164f24d9478d` | absorbed        | `go.mod`、`go.sum` 升级 `klauspost/compress` 到 1.18.7；V1、V3。                                                                                                                                                                                                                                                               |
| 2   | `4d36f2c5245dd6676075be1c4f668dfe56d3340a` | not applicable  | 上游只升级 `/web` 的 Vitest 5；Vue 独立使用锁定的 Vitest 4.1.11，本轮不迁移测试框架主版本；V7 验证现行 Vue 栈。                                                                                                                                                                                                                |
| 3   | `7d8b91268dab69d7150f345c448b5e250163146a` | adapted         | Go、Docker/开发镜像、RelayKit、CI 与本地测试统一 Go 1.26.10；race 测试固定本地工具链版本；V3、V4。`release.yml` 的 `go-version-file: go.mod` 在完整审计中补齐。                                                                                                                                                                |
| 4   | `0c2af9780e45f6f9073528fb0fcbdcd64b5d9495` | adapted         | Responses 完成事件、错误写入/Flush、取消及结算生命周期；V1、V4、V6。完整审计后 `relay_responses.go` 与上游原文一致，去掉本地 `HasErrors` 条件，测试夹具同步上游。                                                                                                                                                              |
| 5   | `7e6596728ff640392bd77169ebb6137a8fe0c309` | adapted         | `relay/helper/stream_scanner.go` 集中写入预算并覆盖慢客户端、SSE/Responses/WebSocket；真实读取错误与关闭清理错误分开；V1、V4、V6。                                                                                                                                                                                             |
| 6   | `94cc3ff0906db37703d5ce2bc6a479209b7b9b23` | adapted         | Claude 缓存写入未拆分时保留可计费用量；扩展 DTO、merge、变量、结算快照与日志；V1、V2。                                                                                                                                                                                                                                         |
| 7   | `556517fa57d255c73428325f6a094fd8b4cab76c` | adapted         | 原生任务路由支持 JSON/multipart；模型限定路由仅 JSON、query 无请求体、urlencoded 拒绝；文本字段受全局请求体限额约束，保留部件/文件/UTF-8/临时文件检查；V1。插件 API 文档在完整审计中补齐。                                                                                                                                     |
| 8   | `dbe97beb9b06e8a4e23b5b8f009d882cc49f70bb` | adapted         | 插件请求头改安全 allowlist（`Accept`、`Accept-Language`、`Content-Type`、`Prefer`、`User-Agent`、`X-Request-Id`），未发送的头省略，不透传认证、Cookie 与代理凭据；V1。插件 API 文档按 allowlist 描述。                                                                                                                         |
| 9   | `db80ac8e0c4063e67938cf651c240c69f4f4cf54` | adapted         | 移除宿主对业务请求字段的递归计费校验；8 个任务插件在提交前校验自身上游参数，usage facts 仍由宿主限制；V1。Jimeng S2 固定 5 秒、Doubao 帧数向下取整沿用插件现有合同。Kling 出站请求写回 `model_name`/`model`，`mode` 按解码请求选择，使计费与上游请求使用同一模式。插件 API 文档在完整审计中补齐。                              |
| 10  | `e2321a5a9f590e8398868cb7ef56af3d833faa07` | not applicable  | 仅上游 React locale 排序；Ren2Hub Vue 不带回 React 文案体系。                                                                                                                                                                                                                                                                  |
| 11  | `eb169e74d772afdb281970d0cbe0b62fbf9fa994` | adapted         | 原记延期，已在"2026-10-28 延期项与 main 新增 2 提交吸收"一节落地：后端 `include_unchanged`、表达式优先级与空行过滤；Vue 价格同步按模型选择保持/除以/乘以站点汇率，保存值仍为 USD 基础价。                                                                                                                                      |
| 12  | `8802113263b8a94bf8957a1231b4ec04705d7dd9` | adapted         | 原记延期，已在"2026-10-28 延期项与 main 新增 2 提交吸收"一节落地：普通渠道与插件渠道在共享端点按 Token 自动/手动路由竞争，价格守卫、单插件展示、按张图片价与普通价配置随之带入。                                                                                                                                               |
| 13  | `d8e65613c09adf3663f960ea4316414a99694751` | adapted         | 后端 `other.response_model` 记录请求、上游与返回模型；上游 React 的三模型展示与复制按钮不带入，Vue 只读取该对象提示返回模型不一致（完整审计修正字段读取）。                                                                                                                                                                    |
| 14  | `d0b840e75ff36d731b04ff981ee361819acd593f` | adapted         | JSON Schema 属性顺序保真；检查本地 RawMessage/类型断言并保留 Ren2Hub 转换器约定；V1、V2。                                                                                                                                                                                                                                      |
| 15  | `e9d4d6353ce2c11df753d751ba1ad778e2433a88` | adapted         | OpenAI 12 小时缓存写入字段与计价接入，保持 `ChannelRatioInfo` 和 `prompt_cache_options`；V1、V2、V7。价格目录通过 `billing_expr.cc12h` 呈现，当前 `/api/pricing` 没有独立 12h ratio 字段；补齐上游内置表达式在普通/长上下文的 12h 计费断言（V9）。完整审计补齐 Vue 模型广场 `cache_create_12h` 维度与可视化定价 `cc12h` 变量。 |
| 16  | `7fb6e57a5ff671582131c74e00bdfaa867d4e985` | adapted         | 渠道高级自定义全局/路由级透传 roundtrip 与全路由风险提示；不放宽后端路由/转换器限制；V7。                                                                                                                                                                                                                                      |
| 17  | `30748c6558bc97de49ff74871cfec717e50f3322` | adapted         | 访问日志统一清理查询凭据；其他支付入口 URI 和 RelayInfo 调试日志复用同一清理逻辑；保留上游 logger_test.go 全部回归并增加签名/OAuth 用例；V1、V3、V9。                                                                                                                                                                          |
| 18  | `33c61a24234aa20cb10e8a644304322c476361de` | not applicable  | 仅 React 编辑抽屉状态修复；当前 Vue 页面不包含该组件。                                                                                                                                                                                                                                                                         |
| 19  | `a404460413fb7c0c2182e935bb373ba51904df70` | not applicable  | 该提交迁移上游 `/web` 到 Bun 1.4.4 与 `bun check`；Vue 仍统一固定 Bun 1.4.0，并用 vue-tsc、ESLint、Prettier 检查。按本轮既定取舍不连带迁移工具链，不能归因于 Vue 不使用 Bun。                                                                                                                                                  |
| 20  | `0034b24914981078e6ef1a3794e43afe28df43af` | adapted         | 任务超时默认改 4320 分钟，保留环境覆盖及 `0` 禁用；覆盖旧 24h、60h、正好 72h、73h、重复退款与零值；V1。                                                                                                                                                                                                                        |
| 21  | `1dafb19a429aa7c17b3f4efa457078984b6a4fd3` | adapted         | RelayKit Gemini 转换保留顶层 `imageConfig`；V2。                                                                                                                                                                                                                                                                               |
| 22  | `b6b85cae5931cd27e9adf21cfccfede800980a83` | not applicable  | 仅 React panels 依赖；未引入 React 依赖体系。                                                                                                                                                                                                                                                                                  |
| 23  | `4105bd089cce1a7c1067e77babf45ebd8d4455ec` | not applicable  | 仅 React input-otp 依赖；未引入 React 依赖体系。                                                                                                                                                                                                                                                                               |
| 24  | `0b667ca478d6dbce6f60a2e3c4a217a7aab64031` | not applicable  | 仅 React i18next 依赖；继续使用现有 Vue I18n。                                                                                                                                                                                                                                                                                 |
| 25  | `5c8032d7002f82fb04fabf9a9c9b143de78547fe` | not applicable  | 仅 React react-i18next 依赖；未引入 React 依赖体系。                                                                                                                                                                                                                                                                           |
| 26  | `e835b1bfc43b565d625537830dbfb19328925134` | already present | Vue 锁文件已使用 Axios 1.20.0；未重复改动。                                                                                                                                                                                                                                                                                    |
| 27  | `9823ebc7ea4ac7102d5d8478d395c7d7eaf6c285` | adapted         | 计入 `prompt_tokens_details.cache_creation_input_tokens`，与缓存写入拆分、缺失/显式零价格和聚合字段去重规则协同；V1、V2、V7。                                                                                                                                                                                                  |
| 28  | `bc795cc1185a0ecef0ca51f73d337667026ec62f` | adapted         | `stop` 非字符串和字符串数组的协议转换 golden 覆盖；V2。                                                                                                                                                                                                                                                                        |
| 29  | `f1569d1ea7e87cd1a73c5f89d989b6b3bdaf68b3` | adapted         | Vue 价格目录按同一分类函数驱动筛选/计数；固定层级 request、Token/混合 token、有 usage schema 的 task，解析失败回退旧 quota 类型；V2、V7。                                                                                                                                                                                      |
| 30  | `4d962e464de7a16ff8bf16c660307899d49108ba` | adapted         | Claude 工具参数数字保真；V2。                                                                                                                                                                                                                                                                                                  |
| 31  | `0165fef98d41b02a7368e89dc5fa2df651f8587b` | adapted         | Gemini 混合文本及工具调用保真；V2。                                                                                                                                                                                                                                                                                            |
| 32  | `34003e9c2eed2c20e617f583fdfe54bf1bf42237` | adapted         | 缓存统计测试以原子序列和测试名构造键，适配本地两参数缓存查询接口；`service` 测试重复 20 次；V1、V5。                                                                                                                                                                                                                           |

验证记录（Go 工具版本 `go1.26.10 linux/amd64`；未提交工作树 `candidate_sha=null`）：

- V1：`GOTOOLCHAIN=local GOWORK=off go test -count=1 -timeout 60s ./middleware ./relay/channel/task/jsplugin ./relay/channel/openai ./relay/helper ./relay/common ./relay/channel/aws ./relay/channel/ollama ./plugins ./service ./model ./pkg/billingexpr` 通过；含 12 MiB multipart 字段、文件 boundary 重建重试、插件自校验、计费与零/72h 超时回归。
- V2：RelayKit `GOWORK=off go test -count=1 -timeout 60s ./...` 与 `GOWORK=off go build ./...` 通过，覆盖 Claude 数字、Gemini 混合内容、Schema 顺序、stop 和缓存 usage。
- V3：Ren2Hub `go build ./...`、`go vet -p=2 ./...` 通过；审计后来源差异清单刷新为 803 upstream_equal、274 adapted、227 extension、43 removed。
- V4：Go 1.26.10 `golang:1.26.10-bookworm` 容器中，relay/helper、OpenAI Responses、controller 流式/keepalive/WebSocket/插件输出定向 `go test -race -count=1 -timeout 60s` 通过。首次 race 暴露 Responses 真实读取错误被关闭清理状态盖过，已修正并复跑通过。
- V5：`go test -count=20 -timeout 60s ./service -run '^TestObserveChannelAffinityUsageCacheByRelayFormat'` 通过。
- V6：controller 定向渠道价格、目录价格、插件流、SSE/WebSocket 结算和查询凭据清理回归通过。首轮全量因未配置 `SQL_DSN` 出现 options 表缺失，未计通过；2026-10-28 已以新建隔离数据库补齐本地合同，结果见 V10。没有执行真实供应商付费请求。
- V7：Vue Vitest 全量 267 文件/1854 项、定向 35 项、typecheck、ESLint、format check 和生产构建通过。首轮 8 个 Playwright 用例因环境缺少 `libnspr4.so` 未启动；2026-10-28 复用现有浏览器库重跑，报价和高级渠道两个 spec 共 8/8 通过、零跳过/零重试，覆盖桌面/手机和明/暗主题。Bun 不在本地 PATH，使用 Node 24.21.0 直接运行已有 Playwright/Vite；是受控 API 的浏览器行为/布局验证，不是像素快照或真实供应商验证。
- V8：`python3 -m unittest discover -s scripts/testing -p 'test_*.py'`，34 项通过；2026-10-28 重跑仍通过。
- V9（2026-10-28 审计增量）：common、middleware、JS task adaptor、plugins、builtin billing 全包测试通过；helper/common/OpenAI 三包完整测试、20 次定向 race 和 vet 通过。含取消即时读错、独立上游 Canceled/ClosedPipe、真正 HTTP Body.Close、完成事件与真实读错竞争、Epay GET/POST 凭据日志和先验签回归。Sora 出站 JSON/multipart 字符串与数字 facts、内置 12h 普通/长上下文精确计费断言通过。CSV 定向 3 项、目标 ESLint/Prettier、前端 typecheck 与生产构建通过。后端包均 `-timeout 60s`。最终根模块 `go build -p=2 ./...`、`go vet -p=2 ./...` 与两个生产嵌入资源合同测试通过。第二轮审计补出的畸形 query 原文回退已修复，新增 9 个无泄露回归，relay/common、middleware、controller 的日志定向测试通过。
- V10：controller 发现 523 项，非矩阵 497 项分 25 批通过；新增日志回归后 55 项相关 Stream/Responses/WebSocket/KeepAlive/Epay 再次通过，零 skip/fail。隔离三库矩阵 71/71 suite 全通过（SQLite 41、MySQL 46、PostgreSQL 46 次方言执行），零失败/零跳过，清理错误为空且本轮 label 的容器/网络均已清理。controller 最终发现 524 项，累计执行 523 个唯一顶层测试，仅 `TestVLLMStatusLive` 不执行。使用 Go 1.26.10、MySQL 8.4.11、PostgreSQL 15.19；真实 VLLM/供应商不在本地门槛内。当时的完整摘要与证据哈希已留在该批次 Git 历史；此处保留实际套件与方言执行摘要。

2026-10-28 审计后修复与来源复核：

- Sora 提交统一发送字符串 `seconds`（`"4"` / `"8"` / `"12"`），计费 facts 保持数值；JSON、multipart、默认值、duration 别名和非法类型/枚举都有回归。合同按 OpenAI Videos Create API 的字符串枚举核对，未执行真实付费调用。
- 任务 multipart 将文本字段总预算与文件驻留内存预算分开，复用标准库 FileHeader 和落盘清理；新维护点 `common/task_multipart.go`。12 MiB 文本、多个文件共享预算、解析失败、重试清理与前置读尽后再次解析均有回归。`GetBodyStorage` 本身已经复位，首轮审计的缺少 Seek 判断不成立；原有出站缓冲方式未改。
- 日志进一步关闭 `RelayInfo.ToString()` 查询凭据出口；Epay 共享清理增加 `sign`，删除下单/回调的 `params` 与 `verify_info` 完整对象日志，验签/结算逻辑不改。
- 流式取消即时返回 `context.Canceled` / EOF / `io.ErrClosedPipe` 的分类已补回归；重复 race 又暴露完成事件与真实读取错误的先后竞争，修复及复验结果见 V9。
- 本批 `db80ac8` 的本地适配包括 8 个任务插件自校验（kling 来自上游，另外 7 个为保持出站参数与计费 facts 一致的本地适配），并非全部原生 JS 原文照搬。沿用已批准的插件自校验范围，不引入新 ABI 或第二计费入口。Jimeng 删除非 V3 分支无效调用；Google/Vertex 的声明模型只有 Veo 3.0/3.1，审计提出的 Veo 2 枚举问题在现行列表不成立。
- CSV 新增 12h 列移至末尾，保持既有列序；价格页面的 mode 从未从 URL 或持久状态恢复，移除 expression 选项没有旧链接兼容缺口。
- 删除刷新脚本中的 `SYNC_REASONS` 第二份映射，理由只维护于 `CORE_SOURCE_DELTA.json`；`common/gin.go` 更正为 multipart 适配，回到上游一致的文件自动删除理由。

计费结果变化：Claude 表达式没有单列 `cc1h` 时，1h 写入按 1.6 倍合入 `cc`；没有单列 `cc` 时再计入输入 `p`。OpenAI 12h 写入有独立 `cc12h` 时单独计价，否则按短效写入的两倍权重归入已有表达式；ratio 路径的 12h 倍率也保持独立。`prompt_tokens_details.cache_creation_input_tokens` 现在纳入实际扣费，聚合与 TTL 拆分去重。这些属于本轮明确的扣费结果变化，不是钱包机制变化。

审计点名的逐文件归因：

| 文件                                                                                                                                                             | 上游来源与处置                                                                                                                                                                                                                                         |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `controller/ratio_sync.go`、`controller/ratio_sync_test.go`                                                                                                      | `eb169e74d772afdb281970d0cbe0b62fbf9fa994`；原记汇率联动延期，后续吸收后 `ratio_sync_test.go` 与上游一致，`exchange-rate-pricing-deferred` 标签已删除。                                                                                                |
| `dto/channel_constraints.go`、`middleware/distributor.go`、`model/channel_constraint.go`、`model/model_pricing_config.go`、`relay/relay_task_test.go`            | `8802113263b8a94bf8957a1231b4ec04705d7dd9`；原记共享插件端点竞争延期，后续吸收后 `plugin-shared-route-competition-deferred` 标签已删除，各文件按实际差异改记 `token-routing` 等理由。`model_pricing_config.go` 不属于 f1569d1；后者只改上游 Web 分类。 |
| `i18n/locales/en.yaml`、`i18n/locales/zh-CN.yaml`、`i18n/locales/zh-TW.yaml`、`i18n/keys.go`、`pkg/jsplugin/routing.go`、`router/task-plugin-protocol-router.go` | 同属 `8802113263b8a94bf8957a1231b4ec04705d7dd9`，后续吸收后 `i18n/keys.go`、`pkg/jsplugin/routing.go`、`router/task-plugin-protocol-router.go` 与上游一致；三份 locale 只余工单文案与去分组措辞差异。                                                  |
| `setting/billing_setting/builtin_billing_test.go`                                                                                                                | `e9d4d6353ce2c11df753d751ba1ad778e2433a88` 的 12h 普通/长上下文用例已补到本地 ChannelRatio 合同，精确额度断言保留。                                                                                                                                    |
| `middleware/logger_test.go`、`middleware/logger_sync_test.go`                                                                                                    | `30748c6558bc97de49ff74871cfec717e50f3322`：前者恢复完整上游原文，后者补本地 Google 签名/OAuth 覆盖，不再记为删除上游测试。                                                                                                                            |

首轮与增量只读交叉审计均已完成，最终增量复审无阻塞缺陷，五项上下文全部关闭。复审修正了 URL 查询解析失败时的敏感信息回退，去除 URL userinfo 与 fragment，并补充任务 DTO、Vidu、RawMessage 工具和 SSE 写预算上下文的复核。最终代码指纹 `cac9684d25728e712251c6d15940237ee85ee8d36e2a1177961f0512c0812a3a`（base `ae9be9d32116e7511633dcf3c7e6814cd5fc0737`，覆盖全部修改/新增源码，账本与生成清单单独核验）；复审后源码未再变化。`ClaudeRequest.AddTool`→`GetTools` 的混合 RawMessage 边界与固定上游原文相同，全仓无调用；现有请求计数、转换和搜索计费路径经静态核对及定向回归通过。旧 CLI 原始输出已随临时审计资料清理；源码实现与结论见当前代码和本账本。

最终审计另登记非阻塞边界：Vidu 的非声明、非前缀映射名（如 `xxx-viduq2`）可能触发不同的模型判定；`RelayInfo.ToString()` 的渠道 BaseURL 仍按现行逻辑记录，若管理员将凭据置于 BaseURL 会存在泄露风险；未知字段名或无值键中的凭据不在现行敏感键清单保证内。以上未在当前声明模型/既有配置合同内扩修，未来引入相应输入或修改日志合同前须重评。真实供应商、真实 VLLM 和应用候选/正式部署均未执行，不冒充本地验收覆盖。

本轮来源仅消费临时缓存的固定 Git 提交对象（`git show` / `ls-tree` / `log`），不读取其脏索引或未展开的工作树。默认 `reference-projects/new-api` 不存在，标准离线采集不代表远端最新状态；固定窗口对象与 32 个完整 SHA 已另行校验，未 fetch 或移动参考目录。

2026-10-23 之前的批次记录、浏览器迁移清单和渠道历史处置已于 2026-10-27 移出工作树；下文括号内的 `<提交>:<路径>` 可用 `git show` 读取，Ren2Hub 记录在本仓库执行，工作区记录在根仓库执行。历史段落提到的 `/home/work/ren2hub-*` 实施副本与证据目录已删除，仅作当时事实，不能再作为可复核证据。

## 2026-10-26 Dashboard 系统状态卡片共享快照

按用户批准，Dashboard 系统状态卡片对所有登录用户开放。新增扩展接口 `GET /api/user/system-status`（`service/dashboard_system_status.go`、`controller/dashboard_system_status.go`），从现有唯一观测缓存投影 CPU、内存、磁盘（含绝对容量）、应用 HTTP 正文流量及 1h 分桶历史，并读取主机负载均值和逻辑核数（`service/dashboard_system_load_{unix,windows}.go`，gopsutil `load.Avg` 带 recover）。成功率取 `perf_metrics.QueryHourlyAll(24)`，所有调用方共享 60 秒缓存，失败缓存 10 秒，并以 `state` 区分 ok、no_requests、disabled、error。原生查询不接受 context，因此等待者最多等 3 秒；超时返回不缓存的 error，资源读数照常返回，共享查询结束后再写缓存。数据库/Redis 探测、检查原因、阈值、进程和连接池仍只在 Root 的 `/api/performance/*` 返回；2026-10-31 起投影另带资源阈值判定和粗粒度 `storage_status` 结果，见该日条目。

该接口仅限会话访问，拒绝 PAT；关闭缓存，按用户限制每分钟 30 次（`middleware/dashboard_status_rate_limit.go` 复用原生 `userRateLimitFactory`）。原生文件和 `router/api-router.go` 不改，路由登记在 `router/extensions.go`。核心清单新增 7 个 extension，upstream_equal 与 adapted 数量不变；截至本次 2026-10-26 更新，NewAPI 基线为 `206e0fa5c600127a314a97f88592c067f784f4a6`。

## 2026-10-24 本地修复、独有观测与普通 API Key 计数

从 Ren2Hub `6e4539904d649f892cb7d2f61ea5fedc8c5198bb` 实施用户批准的日志、观测、Dashboard 和原生计数批次；历史参考为 `9a63062227c31ba54a6346e07ee8b4fd8802f22f`。NewAPI 核心仍冻结在 `206e0fa5c600127a314a97f88592c067f784f4a6`，另核对固定快照 `33c61a24234aa20cb10e8a644304322c476361de`，没有整包升级。Sub2API `a34f373973bf547ccec301c7405eb45194e1f28a` 仅择取普通 API Key 真实计数合同；AxonHub `cbdf9a6d4a3dab1e4bfe5168ce5d80205c805913` 的图片变体转换仍是候选，本轮不开放。

日志采用当前取消/序列机制消除失败和筛选交错下的旧结果；观测以当前 performance 扩展的唯一缓存快照提供资源范围、探测、GC、HTTP body 吞吐及实例历史，不恢复旧评分/队列/账务。Root 运维权限保持；Dashboard 请求限制来自实际限流配置，移除统一折扣空卡和 Alchemy 占位。Claude/Gemini 两版本计数使用独立真实转发，复用现有权限/路由/限流/并发，不进入生成或估算成功回退。修复前 Gemini `/v1` 计数误走生成及预扣退款已由本地 mock 复现，没有真实供应商成本。

本段实施记录已在 2026-10-29 清理出工作树；当时的来源取舍、兼容边界和验证状态保留在 Git 历史。共同缺陷清单同样按当时的基线登记，不作为当前 `e449cc7` 的未复核待办；当前实现和验证以本账本最新审计段及源码为准。未提交阶段 `candidate_sha=null`，不以本段历史记录表示已发布。

发布前独立审计另修正本地计数的渠道头覆盖遗漏与工具 JSON 误拒绝，复用现行头规则并限定协议引用位置；五包 24 个顶层/85 个含子用例及 relay/router vet 通过。此结论属于 2026-10-24 固定基线的历史记录；当前来源状态见 2026-10-28 审计段。

## 2026-10-24 Vue 前端审计修复

从 `79e237455262fcb652379e13965ce86fb1a4eefa` 按已批准计划处理 A、B、E 类本地问题，冻结 NewAPI `206e0fa5c600127a314a97f88592c067f784f4a6`。当时的前端实施记录和共同缺陷清单已在 2026-10-29 退回 Git 历史；本节只保留历史处置摘要，不代表当前适配基线或应用部署。

## 2026-10-23 全量审计修复

本轮以 NewAPI `206e0fa5c600127a314a97f88592c067f784f4a6` 为冻结核心，从 `a0a1f8dd1b7909089aed0339bd714406a5ccb66b` 整合并修复。邀请返利参考 Sub2API `a34f373973bf547ccec301c7405eb45194e1f28a` 的来源追踪和操作幂等，另按本地净额冲销合同记录已转出负债及后续抵债；AxonHub `cbdf9a6d4a3dab1e4bfe5168ce5d80205c805913` 没有可直接复用的返利实现。

已处理多 Key 缓存轮询、额度单位、目录报价、返利合规与额度幂等、OAuth 邀请归属、设置保存和草稿、Dashboard 数据降级、日志错误状态、工单附件提交与崩溃孤儿、渠道和用户提交后收尾、统计精度及错误文案。该批详细修复记录已退回 Git 历史；实际提交 SHA、内部 bare 发布、根 gitlink 和正式应用身份只在工作区[服务器档案](../../../docs/server/SERVER_PROFILE.md)维护。

验证：前端 Vitest 250 个文件/1660 项通过，typecheck、lint、生产构建通过；修复专项浏览器 32/32 通过，覆盖桌面/移动和明/暗主题。后端定向检查、build、vet、三库返利账本与系统任务矩阵及轮询 race 通过。浏览器业务接口使用受控 fixtures，没有替代真实 OAuth 或支付供应商联调。controller 中未配置权限的 MySQL database-matrix 分支不计为通过。

### 206e0fa5 基线下记录的共同缺陷（历史状态）

下列问题在 2026-10-23 以 NewAPI `206e0fa5c600127a314a97f88592c067f784f4a6` 为基线登记；它们没有在当前账本中重新逐项确认，不构成当前待修清单。需要处理时，先对照最新上游和 Ren2Hub 源码复核。

| 当时登记的问题                           | 当时的复核条件                     |
| ---------------------------------------- | ---------------------------------- |
| Realtime 最终累计结算重复扣费            | 对照上游结算路径并补一次性结算回归 |
| Waffo 将处理中或授权等待状态作为失败终态 | 对照上游任务状态合同及本地映射     |
| Uptime Kuma 缺失心跳按 0 统计            | 对照上游缺失值语义与展示消费端     |
| 服务端日志导出缺少行数/字节上限          | 对照上游限制及流式导出实现         |

此表是历史待核事项，不说明问题仍存在、已修复或通过运行时验证。

## 2026-10-22 前后端对接修复（历史集成批次）

从 `a6436f2494` 实施的批次 0–2 固定 NewAPI 核心 `206e0fa5c600127a314a97f88592c067f784f4a6`。认证、管理、设置、资金和渠道五方向集成提交 `c6880aa8cf4f596fd41e98792d25a0122aa421b3`，后续形成审计修复基线 `a0a1f8dd1b7909089aed0339bd714406a5ccb66b`。源码与运行态以本账本顶部当前发布记录及工作区服务器档案为准。

逐项处置、5处原生非测试改动、审计修复和候选验证限制见本批记录（Ren2Hub `888498f1b19e08acbafae91fefe1b755048fa7eb:docs/upstream/INTEGRATION_FIXES_20261022.md`）。批次3延期，退役功能不恢复，原生多扣行为按用户决定保留；来源清单刷新为140个有理由标注的非测试 adapted 文件。新依赖 qrcode.vue 及原清单遗漏的 acorn 已纳入102个直接依赖的许可核对。

## 2026-10-21 修复、收敛、借鉴与上游吸收

NewAPI 核心从 `09109ec315a5086366f94bcce00f177ec831dbe1` 推进到 main HEAD `206e0fa5c600127a314a97f88592c067f784f4a6`（官方 latest `v1.0.0-rc.45` 之后，含任务凭据安全修复）。参考副本为 Sub2API `a34f373973bf`、AxonHub `cbdf9a6d4a3d`，只借鉴做法，没有搬代码。用户决定保留现行裁剪、本批不建新表、Vue 不回归上游、上游代码尽量原样照搬，与上游相同且属于多扣的资金问题保持上游原样。

窗口内 25 个提交的逐项处置见批次记录（Ren2Hub `888498f1b19e08acbafae91fefe1b755048fa7eb:docs/upstream/CONVERGENCE_20261021.md`）阶段 1：后端全部吸收，4 个只涉及 React 的提交刻意不吸收；`5e9595190`（渠道优先级拖拽后端）已由 `87ad554d9` 逐字节带入，本批补记。阶段 2 逐文件把没有合同依据的适配差异回归上游原文，`CORE_SOURCE_DELTA.json` 每个非测试 adapted 行都带合同标签，缺标签时刷新脚本拒绝写入。

阶段 3 修复研究报告确认的缺陷，并实施 3E 评估中的 12 项 do-now 重构；7 项 defer、25 项 reject 的理由和复评条件见批次记录“3E 组”。本批新增的上游偏离以 `upstream-defect` 标注，上游修复后回归上游写法：`UpdateOption` 写库失败不再报告成功（E3-2）；分发器首轮检查 `SetupContextForSelectedChannel` 的返回值（B3）；订单计数用派生表封顶，上游的 `Limit(n).Count()` 不生效（A4）；支付回调统一核对实付金额并区分永久和暂时错误（A2、A7）。A2 在 Creem、Waffo、Waffo Pancake 上没有启用，登记为待验证。Realtime WSS 逐轮直扣后结算再扣一次全额（E1-1）与上游相同，按用户决定不修。运维扩展以 `ops-metrics` 标注。

截至阶段 4，非测试 adapted 文件为 133 个、6,687 行差异。build、vet、全量测试（controller 分批、单包 60 秒；根包 embed 测试使用前端构建产物）、RelayKit、TokenKit 通过；隔离 SQLite、MySQL、PostgreSQL 的全量测试通过；定向 race 通过，`TestUpdateVideoTasks*` 的竞争在上游 206e0fa 上同样复现。

Codex `gpt-6-astra` 共同审计 6 轮，按资金与权限、路由与并发、上游合并、前端合同四个方向进行，确认项全部修复并由下一轮复核，不成立与延期项写明理由，见批次记录执行记录。审计修复集中在路由与并发：精确模型优先在 Key 策略之后判定、删除渠道与用户的并发清理（与上限发布串行、拒绝删除前取得旧数据的请求、准入失败统一分类）、直接写流发出器的首输出观察，以及 Vue 的预览参数、监控读数、Resend 状态和充值排行空态。最终 Vue 225 个文件、1,273 项测试通过。源码发布、候选验收和正式更新由根工作区交付记录登记，本段不代表已部署。

## 2026-10-21 历史业务与原生接口回归

以 `f96b9c516284323e2606845f1c11bab51ea3075c` 为开发基线，保留 `9a63062227c31ba54a6346e07ee8b4fd8802f22f` 的有效 Vue 视觉，按文件复用 `324dcb3bcac8f5d7fe89b63284b067f6a1658a02` 已有原生适配。NewAPI 核心仍为 `09109ec315a5086366f94bcce00f177ec831dbe1`，没有上游升级。

工单、GMPay 和邀请返利按独有业务标注；认证/金额/路径/提交语义、模型与路由、统计和管理运维改用现行领域合同。补 GMPay 未知创建的可查询身份、返利十二月真实人数，以及 summary 不含同比时的实际查询边界。移除排除业务及旧账务依赖，不恢复 `/api/next` 兼容后端。来源、功能处置、验证边界见本批记录（Ren2Hub `888498f1b19e08acbafae91fefe1b755048fa7eb:docs/upstream/API_REGRESSION_20261021.md`）；源码发布不等于正式应用切换。

## 2026-10-19 Vue 历史覆盖与审计

按用户“直接使用 Git 历史版本进行覆盖并进行审计”“不动后端”，从 `e8bf2aa0c16f5ad39f15fdd701c66dc0e34ad063` 恢复遗漏的 Vue 界面。来源为 `9a63062227c31ba54a6346e07ee8b4fd8802f22f`，实际提交时间为 2026-10-16 13:04:14 UTC。先覆盖历史页面及组件，再仅在前端适配已有原生接口；不推进 NewAPI 核心，不修改 Go、迁移、依赖、数据库或正式配置。

恢复运维、Key 路由、自动路由卡、订单、充值、GMPay、发票及活动布局；缺失监控数据与发票接口保持明确不可用。保留当前无分组、渠道倍率、权限、支付和返利边界。逐项页面清单、审计修正与执行证据见恢复记录（Ren2Hub `888498f1b19e08acbafae91fefe1b755048fa7eb:docs/upstream/FRONTEND_REGRESSION_20261019.md`），最终源码身份与发布状态由根工作区交付记录（工作区 `61d7528693a832dc759014f15179ad42f0e324f3:docs/workflows/REN2HUB_FRONTEND_REGRESSION_20261019.md`）登记；源码同步不代表正式部署。

## 2026-10-18 渠道路由审计修复

从 `0934cbfbcf7c69bb7e7b4f55d109aecab9afd314` 修复 3 项 P1、6 项 P2：删除邀请人不能阻断付款入账；管理员加额操作身份只在结果未确认时复用；已提交并发限制必须发布且保留在途计数；原任务使用原渠道例外；流式调度延迟从首个有效输出计时；任务/WS 区分撤权与技术故障；订单 PAT 复用目录中的 `billing:read`；兑换码接通四状态与双向排序。

NewAPI 核心不变，保持原生额度事务、首字节重试边界、任务和权限生命周期。相关本地扩展不能登记为上游整包吸收。2026-10-18 13:38 UTC 只读来源采集完整；逐项代码、来源边界和回归见本批修复记录（Ren2Hub `888498f1b19e08acbafae91fefe1b755048fa7eb:docs/upstream/AUDIT_FIXES_20261018.md`），源码与候选、模拟渠道压力及真实浏览器结果见根工作区交付记录（工作区 `61d7528693a832dc759014f15179ad42f0e324f3:docs/workflows/REN2HUB_AUDIT_FIX_RELEASE_20261018.md`）。未提交时 `candidate_sha=null`；不复用旧镜像候选，不修改正式数据。

## 2026-10-18 渠道路由与历史能力回归（代码完成，发布候选待验）

开发基线 `39daa6ccee738f7a26fdba131c829be1adbbdd5b`，Vue 视觉依据为 2026-10-16 的 `9a63062227c31ba54a6346e07ee8b4fd8802f22f`。本次授权替代历史记录中的分组、无渠道倍率和无本地并发合同；不推进冻结 NewAPI 核心 `09109ec315…`。参考 NewAPI `6cf0d90b5f…` 的渠道优先级版本/预览/事务接口，Sub2API 的邀请与模型目录、AxonHub 的观察/释放边界；后二者仍只是参考项目。

业务分组删除，每 Key 自动/有序手动渠道路由；统一资格服务，原生价格乘渠道倍率，强制预扣与更贵重试补足；用户/渠道即时并发、进程内尝试观察。Vue 保留 9a 页面结构并对接原生额度、工单、GMPay、签到和设置，补统一目录、客户端配置、统计及领域返利事务。未恢复历史认证/协议诊断/插件/产物增强、独立商城、会员等级、新人任务、七日阶梯或 schema 5。

详细落点、历史处置、实际验证和未完成发布阶段见执行记录（Ren2Hub `888498f1b19e08acbafae91fefe1b755048fa7eb:docs/upstream/CHANNEL_REGRESSION_20261018.md`），文件差异见[核心差异清单](CORE_SOURCE_DELTA.json)。本地代码及适用验证已完成，默认浏览器 506/506 通过；四条 AWS 文案增量另经定向与生产构建验证。提交前阶段 `candidate_sha=null`，发布后最终身份及候选状态由根工作区交付记录（工作区 `61d7528693a832dc759014f15179ad42f0e324f3:docs/workflows/REN2HUB_CHANNEL_RELEASE_20261018.md`）登记；单测、三库与浏览器结果不能代替隔离候选。正式空库切换需要独立批准，当前库、Redis db1 和附件均未修改。

## 2026-10-17 Vue 体验恢复

用户要求恢复回归前 Vue 独有页面、二次设计和 Codex/Pi 客户端配置，后端接口暂不补充。基点为已正式运行的 `c7fcfb6350f7067bc2206c8befa01062e8cd916f`；页面来源为 `9a63062227c31ba54a6346e07ee8b4fd8802f22f`。本批仅修改 Vue、浏览器合同与交付记录，不推进 NewAPI 核心，不恢复已退役后端。

首页双栏和文档入口、模型三视图、概览五格指标与 Token 趋势、订单概览/详情/导出、账户/活动/邀请和运维布局已适配现有原生接口。缺失统计明确不可用；模型比较采用用户分组，订单汇总和导出注明当前页。Key 配置恢复预览、复制、下载，使用环境变量引用，不读取实际密钥；协议目录、当前 Token 与路由资格在生成时复验。

完整范围、实现边界、测试和用户指定 Gemini 4/Gemini 3.8 视觉审计见 Vue 恢复记录（Ren2Hub `888498f1b19e08acbafae91fefe1b755048fa7eb:docs/upstream/VUE_RESTORATION_20261017.md`）。源码发布与正式运行身份由根工作区本批交付记录（工作区 `61d7528693a832dc759014f15179ad42f0e324f3:docs/workflows/REN2HUB_VUE_RESTORATION_20261017.md`）分别登记，源码同步不等于重新部署。

## 历史批次：2026-10-17 已授权审计修复

以下保留当时实施阶段的记录。其后最终 `c7fcfb635…` 已完成候选并于 2026-10-17 10:37:49 UTC 正式切换，8 项问题及 A4 遗漏已关闭；最终身份和证据见根工作区审计交付记录（工作区 `61d7528693a832dc759014f15179ad42f0e324f3:docs/workflows/REN2HUB_AUDIT_RELEASE_20261017.md`），不以此处较早的待执行状态覆盖最终结果。

`01098b7387fb67a79ea01b4192d71e96a62a78ad` 的完整审计发现 8 项代码缺陷和 1 项验收遗漏。首阶段只参考上游已有处理方式，发布为 `29a1d1c78ad576feda3e280dd359e37219b49df2`。用户随后批准：先核对 NewAPI，再研究两个参考项目；没有完整对应实现时做最小本地修复。先前“待授权”是历史阶段状态，当前执行限制已由本批授权替代。

当前实施副本 `/home/work/ren2hub-audit-repair-20261017`，应用基点为 `29a1d1c78…`，证据 `/home/work/ren2hub-audit-repair-evidence-20261017/`。源码及第一候选已发布为 `1c04f945e4f0927a3a803347fad3b4831e9c37bf`；A1/A3/A4/A5/A6/A7/A8 的源码及定向检查已完成，A2 复用先前已发布修复。A9 及最终本地检查已完成：Vue 174 文件/1012 单测、默认浏览器 55 文件/416 场景全部通过，无跳过/重试通过；Go 全套分批、vet、三库定向、race、生产构建与嵌入 HTTP 检查通过。发布、新候选待执行，不能关闭整批审计。

标准采集 `source-state.json` 在 `2026-10-17T08:36:51.9100792Z` 为 complete。NewAPI 冻结核心仍是 `09109ec315a5086366f94bcce00f177ec831dbe1`，观察 main 为 `3e2ce2c8856e32ca7054d673f8cd059ae466e80c`；两者间 4 个提交未处理本批问题且未吸收。参考 Sub2API main 为 `b0a02a3304a0a78cdd003ddda1dd23ba02c03084`，AxonHub unstable 为 `a946fca495c52344ed073abb6c3562b9ff728b12`；参考副本干净且引用未变。官方 latest 的标签、提交、发布时间及原始标志见审计修复记录（Ren2Hub `888498f1b19e08acbafae91fefe1b755048fa7eb:docs/upstream/AUDIT_FIXES_20261017.md`）。

| 范围          | 当前实现与来源边界                                                                                                                                                                                                                                                                                                                   |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| A1 工单通知   | 最终邮件正文按工单类型转义，保留其他通道及原生 HTML。NewAPI 仅有转义惯例，按类型边界属于本地修复；真实 SMTP/HTTP 回归通过。                                                                                                                                                                                                          |
| A3 GMPay      | 删除无效结算开关，真实开关只控制新订单，历史订单仍验签并入账。上游无 GMPay，不改造支付账本或增加结算暂停语义。                                                                                                                                                                                                                       |
| A4 设置保存   | 共用保存器先停用、再依赖参数、后启用，保留逐项成功基线和未确认草稿；所需 OIDC discovery 先于任何写入。原生 discovery 后端来自 NewAPI，通用顺序及 Vue 整合是本地修复，非原子事务。                                                                                                                                                    |
| A5 PAT        | 从现有 authz 资源元数据和实时权限生成普通用户可授予 routing scopes，不复制权限目录；其他管理资源仍有角色门槛。                                                                                                                                                                                                                       |
| A6 候选交集   | 复用原生规范化函数，按实际请求名到 ability 单向求交；Models 返回获准请求名，具体同族不同模型不能互相授权。                                                                                                                                                                                                                           |
| A7 WS         | manual+pin 的首次/续次共用当前组/模型/列表资格检查；保持锁定计价组、auto pin 和原任务边界。撤权 403/1008，技术故障 500/1011，发送和预扣前失败。共享 SQL 任一错误立即传播；定向真实 WS 与 race 通过。                                                                                                                                 |
| A8 数据身份   | 验证器将首次安装数据库身份与当前镜像 SHA 分离，仍核对 release.env、实际 DSN、Redis、目录及镜像；19 项本地验证器/安装器回归通过，未执行正式更新。                                                                                                                                                                                     |
| A9 Vue/浏览器 | 迁移旧合同并保留真实断言，发现并修复元数据 preview candidates / apply selections；插件预览采用 NewAPI 静态 AST 解析，不执行源码；原生 upload 权威处理动态插件。55 原文件逐一映射由浏览器迁移记录（Ren2Hub `888498f1b19e08acbafae91fefe1b755048fa7eb:docs/upstream/BROWSER_TEST_MIGRATION_20261017.md`）维护，完整默认 416 场景通过。 |

Sub2API 的 WS 逐次白名单与 AxonHub 的权限交集只是参考惯例，没有本地 routing/PAT/pin 的完整实现；A1、A3-A8 的新增本地边界不可登记为整个参考提交已吸收。NewAPI 仍是唯一上游，不新增调度、账务或权限来源。详细落点、先红/后绿证据和历史阶段的准确状态见审计修复记录（Ren2Hub `888498f1b19e08acbafae91fefe1b755048fa7eb:docs/upstream/AUDIT_FIXES_20261017.md`）。最终文件指纹在 `a5-a7-final-files.sha256`，当前核心范围清单见 [CORE_SOURCE_DELTA.json](CORE_SOURCE_DELTA.json)。

## 历史阶段：首轮上游依据修复

截至 `29a1d1c78…`，A2 已完成、A3/A4 部分完成，其余 6 项尚无本地修复授权。GitHub 等 OAuth 与 Turnstile 的先启用问题在观察到的 NewAPI 中也存在，不能声称已有上游修复。该阶段只发布源码，没有新镜像或新候选；当时的测试不证明后续新增修复已经验证。

## 历史基线：2026-10-17 核心回归

本轮以 NewAPI `09109ec315a5086366f94bcce00f177ec831dbe1` 为冻结核心，Ren2Hub 开发基点为 `ab9fa349c75b39b53cf8f783c5c8be44e8ca3a9f`。这次重建取代此前逐项维护本地账务、协议和准入合同的方案。源码仍在原 Ren2Hub Git 历史内；正式旧系统尚未切换。

标准来源采集于 `2026-10-17T04:48:58.9793113Z` 完整通过，使用工作区外只读参考仓库，无 fetch、无截断。NewAPI main 与冻结 SHA 相同；最新发布为 `v1.0.0-rc.44`（标签提交 `633b4c5b190dc1af99a2814391354d86948e7943`）。官方 release 的 prerelease=false 不等于将 rc 名称认定为稳定版。此次没有推进两个参考项目的追踪点。

| 模块                                             | 当前处置                                              | 明确差异                                                                                                           |
| ------------------------------------------------ | ----------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| 用户、权限、PAT、登录及 Passkey                  | 采用冻结上游实现与资源目录                            | 保留站点 OAuth；新增 routing、ticket 权限；不恢复旧独立权限模型                                                    |
| 钱包、Token 额度、默认价格、分组倍率、预扣及结算 | 采用冻结上游数值额度和计算                            | 删除资金事件、独立恢复/准入、渠道倍率及旧价格投影；不提供旧 schema 兼容层                                          |
| 协议、重试、自动选路、任务                       | 采用冻结上游实现                                      | 私有路由仅缩小候选集合；既有任务维持原渠道约束；WS 每次 create 校验当前列表；后续 A7 补齐 manual+pin 的组/模型资格 |
| 私有路由（Ren2Hub 独有）                         | 每 Key 单份有序列表，版本冲突 409、空列表失败         | GET/PUT `/api/token/:id/routing`，只读候选和 preview；无额外权重、分组或健康策略                                   |
| 工单（Ren2Hub 独有）                             | 用户提交/回复/关闭/重开、管理队列/分派/回复/状态/附件 | `/api/tickets`、`/api/admin/tickets`；每次最多 5 个、每个 10 MiB；实际 MIME 与归属校验；用户入口要求会话           |
| GMPay（Ren2Hub 独有）                            | 原生 TopUp 事务入账，固定订单应入额度与商户版本       | CNY 十进制金额；网关资产目录；回调/补单共享幂等事务；验签不宣称验证回调未携带的字段                                |
| 官方订阅账号、套餐、io.net、Playground           | 删除                                                  | type 57 不可创建/选用；普通 API Key、普通充值和钱包额度兼容路径保留                                                |
| Vue Web                                          | 重接上游接口及实际类型                                | 移除 React、Electron、旧聚合后端、任务样例价及模型市场投影；保留 Vue 布局和品牌                                    |
| 单节点部署                                       | 只允许 master 单应用实例                              | 保留上游 Redis、任务轮询、日志及 WS 功能；旧库守卫在任何迁移前拒绝 schema 5                                        |

NewAPI `09109ec` 的 cgroup CPU/内存采样与连续阈值熔断随核心整体采用，不再保留旧宿主口径采样或自建运维聚合接口。原生插件按当前激活版本执行，并携带任务的 producerVersion；本轮没有恢复旧版固定源码运行机制。插件重启验收必须据此判断，不能将已退役断言记作通过。

冻结上游的 Stripe 在 TOKENS 展示模式下存在充值数量合同不一致：最低值乘 quota_per_unit，但商品数量/入账及最大值沿用原始数量，报价又除以 quota_per_unit。本轮保持其原生后端，Vue 在该组合下明确停用 Stripe 入口；USD/CNY/CUSTOM 入口保留。EPay/GMPay 的 TOKENS 预设及最低值按真实接口换算，Waffo/Pancake 保留各自最低值语义；四类支付的 TOKENS 手动输入须为 quota_per_unit 的整数倍，避免原生向下取整入账与付款数量不一致。不能把这些界面约束记为上游后端缺陷已修复。

价格编辑采用 `/api/option/model_pricing`，公开展示采用 `/api/pricing`；原生数值的显示转换不构成结算精度升级。静态表达式首档只用于说明；任务表达式不推导样例总额或不存在的 token 单价。设置多项保存逐项执行，不承诺后端原子事务。

## 核心回归阶段的证据与发布状态

实施副本：`/home/work/ren2hub-core-20261017`。证据目录：`/home/work/ren2hub-core-evidence-20261017`。`source-state.json` 记录标准来源采集，`source-manifest.json` 记录实施前基线及上游文件哈希；[源码差异清单](CORE_SOURCE_DELTA.json)逐文件区分一致、适配、扩展与删除。真实候选 SHA 在提交形成后绑定到根工作区候选报告，不使用工作树指纹冒充提交。

以下仅为核心回归阶段的运行证据；本批审计修复的证据见前文，不能将历史通过结果泛化到当前工作树：

- `go-full-final.log`：主模块全部包与 controller/router 分批测试，单包 60 秒；测试夹具使用原生 PAT，保留权限和审计断言。
- `go-vet-2.log`、`relaykit-1.log`、`tokenkit-1.log` 及对应 vet/build 日志：根静态检查、两个独立模块测试及构建。
- `startup-databases-1.log`：SQLite/MySQL/PostgreSQL 空库初始化、重复启动保留数据、旧库标记拒绝。
- `routing-databases-final.log`、`extensions/evidence-extensions-databases-1.log`、`audit-databases-1.log`：三库路由版本并发、GMPay 回调/补单及工单、原生 PAT 审计。
- `core-race-3.log`：路由、工单、支付、WS 定向 race 通过。此前缺编译器和临时目录空间不足的失败日志保留；通过结果来自固定 Go 1.26.9 编译镜像。
- `frontend-embedded-assets-release.log`、`pgo-build-final.log`、`pgo-binary-info.txt` 与 `pgo-sha256.txt`：真实 Vue 嵌入资源图检查及 moejs profile 的实际 PGO 构建通过，二进制工具链为 Go 1.26.9。
- `sdk-preflight.json`：官方 Python/Node SDK 的 18 项本地协议预检通过；此结果不代替真实候选的扣费验证。
- `vue/vue-vitest.log` 与 `vue/vue-vitest-exit.json`：167 文件、909 项 Vue 测试通过并确认 exit 0。类型、lint、格式和生产构建通过；工单、渠道、原生价格管理及公开页桌面/手机、明暗主题浏览器检查已通过。`vue/vue-topup-units-*` 另记录后续 36 项充值单位定向测试、四个 USD 浏览器组合及 TOKENS 组合通过，最新静态检查和构建均 exit 0。真实候选结果另行绑定。
- `vue/vue-models-http-final.log`：真实嵌入资源 HTTP 回归通过；修复同名 `/models` 目录截获 Vue 页面导致的循环重定向，图标文件与 API 鉴权保持正常。
- `vue/vue-live-contract-tests.log`：后续 22 项定向回归覆盖原生签到开关、任务 list/current/detail 与导航状态。删除不存在的任务 summary API 和旧价格同步结果投影；关闭签到时显示实际状态，不请求禁用接口。相应类型、lint、格式及生产构建通过，旧摘要断言单独登记退役。
- `licenses-1.log`：实际 Go/Vue 的 100 项直接依赖、110 份许可源文件已核对，生成完整许可及 NOTICE。
- `release/archive-preflight-2/manifest.json`：在线只读归档及隔离恢复演练通过；检查时旧任务、未完成恢复、支付订单、附件均为 0，Redis db1 为空。这是切换准备，不能替代停旧应用后的最终归档。

这些是提交前代码验证结果；源码发布、不可变镜像候选和正式空库切换由根工作区[路线图](../../../docs/workflows/REN2HUB_UPSTREAM_ROADMAP.md)及绑定实际 SHA 的候选报告分别记录，不能由本段推定完成。旧核心删除文件及消失的 Go 测试函数在 `legacy-test-retirements.json` 全量登记，裁剪上游订阅用例及旧候选断言另在 `retired-test-cases.json`、`retired-candidate-tests.json` 登记退役，不计作通过。失败、重跑和缺环境的原始日志保留。实际供应商和支付请求仅使用本地模拟服务。

首次正式切换必须使用新库 `ren2hub_core_<首次安装提交前12位>` 和核实为空的 Redis db1，保留旧库、db0、镜像、配置和密钥，不迁移旧业务数据。准备完成后按[更新技能](../../../skills/update-ren2hub-preview/SKILL.md)请求本次空库切换批准。新系统有新增业务数据后停止自动回退，不恢复旧库覆盖新数据。首次安装后数据库身份保存在 release.env；普通应用更新保留该数据目标，不按新镜像 SHA 重命名数据库。

## 历史来源及等待项

2026-10-16 的完整 NewAPI 19 项、Sub2API 48 项、AxonHub 15 项清单，以及此前历次处置，保存在 Ren2Hub 提交 `ab9fa349c75b39b53cf8f783c5c8be44e8ca3a9f` 的同路径账本中：

```sh
git show ab9fa349c75b39b53cf8f783c5c8be44e8ca3a9f:docs/upstream/SYNC_LEDGER.md
git log -- docs/upstream/SYNC_LEDGER.md
```

历史追踪点为 NewAPI `d032204733db31208f10bac118b6e683aab3bf29`、Sub2API `b0a02a3304a0a78cdd003ddda1dd23ba02c03084`、AxonHub `a946fca495c52344ed073abb6c3562b9ff728b12`。旧账本中的“保留本地合同”仅描述当时实现，不能覆盖本轮已批准的上游回归边界。历史等待 NewAPI 或实际供应商验证的事项不因重建自动变成完成；例如 `SYNC1008-06` 的百炼实际 reasoning_effort 仍未通过真实供应商验证。已删除产品的原测试及合同按退役记录，不据此声称修复其历史问题。

本批首个不可变镜像的 48 项后端候选检查通过；真实浏览器修正了协调器凭据格式及登录组件就绪等待后，两个组合共 38 次保留页面访问、公开归属和真实 GitHub/OIDC 设置写入均通过。前两次测试驱动失败保留原始日志。等待修正只影响 live 辅助代码，不改变应用行为；最终提交对应的镜像和全部候选将重新执行。正式未切换。最终发布身份、候选通过及正式批准点由根工作区[路线图](../../../docs/workflows/REN2HUB_UPSTREAM_ROADMAP.md)登记，本文随应用镜像冻结，不用旧结果覆盖新候选。
