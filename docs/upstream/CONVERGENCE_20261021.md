# 修复、收敛、借鉴与上游吸收批次（2026-10-21）

本批次把 Ren2Hub 的核心基线从 NewAPI `09109ec315a5086366f94bcce00f177ec831dbe1` 推进到 `206e0fa5c600127a314a97f88592c067f784f4a6`，按 2026-10-21 研究报告修复已确认的缺陷，收敛与原生重复的实现，并在不违反现行合同的前提下借鉴 Sub2API 和 AxonHub 的做法。最后由 Codex `gpt-6-astra` 共同审计，审计通过后发布源码、做候选验收并正式部署。

## 用户决定与范围

| 事项 | 决定 |
| --- | --- |
| 回归程度 | 保留现行裁剪：业务分组继续删除；订阅、io.net、Codex 订阅账号、Playground、type 57、system-info 继续退役。其余后端适配逐段回归 NewAPI 最新 HEAD，保留下来的每段差异都要能对应到一项独有功能或裁剪 |
| 前端 | 不做“回归上游”，Vue 保持现状。本批只为后端修复接通必要的前端调用 |
| 新表 | 本批不做“渠道×模型历史统计”这张新表。按渠道维度的健康时间线和统计继续标为不可用 |
| 交付终点 | 发布并部署。正式更新按普通更新执行，数据库、Redis db2 和附件目标都不变。如果需要更换数据目标或恢复备份，届时单独请求批准 |
| 共同审计 | 本机 `codex-cli 0.163.0`，模型 `gpt-6-astra`，只读沙箱 |
| 与上游相同的资金问题 | 上游存在相同的资金问题、且属于多扣时，保持上游原样，不做本地修复（2026-10-21 用户决定） |

## 基线与工作位置

| 对象 | 身份 |
| --- | --- |
| Ren2Hub 起点 | `4c7c12c0fcfc1afeba9f3e46c687cf47b7466909`（origin/main） |
| 隔离工作树 | `/home/work/ren2hub-convergence-20261021`，分支 `codex/convergence-20261021` |
| 证据目录 | `/home/work/ren2hub-convergence-evidence-20261021`（0700） |
| NewAPI 目标 | `206e0fa5c600127a314a97f88592c067f784f4a6`。官方 latest 为 `v1.0.0-rc.45`，指向 `6cf0d90b5ff2`；本批取 main HEAD，以纳入 rc.45 之后的任务凭据安全修复 |
| 参考副本 | `/home/work/upstream-refresh-20261021T102730Z/reference-projects/`，其中 Sub2API 为 `a34f373973bf`，AxonHub 为 `cbdf9a6d4a3d`。这两个项目只作参考 |
| 正式运行 | `ren2hub-test:324dcb3bcac8…-v1`，库 `ren2hub_core_e8bf2aa0c16f`，Redis db2 |

本次部署会把正式环境从 `324dcb3bc` 一次推进到本批最终提交。中间的 `4c7c12c0f`（前端原样恢复及其依赖的后端改动）也会随之上线。

## 编码约定

实现时参照 NewAPI 的写法：

- **分层**：保持 Router → Controller → Service → Model。
- **复用**：JSON 用 `common.Marshal/Unmarshal`，响应用 `common.ApiError*`，文案走 i18n key，SQL 兼容 SQLite、MySQL、PostgreSQL，锁和事务沿用上游的写法。
- **扩展点**：本地逻辑优先放进已有的扩展文件，比如 `*_extension`、`route_*` 和 `topup_settlement`，不在原生文件里散落修改。原生文件只有在必须接入时才加一行调用，并在账本里说明原因。
- **上游代码原样照搬**（2026-10-21 用户要求）：凡是 NewAPI 已经实现的功能或写法，都直接复制上游代码，不重写，也不写等价实现。原生文件尽量保持上游原文，合同要求的差异只允许两种改法：删除分组、订阅相关的行，或者加一行调用本地扩展函数。本地逻辑都放在扩展文件里。阶段 3 的修复如果在上游已有对应实现（如 `sanitizeLikePattern`、`pinnedChannelUnavailable`、`ratio_sync` 预设、`notifyAdmins`），直接复用或复制上游代码。Sub2API 和 AxonHub 的架构不同，只借鉴做法，不搬代码。
- **经评估的重构例外**（2026-10-21 用户要求）：评估后确实值得彻底重构或优化的上游代码，允许大量删改。原样照搬仍是默认做法，重构必须先通过阶段 3E 的评估。评估要给出类别、证据、收益、风险、上游近 6 个月的改动频率和同步成本，只有收益明显大于同步成本和风险的才实施。协议适配器和 RelayKit 不做风格性重写。
- **规则只写一处**：每条不变量只表达一次。同一检查不在多个支付渠道、多条删除路径或多个控制器里各写一遍，抽成一个共享函数，由各处调用。
- **可用性**：进程内状态必须有上限、有过期，超限时显式淘汰并记日志，不静默丢弃。外部依赖失败要明确返回错误或降级状态，不伪造成功，也不吞掉错误。缓存刷新失败不能把已经提交的写操作报告为失败。
- **测试**：新增测试要保护真实行为或失败边界，不要写复刻实现细节的测试。Go 测试统一加 `-timeout 60s`，RelayKit 和 TokenKit 用 `GOWORK=off`。

## 阶段 0：基线

在起点提交上运行 `go build`、`go vet`、全量 `go test` 和两个独立模块的测试，把已有失败单独记录下来，避免和本批引入的失败混在一起。证据保存为 `baseline-*.log`。

## 阶段 1：吸收 NewAPI 上游窗口

窗口为 `09109ec..206e0fa`，共 25 个线性提交。Ren2Hub 不含上游历史，因此按文件做三方合并：base 取 `09109ec`，ours 取本地，theirs 取 `206e0fa`。规则如下：

- 本地与 base 相同的文件，直接采用上游版本。
- 已适配的文件，在保留本地合同的前提下合入上游改动。
- 上游新增的文件原样加入。
- `web/`（React）不吸收，React 已退役。

每个提交在账本中登记处置。

| 提交 | 内容 | 处置 |
| --- | --- | --- |
| `590042682` | 任务查询重定向凭据限制在允许的主机内 | 吸收，安全优先。三方合并 `jsplugin/adaptor.go` |
| `9a42033a6` | 修复任务提交和轮询时请求头被覆盖 | 吸收。三方合并 `relay_task.go`、`api_request.go`、`task_polling.go`、`jsplugin/adaptor.go` |
| `55e2ae54d` | 任务每次重试都重新定价，并在提交前预留 | 吸收。合并后按渠道倍率核对“重试提价须先补足”的合同 |
| `25372e30b` | Claude 流式请求在准备和重试期间保持保活 | 吸收。三方合并 `controller/relay.go`、`relay/helper/common.go`、`api_request.go` |
| `6cf0d90b5` | DeepSeek V4.1 Flash、Search/Fetch API、`@` 推理修饰 | 吸收。三方合并 `distributor.go`、`ability.go`、`relay_info.go` 等文件 |
| `603a5ceb5` | 仪表盘同时显示缓存用量 | 后端 `quota_data` 缓存列和 `log.go` 吸收（AutoMigrate 只增列），React 部分不吸收。Vue 统计在阶段 3 接入 |
| `6461f084f` | 每类通知可单独开关 | 后端吸收，默认全部开启，行为不变。Vue 设置入口按阶段 3 D6 的评估决定 |
| `32e587406` | Resend 邮件服务商 | 后端吸收。Vue 邮件设置补充服务商选项（D6） |
| `55bb6b903`、`058c8231b`、`e2a0c4b16`、`fc8f7d6fe` | 通知限额窗口、审计日志复合索引、限流脚本在 Redis flush 后恢复、DeepSeek CNY 回退 | 吸收 |
| `afe73b08f`、`783735607`、`ff3684658` | qwen3-vl-embedding、Claude 合并相邻消息时保留工具调用、Responses 转换保留 `input_image.detail` | 吸收。RelayKit 用 `GOWORK=off` 验证 |
| `3e2ce2c88` | 模型卡摘要的起点与状态栏小时数对齐 | 吸收，三方合并 `pkg/perf_metrics/metrics.go` |
| `206e0fa5c`、`a8535fac1` | Kling 插件 1.1.1，接受单个 API Key | 吸收（插件文件与 base 相同） |
| `120dbeafb`、`f9a0634f8` | 测试修正 | 吸收 |
| `5e9595190` | 渠道优先级拖拽的后端 | 已由 `87ad554d9` 逐字节带入，本批只补账本 |
| `5c31c1fa9`、`5a59d6915`、`f92c57650`、`b0a98ac49` | React 拖拽引导、任务零价隐藏、厂商规则、主题首帧 | 刻意不吸收（仅涉及 React）。复评条件：Vue 出现对应页面需求 |

合并完成后，`CORE_SOURCE_DELTA.json` 的 `upstream_sha` 改为 `206e0fa…`，用 `scripts/refresh-core-source-delta.py` 重新生成。

## 阶段 2：适配差异收敛

以 `206e0fa` 为基准，逐个审阅非前端的 `adapted` 文件。阶段 1 前约有 213 个文件、9,000 行差异。每段差异必须能对应下列合同标签之一，对应不上的回归上游：

| 标签 | 范围 |
| --- | --- |
| `group-removal` | 删除业务分组 |
| `retired` | 订阅、io.net、Codex 订阅账号、Playground、type 57、system-info |
| `token-routing` | 渠道级自动/手动路由 |
| `concurrency` | 即时并发 |
| `channel-ratio` | 渠道倍率 |
| `extension-hook` | 钱包和 PAT 扩展挂点 |
| `ticket` | 工单 |
| `gmpay` | GMPay |
| `affiliate` | 返利 |
| `catalog-stats` | 模型目录与统计 |
| `install-guard` | 启动前拒绝旧库 |
| `single-node` | 单节点 |
| `branding` | Ren2Hub 品牌 |
| `vue-frontend` | Vue 前端替代 React |
| `ops-metrics` | 运维监控扩展：主机资源、连接池、跨模型质量序列（阶段 3 新增） |
| `upstream-defect` | 修复上游 206e0fa 同样存在的缺陷；上游修复后回归上游写法（阶段 3 新增） |

理由记录在 `CORE_SOURCE_DELTA.json` 各行的 `reasons` 数组中，清单因此仍是唯一的差异来源。刷新脚本要保留 `reasons`；`adapted` 行缺少 `reasons` 时直接报错。这样以后新增的漂移在刷新时就会暴露。

已知需要在本阶段处理的漂移：

- `TrustQuotaUSD`：仍可配置，但已不生效。恢复原生行为，或者按退役选项处理，二者取一，不保留空配置。
- `model/pricing_default.go`：恢复被删除的 `defaultVendorRules`，修复 ernie、doubao、mistral 等裸模型名在定价页落到 vendor 0 的问题。
- 原生 `SearchUserTopUps` 等查询函数：本地已无调用方，处理方式见 A4。

本阶段只做回归和标注，不修 bug。行为修复全部在阶段 3 进行，便于审计时区分“回归上游”和“本地修复”。

## 阶段 3：修复、收敛与借鉴

阶段 2 结束后，以它的提交为基点，拆成四组在各自的工作树中并行实施，每组只改自己负责的文件，最后由主代理集成。优先级说明：P0 是资金或权限风险，P1 是功能错误，P2 是可维护性和观测问题。

### A 组：资金与支付

文件归属：`middleware/access_token_*`、`model/topup*.go`、`model/gmpay*.go`、`service/gmpay_*`、`controller/topup*.go`、`controller/gmpay.go`、`model/affiliate*.go`、`controller/affiliate.go`。

- **A1（P0）返利转入改为只接受浏览器会话。** 把 `POST /api/user/aff_transfer` 声明为 session 规则。现在 legacy PAT 能放行所有非 session 路由，带 `wallet:write` 的 scoped PAT 也能调用这个旧路由，结果新路由的 session-only 限制被绕开。改完后新旧两个路由的约束一致。回归测试要覆盖三种调用方式：scoped PAT、legacy PAT、浏览器会话。
- **A2（P0）回调入账前核对金额。** 在 `completeTopUpTx` 前加一个共享函数，用“回调中已验签的实付金额和币种”核对订单应付值，不一致就拒绝入账并记审计日志，订单保持未完成。各渠道的实付金额语义要先从代码和 SDK 核实，包括 Stripe `amount_total` 是否含税或折扣、EPay `money` 的单位。核实不了的渠道不启用这项检查，在账本里登记为待验证。思路借鉴 Sub2API 的 `confirmPayment`。上游没有这项修复，属于本地偏离，需要登记。
- **A3（P1）GMPay 状态处理。**
  - 管理员补单只允许 pending 或 unknown 状态的快照，failed 和 settled 拒绝。
  - 回调遇到验签失败等永久错误时，按 epusdt 的重试语义返回。
  - GET `notify` 不能直接删：epusdt 的 EPAY 类型通知用的是 GET 查询串，先核实 GMPay 实际用哪种类型，再决定保留还是退役。
  - epusdt 如果没有商户查单接口，unknown 订单在文档里写明处理路径：等迟到的回调，或由管理员补单；不自造对账。
- **A4（P1）订单查询加固。** 本地的 `ListTopUps` 支持状态、币种、渠道等过滤，原生函数不支持，所以保留，但要复用原生的 `sanitizeLikePattern` 和 `searchTopUpCountHardLimit`。普通用户的 `start_time` 不能早于 30 天截止点；管理员也要有计数上限。已无调用方的原生查询函数按阶段 2 的规则二选一：确认确实被替代后删除，或者改回由原生函数承担查询。
- **A5（P2）订单统计。** 不建新表，在 `/api/user/topup/stats` 里按支付方式和币种汇总占比，供 Vue 的 `OrdersView` 使用。管理员端额外提供充值排行，只按用户和到账额度排序，不暴露支付凭据。
- **A6（P2）返利策略局部更新。** `UpdateAffiliateUserPolicy` 只更新请求中提交的字段，只改 enabled 时不能把专属比例清空。
- **A7（P1，阶段 2 分类时发现）Creem、Stripe 回调的状态码。** 分类时发现两处：Creem 收到非一次性订单时，返回值从 200 改成了 400；Stripe 入账失败或找不到订单时返回 500。这两种情况都会让支付平台一直重试。处理方法：先和上游 206e0fa 的 webhook 处理逐条对照，再统一按这条规则返回——永久性错误（订单不存在、类型不支持、验签失败）返回不触发重试的状态并记审计日志，暂时性错误（数据库不可用）返回可重试的状态。和上游语义一致的地方回归上游，确有必要的偏离登记到账本。

### B 组：路由与容量

文件归属：`service/route_*.go`、`service/channel_select.go`、`service/token_route*.go`、`service/concurrency.go`、`service/channel_request.go`、`model/token_route.go`、`model/route_candidates.go`、`middleware/distributor.go`、`controller/relay.go`、`controller/token*.go`、`controller/channel_priority.go`、`controller/channel_upstream_update.go`、`relay/helper/price.go`、`middleware/retired_groups.go`。

- **B1（P1）** prefer 模式的会话亲和要真正生效。亲和渠道合格时优先使用；不合格时退回正常选路，不报错。strict 模式保持现有行为。
- **B2（P1）** 重试轮次里找不到候选渠道时，保留上一次真实的上游错误返回给客户端，不能被“无可用渠道”覆盖。
- **B3（P1）** 首轮也要检查 `SetupContextForSelectedChannel` 的返回值。目前“插件身份不匹配”和“多 Key 渠道没有可用 Key”这两种拒绝在首轮被跳过，要让它们在首轮就生效。
- **B4（P1）** 删除渠道时，清理 Key 路由条目和并发资源表项。为此新增一个共享清理函数，原生的四条删除路径各加一行调用，不在每条路径里复制清理逻辑。
- **B5（P1）** `channel_upstream_update.go` 的查询字段去掉已经删除的 `group` 列，并补上真实执行这条查询的回归测试。
- **B6（P1）** PAT 创建手动路由 Token 时，要同时检查 PAT 自身是否有 `routing:write`，不能只看用户角色。
- **B7（P1，借鉴 AxonHub）** 健康分档改用按“渠道×实际模型”计数的熔断状态，修掉“样本数达标后一次失败就进最差档”的问题。
  - 三个状态：连续失败达到阈值后打开；冷却结束后进入半开，权重乘 0.3；半开期间连续成功若干次后闭合。
  - 上游返回 429 并带 Retry-After 时，在这段时间内把该渠道降到最差档。只降档、不剔除，手动列表的顺序语义不变。
  - 状态只放在进程内，沿用现有的 5 分钟过期，不建新表，不排队，也不恢复旧的准入框架。
  - 流式请求的成功判定以第一个有内容的事件为界。先核对 `route_stream_output.go` 是否已经这样处理。
- **B8（P2）** 观察表满 1 万键时不能再静默丢弃新键。改为先淘汰最旧的过期项，并按限频记录日志。
- **B9（P2）** 按路径的端点过滤目前只对 AdvancedCustom 渠道生效，导致 compact 请求在不支持的渠道上返回不可重试的 400。改为凡是能确定渠道端点能力的情况都过滤，或者把这类错误标成可以换渠道重试。具体方案选一种和原生一致、改动最小的。
- **B10（P2）**
  - 优先级重排时，`ChannelRoutingChanged` 失败只记日志，不能把已经提交的写入报告为失败。
  - 渠道倍率的日志改用精确的十进制字符串。
  - 路由预览接受 stream 参数，并说明加权随机是按请求计算的。
  - `RejectRetiredGroups` 不再依赖 Content-Type，没有这个头的请求也要检查。

- **B11（P0，阶段 1 发现）渠道倍率无效时任务被按 0 计费。** `HandleChannelRatio` 在倍率无效时会吞掉错误、返回 0。`ModelPriceHelper` 检查了返回值，但任务表达式计费分支（`relay/relay_task.go:304`）没有检查，渠道倍率缺失时任务会按 0 额度计费。修复方式：让 `HandleChannelRatio` 返回 error，所有调用方都必须处理，以后新增的调用方也绕不过去。

- **B12（P0，阶段 2 分类时发现）修改渠道倍率要有敏感写权限。** 上游把 `group` 列为非敏感字段，因为计价跟着用户分组走，分组倍率只能在 root 设置里改。Ren2Hub 的 `channel_ratio` 直接决定用户付多少钱，现在却在 `channelNonSensitiveFields` 里，只有 ChannelWrite 的管理员也能改所有用户的计费。改为要求 ChannelSensitiveWrite，并补权限回归测试。`max_concurrency` 只影响运行，继续按非敏感处理。
- **B13（P1）** 固定渠道（pin）的错误返回要和上游一致：
  - `service/channel_select.go:185`：非原任务的 pin 找不到渠道时，返回的 message 和 code 都是空的。
  - `:187-189`：原任务的渠道被禁用时返回 403，没有返回 `origin_task_channel_disabled`。
  
  这两处都改为按上游 `pinnedChannelUnavailable` 的语义返回。
- **B14（P2）** 删除用户时的硬删除路径要清理 Key 路由表，复用 B4 的共享清理函数。任务轮询遇到满载跳过本轮时，要按限频记一条日志。
- **B15（P1，阶段 2 发现）拉取模型列表时拒绝已退役的 type 57。** 阶段 2 保留了 type 57 的 BaseURL 置空，`FetchModels` 和 `FetchUpstreamModels` 因此不会把管理员 key 发往 chatgpt.com，但它们靠请求失败间接拒绝，错误信息也不明确。在两者共用的 `fetchChannelUpstreamModelIDs` 开头显式拒绝 type 57，返回和渠道写入一致的退役错误，并补回归测试。

### C 组：运营与可观测

文件归属：`service/model_catalog.go`、`controller/model_catalog.go`、`*/dashboard_summary.go`、`*/home_metrics.go`、`controller/performance.go`、`model/ticket.go`、`controller/tickets*.go`、`model/channel_lab.go`、`pkg/modellab`、`pkg/pricesource`、`*/price_source.go`。

- **C1（P0）** 模型目录和 `/api/token/:id/routing/channels` 不再向普通用户返回 `in_flight`。保留 `channel_ratio` 和 `max_concurrency`，手动选路和计价要用到这两个字段。做法是给用户单独定义一个视图类型，不改动 B 组负责的 `RouteChannel`。管理员视图不变。
- **C2（P1）** `/api/data/self/summary` 的请求数和 token 数改读 `quota_data`，包括阶段 1 新增的缓存列；hourly 改成真正按小时排列的时间序列。个人的耗时和成功率仍按本人日志计算，不能拿全站的 `perf_metrics` 冒充个人数据。成功率给不出时显式返回不可用。
- **C3（P2）** 新增运维质量接口，权限 `ops:read`，按小时跨模型汇总 `perf_metrics`。如果原生 `perf-metrics/summary` 已经能满足需要，就直接复用，不新开接口。
- **C4（P2）** `/api/performance/stats` 增加两项：
  - 主机 CPU 和内存。性能监控关闭时返回 null，不返回 0。
  - 数据库连接池，数据来自 `sql.DB.Stats()`。
  
  定时任务登记表本批不做。
- **C5（P1）**
  - 工单附件在解析时就检查数量和大小，超限立即中止，并返回明确的错误码。
  - 附件下载加 `X-Content-Type-Options: nosniff`。
  - 附件先写临时文件，事务提交后再改名，避免孤儿文件。
- **C6（P2）** `home_metrics` 合并查询改用独立的 context，第一个调用方取消时，同批等待者不再一起失败。
- **C7（P2）** `channel_lab` 以 NewAPI vendors 作为唯一的厂商词表，modellab 的别名只作补充，不再维护两套词表。依赖阶段 2 恢复的 `defaultVendorRules`。
- **C8（P2）** 价格来源：
  - 与原生 `ratio_sync` 预设重复的 basellm、models_dev、openrouter 三个来源，改走原生路径。
  - 本地只保留 sub2api 和 axonhub 两种格式到表达式的转换。
  - sub2api 解析器把已知、但本地不计费的字段（如 `cache_storage_cost_per_token_per_hour`）列为可忽略，在草稿里给出警告，不再整条拒收。其他未知的计价字段仍然拒收。
  - 草稿中显示来源 `.sha256` 指纹，借鉴 Sub2API。

- **C9（P1，阶段 2 分类时发现）渠道列表的类型计数被筛选条件污染。** `controller/channel.go:224-226` 同时按 lab 和 type 筛选时，计数查询与列表查询共用一个 gorm Statement，type 条件会被带进 `type_counts`。已用 DryRun 生成的 SQL 确认。修复办法：计数查询改为独立构建，并补一个同时带两种筛选的回归测试。

### D 组：Vue 接通

文件归属：`frontend/`。

- **D1（P1）** Passkey 二次验证改调 `/api/user/login/passkey/*`，并携带 `flow_token`。
- **D2（P2）** 维护页的缓存命中率不再读不存在的 `hit_count`，改为显示后端实际提供的字段。
- **D3（P2）** 模型统计中按模型的部分接入 `/api/perf-metrics`。按渠道的部分继续标为不可用。
- **D4（P2）** 运维质量卡接 C3，运维资源区接 C4，订单统计接 A5，个人统计按 C2 的新字段调整。
- **D5（P1）** 目录和手动路由页面不再依赖 `in_flight`，与 C1 一致。
- **D6（P2）** 如果现有邮件设置页能直接扩展，就补上 Resend 服务商选项和通知开关；不能扩展的，在账本里登记为延期。

D1、D2、D3 不依赖其他组，可以先做。D4、D5、D6 要等 A 组和 C 组把接口合同定下来，再按真实接口接通。

### 3E 组：上游代码重构评估与实施

评估范围分三块，分别出评估报告：

- **中继与计费核心**：`phase3e-eval-relay-billing.json`
- **渠道、路由、定价与模型数据**：`phase3e-eval-routing-pricing.json`
- **权限、配置、用户、支付、启动与系统任务**：`phase3e-eval-platform-payment.json`

每个候选按以下类别归类：

- 本地合同下已失效的代码，比如只剩一个分支的分组或订阅 switch、恒等函数、走不到的分支、单节点下的多实例分支
- 可以证实的性能问题
- 上游还没修的正确性缺陷
- 可以明显简化的复杂度
- 重复的规则实现

报告结论分为 do-now、defer、reject 三种。do-now 的条目按文件归属并入 A、B、C 组实施，每项都要有回归测试，并在账本里登记重构理由和复评条件。defer 和 reject 的条目只在账本中登记。

三份报告共 44 项（E1 16 项、E2 13 项、E3 15 项），其中 12 项定为 do-now：

| 编号 | 内容 | 实施组 |
|---|---|---|
| E1-2 | 删除 `Generate*OtherInfo` 没有读取方的 `userChannelRatio` 参数和 5 处 `-1` 实参 | R |
| E1-3 | 与 B11 同批：删除 `ChannelRatio == 0` 的不可达分支，FreeModel 判定只留一处 | R |
| E1-4 | 与 B14 同批：任务轮询的满载跳过和取消处理抽成一个共享函数 | R |
| E2-1 | 定价缓存改为 `atomic.Pointer` 发布的不可变快照，`updatePricing` 函数体不动 | R |
| E2-2 | 删除分组选路退役后留下的孤立代码（`model2channels`、两套候选过滤器、`CheckChannelEnabledForModel`、`channel_satisfy.go`） | B2 |
| E2-3 | `RouteCandidates` 复用请求内快照，排在 B1、B2、B3、B7、B9 之后 | B1 |
| E2-4 | 去掉 `Channel.AfterFind` 的隐式 `in_flight` 填充，管理视图显式附加 | B2 |
| E2-5 | 渠道倍率和并发上限的校验只保留模型层一份 | B2 |
| E2-6 | 分发器三次 gjson 扫描合并为一次 `GetManyBytes` | B1 |
| E3-1 | 七个入账函数的外壳收拢为 `settleTopUp`，A2 的金额核对和 A7 的状态码映射各只写一处 | A |
| E3-2 | `UpdateOption` 写库失败时返回错误，不再改写内存并报告成功；上游同样存在 | A |
| E3-3 | 删除本地改写后没有调用方的符号（`GetUserIdByAffCode`、`TransferAffQuotaRequest`、`isPositiveOptionValue`、`GetChargedAmount`、`QuotaForInviter/QuotaForInvitee`） | A |

E1-1（Realtime WSS 每轮已直扣钱包，结束时 `SettleBilling` 又按预扣 0 补扣一次全额）评估时定为 do-now；它与上游 206e0fa 相同，属于多扣，按用户决定改为 reject，保持上游原样。复评条件：上游修复这个问题，或用户另行决定。

defer 8 项：E1-5、E1-6、E1-7、E1-9、E2-7、E2-8、E3-5、E3-8，其余 24 项 reject，全部在阶段 4 连同复评条件登记到账本。E1-7（没有计费会话时 `SettleBilling` 退回不预扣的直扣路径）只在 `enable_free_model_pre_consume=false` 时可达，默认值为 true；本批不读取正式库，阶段 6 把停机备份恢复到隔离库比对时核对这个选项，若存的是 false，立即升为 do-now。

### 实施分组（执行版）

原 B 组的工作量拆成两组，C1 整体并入 B1，避免用户视图类型定义两份。每组在独立工作树中以本计划提交为基点实施，只改自己负责的文件。

| 组 | 条目 | 文件归属 |
|---|---|---|
| A 资金、支付与平台 | A1–A7、E3-1、E3-2、E3-3 | `middleware/access_token_*`、`model/topup*.go`、`model/gmpay*.go`、`service/gmpay_*`、`controller/topup*.go`、`controller/gmpay.go`、`model/affiliate*.go`、`controller/affiliate.go`、`model/option.go`、`controller/option.go`、`controller/user.go`、`common/constants.go`，`model/user.go` 中的 `GetUserIdByAffCode` |
| R 中继计费与定价 | B11、B10 中的倍率日志、B14 中的轮询日志、E1-2–E1-4、E2-1 | `relay/helper/price.go`、`relay/relay_task.go`、`relay/request_billing.go`、`service/quota.go`、`service/billing*.go`、`service/text_quota.go`、`service/log_info_generate.go`、`service/task_billing.go`、`service/task_polling.go`、新增的轮询扩展文件、`model/pricing.go`、`model/pricing_refresh.go`，`controller/channel-test.go` 中 E1-2 的调用点 |
| B1 路由选择 | B1、B2、B3、B7、B8、B9、B13、C1、B10 中的路由预览、E2-3、E2-6 | `service/route_*.go`、`service/channel_select.go`、`service/channel_request.go`、`service/token_route.go`、`service/token_route_preview.go`、`service/model_catalog.go`、`controller/model_catalog.go`、`middleware/distributor.go`、`controller/relay.go`、`controller/token_routing.go`、`model/route_candidates.go`、`model/channel_constraint.go` |
| B2 渠道与令牌管理 | B4、B5、B6、B10 中的优先级重排和 `RejectRetiredGroups`、B12、B14 中的硬删除清理、B15、C9、E2-2、E2-4、E2-5 | `controller/channel.go`、`controller/channel_authz.go`、`controller/channel_capacity.go`、`controller/channel_priority.go`、`controller/channel_upstream_update.go`、`controller/token.go`、`model/channel.go`、`model/channel_cache.go`、`model/channel_satisfy.go`、`model/channel_ratio.go`、`model/channel_priority.go`、`model/token_route.go`、`service/concurrency.go`、`middleware/retired_groups.go`，`model/user.go` 中的删除路径 |
| C 运营与可观测 | C2–C8 | `*/dashboard_summary.go`、`*/home_metrics.go`、`controller/performance.go`、`controller/perf_metrics.go`、`model/perf_metric.go`、`model/ticket.go`、`controller/tickets*.go`、`model/channel_lab.go`、`pkg/modellab`、`pkg/pricesource`、`*/price_source.go`、`controller/ratio_sync.go`、`model/pricing_default.go` |
| D Vue 接通 | D1–D6 | `frontend/` |

`i18n/locales/*.yaml`、`i18n/keys.go`、`router/api-router.go`、`router/extensions.go` 和 `model/user.go` 允许多组做互不重叠的追加或局部修改，集成时由主代理合并。`docs/upstream/` 下的计划、账本和 `CORE_SOURCE_DELTA.json` 只由主代理修改，各组在交接中列出新增偏离和对应的合同标签。各组同时负责自己文件范围内的原文核对：对清单中标为 `adapted` 的非测试文件逐段对照上游 206e0fa，没有合同理由、也不属于本组 do-now 条目的差异恢复为上游原文。

### 文档与工具

本批代码改完后，下列文档要与代码对齐：

- `GATEWAY_API.md`：
  - `/api/performance/routing` 的访问权限是 Root。
  - 优先级冲突返回 200 加 `success:false`。
  - 路由条目的 position 字段不作为输入。
- `GMPAY.md`、`EPAY.md`、`authentication.md`：删掉分组相关内容。
- `RECOVERY.md`：插件版本的描述改成原生行为，即轮询用当前插件，同时传入 `producerVersion`。
- `MOBILE.md`：删掉已移除的 InvoiceView。
- 根仓库：`scripts/README.md` 里的 Redis 改为 db2；`docs/README.md` 不再把 `routing-simulator` 列为验证入口。
- `SYNC_LEDGER.md`：登记本批全部提交的处置结果，并补记 `5e9595190`。

### 不借鉴的内容

以下做法与现行合同冲突，或者在单节点部署下多余：

- 等待队列
- 分组路由和分组倍率
- Redis 槽位与选主
- 价格版本化
- Sub2API 和 AxonHub 的权限模型
- 余额兑换订阅

## 阶段 4：集成与完整验证

集成步骤：

1. 按 A、B、C、D 的顺序合入四组改动，解决集成冲突。
2. 刷新 `CORE_SOURCE_DELTA.json`，确认每个 `adapted` 行都有 `reasons`。

验证项目：

- Go 后端：`build`、`vet`，全量 `go test -timeout 60s`，并与基线失败逐项比对。
- RelayKit 和 TokenKit：用 `GOWORK=off` 单独测试。
- 数据库：涉及 model、事务和查询的改动，在隔离的 SQLite、MySQL、PostgreSQL 上跑定向测试。
- Vue：`test:run`、`typecheck`、`lint`、`format:check`、`build`。

## 阶段 5：Codex 共同审计

用 `codex exec -m gpt-6-astra -s read-only -C <工作树>` 对 `4c7c12c0f..HEAD` 的完整差异做审计，按四个方向分别跑：

- 资金与权限
- 路由与并发
- 上游合并正确性
- 前端合同

报告保存到证据目录。每条发现由主代理回代码核实，结论记为确认、不成立或延期三类，并写明理由。确认的问题修复后，重跑受影响的测试，再让 Codex 复核一轮，直到没有未处理的确认项。

## 阶段 6：发布与部署

- **源码发布**：
  1. 把 Ren2Hub 分支快进推送到 bare `main`。
  2. 在根仓库的独立 checkout 中提交 gitlink 和文档，推送到根 bare `main`。
  3. 运行 `verify-renai-workspace`，取得 `VERIFY_OK` / `SYNC_OK`。live 工作树不做手工修改。
- **镜像与候选**：
  1. 用 `build-ren2hub-test.sh` 从已发布的 SHA 构建 `ren2hub-test:<SHA>-v1`。
  2. 核对 image ID、OCI revision、Go 1.26.9 和内嵌的 Vue 资源。
  3. 在隔离候选环境中跑五组验收（billing、WebSocket、Compact、插件、管理与扩展）和真实浏览器检查。任何一组失败，就停止部署。
- **正式更新**：按普通更新流程执行。
  1. 暂停新请求，排空在途请求，停机备份。
  2. 把备份恢复到隔离的 PostgreSQL 做比对。
  3. 只替换应用容器，然后运行 `verify-ren2hub-app.sh`，检查两个 HTTPS 入口。
  4. 提升发布记录，更新 `SERVER_PROFILE.md` 并同步。

数据目标保持 `ren2hub_core_e8bf2aa0c16f`、Redis db2、`ticket-attachments_e8bf2aa0c16f`。本批的 schema 变化只有新增列和新增索引，旧程序可以忽略这些列，所以回退时用旧镜像加原数据目标即可。

## 验收与风险

本批完成需要同时满足：

- 阶段 1 到 3 的每一项在账本里都有处置结果和验证证据。
- 全量测试相对基线没有新增失败。
- Codex 审计没有未处理的确认项。
- 取得 `SYNC_OK`。
- 候选五组验收和浏览器检查通过。
- 正式环境取得 `REN2HUB_VERIFY_OK`，服务器档案已更新。

主要风险：

- 25 个上游提交和收敛回归同时改动了计费和中继的核心文件，靠基线比对和三库测试兜底。
- A2 的金额核对如果语义判断错了，会拒收合法支付。所以实付金额语义没有核实的渠道一律不启用。
- B7 改变了自动排序的健康判定，需要用确定性测试覆盖打开、半开和闭合三种状态的转换，以及 429 冷却。

## 执行记录

- **阶段 0（2026-10-21）**：环境为 Go 1.26.9。
  - 本工作树位于子模块的 worktree 内，VCS 戳记会失败，构建时需加 `-buildvcs=false`。
  - 加上该参数后 `go build` 和 `go vet` 均通过。全量测试有 46 个包通过，失败的只有根包里的 `TestProductionEmbeddedAssetGraph` 和 `TestProductionEmbeddedModelPagesAndFiles`，原因是前端未构建、`embed-dist` 里没有资源，属于环境前置条件。
  - RelayKit 和 TokenKit 测试通过。
  - 日志见证据目录下的 `baseline-*.log`。
- **阶段 1 合并（2026-10-21）**：用 `absorb_upstream.py` 逐个文件做三方合并，结果是：
  - 直接采用上游 57 个，干净合并 39 个，新增文件 12 个；
  - 冲突 15 个文件，已逐项人工解决；
  - 碰撞 2 个，即 `channel_priority` 控制器和测试。
  
  处理原则：
  - 删除分组：去掉 `UseGroup` 和分组选路；`perf_metrics` 去掉分组维度。
  - 订阅已退役：只保留钱包提醒用的 `quotaReminderDue`。
  - Claude 保活：采纳上游实现；预扣仍放在选定渠道之后执行。
  - 强制预扣：`Reserve` 与 `ReserveBeforeSubmit` 统一为严格补扣，任务重试改为上游的“补足后再提交”。
  - 插件：查询函数同时接收 context 和 key。
  - 工单通知：改用上游新的按语言分发管理员通知的 `notifyAdmins`。
  
  非测试代码已能编译，测试适配进行中。合并明细见 `absorb-report.json`。
- **阶段 1 完成（2026-10-21，提交 `d4583d633`）**：上游测试中引用分组和订阅的 8 个测试文件和 2 个夹具已按合同改写。build、vet、RelayKit、TokenKit 通过；全量测试的失败仍只有根包那两个依赖前端构建产物的测试，与基线相同。
- **阶段 2 完成（2026-10-21，提交 `4d8e32a0b`）**：非测试 adapted 文件从 136 个、6,241 行降到 133 个、6,106 行，清单中每个 adapted 非测试行都带合同理由，`refresh-core-source-delta.py` 缺理由时拒绝写入。
  - 回归上游原文：分发器 i18n 文案、百度适配器、定价缓存的加锁粒度、预扣日志；`request_billing` 的重试补扣改走 `PreConsumeBilling`，删除本地的 `Reserve` 分支；`Channel` 的本地字段移到结构体末尾；恢复上游的 `createRootAccountIfNeed`（上游同样没有调用方，E3-10 决定保持原样）。
  - 保留：type 57 的 BaseURL 置空（否则 FetchModels 会把管理员 key 发往 chatgpt.com），`model_pricing_config.go:98` 保持现状。
  - 验证：build、vet、RelayKit、TokenKit 通过，全量测试结果与基线相同。
  - 12 个差异最大的文件没有做逐段原文核对，改由阶段 3 各组在自己的文件范围内结合 3E 结论完成。
- **阶段 3E 评估（2026-10-21）**：三份报告见证据目录 `phase3e-eval-*.json`，结论和分组见上文“3E 组”和“实施分组（执行版）”。

<!-- EXECUTION-LOG -->
