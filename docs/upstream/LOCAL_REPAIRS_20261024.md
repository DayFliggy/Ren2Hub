# 2026-10-24 本地修复、观测重构与原生计数

本轮从 `6e4539904d649f892cb7d2f61ea5fedc8c5198bb` 按用户批准的修复计划实施。代码覆盖绘图/任务日志结果归属、独有观测、Dashboard 指标语义和普通 API Key 原生计数；实际验证及边界见末尾记录。未提交工作树的 `candidate_sha=null`。本记录保存实施阶段证据；发布、镜像候选和运行身份由根工作区 `docs/workflows/REN2HUB_REPAIR_RELEASE_20261024.md` 单独绑定。

后端已有而 Vue 无入口的 D 类不纳入本轮；不恢复 `/api/next`、业务分组、旧等待队列、旧账务、React 或已退役功能。10 组共同缺陷仍在[待上游台账](UPSTREAM_KNOWN_ISSUES_20261024.md)，登记不等于修复。

## 固定来源和证据

| 来源 | 完整 SHA | 本轮用途 |
| --- | --- | --- |
| 根工作区采集基点 | `12dcda5872598f4e38cbc1e8684eec5abd0c2247` | 区分根仓库与 Ren2Hub 子模块身份 |
| Ren2Hub 实施基点 | `6e4539904d649f892cb7d2f61ea5fedc8c5198bb` | 当前合同、既有修复和差异复核 |
| Ren2Hub 历史参考 | `9a63062227c31ba54a6346e07ee8b4fd8802f22f` | 仅评估有效 Vue 视觉、探测、GC、吞吐与环形历史算法 |
| NewAPI 冻结核心 | `206e0fa5c600127a314a97f88592c067f784f4a6` | 唯一上游基线，本轮不整包推进 |
| NewAPI 已核对快照 | `33c61a24234aa20cb10e8a644304322c476361de` | 审阅相对冻结核心的 18 个提交；目标路由未补齐，共同缺陷无对应修复 |
| Sub2API 参考 | `a34f373973bf547ccec301c7405eb45194e1f28a` | 原生 API Key 计数的真实供应商动作、请求和错误边界 |
| AxonHub 参考 | `cbdf9a6d4a3dab1e4bfe5168ce5d80205c805913` | 评估图片变体转换器；本轮不吸收实现 |

标准 PowerShell 采集器因 PowerShell 7.4 不可用未运行。等价的只读 Git/内容哈希采集保存在 `/home/work/ren2hub-implementation-20261024/source-state.json`，采集时间为 `2026-10-24T09:46:21.167894+00:00`，`remote_refresh=false`，没有移动参考仓库或修改其 refs。它记录的是并行实施中的状态，不是最终干净工作树证明。

完整协议评估已复读，位于 `/home/work/ren2hub-repair-plan-20261024-cbb5r8ts/protocol-evaluation.md`，SHA-256 为 `8fb1d9c466f984a8fd8700f353078012354576f6a10abb0a6ca8773a45cf86c5`。固定 NewAPI 官方源码在该目录的 `source/` 下：

- `new-api-33c61a24234aa20cb10e8a644304322c476361de-router_relay-router.go`：SHA-256 `9e4b197daea6c3bc9549e32fe6b16b9cf4ab84967a184f130563941b1986436c`，与冻结基线路由逐字节一致。
- `new-api-33c61a24234aa20cb10e8a644304322c476361de-controller_relay.go`：SHA-256 `52d154e1361e06f09b58bf4038c3bbfae97c66fb8cfa3e26682a26e6e3783dc3`。

这些是固定版本来源证据，不建立追平两个参考项目的义务。实施阶段代码内容 SHA-256 为 `ddfd95ba292ee97ae8ad01e443ade4ed3196b455358f7b1cd6cb887e5d606794`；清单位于 `/home/work/ren2hub-implementation-20261024/code-state-final.json`，覆盖 Git 跟踪及未忽略新增路径，排除 `docs/**` 和生成的 `frontend/embed-dist/**`。该指纹不包含本记录自身，不代表干净提交或发布候选；后续发布审计的修正与新身份另行记录。

## 日志与 Dashboard

| 本地落点 | 已实施行为 | 来源与取舍 |
| --- | --- | --- |
| `frontend/src/views/console/DrawingLogsView.vue`、`frontend/src/views/console/TaskLogsView.vue` | API 请求和响应解析置于同一个 `useLatestRequest.run`；筛选、分页变化清空旧列表/总数；当前失败展示错误及重试，桌面和手机一致；取消、过期成功/失败均不能覆盖当前结果、错误或 loading | 历史两页也有失败留旧数据的问题，不回滚。吸收 NewAPI `web/src/features/usage-logs/components/usage-logs-table.tsx` 的查询结果归属思路，复用现行取消机制，不引入新查询框架。保留绘图毫秒/任务秒、日期边界和动态平台合同 |
| `frontend/src/composables/useDashboardResources.ts` | 请求限制和 Root 观测各自有加载、错误、重试和取消；不连累钱包、用量；用户身份/权限变化清除并取消旧请求；Root 观测定时刷新且避免重叠 | 单独补充读取，不把补充接口失败升级为整页用量失败。普通用户及非 Root 管理员不调用 Root 性能接口 |
| `frontend/src/api/dashboardResources.ts`、`setting/rate_limit.go`、`controller/user.go`、`middleware/model-rate-limit.go` | `/api/user/self` 兼容追加 `request_limits`；摘要与实际限流器共同读取 `CurrentModelRequestLimits`，显示原生窗口、总请求上限、成功请求上限 | 历史 `limits=null` 无可恢复实现；不恢复 account group，也不换算虚假“剩余 RPM”。单个上限 `<=0` 明示不限，整体关闭明示未开启，非法窗口不冒称正常 |
| `frontend/src/components/console/dashboard/OverviewKpiStrip.vue` | RPM 格改为“请求限制”，补充读取有独立加载和重试 | 保留概览视觉及用量下钻，只改失效业务语义 |
| `frontend/src/components/console/dashboard/SystemStatusCard.vue`、`frontend/src/views/console/DashboardView.vue` | 仅 Root 展示观测快照；CPU、内存、磁盘、应用 HTTP body 吞吐使用原生可空值；展示资源范围和采样时间，过期或缺测不伪造 0；曲线不跨空值连接 | 历史资源视觉可用，但不恢复主机核数与容器 CPU 混用、虚假成功率、浏览器自造采样或普通用户主机信息入口 |
| `frontend/src/constants/navigation/topNav.ts`、`frontend/src/components/console/TopNavMenu.vue`、`frontend/src/components/console/ConsoleTopbar.vue` | 删除永久禁用的 Alchemy 入口及专用分支，保留其他真实导航 | 历史也只是无页面占位，不根据名称创建新模块 |

删除没有现行计价依据的 `DiscountCard.vue`、旧 `RpmRing.vue` 和仅服务旧主机展示的 `CpuLoadCells.vue`、`PrecisionTickDial.vue` 及独占测试；删除 `useDashboard.ts` 的三组固定 `null` 补充值和旧类型。模型目录、渠道倍率及路由报价继续表达具体价格，不填统一 1.0 折扣，也不恢复业务分组。

日志唯一请求生命周期仍在两页的 `load` 和现有 `useLatestRequest`；补充资源读取唯一位于 `useDashboardResources`，请求限制合同唯一位于 `dashboardResources.ts`；观测前端合同统一由 `frontend/src/api/observability.ts` 解析。未另建列表、权限或查询状态框架。中英文文案在现有 i18n 域维护。

## 观测来源与历史实现评估

历史 `service/ops_monitor.go` 的后台探测、`PauseNs`、内存环和分桶，以及 `common/network_monitor.go` 的字节差分可借鉴；它们依赖旧准入、等待队列、重试预算和账务，不整文件回填。旧健康分为人为扣分公式，本轮不恢复。

| 唯一维护位置 | 已实施合同 |
| --- | --- |
| `common/system_monitor.go`、`common/container_usage.go` | 沿用现有资源采样和过载哨兵，补资源 availability、容量、采样时间及容器/宿主范围；保留 cgroup 优先、连续过载和过期规则，不再创建第二套 CPU/内存采集器 |
| `service/performance_observation.go`、`performance_observation_types.go` | 唯一实例观测快照，后台每 10 秒采样；读取接口只读缓存；启动和关闭在 `main.go` 管理，关闭等待探测取消结束；30 秒过期明确失效 |
| `service/performance_observation_probes.go` | 用现有 DB/Redis 客户端探测，2 秒超时；共享底层主/日志 DB 池只探测一次；Redis 未启用为 disabled，失败延迟为 null，原因区分超时、不可用等 |
| `service/performance_observation.go:observationProcess` | GC 停顿来自运行时；没有 GC 事件时为 null，不把数组初始零当测量 |
| `common/application_traffic.go`、`middleware/application_traffic.go`、`router/main.go` | 仅计应用读取的请求 body 和实际写出的 HTTP 响应 body 字节；不缓存内容，不推断请求成功，不参与用量或费用；保持 writer 的 Flush/Hijack/关闭能力。WebSocket hijack 后帧不在此统计范围，不将该指标称网卡带宽或全协议流量 |
| `service/performance_observation_series.go` | 首采样、时钟回退、计数回退、过长采样间隔重建基线，吞吐为 null；有效相同计数才是实测空闲零。内存历史保留 24 小时，支持 1h/6h/24h 分桶，缺桶留 null，实例重启清空 |
| `service/performance_observation_health.go` | 检查状态、原因和已测量/总计覆盖率；unknown、disabled、stale 与实测健康分开，不提供硬编码评分 |
| `controller/performance.go`、`performance_extension.go` | `/api/performance/stats` 兼容追加 `observability`，旧 `system_usage` 也从同一快照投影；保留原生字段和 RootAuth，不恢复 `/api/next` |
| `frontend/src/api/opsMonitor.ts`、`frontend/src/views/admin/OpsMonitorView.vue`、`frontend/src/components/admin/ops/OpsMonitorContent.vue` 等 | 运维页消费同一个后端快照，展示探测、GC、状态、覆盖率和真实历史；Root 权限或身份变化时卸载内容、取消全部轮询并清除旧数据；资源曲线容纳超过 100% 的容器 CPU 读数且保留空值；Dashboard 与运维不各自采样，未知或失败不变成零/健康 |

观测不会调整钱包、预扣、结算或成功请求计数。普通用户和非 Root 管理员无法通过 Dashboard 取得主机资源或 Root 运维信息。主机资源展示权限沿用现行 Root 规则，不因历史接口曾允许 UserAuth/AdminAuth 而放宽。

## 普通 API Key 原生计数

NewAPI 冻结核心及已核对快照没有本批所需的真实原生计数入口：Claude 路由禁用，原 `CountClaudeTokens` 是本地估算；Gemini `/v1beta` guard 拦截计数，而 `/v1/models/*path` 别名直接进入生成适配。

实施前用本地供应商 mock 验证了 Gemini 别名风险：`POST /v1/models/gemini-2.0-flash:countTokens` 实际到达 `/v1beta/models/gemini-2.0-flash:generateContent`，并发生生成管线预扣，模拟 400 后退款。证据 `/home/work/ren2hub-native-count-baseline-20261024.log` 中 `TestNativeCountBaselineGeminiAlias` 通过；这是修复前隔离复现，不是修复验收，也没有真实供应商费用。原报告仅记静态风险，本轮由 mock 补足运行证据。

| 来源/落点 | 吸收或实现范围 |
| --- | --- |
| Sub2API `backend/internal/service/gateway_count_tokens.go` 的 `forwardCountTokensAnthropicAPIKeyPassthrough` / `buildCountTokensRequestAnthropicAPIKeyPassthrough` | 参考真实 Anthropic API Key 请求、version/beta header、供应商 count 动作、受限响应读取和错误边界；按本项目原生中间件与客户端重新适配 |
| Sub2API `backend/internal/service/gemini_upstream_url.go:buildGeminiAIStudioModelActionURL`，`gemini_messages_compat_service.go:ForwardNative` 的普通 API Key 分支 | 参考模型路径段、action 白名单和真实 AI Studio `countTokens` 转发；不搬生成处理器或调度器 |
| `relay/constant/native_count.go` | `NativeCountChannelType` 为准入、路由过滤和分发共享的能力规则；`ParseGeminiModelAction` 统一校验 `/v1beta` 与 `/v1` 模型段和动作，未知 action 不能默认进入生成 |
| `router/relay-router.go`、`controller/native_count.go` | 注册 Claude `POST /v1/messages/count_tokens`；Gemini 两版本 `:countTokens` 共用独立计数入口，已有生成动作继续原管线 |
| `middleware/distributor.go`、`model/channel_constraint.go` | 复用原生 Token 鉴权、模型权限、渠道过滤、自动/有序私有路由、限流和即时并发；按计数能力选择普通 Anthropic/Gemini API Key 渠道，不另建选择器 |
| `relay/native_count.go` | 唯一真实计数适配，仅 Anthropic（类型 14）和 Gemini（类型 24）；复用模型映射、现有渠道 HTTP 客户端/代理/TLS/取消；超时默认 60 秒或已有 `RelayTimeout`。只发送供应商 count 动作，成功保留非负整数计数和响应字段，响应限 1 MiB；保留供应商 JSON 错误状态与重试提示，无估算成功回退；明确的用户输入 400 使用现有 Ignored 结果，未知 400、凭据错误、429 和 5xx 仍作为渠道失败，避免错误影响生成路由健康 |

计数不进入生成转换、生成预扣/退款或生成费用结算，不创建第二钱包。原生鉴权中的余额/Token 限制及既有模型、路由、速率、并发策略仍适用；“无生成扣费”不等于越过访问策略。Anthropic system/tools 等计数输入不注入生成默认值；Gemini URL 模型为权限依据，body 不能覆盖为另一模型。供应商私有文件/缓存引用暂不支持，须先有对象归属及原渠道绑定合同。

明确排除 Sub2API 的 OAuth/订阅账号体系、分组、等待队列、生成账务后处理、跨协议 `/responses/input_tokens` 转换及网络失败后估算 200。AWS、Vertex、Code Assist、跨协议计数不在本轮；不把原 NewAPI 本地 tokenkit 估算作为远端失败的成功兜底。现有生成请求和任务协议保持原通道。

### 仍未实施的原生协议

| 能力 | 本轮状态与后续条件 |
| --- | --- |
| `images/variations` | 仅候选研究。AxonHub `llm/transformer/openai/image_inbound.go` / `image_outbound.go` 有 multipart 入站、`buildImageVariationRequest` 出站和转换测试，但 `internal/server/routes.go` 公共入口禁用；部分字段完整性仍需验证。未开放路由，未复制图片账务。须先明确供应商/模型能力、上传限制、请求字段、响应数量、计价及原生失败退款合同 |
| Files | 等待 NewAPI 完整对象归属、租户隔离、原渠道/Key 绑定和受限下载/删除；内部上传 helper 不是 Files 网关 |
| 旧 `fine-tunes`、新 `fine_tuning/jobs` | 分开等待真实任务/文件/模型归属、取消/事件、异步成本和清理合同；不能仅透传创建 |
| `DELETE /v1/models/:model` | 等待供应商资产所有权和原渠道绑定，不能映射为本地模型目录删除 |
| Assistants / Threads | 等待对象隔离、持续运行、工具回调、SSE 生命周期和原生用量结算；内部 Thread/Trace 不是对应协议 |
| OpenAI Batches | 等待 Files 依赖、JSONL/endpoint 验证、异步竞态和每项最多一次结算。Sub2API 图片批任务不是 OpenAI Batches，冻结/专属结算不吸收 |

上述固定来源未找到可直接吸收的完整可达方案；真实状态继续是候选或等待，不填空列表、假成功或不带所有权的通用反向代理来补 API 清单。

## 上游替换条件

- NewAPI 提供真实注册的 Claude/Gemini 独立 count 操作，具备模型权限、渠道选择、原生响应/错误和无生成扣费合同后，先用同一本地 mock 合同比较，再优先替换 `relay/native_count.go` 等本地差异并删除被覆盖逻辑。仅取消路由注释、本地估算或只修一个 guard 不够。
- NewAPI 有对应观测/限制摘要方案时，复用本轮资源范围、权限、null/过期和单一来源回归，评估合并；不得并存两个采样器、两个限流配置来源或放宽 Root 权限。历史监控模块不是另一个持续同步目标。
- 图片变体及有状态协议须同时具备完整入站/出站、能力、所有权和原生结算合同后才重新评估；转换器、DTO 或通配路径不算完整实现。共同缺陷按原 ID 等待清单的具体条件重开。

每次替换固定来源完整 SHA，注明覆盖范围，删除已无维护价值的临时分支及测试，不把 Sub2API/AxonHub 建成第二上游。

## 验证与剩余边界

| 项目 | 已执行结果/证据 |
| --- | --- |
| 日志定向组件回归 | `RelayTaskLogsLoading.spec.ts` 与 `RelayTaskLogsDates.spec.ts`：2 文件、29 项通过，覆盖首次失败、筛选/分页/每页数量失败、非法响应、重试、交错、卸载与中英文桌面/移动错误态 |
| Dashboard/导航定向回归 | `OverviewKpiStrip.spec.ts`、`SystemStatusCard.spec.ts`、`useDashboardResources.spec.ts`、`useDashboard.spec.ts`、`TopNavigation.spec.ts`：5 文件、62 项通过；覆盖窗口/上下限、disabled/invalid、Root 独占读取、独立错误/重试、取消/降权/logout、原生快照与缺测/过期 |
| 测试类型修正后的复跑 | 两个受影响文件 `SystemStatusCard.spec.ts` / `RelayTaskLogsLoading.spec.ts`：2 文件、42 项通过，是上述用例子集，不额外累计为 133 项 |
| 本分支静态检查 | 目标源码/测试 scoped ESLint、Prettier 检查和 `git diff --check` 通过；测试 wrapper 类型已修正，最终统一类型检查见下一行 |
| 整合前端单元测试 | 主代理全量 Vitest 执行 252 文件/1724 项，初跑 1718 项通过，6 项因旧维护页 fixture 缺少新增合同而失败；补齐 fixture 后该文件 8/8 通过。上述全量和失败文件复跑合计覆盖全部用例最终通过，不写成第二次全量重跑 |
| 运维权限与 CPU 曲线最终回归 | `OpsMonitorView.spec.ts` 19/19（含新增 6 项降权、登出、切换身份及晚到响应回归）；`OpsResourcesCard.spec.ts` 新增 2/2，验证超 100% 不截断和缺测不补零。总计 21/21 通过 |
| 整合前端静态检查与构建 | 主代理全量 vue-tsc、ESLint、Prettier 和 Vite 生产构建通过 |
| 新增浏览器验收 | 主代理 20/20 通过：桌面/移动 × 明/暗主题，覆盖日志清空/重试、Dashboard 权限和请求限制、运维覆盖率及后端 6h 窗口。证据 `/home/work/ren2hub-implementation-20261024/ui-repairs-final`；最终权限生命周期/曲线调整后，受影响的 4 个运维场景再跑 4/4 通过，证据 `ui-ops-final`；接口使用受控 fixture，不替代真实供应商联调 |
| 既有浏览器回归 | 最终统一入口 `ui-contracts`：32/32 通过，0 跳过、0 失败、0 flaky；运行期间源码指纹一致。证据 `/home/work/ren2hub-implementation-20261024/run-k5im35mf/report.json`，该次为未提交工作树，不冒称候选验收 |
| 后端观测、限流摘要、HTTP body 埋点及 Root 权限回归 | 最终覆盖 common/service/controller/middleware/router 五包，68 个含子用例的唯一测试通过（32 个顶层测试）；包含容器容量/工作集、连续过载、热配置/过期、共享 DB/禁用 Redis/超时、GC、字节计数和流式 writer 能力、历史缺桶/计数回退、Root 权限及请求限制摘要。证据 `observation-tests-final.log`、`observation-contracts-final.log`，无跳过或失败 |
| 后端原生计数回归 | 协议代理执行 `go test -p 1 -timeout 60s ./router ./controller ./relay ./relay/constant ./middleware ./model` 六包全部通过，六包 `go vet` 通过；随后模型映射/body 保真/断连用例的 router、middleware、constant 定向复跑通过。`router/native_count_test.go` 14 个顶层测试使用真实路由和本地供应商 mock，包含无账务副作用；另有 constant/model/controller 回归。整合中追加 `relay/native_count_outcome_test.go` 的 22 个协议错误分类案例通过；随后 router `TestNativeCountUpstreamErrorsNeverEstimate` 复跑通过，`go vet ./relay` 通过 |
| 受影响 Go 静态/构建、隔离 smoke | 根模块及独立 relaykit/tokenkit 的 `go vet`、构建通过；最终计数错误分类修改后另跑 `go vet ./relay` 通过。最终前端生产构建后重新准备嵌入资产，`TestProductionEmbeddedAssetGraph`、`TestProductionEmbeddedModelPagesAndFiles` 两个顶层/含子用例 10/10 通过，实际嵌入该产物的 Go 二进制构建通过。证据 `/home/work/ren2hub-implementation-20261024/production-smoke-final/result.json`；测试后已恢复跟踪的 embed 占位 index；Go 单元测试均使用 `-timeout 60s` |

新后端接口为兼容追加字段，未新增数据库表或迁移。验证仅使用隔离本地 DB/Redis/供应商 mock；不调用生产或付费 API。没有把静态来源核查、修复前复现、过去测试或浏览器 fixture 当作真实供应商联调。实施阶段尚未执行正式部署或创建发布候选。后续发布状态以根工作区交付记录为准，不据本段历史状态推断。

## 发布前审计修正

审计覆盖 `79e237455` 后三批前端修复及本批修改。前端独立审计 21 个 Vitest 文件、348 项通过；观测五包本轮 25 个顶层测试通过，没有新增阻断项。协议审计先用真实路由和本地 mock 复现三例失败，再修复以下两个根因：

- `relay/native_count.go` 生成供应商头后复用 `channel.ApplyHeaderOverride`，使计数使用同一渠道鉴权/Host/占位符合同；无效配置在发出请求前失败。未复制头处理规则。
- 私有引用检查限定为供应商协议定义的消息内容、来源、缓存和媒体位置，不再扫描任意工具参数/返回 JSON 的键名。合法 `file_id` 和大整数原样保留；真实私有文件、缓存引用仍拒绝。

`router/native_count_test.go` 补双供应商头配置和 14 组工具/私有引用回归。最终五包定向回归 24 个顶层测试、85 个含子用例通过（含工作区外三例实际路由复现），`go vet ./relay ./router` 和 gofmt 通过，均未调用真实供应商。证据 `/home/work/ren2hub-audit-20261024-protocol/summary.json`、`final-protocol-tests.log`、`final-protocol-vet.log`。这两处是本地新计数适配缺陷，已修复；10 组 NewAPI 共同缺陷仍按台账等待。

前文实施阶段内容指纹不包含上述修正。最终交付以已发布提交和匹配镜像为准；候选或部署未执行前不计通过。
