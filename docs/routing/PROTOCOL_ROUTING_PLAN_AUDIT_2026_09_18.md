# 协议转换与路由计划逐项审计

本次对已交付版本 `52e331e02d49fe4798ce389d3b4e2b6f4ea0a318` 按用户原计划逐项检查。结论是：首次交付存在遗漏，不能认定所有行为已经正确落实。本次补充正式反例、修复实现并完成受影响验证；下表记录本期计划的最终状态和明确拒绝边界。

未修改价格、倍率、预扣、结算、退款、恢复、账务表结构或用量归一化算法。没有增加自动续跑、隐式模型降级或部署应用。本次新增或调整测试中的 usage 均沿用已有账务流程，不为结算补造成功输出。

## 五阶段核对

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

## 本次额外发现的缺陷

1. 只检查首个候选终态；首个 stop 或 `[DONE]` 可掩盖后续未完成候选。现在按身份累计所有已观察候选。上游意外返回多答案时，也不会合并为 Responses 单答案。
2. Claude ↔ Responses 图片虽然保留下来，Chat 中间历史仍改变工具块与普通文本的顺序。现在媒体历史直接转换，参数外壳与历史处理分离。
3. 缓冲 SSE 仍只拼文本和函数参数，独立引用、托管搜索以及部分最终快照的元数据丢失。现在按输出身份累计并合并，无法识别身份明确失败。
4. 部分宿主仍走无状态 Chat 流转换，缺乏引用／终态累计和统一大小限制。现在采用请求内 `ResponseStreamState` 并及时观测终态、诊断及用量。
5. 上游 HTTP 200 内嵌失败沿用 200 错误状态并丢掉 usage。JSON 及 SSE 失败现在明确返回 502，同时保留现有用量传递。
6. Compact 预览未采用实际初始阶段和模型限制，路径识别过宽，禁用自动 Profile 的无候选原因未展示。现在预览与执行共享阶段和入口识别，页面展示全局排除原因。
7. 缺终态被归为本地转换错误，取消与转换同时发生时归因也不准确。新增独立缺终态错误并调整取消优先级；转换错误只保留脱敏后的稳定诊断。
8. 非流式转换仍接受缺少 finish reason／stop reason 的最终 JSON，可能输出 completed。现在也必须有生成终态；缺失时沿用上游响应失败和接受状态规则，不伪造完成。

以上主要缺陷均有正式反例先失败、修复后通过。补充的边界保护不通过伪造终态或放宽既有接受状态来使测试成功；原来只有 `[DONE]`、没有供应商 finish reason 的“成功”夹具已改为符合协议的终态。

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
| 前端 | 本次相关 Vitest 22 用例、typecheck、受影响文件 ESLint/Prettier 和生产构建通过；共享层未改，复用上轮 591 用例全量结果 |
| 浏览器 | Playwright gateway 控制台桌面／移动端 × 明／暗主题四场景通过，覆盖错误／禁用状态、横向溢出和焦点；使用本地模拟 API |
| 计费隔离 | 网关回归检查 limited 单次发送／消费、原值 usage 传递、未知终态禁止替代请求、工具媒体拒绝时无预扣及 Permit 释放；账务实现文件无修改 |
| 差异审查 | 检查无隐式成功、未实现能力放行、正文诊断泄漏及第二套路由逻辑；`git diff --check` 通过 |

日志保存在工作区忽略目录 `tmp/protocol-routing-audit/`；前端由本次路由审计工作流验证，历史有效检查在 `tmp/protocol-routing-review/`。这些目录不发布。新增支持边界同步到 [支持矩阵](GATEWAY_SUPPORT_MATRIX.md)、[路由契约](UNIFIED_ROUTING.md) 和 [RelayKit README](../../relaykit/README.md)。

## 明确保留的边界

本期计划在上述本地验证范围内落实。明确拒绝不等于实现了供应商全部能力：工具图片目前只实现 Claude ↔ Responses；原生文件引用、暂停续跑必需数据或无等价目标表示的托管动作仍需合格原生渠道，否则拒绝。纯文本历史已有的 namespace/custom-tool 显式降级继续遵守既有 allow 契约，不能据此放行尚未实现的工具媒体。

路由级透传、Responses WebSocket、自适应评分、消费预算、对账管理、Provider 限流性能改造及任何计费状态机调整仍按原计划后置。真实供应商兼容认证、生产压测和应用部署没有执行；仅同步验证后的源码。参考设计仍采用首轮记录的固定版本与许可边界，本次没有合并参考仓库或引入额外依赖。
