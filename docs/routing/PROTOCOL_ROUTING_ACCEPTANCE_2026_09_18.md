# 协议转换与路由修复验收（2026-09-18）

本文合并首次交付和两轮复审的验证证据，不再维护已完成的阶段计划。首轮基于 `1fa59c2ff3ba39040e51e564d956822c31447e5a` 修复六项问题，并接入 Token 路由预览；交付提交 `52e331e02d49fe4798ce389d3b4e2b6f4ea0a318` 后仍发现终态、工具媒体、缓冲元数据和预览一致性遗漏。下表记录补充反例后的修复结果。

第二轮复审基线为 `b7f9f469391465488f078efaa6fe0b9defbe3b81`，修复与验证记录归于 `dde2659a55b3361083dd509c8a0e675bfe13e9a0`。收紧的原生引用边界及新增验证见“第二轮复审”。当前能力以[支持矩阵](GATEWAY_SUPPORT_MATRIX.md)和[统一路由](UNIFIED_ROUTING.md)为准；下列结果保留原测试条件，不证明所有供应商扩展均可转换。

这些协议修复未修改价格、倍率、预扣、结算、退款、恢复、账务表结构或用量归一化算法，也未部署应用。后续订阅下线属于独立变更，见[现行验收记录](SINGLE_INSTANCE_ACCEPTANCE.md)。协议测试中的 usage 沿用已有账务流程，不为结算补造成功输出。

## 修复与回归证据

| 计划要求 | 审计及修复结果 | 正式证据 |
|---|---|---|
| 六组原问题的行为测试 | 已保留首轮回归，并补充此前未覆盖的交错历史、多候选、独立引用和部分终态快照 | `response_termination_test.go`、`tool_media_contract_test.go`、`response_audit_test.go`、`buffered_artifacts_test.go` |
| JSON／SSE／缓冲 JSON 终态一致 | 完成及已知限长／过滤保留正文和 usage；未知原因跨协议明确失败且不重放；嵌入 HTTP 200 的上游失败现在返回 502 并保留已观察 usage | `router/gateway_protocol_audit_test.go`、OpenAI 宿主 `response_audit_test.go` |
| 传输与生成终态分离 | `StreamStatus` 保留断流、超时和取消原因，RelayKit 终态描述独立；所有候选结束后才能正常完成 | `relay/helper/stream_candidates_test.go`、RelayKit `response_audit_test.go` |
| 不补造成功及提交屏障 | EOF／`[DONE]` 不补 stop；重复快照不重复输出，冲突终态报错；已输出失败不再拼接新响应 | `stream_delivery_test.go`、`terminal_stream_test.go`、官方 SDK 中断用例 |
| 完整 SSE 解码 | 流式和缓冲使用公共事件解码器，覆盖多行 data、换行差异、BOM、注释、事件名、整事件上限和完整 EOF 尾事件 | `relay/helper/sse_reader_test.go` 及现有缓冲测试 |
| 本地转换错误归因 | 本地不可表达结果归 `local_convert`、禁止重试且不记健康故障；缺少生成终态仍归上游异常；取消优先归客户端 | `response_conversion_test.go`、`controller/relay_attempt_test.go`、网关终态契约 |
| Claude ↔ Responses 工具媒体 | 从按 ID 回填改为直接有序历史转换，保留文本／工具／文本及媒体内部顺序、调用 ID、URL／内联来源；参数外壳继续复用现有转换器 | `tool_media_contract_test.go` 双向往返、原对象不变、0/false、无媒体解析器调用 |
| 不支持的工具媒体发送前拒绝 | Chat/Gemini 工具结果媒体被识别；不支持路径、缺失／重复 ID、失败状态、非默认 detail 和未知块均明确拒绝，allow 不绕过 | 请求规划测试及 `TestGatewayUnsupportedToolImageNeverSendsOrDebits`，三种策略均零发送、零预扣 |
| 引用及托管工具伴随信息 | 结构化保留可表达引用和工具结果；缓冲独立 annotation 与托管项也保留；最终快照按身份补全和去重，不用压缩后的数组位置覆盖旧项 | `response_artifacts_test.go`、`response_audit_test.go`、`buffered_artifacts_test.go`、宿主缓冲回归 |
| 引用与托管工具失败边界 | 无效 grounding 范围／缺来源失败；搜索展示损失带诊断且 strict 拒绝；搜索失败不变为 completed，不能表达的 open_page 等动作拒绝 | RelayKit 响应审计与引用测试 |
| 大小与请求内生命周期 | 流状态和缓冲累积器增加累计输入限制；宿主采用现有响应大小配置，默认 128 MiB；超限保留已观察 usage 并禁止替代请求 | 流累计限制和缓冲限制回归；无新增数据库存储 |
| 原始请求分析与候选资格 | 当前请求缓存原始语义分析，候选计划纯计算；最终具体 DTO 仍复核，不以请求缓存代替授权。资格先于排序和 64 上限 | 统一选择器及既有 63/64 边界 HTTP 回归 |
| 自动／手动规则与首次发送 | 保留 priority、position、负载桶、软亲和及稳定加权；none/same_channel 在首次真实发送后生效，此前保留完整扫描集 | 选择器、手动策略、准入 HTTP 回归 |
| FIFO、排队和资格变更 | 立即扫描后最多一次渠道排队，共享等待预算；排队后及发送前复核 Token/Profile/Entry、权限、端点、凭据、能力和健康 | 准入与资格变更既有回归、定向竞争检测；本次未另建选择器或队列 |
| 总重试和接受状态 | 保留最多 3 次发送、共享储备及副作用接受状态保护；部分输出、取消及本地转换失败不重放 | 网关契约、官方 SDK 工具和断流用例 |
| Token／旧 Profile 预览 | 覆盖自动、手动和无 Profile；共同选择入口计算 compact 初始阶段／基础回退及模型权限；拒绝伪路径、禁用账号和 Token | `token_route_preview_test.go`、`distributor_compact_test.go` 及原 API 所有权用例 |
| 预览无副作用及解释 | 仍无发送、预扣、Permit；输出版本、时间、原因、转换计划与排序快照；前端补全无条目时的排除原因，移除过期能力开关提示 | Token 路由组件测试、Playwright 四视口主题场景 |
| 诊断与指标 | 沿用 attempts，记录终态及稳定 code/path；队列丢弃计数接入网关指标；转换错误清理供应商值，移除相关原始正文调试输出 | 脱敏回归、网关日志不含响应正文断言、既有指标回归 |
| 独立模块和兼容性 | RelayKit 无 HTTP、数据库、认证或计费依赖；公开入口保持兼容，新增字段和可选限制；无数据库迁移 | 独立测试／vet／构建和主模块构建 |

“原生优先”仍是资格约束，不在原有层级之外插入原生加分。价格上限继续调用已有判断，预览不承诺下一次请求的精确排名或实时准入结果。

表中的主要缺陷均有正式反例先失败、修复后通过。各宿主采用请求内 `ResponseStreamState` 统一累计终态、诊断和用量；JSON 缺少 finish reason／stop reason 同样不能输出 completed。原来只有 `[DONE]`、没有供应商 finish reason 的“成功”夹具已改为符合协议的终态。

## 验证记录

所有后端测试命令带 `-timeout 60s`。各检查运行于隔离 SQLite、模拟 Redis 和本地 HTTP 上游；MySQL 5.7、PostgreSQL 16 为本机独立测试进程，未访问生产数据库或真实供应商。

| 检查 | 本次结果 |
|---|---|
| 主模块全量 | `go test -p 2 ./... -timeout 60s -count=1` 通过。后续终态修复对所有适配器、helper、router 及相关 controller 全包补测；缺终态的成功夹具修正后验证 |
| RelayKit | `GOWORK=off go test ./... -timeout 60s`、vet、独立构建通过；后续缓冲修复追加整个转换包和累积器专项验证 |
| 竞争检测 | controller/helper/service 的归因、候选终态、Token 路由预览目标用例，以及 RelayKit 响应审计、工具媒体、终态和缓冲用例通过 |
| 三库 | BillingDatabaseIntegration 三库共 102 子场景通过；RoutingDatabaseIntegration MySQL/PostgreSQL 及 SQLite 用例通过，无缺 DSN 跳过 |
| 官方 SDK | Python/Node 基础、两套语义、云基础、云工具／断流五个测试入口实际执行并通过。其余未受影响专项复用此前有效记录 |
| 后端静态／构建 | 主模块 vet、Windows 构建、Linux amd64 构建通过；独立 RelayKit vet/build 单独执行 |
| 前端 | 本次相关 Vitest 22 用例、typecheck、受影响文件 ESLint/Prettier 和生产构建通过；共享层未改，复用首轮 125 文件、591 用例全量结果 |
| 浏览器 | Playwright gateway 控制台桌面／移动端 × 明／暗主题四场景通过，覆盖错误／禁用状态、横向溢出和焦点；使用本地模拟 API |
| 计费隔离 | 网关回归检查 limited 单次发送／消费、原值 usage 传递、未知终态禁止替代请求、工具媒体拒绝时无预扣及 Permit 释放；账务实现文件无修改 |
| 差异审查 | 检查无隐式成功、未实现能力放行、正文诊断泄漏及第二套路由逻辑；`git diff --check` 通过 |

日志保存在工作区忽略目录 `tmp/protocol-routing-audit/`；前端由本次路由审计工作流验证，历史有效检查在 `tmp/protocol-routing-review/`。这些目录不发布。新增支持边界同步到 [支持矩阵](GATEWAY_SUPPORT_MATRIX.md)、[路由契约](UNIFIED_ROUTING.md) 和 [RelayKit README](../../relaykit/README.md)。

## 明确保留的边界

上述修复在本地验证范围内完成。明确拒绝不等于实现了供应商全部能力：工具图片只实现 Claude ↔ Responses；原生文件引用、暂停续跑必需数据或无等价目标表示的托管动作仍需合格原生渠道，否则拒绝。纯文本历史已有的 namespace/custom-tool 显式降级继续遵守既有 allow 契约，不能据此放行尚未实现的工具媒体。

此次协议修复没有实现路由级透传、Responses WebSocket、自适应评分、消费预算、对账管理或 Provider 限流性能改造，也没有执行真实供应商兼容认证、生产压测或应用部署。它们不构成后续发布的默认承诺。

## 第二轮复审

基于 `b7f9f4693` 再次检查五阶段计划，发现并修复以下八项遗漏。没有改变账务实现、数据库结构或原计划的后置范围。

| 问题 | 修复及行为证据 |
|---|---|
| 流式引用仍走另一套快照合并 | 删除独立 annotation 缓存逻辑，流式与缓冲复用同一按身份累计器。部分最终快照不会丢失较早引用或将其附到另一条消息，字符范围保留全局偏移；ID 先于 output_index 到达时允许把临时位置绑定到真实位置。`TestResponsesStreamPartialSnapshotPreservesCitationIdentityAndOffsets` 先失败后通过，既有参数先到的兼容测试继续通过 |
| incomplete 事件借用 completed 快照而被判成功 | 事件明确 incomplete 时，按限制原因分类，不因内嵌 status=completed 伪造完成；未知原因拒绝转换。`TestIncompleteEventCannotBorrowCompletedSnapshot` 先失败后通过 |
| 终态之后还能转换文本事件 | 已观察终态后重复相同终态仍去重；后续非终态事件明确失败，不输出终态后的文本。`TestResponsesCannotEmitTextAfterTerminal` 先失败后通过 |
| Claude 引用丢失原生状态或伪造来源摘录 | Claude 搜索涉及加密续轮引用，跨协议请求预先拒绝；意外收到带加密引用／搜索结果的跨协议响应硬拒绝，allow 不放宽。普通 URL 注释缺少 Claude 原生来源信息时不再把生成答案截成 cited_text。`TestClaudeCitationStateCannotBeInventedOrDropped`、`TestClaudeSearchRequiresNativeResponseCapability`、`TestEncryptedHostedSearchResultsRequireNativeResponse` 覆盖；HTTP 回归确认单次发送、local_convert、禁止重试、不处罚健康 |
| 原生 encrypted_index 未纳入归属检查 | 在现有状态引用提取中识别引用块及加密索引，沿用已有摘要归属表，原生续轮锁定 Token／渠道／凭据／端点。跨 Token 拒绝，数据库不保存加密引用原文；`TestGatewayCitationStateDatabaseIntegration` 在三库实际执行 |
| 排队期间收窄 IP 白名单后仍发送 | 最终资格复核接收真实客户端 IP，并重读当前 Token 白名单；无有效 IP 或不在新范围内拒绝整个请求，不能通过换渠道绕过。Service 正式反例及 `TestGatewayQueuedTokenIPRevocationPreventsSendAndDebit` 验证零发送、零预扣及资源释放 |
| 手动预览在索引落后时错误显示合格 | 数据库元数据不能单独授予资格；只有统一选择器发现并确认的候选才合格，索引未更新的 Entry 标为 snapshot_stale。正式测试覆盖能力已发布但选择索引尚未看到的窗口 |
| 保存 Token 设置后仍显示旧预览 | 子组件保存成功通知父页面，取消旧请求并清空旧预览；此前在途结果不能回填。前端从实际保存事件验证已完成和在途两种情况 |

Claude 网页搜索文档明确要求后续轮次保留结果中的 encrypted_content，并描述引用中的 encrypted_index 与 cited_text；因此把它们当作可随意丢弃的展示信息不符合本计划的原生状态边界。参见 [Claude 官方搜索工具文档](https://platform.claude.com/docs/en/agents-and-tools/tool-use/web-search-tool)，2026-09-18 核对。本次只修正资格、转换和现有归属提取，不引入私有续跑协议、自动续跑或新持久化历史。

媒体双向结构、64 个合格候选上限、首次发送后的 none/same_channel、FIFO、单队列等待、三次发送及共享重试储备继续采用已有实现和有效回归结果。

| 第二轮验证 | 结果 |
|---|---|
| 主模块与 RelayKit | 主模块全量测试通过；后续状态归属改动补测 service/controller 和新增网关用例。RelayKit 独立全量测试、vet/build 通过；两模块分别构建 |
| 协议和 SDK | 基础 SDK、Python/Node 语义、云基础及云工具／断流五个入口实际执行通过；新网关反例和 Responses 引用、终态专项通过 |
| 竞争检测 | service/router 的排队复核、预览、状态归属目标，以及 RelayKit 引用／终态／缓冲目标用例通过 |
| 三库 | 既有计费 102 子场景、路由三库契约通过；新增原生引用归属在 SQLite/MySQL/PostgreSQL 三库均通过，不将缺 DSN 跳过计为通过 |
| 前端 | 相关 Vitest 23 用例、类型检查、受影响文件 ESLint/Prettier、生产构建通过；Playwright 桌面／移动端 × 明／暗四场景通过 |
| 构建与差异 | 主模块 vet、Windows 与 Linux amd64 构建通过；diff、格式、文档引用和计费实现隔离检查通过 |

所有后端测试使用 60 秒超时。新日志在忽略目录 `tmp/protocol-routing-reaudit/`；未变部分复用此前有效结果。只使用本地模拟上游，未执行真实供应商兼容认证、生产压测或应用部署。当前支持范围以更新后的支持矩阵为准；没有把“存在转换路径”解释为“能保留所有供应商原生状态”。

## 参考版本与历史记录

首轮固定参考版本为 new-api `7209b6db95ef`、AxonHub `19a3c27d8b94`、sub2api `efe9aab1e4ec`。选择性吸收 new-api 的响应伴随信息和引用设计，移植文件保留来源标记并遵循项目 AGPL-3.0 许可；未整体合并参考仓库，也未引入额外依赖。AxonHub 的工具媒体结构作为行为参考，Ren2Hub 保留自身的预算、健康归因和提交屏障，不采用部分输出后重放或 EOF 补成功的行为。

被合并的首次交付报告和原逐项审计可在 `dde2659a55b3361083dd509c8a0e675bfe13e9a0` 中查看，原路径分别为 `docs/routing/PROTOCOL_ROUTING_IMPROVEMENTS_2026_09_18.md`、`docs/routing/PROTOCOL_ROUTING_PLAN_AUDIT_2026_09_18.md`。该提交保留完整阶段记录；工作树只维护本页及现行契约。
