# 上游同步账本

NewAPI（`QuantumNous/new-api`）是 Ren2Hub 唯一上游；项目发布所用内部 bare `origin` 不是上游。Ren2Hub checkout 保留 `upstream` 指向 NewAPI；Sub2API（`Wei-Shaw/sub2api`）与 AxonHub（`looplj/axonhub`）仅作具体问题参考，不作为 Git 远端或并列上游。

当前源码适配基线为 `e449cc700eb569cdfc2199cb73e97de46944f43c`，最后一次完整核对日期为 2026-10-28；这不表示该 SHA 仍是上游当前 HEAD。2026-10-23/24 的专项报告已于 2026-10-29 从工作树清理；可在 Ren2Hub 仓库用 `git show 85a34bc6bb94e69a323f80a137c3f09effb7e474:docs/upstream/<路径>` 读取清理前版本。旧条目中的“待上游”、`candidate_sha=null` 和缺陷状态只描述当时输入与工作阶段；不得作为当前待办，重新启动时须按最新上游复核。

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

按用户批准，Dashboard 系统状态卡片对所有登录用户开放。新增扩展接口 `GET /api/user/system-status`（`service/dashboard_system_status.go`、`controller/dashboard_system_status.go`），从现有唯一观测缓存投影 CPU、内存、磁盘（含绝对容量）、应用 HTTP 正文流量及 1h 分桶历史，并读取主机负载均值和逻辑核数（`service/dashboard_system_load_{unix,windows}.go`，gopsutil `load.Avg` 带 recover）。成功率取 `perf_metrics.QueryHourlyAll(24)`，所有调用方共享 60 秒缓存，失败缓存 10 秒，并以 `state` 区分 ok、no_requests、disabled、error。原生查询不接受 context，因此等待者最多等 3 秒；超时返回不缓存的 error，资源读数照常返回，共享查询结束后再写缓存。数据库/Redis 探测、检查原因、阈值、进程和连接池仍只在 Root 的 `/api/performance/*` 返回。

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
