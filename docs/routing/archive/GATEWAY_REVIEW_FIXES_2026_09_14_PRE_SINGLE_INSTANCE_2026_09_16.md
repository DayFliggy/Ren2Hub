> 历史归档：2026-09-16 单实例改造前的工作树材料。文中 Redis 并发、多实例和旧测试结果只适用于所记版本，不是当前实现的验收证据。现行契约见 [统一路由](../UNIFIED_ROUTING.md)。

# 2026-09-14 网关二次评审修复

本轮处理 [二次对比报告](C:/Users/WhiteDream/Desktop/RenAI-Workspace/reference-projects/reports/2026-09-14-route-feasibility/README.md) 的 R01–R05。评审基线为 `a2d7371b2bf0a3ee50e7a69b895caa08730460b6`；下述变更位于本地工作树，尚未发布或部署。历史报告中的复现结果保留，不改写成修复后的结果。

## 处置与边界

| 编号 | 已实现行为 | 正向回归 |
|---|---|---|
| R01 | Gemini 原生、Chat、Responses JSON 与共用 SSE 先解析带内错误、政策、有效内容和终止信号。HTTP 200 的错误信封按上游 code/status 返回错误；空体、畸形体、未知结束原因明确失败。政策阻断不处罚供应商；已交付部分输出按现有可信 usage 或估算规则结算。Gemini/Vertex 转 Anthropic 的 MAX_TOKENS 保留 `max_tokens`，不再被合成 `end_turn` 覆盖 | `relay/channel/gemini/response_signal_test.go`、`router/gateway_gemini_signal_test.go`：Gemini/Vertex × 四种入站，104 个 HTTP 场景，含 JSON/SSE、成功预留释放、退款、健康和禁止重复发送 |
| R02 | 将 Responses 历史 `function_call.namespace` 识别为工具身份，即使没有 tools 声明也执行同一策略。`safe/strict` 拒绝不保真的跨协议转换，`allow` 保留明确损失诊断，原生保留。缺省/null 不误判，空白或错误类型拒绝 | `relaykit/relayconvert/request_namespace_test.go`、`router/gateway_namespace_test.go`：24 个预览及 HTTP 场景，包含原生和三个转换目标、三种策略、JSON/SSE；拒绝时无发送、预扣、日志或健康处罚 |
| R03 | 要求和计划增加操作区分；宿主登记可执行的旧操作，预览、静态候选过滤与执行共用规则。无法执行的跨操作组合在预扣及发送前排除，`allow` 不能绕过。Chat→Responses 自动升级不影响旧接口，非文本操作写入计划诊断 ID | `relaykit/relayconvert/request_operation_test.go`、`service/routing_legacy_operation_test.go`、`router/gateway_legacy_operations_test.go`：操作变更拒绝、预览一致性，17 个 HTTP 场景覆盖零发送、原生输入与响应结构，以及跳过高优先级不支持渠道 |
| R04 | `/v1/edits` 经 Token 鉴权后直接返回 501 `api_not_implemented`，不再进入图片 DTO、准入或计费。`/v1/images/edits` 保持独立产品接口 | `router/gateway_legacy_endpoints_test.go` |
| R05 | `/v1/engines/:model/embeddings` 进入真正的 Embedding 处理链，模型来自路径；正文同名可接受，冲突返回 400。实际路由和发送路径规范到 `/v1/embeddings`，保留 RequestURI 用于追踪。标准入口缺少模型也明确拒绝 | 同上，R04/R05 共 19 个 HTTP 场景，包含 OpenAI/Azure、路径模型、权限、参数、模型映射、认证头、向量结果和账务 |

旧 completions 保留已具备专门路径及响应处理的 OpenAI 兼容适配器、NewAPI、DeepSeek、Ali、Moonshot；moderations 仅允许已登记的原生通路。Moonshot 的特殊套餐端点只实现 Chat，不能借 completion 声明进入该端点。能力登记表示宿主能执行请求和响应契约，不表示任意供应商账号或任意模型均提供此产品。AdvancedCustom 仅在显式匹配的原生路由下允许旧操作，配置 Chat 转换器仍会拒绝。

没有新增 legacy edit 转换器、moderation 分类器或可逆 namespace 映射。`allow` 对 namespace 的行为仍是显式接受损失，不能当成保真支持。

## 同链路补充修复

失败响应验证暴露出零预扣的恢复记录没有终态。`BillingSession.Refund` 现在会终结可信用户或显式零预扣请求的恢复记录并停止 heartbeat；前置持久化失败不误标资金已退，最后状态写成功才锁定已退款。重复退款不加钱，失败可由同一会话或恢复任务重试，已开始结算的请求不回退。

回归位于 `service/billing_session_refund_test.go`。现有 `TestBillingDatabaseIntegration` 追加零预扣和终态写入失败两种场景，在 SQLite、MySQL、PostgreSQL 上各运行 14 个场景，共 42 个；包含原有非零预扣、重复结算、取消、Playground、日志/统计失败及恢复。

Gemini 生成请求在读取路径模型时复用现有请求校验器，先验证 JSON 和 DTO，再进入状态归属查询，防止非法 JSON 被误归类成状态不可用 503。`router/gateway_gemini_input_test.go` 覆盖 JSON/流式两个 action 的 10 个本地拒绝场景，不发送、不预扣、不处罚供应商；Embedding action 继续使用独立 DTO。

## 验证与运行

本轮新增 174 个隔离 HTTP 正向场景，另有计划、预览、信号解析和账务故障回归。上游由本地 HTTP fixture 提供，Redis 使用测试实例或 miniredis；不代表真实供应商认证。

| 检查 | 结果或证据 |
|---|---|
| 后端全测 | 通过：`go test -p 4 -timeout 60s ./...`；`.local-tests/gateway/review-fixes-backend.log` |
| 独立 RelayKit | 通过：`GOWORK=off` 下全测、vet、build；`.local-tests/gateway/review-fixes-relaykit.log` |
| 官方 SDK 冒烟 | 通过：Python/TypeScript 每语言 32 条文本路径、16 轮工具往返、16 条中断流，以及原生状态续轮；使用已有锁定版本和本地上游，见 `.local-tests/gateway/review-fixes-sdk.log` |
| 三数据库账务 | 42 场景通过；`.local-tests/gateway/review-fixes-billing-databases.log` |
| 静态检查与构建 | 通过：主模块 vet、受影响构建及 diff 检查；`.local-tests/gateway/review-fixes-vet.log`。构建产物为 `.local-tests/gateway/ren2hub-review-fixes.exe` |

回归可直接运行，不需要研究目录的 Go overlay。数据库测试使用 `BILLING_TEST_MYSQL_DSN` 和 `BILLING_TEST_POSTGRES_DSN`，只通过提供的连接创建并销毁临时测试数据库，不修改提供连接对应数据库的业务表。SDK 测试使用既有 `GATEWAY_SDK_PYTHON`、`GATEWAY_SDK_NODE` 与测试锁文件，详细操作沿用 [验证说明](../GATEWAY_VALIDATION.md)。

宿主能力合同由 v11 升至 v12，现有指纹刷新会重建受影响能力快照。本轮没有数据库结构变更，不重写历史账单，不重置管理员配置。源码、构建产物、本地运行进程及生产部署是分别记录的状态；本轮没有替换运行中的应用或执行生产操作。

控制台产品代码未变化；本轮不重跑无关前端套件。历史报告列出的停用订阅页面 API 不一致仍属于重新启用前的独立工作。本轮完成 R01–R05 的本地整改，不扩张为全部路由、所有供应商或原六阶段全部发布条件已经满足。
