# 上游同步账本

NewAPI（`QuantumNous/new-api`）是 Ren2Hub 唯一上游；项目发布所用内部 bare `origin` 不是上游。Ren2Hub checkout 保留 `upstream` 指向 NewAPI；Sub2API（`Wei-Shaw/sub2api`）与 AxonHub（`looplj/axonhub`）仅作具体问题参考，不作为 Git 远端或并列上游。

## 2026-10-27 NewAPI main 32 提交吸收

2026-10-28 源码修复、适用验证和最终独立复审已完成；源码验收通过。提交、发布和部署不在本次完成状态内，`candidate_sha=null`。

随后一次交叉审计发现日志页未读取后端 `cache_tokens`，且 OpenAI 缓存明细被重复计入总量；现已修复字段映射，并按后端显式输入总量及 Anthropic 用量语义计算总量和命中率。同步清单补记任务插件请求头 allowlist 与流式输出观测两项适配理由。针对性 UI 测试 12/12、typecheck、ESLint、Prettier、前端生产构建和 `git diff --check` 均通过；改动仍未提交、发布或部署。

按用户批准的计划，从 Ren2Hub 工作树基线 `ae9be9d32116e7511633dcf3c7e6814cd5fc0737` 对照 NewAPI `206e0fa5c600127a314a97f88592c067f784f4a6..34003e9c2eed2c20e617f583fdfe54bf1bf42237` 的 32 个提交。rc.47 分界是 `e835b1bfc43b565d625537830dbfb19328925134`；其前 26 项至该 Axios 提交，后 6 项为缓存计费、协议及缓存测试更新。没有合并上游分支；钱包预扣/结算机制、渠道倍率/并发和扩展模块合同保持，缓存计费结果的变化见下文。本轮源码尚未提交、发布或部署。

| # | NewAPI 完整 SHA | 处置 | Ren2Hub 落点、证据与边界 |
| --- | --- | --- | --- |
| 1 | `eea05e010c7f212e1a617638fe27164f24d9478d` | absorbed | `go.mod`、`go.sum` 升级 `klauspost/compress` 到 1.18.7；V1、V3。 |
| 2 | `4d36f2c5245dd6676075be1c4f668dfe56d3340a` | not applicable | 上游只升级 `/web` 的 Vitest 5；Vue 独立使用锁定的 Vitest 4.1.11，本轮不迁移测试框架主版本；V7 验证现行 Vue 栈。 |
| 3 | `7d8b91268dab69d7150f345c448b5e250163146a` | adapted | Go、Docker/开发镜像、RelayKit、CI 与本地测试统一 Go 1.26.10；race 测试固定本地工具链版本；V3、V4。 |
| 4 | `0c2af9780e45f6f9073528fb0fcbdcd64b5d9495` | adapted | Responses 完成事件、错误写入/Flush、取消及结算生命周期；保留 `ObserveRouteStreamOutput`；V1、V4、V6。 |
| 5 | `7e6596728ff640392bd77169ebb6137a8fe0c309` | adapted | `relay/helper/stream_scanner.go` 集中写入预算并覆盖慢客户端、SSE/Responses/WebSocket；真实读取错误与关闭清理错误分开；V1、V4、V6。 |
| 6 | `94cc3ff0906db37703d5ce2bc6a479209b7b9b23` | adapted | Claude 缓存写入未拆分时保留可计费用量；扩展 DTO、merge、变量、结算快照与日志；V1、V2。 |
| 7 | `556517fa57d255c73428325f6a094fd8b4cab76c` | adapted | 原生任务路由支持 JSON/multipart；模型限定路由仅 JSON、query 无请求体、urlencoded 拒绝；文本字段受全局请求体限额约束，保留部件/文件/UTF-8/临时文件检查；V1。 |
| 8 | `dbe97beb9b06e8a4e23b5b8f009d882cc49f70bb` | adapted | 插件请求头改安全 allowlist，不透传认证、Cookie 与代理凭据；V1。 |
| 9 | `db80ac8e0c4063e67938cf651c240c69f4f4cf54` | adapted | 移除宿主对业务请求字段的递归计费校验；8 个任务插件在提交前校验自身上游参数，usage facts 仍由宿主限制；V1。Jimeng S2 固定 5 秒、Doubao 帧数向下取整沿用插件现有合同。 |
| 10 | `e2321a5a9f590e8398868cb7ef56af3d833faa07` | not applicable | 仅上游 React locale 排序；Ren2Hub Vue 不带回 React 文案体系。 |
| 11 | `eb169e74d772afdb281970d0cbe0b62fbf9fa994` | deferred | 汇率联动价格留待独立定价设计，不改现行价格同步或 `ChannelRatioInfo` 计价。 |
| 12 | `8802113263b8a94bf8957a1231b4ec04705d7dd9` | deferred | 共享插件端点竞争留待单独路由设计；不改渠道选择、并发和插件路由边界。 |
| 13 | `d8e65613c09adf3663f960ea4316414a99694751` | not applicable | 仅上游 React 使用日志复制模型名；Vue 使用自己的 usage-log 组件。 |
| 14 | `d0b840e75ff36d731b04ff981ee361819acd593f` | adapted | JSON Schema 属性顺序保真；检查本地 RawMessage/类型断言并保留 Ren2Hub 转换器约定；V1、V2。 |
| 15 | `e9d4d6353ce2c11df753d751ba1ad778e2433a88` | adapted | OpenAI 12 小时缓存写入字段与计价接入，保持 `ChannelRatioInfo` 和 `prompt_cache_options`；V1、V2、V7。价格目录通过 `billing_expr.cc12h` 呈现，当前 `/api/pricing` 没有独立 12h ratio 字段；补齐上游内置表达式在普通/长上下文的 12h 计费断言（V9）。 |
| 16 | `7fb6e57a5ff671582131c74e00bdfaa867d4e985` | adapted | 渠道高级自定义全局/路由级透传 roundtrip 与全路由风险提示；不放宽后端路由/转换器限制；V7。 |
| 17 | `30748c6558bc97de49ff74871cfec717e50f3322` | adapted | 访问日志统一清理查询凭据；其他支付入口 URI 和 RelayInfo 调试日志复用同一清理逻辑；保留上游 logger_test.go 全部回归并增加签名/OAuth 用例；V1、V3、V9。 |
| 18 | `33c61a24234aa20cb10e8a644304322c476361de` | not applicable | 仅 React 编辑抽屉状态修复；当前 Vue 页面不包含该组件。 |
| 19 | `a404460413fb7c0c2182e935bb373ba51904df70` | not applicable | 该提交迁移上游 `/web` 到 Bun 1.4.4 与 `bun check`；Vue 仍统一固定 Bun 1.4.0，并用 vue-tsc、ESLint、Prettier 检查。按本轮既定取舍不连带迁移工具链，不能归因于 Vue 不使用 Bun。 |
| 20 | `0034b24914981078e6ef1a3794e43afe28df43af` | adapted | 任务超时默认改 4320 分钟，保留环境覆盖及 `0` 禁用；覆盖旧 24h、60h、正好 72h、73h、重复退款与零值；V1。 |
| 21 | `1dafb19a429aa7c17b3f4efa457078984b6a4fd3` | adapted | RelayKit Gemini 转换保留顶层 `imageConfig`；V2。 |
| 22 | `b6b85cae5931cd27e9adf21cfccfede800980a83` | not applicable | 仅 React panels 依赖；未引入 React 依赖体系。 |
| 23 | `4105bd089cce1a7c1067e77babf45ebd8d4455ec` | not applicable | 仅 React input-otp 依赖；未引入 React 依赖体系。 |
| 24 | `0b667ca478d6dbce6f60a2e3c4a217a7aab64031` | not applicable | 仅 React i18next 依赖；继续使用现有 Vue I18n。 |
| 25 | `5c8032d7002f82fb04fabf9a9c9b143de78547fe` | not applicable | 仅 React react-i18next 依赖；未引入 React 依赖体系。 |
| 26 | `e835b1bfc43b565d625537830dbfb19328925134` | already present | Vue 锁文件已使用 Axios 1.20.0；未重复改动。 |
| 27 | `9823ebc7ea4ac7102d5d8478d395c7d7eaf6c285` | adapted | 计入 `prompt_tokens_details.cache_creation_input_tokens`，与缓存写入拆分、缺失/显式零价格和聚合字段去重规则协同；V1、V2、V7。 |
| 28 | `bc795cc1185a0ecef0ca51f73d337667026ec62f` | adapted | `stop` 非字符串和字符串数组的协议转换 golden 覆盖；V2。 |
| 29 | `f1569d1ea7e87cd1a73c5f89d989b6b3bdaf68b3` | adapted | Vue 价格目录按同一分类函数驱动筛选/计数；固定层级 request、Token/混合 token、有 usage schema 的 task，解析失败回退旧 quota 类型；V2、V7。 |
| 30 | `4d962e464de7a16ff8bf16c660307899d49108ba` | adapted | Claude 工具参数数字保真；V2。 |
| 31 | `0165fef98d41b02a7368e89dc5fa2df651f8587b` | adapted | Gemini 混合文本及工具调用保真；V2。 |
| 32 | `34003e9c2eed2c20e617f583fdfe54bf1bf42237` | adapted | 缓存统计测试以原子序列和测试名构造键，适配本地两参数缓存查询接口；`service` 测试重复 20 次；V1、V5。 |

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
- V10：controller 发现 523 项，非矩阵 497 项分 25 批通过；新增日志回归后 55 项相关 Stream/Responses/WebSocket/KeepAlive/Epay 再次通过，零 skip/fail。隔离三库矩阵 71/71 suite 全通过（SQLite 41、MySQL 46、PostgreSQL 46 次方言执行），零失败/零跳过，清理错误为空且本轮 label 的容器/网络均已清理。controller 最终发现 524 项，累计执行 523 个唯一顶层测试，仅 `TestVLLMStatusLive` 不执行。使用 Go 1.26.10、MySQL 8.4.11、PostgreSQL 15.19；真实 VLLM/供应商不在本地门槛内。完整摘要与证据哈希在 `/tmp/ren2hub-resume-20261028-validation/summary.json`、`evidence-sha256.json`。

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

| 文件 | 上游来源与处置 |
| --- | --- |
| `controller/ratio_sync.go`、`controller/ratio_sync_test.go` | `eb169e74d772afdb281970d0cbe0b62fbf9fa994`；汇率联动延期，清单补 `exchange-rate-pricing-deferred`。 |
| `dto/channel_constraints.go`、`middleware/distributor.go`、`model/channel_constraint.go`、`model/model_pricing_config.go`、`relay/relay_task_test.go` | `8802113263b8a94bf8957a1231b4ec04705d7dd9`；共享插件端点竞争延期，逐文件补延期标签。`model_pricing_config.go` 不属于 f1569d1；后者只改上游 Web 分类。 |
| `i18n/locales/en.yaml`、`i18n/locales/zh-CN.yaml`、`i18n/locales/zh-TW.yaml`、`i18n/keys.go`、`pkg/jsplugin/routing.go`、`router/task-plugin-protocol-router.go` | 同属 `8802113263b8a94bf8957a1231b4ec04705d7dd9` 延期，相关路由错误和文案未孤立带入。 |
| `setting/billing_setting/builtin_billing_test.go` | `e9d4d6353ce2c11df753d751ba1ad778e2433a88` 的 12h 普通/长上下文用例已补到本地 ChannelRatio 合同，精确额度断言保留。 |
| `middleware/logger_test.go`、`middleware/logger_sync_test.go` | `30748c6558bc97de49ff74871cfec717e50f3322`：前者恢复完整上游原文，后者补本地 Google 签名/OAuth 覆盖，不再记为删除上游测试。 |

首轮 Claude 只读审计已实际执行，CLI 返回 `canonicalModel=claude-opus-5-5`，原始输出 `/tmp/ren2hub-claude-audit.json`。第二轮复审 `/tmp/ren2hub-resume-audit-20261028/result.json`（CLI canonical model 同上）判定原 6 项关闭或不成立，无阻塞；当时尚未结束的三库矩阵现已完成。其新增低风险日志缺口已修：`ParseQuery` 部分解析失败不再回退原始 query，整个 URL 不可解析时记录显式无敏感值标记；同时去掉日志 URL 的 userinfo 和 fragment，避免原始 `#` 或 URL 用户信息继续泄露凭据。补充任务 DTO、Vidu、RawMessage 工具和 SSE 写预算上下文后，最终增量复审通过、无阻塞缺陷；五项上下文全部关闭。CLI 实际返回 `canonicalModel=claude-opus-5-5`，原始结果 `/tmp/ren2hub-resume-audit-20261028/increment-result.json`，调用与退出状态见同目录 `increment-run.json`。最终代码指纹 `cac9684d25728e712251c6d15940237ee85ee8d36e2a1177961f0512c0812a3a`（base `ae9be9d32116e7511633dcf3c7e6814cd5fc0737`，覆盖全部修改/新增源码，账本与生成清单单独核验）；已确认复审后源码未再变化。审计补充发现的 `ClaudeRequest.AddTool`→`GetTools` 混合 RawMessage 组合边界与固定上游原文相同，全仓无调用；当前请求计数、转换和搜索计费路径经静态核对及现有定向回归通过。未来接入该 helper 或上游修复时重评，本轮不为未使用 API 增加核心偏离。源码未提交、未发布、未部署；延期 2 项仍保留后续设计。

最终审计另登记非阻塞边界：Vidu 的非声明、非前缀映射名（如 `xxx-viduq2`）可能触发不同的模型判定；`RelayInfo.ToString()` 的渠道 BaseURL 仍按现行逻辑记录，若管理员将凭据置于 BaseURL 会存在泄露风险；未知字段名或无值键中的凭据不在现行敏感键清单保证内。以上未在当前声明模型/既有配置合同内扩修，未来引入相应输入或修改日志合同前须重评。真实供应商、真实 VLLM 和应用候选/正式部署均未执行，不冒充本地验收覆盖。

本轮来源仅消费临时缓存的固定 Git 提交对象（`git show` / `ls-tree` / `log`），不读取其脏索引或未展开的工作树。默认 `reference-projects/new-api` 不存在，标准离线采集不代表远端最新状态；固定窗口对象与 32 个完整 SHA 已另行校验，未 fetch 或移动参考目录。

2026-10-23 之前的批次记录、浏览器迁移清单和渠道历史处置已于 2026-10-27 移出工作树；下文括号内的 `<提交>:<路径>` 可用 `git show` 读取，Ren2Hub 记录在本仓库执行，工作区记录在根仓库执行。历史段落提到的 `/home/work/ren2hub-*` 实施副本与证据目录已删除，仅作当时事实，不能再作为可复核证据。

## 2026-10-26 Dashboard 系统状态卡片共享快照

按用户批准，Dashboard 系统状态卡片对所有登录用户开放。新增扩展接口 `GET /api/user/system-status`（`service/dashboard_system_status.go`、`controller/dashboard_system_status.go`），从现有唯一观测缓存投影 CPU、内存、磁盘（含绝对容量）、应用 HTTP 正文流量及 1h 分桶历史，并读取主机负载均值和逻辑核数（`service/dashboard_system_load_{unix,windows}.go`，gopsutil `load.Avg` 带 recover）。成功率取 `perf_metrics.QueryHourlyAll(24)`，所有调用方共享 60 秒缓存，失败缓存 10 秒，并以 `state` 区分 ok、no_requests、disabled、error。原生查询不接受 context，因此等待者最多等 3 秒；超时返回不缓存的 error，资源读数照常返回，共享查询结束后再写缓存。数据库/Redis 探测、检查原因、阈值、进程和连接池仍只在 Root 的 `/api/performance/*` 返回。

该接口仅限会话访问，拒绝 PAT；关闭缓存，按用户限制每分钟 30 次（`middleware/dashboard_status_rate_limit.go` 复用原生 `userRateLimitFactory`）。原生文件和 `router/api-router.go` 不改，路由登记在 `router/extensions.go`。核心清单新增 7 个 extension，upstream_equal 与 adapted 数量不变；NewAPI 基线仍为 `206e0fa5c600127a314a97f88592c067f784f4a6`。

## 2026-10-24 本地修复、独有观测与普通 API Key 计数

从 Ren2Hub `6e4539904d649f892cb7d2f61ea5fedc8c5198bb` 实施用户批准的日志、观测、Dashboard 和原生计数批次；历史参考为 `9a63062227c31ba54a6346e07ee8b4fd8802f22f`。NewAPI 核心仍冻结在 `206e0fa5c600127a314a97f88592c067f784f4a6`，另核对固定快照 `33c61a24234aa20cb10e8a644304322c476361de`，没有整包升级。Sub2API `a34f373973bf547ccec301c7405eb45194e1f28a` 仅择取普通 API Key 真实计数合同；AxonHub `cbdf9a6d4a3dab1e4bfe5168ce5d80205c805913` 的图片变体转换仍是候选，本轮不开放。

日志采用当前取消/序列机制消除失败和筛选交错下的旧结果；观测以当前 performance 扩展的唯一缓存快照提供资源范围、探测、GC、HTTP body 吞吐及实例历史，不恢复旧评分/队列/账务。Root 运维权限保持；Dashboard 请求限制来自实际限流配置，移除统一折扣空卡和 Alchemy 占位。Claude/Gemini 两版本计数使用独立真实转发，复用现有权限/路由/限流/并发，不进入生成或估算成功回退。修复前 Gemini `/v1` 计数误走生成及预扣退款已由本地 mock 复现，没有真实供应商成本。

唯一维护位置、来源取舍、兼容边界、上游替换条件及验证状态见[本轮记录](LOCAL_REPAIRS_20261024.md)。10 组共同缺陷沿用[待上游台账](UPSTREAM_KNOWN_ISSUES_20261024.md)，新增缓存全部清理假成功与任务通用测试证据；登记不等于修复。D 类、其余未实现协议和部署不扩入；未提交阶段 `candidate_sha=null`，整合验证由主代理收尾，不以本账本表示已发布。

发布前独立审计另修正本地计数的渠道头覆盖遗漏与工具 JSON 误拒绝，复用现行头规则并限定协议引用位置；五包 24 个顶层/85 个含子用例及 relay/router vet 通过，详见本轮记录的“发布前审计修正”。来源基线和等待上游政策不变。

## 2026-10-24 Vue 前端审计修复

从 `79e237455262fcb652379e13965ce86fb1a4eefa` 按已批准计划处理 A、B、E 类本地问题，冻结 NewAPI `206e0fa5c600127a314a97f88592c067f784f4a6`。实施与验证见[本轮记录](FRONTEND_FIXES_20261024.md)，共同缺陷统一维护在[待上游台账](UPSTREAM_KNOWN_ISSUES_20261024.md)，本账本不复制条目。C、D、延后项及退役项不扩入，无后端 API、status 或数据库合同改动，不代表应用部署。

## 2026-10-23 全量审计修复

本轮以 NewAPI `206e0fa5c600127a314a97f88592c067f784f4a6` 为冻结核心，从 `a0a1f8dd1b7909089aed0339bd714406a5ccb66b` 整合并修复。邀请返利参考 Sub2API `a34f373973bf547ccec301c7405eb45194e1f28a` 的来源追踪和操作幂等，另按本地净额冲销合同记录已转出负债及后续抵债；AxonHub `cbdf9a6d4a3dab1e4bfe5168ce5d80205c805913` 没有可直接复用的返利实现。

已处理多 Key 缓存轮询、额度单位、目录报价、返利合规与额度幂等、OAuth 邀请归属、设置保存和草稿、Dashboard 数据降级、日志错误状态、工单附件提交与崩溃孤儿、渠道和用户提交后收尾、统计精度及错误文案。详情、迁移语义、实现边界和原始证据见[修复记录](AUDIT_FIXES_20261023.md)。实际提交 SHA、内部 bare 发布、根 gitlink 和正式应用身份记录在工作区[服务器档案](../../../docs/server/SERVER_PROFILE.md)，本文不重复维护镜像或运行态值。

验证：前端 Vitest 250 个文件/1660 项通过，typecheck、lint、生产构建通过；修复专项浏览器 32/32 通过，覆盖桌面/移动和明/暗主题。后端定向检查、build、vet、三库返利账本与系统任务矩阵及轮询 race 通过。浏览器业务接口使用受控 fixtures，没有替代真实 OAuth 或支付供应商联调。controller 中未配置权限的 MySQL database-matrix 分支不计为通过；具体未验证项列在修复记录。

### 保留的 NewAPI 共同缺陷

NewAPI `206e0fa5c600127a314a97f88592c067f784f4a6` 仍有以下相同问题，参考项目没有适用于当前合同的直接修复，本地按已批准范围保留：

| 问题 | 重开条件 |
| --- | --- |
| Realtime 最终累计结算重复扣费 | NewAPI 修复对应路径，或另行批准本地偏离并补一次性结算回归 |
| Waffo 将处理中/授权等待状态作为失败终态 | NewAPI 增加非终态合同，或另行批准本地状态映射 |
| Uptime Kuma 缺失心跳按 0 统计 | 上游区分缺失与零值，或另行批准本地处理 |
| 服务端日志导出无行数/字节上限 | 上游增加上限/流式合同，或另行批准本地服务端偏离 |

本地客户端已避免多份完整导出副本并检测截断，但服务端无界问题仍保留。上表是跟踪状态，不代表问题已修复或通过运行时验证。

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

| 范围          | 当前实现与来源边界                                                                                                                                                                                                                                                  |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A1 工单通知   | 最终邮件正文按工单类型转义，保留其他通道及原生 HTML。NewAPI 仅有转义惯例，按类型边界属于本地修复；真实 SMTP/HTTP 回归通过。                                                                                                                                         |
| A3 GMPay      | 删除无效结算开关，真实开关只控制新订单，历史订单仍验签并入账。上游无 GMPay，不改造支付账本或增加结算暂停语义。                                                                                                                                                      |
| A4 设置保存   | 共用保存器先停用、再依赖参数、后启用，保留逐项成功基线和未确认草稿；所需 OIDC discovery 先于任何写入。原生 discovery 后端来自 NewAPI，通用顺序及 Vue 整合是本地修复，非原子事务。                                                                                   |
| A5 PAT        | 从现有 authz 资源元数据和实时权限生成普通用户可授予 routing scopes，不复制权限目录；其他管理资源仍有角色门槛。                                                                                                                                                      |
| A6 候选交集   | 复用原生规范化函数，按实际请求名到 ability 单向求交；Models 返回获准请求名，具体同族不同模型不能互相授权。                                                                                                                                                          |
| A7 WS         | manual+pin 的首次/续次共用当前组/模型/列表资格检查；保持锁定计价组、auto pin 和原任务边界。撤权 403/1008，技术故障 500/1011，发送和预扣前失败。共享 SQL 任一错误立即传播；定向真实 WS 与 race 通过。                                                                |
| A8 数据身份   | 验证器将首次安装数据库身份与当前镜像 SHA 分离，仍核对 release.env、实际 DSN、Redis、目录及镜像；19 项本地验证器/安装器回归通过，未执行正式更新。                                                                                                                    |
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
