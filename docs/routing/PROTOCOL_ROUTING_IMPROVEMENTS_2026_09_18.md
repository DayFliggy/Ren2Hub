# 协议转换与路由改进验收记录

基于 `1fa59c2ff3ba39040e51e564d956822c31447e5a`，本轮修复六项已复现问题，并把统一转换计划接到 Token 路由预览。只发布源码，不部署应用。

本文记录首次交付时的范围与验证。随后对 `52e331e02` 逐项复审发现响应终态、工具媒体顺序、缓冲伴随信息和预览一致性仍有遗漏；修复与最终验收以 [后续计划审计](PROTOCOL_ROUTING_PLAN_AUDIT_2026_09_18.md) 为准，不能仅凭此前通过的基础用例认定全部语义已覆盖。

## 已交付行为

| 原问题 | 实现与正式回归 |
|---|---|
| Responses 正常限长被当成断流 | `ResponseTermination` 分离生成结果与传输结果；JSON、SSE、缓冲 JSON 保留 limited 内容、usage 和原因。`response_termination_test.go` 验证 max_output_tokens、content_filter 及未知原因 |
| 工具图片退成字符串 | 需求带 `tool_result` 位置；最初使用结构化旁传，后续审计改为直接有序历史转换，修复普通消息与工具块交错时的顺序丢失。`tool_media_test.go` 与 `tool_media_contract_test.go` 验证双向结构和拒绝边界 |
| pause_turn 伪造 completed | 原生保留；跨协议返回 502 转换错误、禁止重放并保留已观测用量。可预知暂停的 Claude 托管搜索跨协议请求在发送前拒绝。`response_semantics_test.go`、Claude 宿主回归覆盖 JSON/SSE |
| 引用和托管工具丢失 | DTO、响应注册表、流式状态、宿主调用入口一起更新；保留可表达 URL 引用和托管搜索／MCP 结果，按身份去重。无法表达时明确诊断或拒绝，不伪造客户端工具调用 |
| 转换资格晚于重试裁剪／64 上限 | 完整资格进入统一选择器；手动 none/same_channel 保留首次发送扫描集合，发送后锁定策略。HTTP 回归覆盖 63/64 个不兼容文本渠道，以及 64 个不兼容流式转写渠道 |
| 缓冲 SSE 按行误解析 | 与流式路径共用完整事件解码器；测试多行 data、三种换行、BOM、注释、事件名、整事件上限和 EOF 完整尾事件，不向 JSON 客户端输出心跳 |

保留已有单实例 FIFO、硬优先级、手动 position、最多 3 次真实发送、共享重试储备、接受状态与输出提交屏障。正常限长不降低渠道健康；响应转换错误归为 `local_convert` 且禁止重试。未知 incomplete 在原生路径原样保留，无法等价转换时明确失败，不猜测 completed。部分输出后失败和没有终态的 EOF 沿用错误结束，不拼接第二份响应。

预览新增 `POST /api/routing/tokens/:id/preview`，支持自动、手动、无 Profile 的旧 Token。旧 Profile 接口委托共同入口。快照显示模式、版本、时间、转换资格、排除原因、priority/position、负载、权重和本次顺序；容量、价格、额度、配置、亲和及状态归属明确待执行复核。预览不创建 Profile、不预扣、不持有 Permit、不发送上游请求。

排队后与发送前重读资格；本轮额外发现并修复 Token 缓存缺少 `ConversionPolicy`，防止后续缓存命中把 strict 变成默认 safe。排队时修改转换策略会拒绝旧计划。缓存只补策略元数据，配额字段和更新算法保持不变。

`route_decision.attempts` 增加终态类别和响应诊断 code/path；指标增加日志队列丢弃计数。响应伴随信息仅在当前请求内保留，不写入正文日志或新持久化历史。

## 计费隔离

未修改价格、倍率、用量归一化、预扣、结算、退款、恢复算法或数据库结构；未修改 `Usage/BillingUsage` 账务字段。协议处理器继续使用已有用量转换／补全函数，只修正终态与传回已观测用量。没有新增结算或退款分支。三库契约和本地 SDK 验收继续检查用量分类、发送次数、消费次数与释放行为。

本轮仍不支持暂停自动续跑、隐式模型降级、路由级透传、Responses WebSocket、自适应评分、消费预算和对账管理。原生文件引用、签名和续跑数据仍受既有 Token／渠道／凭据归属约束。URL 引用支持不能推导为原生文档引用完整支持；无法表达的引用、媒体和托管工具会明确拒绝。

## 验证记录

所有后端正式测试命令使用 `-timeout 60s`，隔离 SQLite、模拟 Redis、本地 HTTP 上游；三库测试另外使用临时本机 MySQL 5.7 与 PostgreSQL 16。没有请求真实供应商或修改服务器应用状态。

| 检查 | 结果和边界 |
|---|---|
| 主模块 | `go test -p 2 ./... -timeout 60s -count=1` 通过；后续相关小改以受影响包全测／定向测试覆盖。`go vet`、Windows 构建、Linux amd64 构建通过 |
| 独立 RelayKit | `GOWORK=off go test -p 2 ./... -timeout 60s`、`go vet ./...`、`go build ./...` 通过；最终转换包再次通过 |
| 竞争检测 | service/helper 准入、手动策略、Token 预览、SSE 目标用例，以及 RelayKit 终态、引用、工具图片目标用例 `-race` 通过 |
| 数据库 | BillingDatabaseIntegration 的 SQLite/MySQL/PostgreSQL 共 102 场景通过；RoutingDatabaseIntegration 的 MySQL/PostgreSQL 与单独 SQLite 用例通过，没有把缺 DSN 的跳过当成通过 |
| 官方 SDK 模拟 | Python/Node 基础、语义、音频、媒体、legacy、云供应商及云工具／断流套件通过；仅本地模拟，不表示真实供应商兼容性全覆盖 |
| 前端 | 全量 Vitest 125 文件、591 用例通过；最终相关用例、类型检查、lint、格式检查和生产构建通过 |
| 浏览器 | Playwright gateway 控制台 4 场景通过：桌面／移动端 × 明／暗主题，包含预览失败及不可用状态、横向溢出和焦点检查；截图人工检查 |
| 变更审查 | `git diff --check` 通过；检查计费实现隔离、响应转换失败禁止重放、终态之后不补成功、预览无资源预留及诊断不含正文 |

全套测试中的外部运行环境可选用例按既有方式跳过；上表所列 SDK 和三库套件另外配置运行环境实际执行。未运行真实供应商验收、生产压测或应用部署。SDK、三库和构建日志保存在工作区忽略目录 `tmp/protocol-routing-review/`，该目录不随源码发布。

## 参考来源与许可

参考固定版本为 new-api `7209b6db95ef`、AxonHub `19a3c27d8b94`、sub2api `efe9aab1e4ec`。仅选择性吸收 new-api 的响应伴随信息／引用设计，移植文件保留来源标记并遵循项目 AGPL-3.0 许可；未整体合并参考仓库。AxonHub 的工具媒体结构保留作为行为参考，保留 Ren2Hub 自身的预算、健康归因和提交屏障，不采用参考项目中允许部分输出后重放或 EOF 补成功的实现。
