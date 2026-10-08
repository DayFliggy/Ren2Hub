# 上游同步账本

本账本的实施/验证段记录提交授权前的本地验收快照；其中 `candidate_sha=null` 指该快照。后续源码提交、服务器候选和正式切换由工作区服务器档案与实际发布记录另行追踪，不能把本地通过解释为已经部署。

## 2026-10-08 三来源增量同步（第四批）

用户批准 Gemini 计费改动并授权更新账本与路线图。窗口：NewAPI `45094bdf41fb13a5d24bd3e8eb3fdfec6241c59f` → `6370b29424168039e94d40d610191e7d2e65dbf4`（7 个提交）、Sub2API `745ff1d3ca79a7f39520f627f6aa44d778a11580` → `5fc0e486c3f6a8a191b8bd140f39b60457f611cf`（17 个修复及其合并）、AxonHub `81e519e914437b4b94f758b2d6d78f94468f64e4` → `b8bb3421e1fe946cfc4f0d86e8412098738f2793`（1 个，System One 渠道测试，无本地对应）。本批基于 Ren2Hub `0446ea195`。

| source_project / source_ref | category / priority / status | local_scope / contract | test_commands / validation | risk / follow_up |
| --- | --- | --- | --- | --- |
| NewAPI `d10f11fff` | 计费 / P2 / 已吸收（改变扣费，本地改写） | Gemini grounding 按一次响应内去重后的非空网页与图片搜索查询数计 `google_search`（跨流式帧与全部候选，流式每帧提交，客户端中断保留已见查询）；原生、Chat、Responses 三个非流式入口共用同一计数。上游另记 `google_search_grounded_prompt` 并以预置价格按模型代际选择计费键；本地不预置价格，该键仅在冻结报价定价时计入，未定价不会让结算失败，且列为保留名。`relaykit/dto` 增加 `imageSearchQueries` | `go test -timeout 60s ./relay/channel/gemini/`（`TestGeminiHandlersCountGroundingQueries` 9 组，含未定价不计入） | 已为 `google_search` 定价的 Gemini 2.5 渠道从每次响应 1 次变为按查询数计费；按次计费需为 `google_search_grounded_prompt:gemini-2.5*` 定价并把同前缀 `google_search` 设为 0。查询数不保证与 Google 账单一致 |
| NewAPI `04c597bf6` | 渠道 / P3 / 已吸收 | 参数覆盖条件新增 `regex`（Go RE2，按条件懒编译，键不存在时不编译，非字符串或非法模式报错）；条件判断块与上游基线一致，原样移植。本地控制台以原始 JSON 编辑参数覆盖，无界面改动，`docs/routing/GATEWAY_API.md` 列出条件模式 | `-run 'Override\|Condition' ./relay/common/`（`TestApplyParamOverrideConditionRegex` 19 组，修复前失败） | 无 |
| NewAPI `0cce21db0` | 插件 / P2 / 已吸收 | 用量键替换器只建一次；同一来源对象的规范化请求体在钩子间复用。用量钩子仍经 `submitUsageContext` 取得新规范化的请求值 | `go test -timeout 60s ./relay/channel/task/jsplugin/ ./plugins/ ./pkg/jsplugin/` | 无 |
| NewAPI `192802ee0` | 构建 / P3 / 已吸收 | Dockerfile、release 与 Electron 构建以 `-pgo=<moejs>/default.pgo` 编译；alpha.5 已带该文件，本地版本注入与构建参数不变 | 本地 `go build -pgo=...` 通过且 `go version -m` 记录 `-pgo`；服务器镜像构建 | 构建时间与运行收益未测 |
| NewAPI `b23471112`、`6370b2942` | 插件 / P2–P3 / 延期 | moejs alpha.6 未在本机模块缓存，需另行批准联网下载，并核对 `metadata_preview.go` 语法树接口、数值转换和 `usage_decimal.go` 辅助函数；`json-order@1` 依赖 alpha.6，冲突面最大且无插件使用。移植时用量钩子守卫须在获取调用槽之前并覆盖 `CallJSONInto`，本地 `BindTaskPluginEndpointToChannel` 每次解码都要重设请求原文 | — | 见路线图 |
| NewAPI `bc8235792` | 测试 / — / 刻意不吸收 | 上游把 relaykit 转换测试移出仓库；被删测试保护 Gemini 工具媒体、Opus 5.5 默认 effort 与后缀别名等本地合同，继续保留 | — | 无 |
| Sub2API `e0019da46` | 前端 / P2 / 已修复（本地设计） | 叠放弹窗按 z-index 判断最上层（同层取后出现的节点），不再只看 DOM 顺序；修复用户管理页安全验证弹窗挂载早于用户表单时 Esc 无效、Tab 焦点跳入被遮挡表单 | Vitest `ConsoleModal.spec.ts`（先挂载高层弹窗、后打开，Esc 与 Tab 只归它，修复前失败）；Playwright `admin-bindings` 4 项 | 共用判断放在 `utils/modalStack.ts`，图片灯箱接入同一判断；`MobileNavDrawer`、灯箱改为内联 z-index。弹窗关闭动画 200ms 内仍计入候选，旧逻辑同样存在，未处理 |
| Sub2API `8167d85b3` | 前端 / P3 / 已修复 | `FilterSelect` 被禁用时收起已展开菜单，禁用状态下选择无效，与 `ActionMenu` 一致 | Vitest `FilterSelect.spec.ts`（禁用后收起、选择无效，修复前失败）；前端全量 Vitest 1059 项、lint、format、typecheck 与构建通过 | 本机 `bun run` 下 vue-tsc 失败属环境问题（Bun 1.3.14 运行时），改用 Node 执行同一脚本通过 |
| Sub2API 其余 15 项 | — / 不吸收 | 推理签名恢复：本地签名状态绑定原渠道与 Key，跨渠道拒绝，上游错误按输入错误处理；健康评分样本下限本地已有 50；其余为本地没有对应页面或已有等价守卫的前端竞态修复 | — | 无 |

上一批保留的本地偏离继续有效。下一轮从 NewAPI `6370b2942`、Sub2API `5fc0e486c`、AxonHub `b8bb3421` 比较。

## 2026-10-08 遗留问题修复（第三批）

用户要求以 NewAPI 上游实现为主、AxonHub 为辅修复前两批遗留问题，并批准会改变扣费的工具计费与 xAI 插件。窗口不变：NewAPI `bb28f2cc` → `45094bdf`、Sub2API 止于 `745ff1d3`、AxonHub 止于 `81e519e9`。本批基于 Ren2Hub `7363f3eaf`。生产只读核查：当前没有任何渠道、阿里渠道日志和已保存的工具价格，本批计费变化不影响现有流量。

| source_project / source_ref | category / priority / status | local_scope / contract | test_commands / validation | risk / follow_up |
| --- | --- | --- | --- | --- |
| NewAPI `acb347a19`（工具计数） | 计费 / P2 / 已吸收（改变扣费） | `relay/common/tool_usage.go` 增加 `SetBillableToolCount`、`ApplyVendorToolUsage`、`QuotePricesTool`，保留字集合扩展为协议内置键加 `web_fetch`、`code_execution`、`tool_search`、`bing_web_search`、`x_search_posts`、`x_search_profiles`、`search_strategy_agent`（静态，不随运营价格变化）；结算时单个工具计数封顶 10,000 并记警告，图片仍按超过 128 判为无效；Claude `web_search` 与 Gemini `google_search` 计数从 gin 上下文移入 `BuiltInTools`，重试换渠道不再继承上一次尝试的搜索次数。不移植上游预置价格、`IsBuiltInToolPriceKey`、`GetToolPriceForModel`：本地价格起始为空、缺价发送前拒绝 | `go test -timeout 60s ./relay/common/ ./relay/helper/ ./service/ ./relay/channel/claude/ ./relay/channel/gemini/ ./relay/` | 客户端同名函数不再按保留键计费 |
| NewAPI `acb347a19`（Claude 服务端工具） | 计费 / P2 / 已吸收（本地合同） | `server_tool_use` 的 `web_fetch`、`code_execution`、`tool_search` 次数按各自键计费，但仅在冻结报价为该键定价时计入；Claude 线格式声明这三类工具时发送前要求定价（明确 0 有效）。`web_search_20260209`/`web_fetch_20260209` 内部使用代码执行且官方要求不另声明，未定价的内部用量不计费也不会让结算失败 | `TestClaudeServerToolUseKeys`（含未定价内部 code_execution 不计入）；`pricing_requirements_test.go` | 运营为 `code_execution` 定价后，动态过滤搜索的内部代码执行也会计费，与上游一致 |
| NewAPI `acb347a19`（图片终态） | 计费 / P2 / 已吸收（改变扣费） | 已完成的 `image_generation` 在流式 failed/incomplete/cancelled/error 终态与非流式 `incomplete` 也计费；未完成（`in_progress`/`partial`）仍不计；网页/文件/函数调用仍只在 completed 终态计入。保留本地去重别名、`in_progress` 排除与超过 128 判无效；删除 `ImageGenerationCallCounter.Reset` | `TestResponsesTerminalImageBilling`；`relay_responses_billing_test.go`；客户端断开用例不变 | 非流式 failed/cancelled 仍不转发、不计费 |
| NewAPI `acb347a19`（供应商搜索计数） | 计费 / P2 / 已吸收（改变扣费） | Azure Responses 按 `tool_usage.web_search.num_requests` 计 `bing_web_search` 并把协议键置零；xAI Responses 按 `server_side_tool_usage_details` 计 `web_search`、`x_search_posts`、`x_search_profiles`；百炼 Responses 按 `usage.x_tools` 计 `search_strategy_agent`；百炼 Anthropic 兼容端点经 `WebSearchBillingKey` 计 `search_strategy_agent` 并补 `cc_entrypoint` 系统标记。本地新增 `VendorToolPriceKeys`：各供应商在读取器旁声明"声明的托管工具 → 额外价格键"，发送前一并要求定价；`InitChannelMeta` 清除钩子并删除上一渠道读取器新建的键。百炼 Responses 改为 `/compatible-mode/v1/responses` | `relay/channel/{openai,xai,ali}` 读取器与 URL 测试；`TestTextToolQuoteIgnoresZeroCountWithoutPrice` | 字段名取自上游原文，未用真实供应商响应核实 |
| NewAPI `acb347a19`（其余） | 协议 / P3 / 延期 | 智谱与百炼 Chat 的 `WebSearchEncoder` 渠道级搜索编码（含强制搜索推断计数）、Advanced Custom 转 Responses、Moonshot Responses 透传未吸收：属于新能力，编码诊断在本地默认 safe 策略下会拒绝请求，需单独设计 | — | 见路线图 SYNC1008-08 |
| NewAPI `0a532f7f7` | 插件 / P3 / 已吸收（源码未改） | 内置插件 `xai` `1.0.0` 与上游逐字节一致，承载 `grok-imagine-video` 系列；审核拦截和报告费用的失败按 `moderated: true` 收费。本地只用完成事实结算：xAI 结果不报告分辨率，读取分辨率或时长而完成事实缺失时按提交时冻结估算结算（编辑按预留 9 秒、延长按 15 秒输入、报告费用的失败按完整估算），可能高于上游合并提交事实后的金额 | `go test ./plugins/ ./pkg/jsplugin/ ./relay/channel/task/jsplugin/`；`router/gateway_xai_video_test.go` | 消除差异需要插件 `+ren2hub.1` 报告请求决定的事实，或放宽"不用提交事实补齐"的宿主合同，待决定；未调用真实 xAI |
| 本地遗留（Responses 拒答转 Claude） | 协议 / P2 / 已修复 | 拒答转为 Claude text 块并设 `stop_reason: "refusal"`，优先于 tool_use、end_turn 与长度截断；流式增量与终态快照只交付一次；Claude 目标记 warning `responses_refusal_reduced`（safe/allow 放行、strict 拒绝），仅针对 assistant 消息。AxonHub 同样静默丢弃，无先例 | `TestResponsesRefusalReachesClaudeUnderConversionPolicy` 等，修复前均失败 | Responses 转 Gemini 仍丢弃拒答，留待后续 |
| 本地遗留（Token 更新） | 安全 / P2 / 已修复 | 设置类 PUT 只写请求体携带的字段，漏传的 `allow_ips`、`model_limits_enabled`、`model_limits`、`expired_time`、名称与预算保持原值；`""`/`null` 仍清空；`status_only` 缺 `status` 拒绝。NewAPI 同样整条替换，AxonHub 漏传保留 | `-run 'Token' ./controller/`（`TestUpdateTokenKeepsOmittedSettings` 修复前白名单被清空）；`-run 'Token\|WalletIngress' ./model/` | 部分请求体的重放指纹与升级前不同；完整请求不变 |
| 本地遗留（自动路由冷启动） | 路由 / P2 / 已修复 | 样本不足 50 的渠道按 good 比较，在同 priority 与负载桶内按权重获得首次尝试；此前已知档位一律排在未知之前，新渠道连 critical 渠道都排不过。NewAPI 按权重随机、AxonHub 零样本满分 | `TestOrderAutoRouteCandidatesGivesColdChannelItsWeightShare`（修复前 0/1000，修复后约 500） | 故障的新渠道在熔断前会承接其权重份额的首次尝试 |
| AxonHub `d6e00c00` 后续（百炼 `reasoning_effort`） | 协议 / P3 / 未验证 | 生产没有阿里渠道，剥离路径当前不可达；百炼对非 none `reasoning_effort` 的实际行为仍未在线核实 | 生产只读查询 | 接入阿里渠道后用真实调用核实 |

上一批保留的本地偏离（tokenkit `/\n` 分段、图片取文件策略、Claude `tool_result` 逐块解码、Gemini 工具媒体拒绝、Claude 改写诊断为 warning）继续保留。下一轮仍从 NewAPI `45094bdf`、Sub2API `745ff1d3`、AxonHub `81e519e9` 比较。

## 2026-10-08 三来源增量同步（第二批）

用户授权结合两轮研究同步 NewAPI 并吸收 Sub2API、AxonHub。窗口：NewAPI `bb28f2ccad3f6286a9dc4a0ad9e20f571f57533b` → `45094bdf41fb13a5d24bd3e8eb3fdfec6241c59f`（4 个提交）、Sub2API `3f1a2ea0a760730e3bc528105c00b4ee4f23e469` → `745ff1d3ca79a7f39520f627f6aa44d778a11580`（90 个提交）、AxonHub `5c78997798b40e44788c38bd6720c0b9c156f26b` → `81e519e914437b4b94f758b2d6d78f94468f64e4`（8 个提交），均无 P0/P1。本批基于 Ren2Hub `36b657611`，只吸收不改变扣费的缺陷修复；会改变扣费的工具计费项留在路线图队列等待批准。

| source_project / source_ref | category / priority / status | local_scope / contract | test_commands / validation | risk / follow_up |
| --- | --- | --- | --- | --- |
| NewAPI `0a03088e0` | 协议 / P2 / 已吸收（改写诊断级别） | `reasoning/claude.go` 为 Opus 5.5、Sonnet 5.5 单独建能力行：关闭思考时 Sonnet 5.5 发 `between_tools`、Opus 5.5 发 adaptive 加 `low`，其余模型仍发 `disabled`；`toolconv/encode.go` 在 Fable 5.1、Mythos 5.1、Opus 5.5、Sonnet 5.5 及渲染为手动思考时把非 Claude 源的强制 `tool_choice` 降为 `auto`。诊断用 warning 而非上游的 error：本地默认策略为 safe，error 会让默认请求失败；safe 放行、strict 拒绝 | `cd relaykit && GOWORK=off go test ./...`（含 `claude_rejected_controls_test.go`、`intent_test.go` 逐模型表）；`-run 'TestGateway.*(Claude|Reasoning|ToolMedia|Effort)' ./router/` | toolconv 只按 `target.Model` 前缀查能力表，带命名空间或渠道别名的模型名不会降级；未调用真实 API |
| NewAPI `0a03088e0`（schema 部分） | 协议 / P2 / 已吸收 | Gemini 函数参数带 `OpenAPISchema` 标记，共享编码步骤为 Chat/Claude/Responses 改写为 JSON Schema，丢弃项给 `gemini_schema_keyword_dropped` warning；Gemini 目标不变。本地探针曾确认 `OBJECT`、`nullable`、`propertyOrdering` 原样发往非 Gemini 目标 | `toolconv/schema_test.go` 5 组（含 strict 拒绝、Gemini 目标不变） | `example`→`examples`、`format` 保留按 Gemini 注释处理；`strict:true` 函数工具对联合类型的接受度未在线核实 |
| NewAPI `0a03088e0`（其余部分） | 协议/计费 / P2–P3 / 刻意不吸收或延期 | 上游对目标不能承载的媒体改为丢弃并给诊断，与本地"任何策略都不得丢弃媒体"冲突，保留本地拒绝；转换流失败后上游追加 JSON 错误体，本地已按已交付输出结算并只发一次 `stream_interrupted` 事件（探针确认），不采纳；Codex namespace 平铺延期 | 本地探针（未提交）验证转换流失败计费与单次错误事件 | 吸收后续上游媒体改动时必须保留本地拒绝 |
| AxonHub `d6e00c00` | 协议 / P2 / 已吸收 | 阿里 `requestOpenAI2Ali` 读取宿主已解析的 `RelayInfo.ReasoningEffort`：为 none 时发 `enable_thinking:false` 并去掉 `reasoning_effort`；保留客户端显式值；关闭意图与显式 `enable_thinking:true` 同时出现时返回客户端错误 | `go test -timeout 60s ./relay/channel/ali/`（`TestRequestOpenAI2AliDisabledThinking`） | 百炼对 `reasoning_effort` 的实际处理未在线核实；生产暂无阿里 Responses 流量证据 |
| Sub2API Retry-After 系列（opencode 提交） | 可用性 / P2 / 已吸收（本地设计） | `ParseRetryAfter` 是唯一解析入口，秒数与 HTTP 日期都封顶为 `RouteRetryAfterCap`（5 分钟，等于本地最长健康冷却）；健康行与 Provider 范围共用；过去日期不设冷却，负数与无法解析的值忽略；超出整数范围的正值按封顶处理 | `-run 'TestParseRetryAfter|TestRetryAfter|TestGatewayProviderRateCapsAbsurdRetryAfter' ./service/` | 供应商若确实要求更长等待，每 5 分钟会多一次探测 429 |
| AxonHub `81e519e9` | 协议 / P3 / 已吸收（真实缺陷） | Chat→Claude 流中 finish 块只有自带真实计数时才结束消息，否则等待仅含用量的块或正常结束；宿主 `OaiStreamHandler` 路径本身不受影响，缺陷在 relaykit 对外转换接口 | `GOWORK=off go test ./relayconvert/internal/oai_chat/` | 无 |
| 本地缺陷（`usage_merge.go`） | 计量 / P3 / 已修复 | `output_tokens_details` 改按"后到非零值覆盖、零值不擦除"合并，两个别名字段保持一致；此前零值快照会清空已有 reasoning token。原测试 `TestUsageMergePreservesExplicitZeroOutputModality` 锁定的旧行为与函数注释矛盾、上游也不如此合并，已由表驱动测试替换 | `GOWORK=off go test -run TestUsageMerge ./dto/` | 宿主 `relay-openai.go` 仍按最后一个带用量的块整体替换，规则不同但互不影响 |
| Sub2API apicompat（refusal） | 协议 / P2 / 已吸收 | Responses 拒答在非流式、流式、缓冲三条路径转为 Chat `refusal` 字段，只交付一次 | `TestResponsesRefusalReachesChatRefusalField` | 转 Claude 方向的拒答损失诊断未实现，延期 |
| Sub2API `412718edf` | 协议 / P2 / 已吸收 | `ClaudeMediaMessage` 对 `type=="thinking"` 始终输出 `thinking` 与 `signature` 键（未知签名为空串），其他块逐字节不变；两个响应 golden 各多一行 | `relaykit/dto/claude_thinking_signature_test.go`、`relayconvert/claude_thinking_signature_test.go` | 无 |
| Sub2API apicompat（legacy function_call） | 协议 / P3 / 已吸收（本地设计） | `dto.Message` 增加 `function_call`；旧版调用与紧随的同名结果配对为工具调用与结果，Claude/Responses 用稳定的 `call_legacy_N`，Gemini 按名匹配；无法配对给 error 诊断（safe/strict 拒绝）；缺 `name` 或无效 JSON 在分析阶段返回 400 | `legacy_function_history_test.go`；本地探针确认原生 Chat 配对历史正常转发、缺名返回 400 且不发送 | 原生 Chat 现在会把历史里的 `function_call` 转发上游，此前解码时被静默丢弃 |

延期或不吸收（见路线图 SYNC1008-05/07/08）：供应商上报搜索次数计费与失败终态图片计费（会改变扣费，待批准）；Codex namespace 平铺；xAI Grok Imagine 插件（被审核拦截也计费，需产品决定）；智谱/DashScope 渠道级搜索编码；`45094bdf4`（React 专属）；Sub2API 界面竞态（本地已有等价守卫或无对应页面）；AxonHub Decisions API、Codex 窗口、Antigravity。

上一批的四项本地偏离（tokenkit `/\n` 分段、图片取文件策略、Claude `tool_result` 逐块解码、Gemini 工具媒体拒绝而非丢弃）在 NewAPI `45094bdf` 仍未修复，继续保留。合并时修正：Gemini schema 测试与 legacy 配对改动并行，`ExtractRequest` 新增目标参数后补齐调用。下一轮从三个目标 SHA 继续比较。

## 2026-10-07 三来源增量同步

用户授权本批同步 NewAPI 更新并吸收 Sub2API、AxonHub 的有益部分。窗口按上轮基线锁定：NewAPI `1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5` → `bb28f2ccad3f6286a9dc4a0ad9e20f571f57533b`（8 个提交）、Sub2API `458b92abd4b0b09d123d4c6727dd8d59060bd883` → `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`（v0.2.14，14 个提交）、AxonHub `2bf8e2c916412df72252ac1514591dc47f692d7e` → `5c78997798b40e44788c38bd6720c0b9c156f26b`（11 个提交）；三个基线均为目标祖先，未截断。Release API 返回 403，采集保持 incomplete。本批基于 Ren2Hub `801b5457f`，参考仓库只读。三个窗口未发现 P0；Sub2API 的易支付伪造回调、初装可猜凭据与删 Key 结算，以及 AxonHub 的 IPv4 映射地址，在本地均无可利用路径。

| source_project / source_ref | category / priority / status | local_scope / contract | test_commands / validation | risk / follow_up |
| --- | --- | --- | --- | --- |
| NewAPI `bb28f2cc` | 计量/性能 / P1 / 已吸收 | 新增独立模块 `tokenkit/`（Go 1.26，不被 relaykit 引用），删除 `service/tokenizer.go`、`token_estimator.go`；工具定义、工具调用与结果、Gemini 系统指令计入估算，用量缺失时补算计入工具调用（Claude 每个 tool_use 另加 40）；图片按 OpenAI/Claude/Gemini 尺寸规则估算，尺寸读取仍经 SSRF 受保护下载与大小上限；单类字符超过 1024 字节分段计数（10 万连续汉字约 0.1 秒）；tiktoken-go v0.8.1。本地保留：出站 JSON 预算仍用于预扣和 Provider TPM，只是改由 tokenkit 计数；HEIF 溢出由 tokenkit 的 int64 运算与单张 patch 上限兜底；Chat 转 Responses 不补 `detail`（本地早已移除默认值，补回归） | `cd tokenkit && GOWORK=off go test ./...`；`go test -timeout 60s ./service/ ./relay/helper/ ./relay/channel/{claude,gemini,openai}/`；`-run 'TestGateway.*(Budget|Usage|Token|Audio|Image)' ./router/`；`TestChatCompletionsRequestToResponsesRequestImageDetail`；`TestGetImageTokenBoundsOverflowingDimensions` | Claude 4.7+ 预扣估算上升 30–55%，余额贴近下限的钱包更早被拒；gpt-4.1/5/o 系列改用 o200k_base 后中文估算下降约 18%，只影响预扣与 TPM 预留，结算仍按实际用量 |
| NewAPI `c6bac617` | 协议/模型名 / P2 / 已吸收 | 思考后缀适配器开关同时管 effort 后缀；关闭时 `-high`/`-low` 等原样用于路由、映射、计费和上游请求，不再回落基础模型名。生产核实两开关均为代码默认（Claude 开、Gemini 关），且无以 effort 后缀结尾的渠道能力 | `GOWORK=off go test -run 'TestParseKnownProviderModelSuffix' ./relayconvert/reasoning/`；`TestApplyReasoningModelSuffix`；`TestBaseModelNameFollowsSuffixAdapterToggles` | Gemini 开关关闭时 `gemini-*-high` 请求会找不到渠道；需要旧行为时打开开关 |
| NewAPI `78bd5b1c` | 协议/计量 / P2 / 已吸收（媒体部分按本地合同改写） | Opus 5.5 默认 effort 为 medium；Gemini 流式 usage 按模态整体替换快照（本地同模态重复条目会少计，已改）；Responses 工具输出里的图片和文档按调用 ID、原顺序以内联数据发给 Gemini（Gemini 3 放 `functionResponse.parts`，1.x/2.x 置于同轮函数结果之后）。不照搬上游的丢弃加诊断与 `allow` 文字占位：音视频、Gemini 不接受的 MIME、供应商自有来源、混有未知块的结果在所有策略下发送前拒绝 | relaykit 全量；`TestGatewayResponsesToolMediaOnGemini`（safe/strict/allow × 接受/拒绝，拒绝时零发送零扣费）；`-run 'TestGateway.*(Gemini|ToolMedia|MediaPlan|Media|Semantic)' ./router/` | 发送前估算不计 `function_call_output` 内的媒体（Chat/Claude 路径同样存在的既有缺口），结算按上游用量；无法识别版本的别名按 Gemini 3 放置 |
| NewAPI `b48b74ab` | 插件运行时 / P2 / 已吸收 | moejs `v0.1.0-alpha.3` → `alpha.5`；请求构建钩子、`parseBatchResult`、openai_video renderer 直接解码，删除 `decodeRequestDescriptor`。本地：用量钩子传入解码目标时在运行前报错，只能经 `exactUsageResult`；已发布内置插件源码不变 | `go test -timeout 60s ./pkg/jsplugin/... ./relay/channel/task/jsplugin/... ./plugins/...`（含 `TestUsageHooksCannotBeDecodedIntoGoValues`、沙箱与固定源码测试）；`go mod verify` | moejs 仍为 alpha、无硬内存上限 |
| NewAPI `0908d9e7`；Sub2API `d1aac6b9` | 资金/支付 / P2 / 已吸收（加固） | 本地 `pkg/epay` 与 go-epay v0.0.5 同规则：键为空或含 `&`/`=`、参与签名的值含 `&` 时拒绝（值中的 `=` 无歧义）；`BuildPurchase` 拒绝带查询串或片段的回调地址；通知中出现 `device`/`notify_url`/`return_url` 即拒绝；未知扩展字段仍参与验签。解析阶段拒绝写不含参数值的告警日志 | `go test -timeout 60s ./pkg/epay/`；`-run 'Epay|Topup|Payment' ./service/ ./controller/ ./model/`（含下单表单重放与真实通知带扩展字段入账） | 网关若在扩展字段值里带 `&` 或回显下单字段，回调会被拒，订单保持待支付，走管理员补单 |
| NewAPI `8ca26d81` | 运维 / P3 / 已吸收（等价，补回归） | 嵌入的构建产物本已不计入 Web 限流；未引入上游默认关闭的独立静态限流器与三个环境变量 | `-run 'Web|Static|Vue|RateLimit' ./router/` | gzip 预压缩延期，待剖析证明静态压缩占用 CPU |
| NewAPI `95601910` | 界面 / P2 / 已吸收 | `/api/next/admin/users` 新增校验过的 `group` 参数与 `group_counts`（GORM + `commonGroupCol`）；Vue 用户页按上游增加分组筛选和分组列，选项来自当前关键词下的实际分组。分组只用于充值倍率与网关准入分类 | `-run 'TestNextAdminUsersFilterByGroup|TestUserGroupFilterAndCountsAcrossDatabases' ./controller/ ./model/`（SQLite、MySQL 5.7.19）；Vitest `UsersView.spec.ts`；Playwright `users-group-filter` 亮/暗 × 桌面/移动 | 列显示设置存储键改为 `_v2`，已保存列设置的管理员恢复默认列；PostgreSQL 由服务器候选环境覆盖 |
| NewAPI `973cf8ef` | 界面 / P3 / 刻意不吸收 | React 专属；Vue 密钥页无分组徽标 | — | Vue 密钥页显示分组时复评 |
| Sub2API `0f9d460d` | 依赖 / P3 / 已吸收 | vue 及 `@vue/*` 3.5.40 → 3.5.43，source-map-js 覆盖为 1.2.2，@babel/parser 随之 7.29.9；无 xlsx | `bun run typecheck/lint/format:check/build`；Vitest 186 文件 / 1057 例 | 其他 `bun audit` 告警不在本批 |
| Sub2API `6e315616`、`c2d5bbd9`、`bbba01da`、`66a9e2e6` 及 VERSION/文档提交 | 安全/资金/产品 / P3 / 刻意不吸收 | root 由安装向导按操作者密码创建，无默认凭据；有预留或在途恢复时拒绝删除 Token；Codex 导出为静态 TOML；无 TypeSafe 渠道 | 删除保护：`-run 'TestWalletIngressDeletionProtectsZeroAmountRecovery|TestAccountDeletionWaitsForSettledConsumptionDelivery' ./model/` | 引入自动初始化、新删除路径、远程目录或 TypeSafe 时复评（SUB2API-01/02 条件不变） |
| AxonHub `8dfb6091`、`95fc2df2`、`c9e866cb` | 安全/计量 / P3 / 已吸收（等价，补回归） | 映射 IPv4 与 IPv4 规则互相匹配；清空 `allow_ips` 解除限制并清缓存；`cost` 为数字或字符串时原样往返 | `go test -timeout 60s ./common/`；`-run 'Token' ./model/`；`cd relaykit && GOWORK=off go test ./dto/` | Token PUT 漏传 `allow_ips` 时清空白名单的语义待产品决定 |
| AxonHub `ac7f4889`、`2f8de312` | 协议 / P3 / 部分吸收 | Responses 流中断错误事件补 `param: null`；终态后传输错误仍判失败（本地合同） | `-run 'Stream|Responses' ./controller/`；`TestGatewayOllamaInterruptedStreamHasOneTerminalError` | 诊断出现"终态后读错误"样本时复评 Claude `message_stop` 规则 |
| AxonHub `5c789977`、`563f89db`、`89d36d61`、`5af5400e`、`dddd7d70`、`4904a5df` | 协议/界面/目录 / P3 / 刻意不吸收 | Codex 订阅渠道已退役；Vue Playground 已退役；敏感词为拦截不改写；模型开发商数据与 neuralwatt 窗口不进入价格和路由 | — | 对应产品能力重新获批时复评 |

验收中修复的既有缺陷：`router/task_model_scope_integration_test.go` 写死的 sunoapi `1.1.0` 自 `+ren2hub.1` 后缀起已不存在，主线上即失败；改为从插件注册表读取当前版本。

合并后审计修复（本地合同，偏离上游处单独标注）：

- `tokenkit/tokenizer.go` 本地偏离：o200k 把符号段与其后的 `\r`、`\n`、`/` 合为一个预分词片段，交替的 `/\n` 不形成单类长串，上游分段不生效，1 MiB 输入在预扣时可占满一核数分钟。符号段后的换行并入同一串后照常分段；`TestCountCutsSymbolNewlinePieces` 无修复时 40 秒超时、修复后 0.4 秒。可反馈上游。
- `service/token_counter.go`：图片尺寸读取复用 `EstimateRequestToken` 唯一的取文件决定，Gemini 原生格式与关闭媒体计数时 URL 图片按默认尺寸估算，不再下载。
- `relaykit/dto/claude.go`：`tool_result` 逐块解码，无法解码的块（如字符串来源的 `search_result`）按原文计入，纯文本 document 计入正文；此前整段工具输出会从估算和敏感词检查中消失。`TestClaudeToolResultCountsUndecodableAndTextDocumentBlocks`。
- Responses 工具媒体转 Gemini：`custom_tool_call_output` 同样按工具结果规划；单个对象形态或与未知块混合的媒体在转换器内直接拒绝，不再退回响应映射把 base64 写成文本；工具媒体只接受函数结果文档所列的 PNG、JPEG、WebP、PDF 与纯文本。`TestResponsesToolMediaGeminiRejectsUnrepresentableMedia` 新增 4 例，无修复时失败。
- 易支付解析拒绝只记录请求方法和固定原因；原因不区分具体规则，后续可按需细化分类码。

下一轮从三个目标 SHA 继续比较。

## 2026-10-03 任务插件回归 NewAPI JS 插件系统

用户选定方案 A：任务插件运行时改用上游 moejs，代码尽量与上游一致，便于后续同步和吸收。Ren2Hub 的插件系统来自 `583ef2d52`，是按 NewAPI rc.40（`0aec08fee`）手工压缩移植的单父提交，与上游 merge-base 为 `0ab020206`，没有任何上游插件提交是本地祖先，因此本批按文件比较收敛，不做 merge。窗口为插件引入提交 `eb48396d5` 至 `1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5`，触及插件路径的上游提交共 43 个；"rc.40 已包含"不等于本地已吸收，`9df450fe5` 的有界轮询失败就在移植中丢失。本批基于 `10ccd9a2f`，候选提交为 `8c947e252`；服务器推送、候选环境和部署仍待执行。

保留的本地合同：提交只预留、终态按冻结 Quote/Usage/Charge 结算；立即结果经 `PersistImmediateTask` 原子落库；请求发出后不重放；凭据按 ID 绑定，不存明文 key；锁顺序 recovery → task → wallet → Token；执行源按 `TaskPluginRef{Key,Version,SourceHash}` 固定；继续不吸收上游 `tryRealtimeFetch` 与提交即结算。

| source_project / source_ref | category / priority / status | local_scope / contract | test_commands / validation | risk / follow_up |
| --- | --- | --- | --- | --- |
| NewAPI `9df450fe5` | 轮询/资金 / P1 / 已吸收（移植时丢失后补回） | `service/task_polling.go` 按上游 `classifyPollHTTP` 分类；404/410 立即失败，其余故障累计 `PollFailures`，达到 `TASK_POLL_MAX_FAILURES`（默认 20）后失败。终态改走本地 `failTaskWithoutResult` 释放预留，不用上游 `RefundTaskQuota`；`fail_reason` 只含分类和状态码，供应商细节只进日志；宿主取消不计数；批量提交恢复查询按块独立失败并在下一轮重试 | `go test -timeout 60s -run 'TestPoll|TestTaskSubmissionsReady' ./service/ ./model/` | 默认 20 次失败上限、约 5 分钟后释放预留与上游一致，供应商长故障仍可能先于上游收费完成而退款；按上游策略保留，后续只复评策略 |
| Ren2Hub 本地缺陷（上游 `model/task.go:368,384,398` 同样存在） | 资金 / P1 / 已修复 | 三个未完成查询只按 status 判断；插件对运行中任务报告 "100%" 时保留状态推导的进度，不再永久冻结预留 | `go test -timeout 60s -run 'TestUnfinishedTaskQueries\|TestPollRecognizedRunning' ./model/ ./service/`；恢复旧过滤后测试失败 | 可反馈上游 |
| Ren2Hub 本地缺陷 | 资金/协议 / P1 / 已修复 | 立即终态结果在任务与资金意图落库后即返回给客户端；资金投递失败由恢复任务补完，不再返回 503 并丢弃已付费结果 | `go test -timeout 60s -run 'TestGatewayDoubaoImmediateDelivery' ./router/`（原断言 503 改为 200，资金断言不变） | 无 |
| NewAPI 插件 ABI（`controller/relay.go:639-653`） | 协议 / P1 / 已吸收 | 立即返回的运行中结果按异步任务落库并轮询；`retainResult:false` 与 OpenAI Images 丢弃结果只作用于终态 | `go test -timeout 60s -run 'TestRunningImmediateResult' ./service/` | 无 |
| NewAPI `controller/channel.go:1638-1642` | 权限 / P1 / 已吸收 | `CopyChannel` 对 type 61 或带插件绑定的渠道要求 `task_plugin.bind` | `go test -timeout 60s -run 'TestCopyChannelWithTaskPluginBinding' ./controller/` | 默认角色无该权限，影响限于自定义授权 |
| NewAPI `UpdateBatchTasks` | 轮询 / P2 / 已吸收 | `fetchMode: "batch"` 插件按固定插件版本和同一凭据合并为一次请求；缺项计一次失败；共享响应不写入其他任务 | `go test -timeout 60s -run 'TestBatchPlugin' ./service/` | 旧 `suno` 平台专用路径保持不变，延期合并 |
| NewAPI `dto/task.go:59-92`、`controller/task.go:434-509` | 数据隔离 / P1 / 已吸收 | 任务列表不返回 data，成功任务清空 result_url；管理员可见 `admin_info`，Root 可见 `root_info`；新增 `GET /api/task/:task_id/artifacts` | `go test -timeout 60s -run 'TestTasksToDto' ./controller/` | 前端改走产物接口 |
| NewAPI `common/init.go:192`、`model/option.go:58-59,422-424` | 运维 / P2 / 已吸收 | 支持 `TASK_PLUGIN_ENABLED`；写入选项后总开关立即生效，不等 30 秒同步 | `go test -timeout 60s -run 'TestTaskPluginEnabledOption' ./model/`（移植上游测试） | 禁用工厂键仍由同步路径合并数据库禁用项，未照搬上游 |
| Ren2Hub 本地缺陷（上游同样 `Select("*")`） | 并发 / P1 / 已修复 | `UpdateWithStatus` 只写轮询拥有的列，不覆盖 quota、user_id、channel_id | `go test -timeout 60s -run 'TestUpdateWithStatus_Keeps' ./model/` | `private_data` 仍整列写入，由 status CAS 保护 |
| Ren2Hub 本地缺陷 | 性能 / P2 / 已修复 | 每轮轮询的提交资金检查从每任务一次查询改为分块批量查询，规则与单任务一致 | `go test -timeout 60s -run 'TestTaskSubmissionsReady' ./model/` | 无 |
| NewAPI `controller/video_proxy.go:62-130` | 协议 / P2 / 部分吸收 | `/v1/videos/:id/content` 与产物接口共用所有权检查；旧视频任务免凭据读取结果 URL。插件内容解析失败仍明确报错，不吸收上游回退到 ResultURL | `go test -timeout 60s -run 'TestVideoContent' ./controller/` | 回退可能用无凭据请求访问需凭据的地址 |
| NewAPI `pkg/jsplugin/**`、`relay/channel/task/jsplugin/**`（RUNTIME-01） | 运行时 / P1 / 已吸收 | sobek 换为上游 moejs `v0.1.0-alpha.3`（h1 与上游 go.sum 一致，Apache-2.0；2026-10-07 随 `b48b74ab` 升至 `alpha.5`，见上节）。`json_state.go`、`routing.go`、`engine_test.go`、`plugins/embed.go`、`submit_stream.go` 与上游逐字一致；`engine.go` 仅多用量 hook 的数值边界（+9 行），`utils.go` 仅注册本地扩展（+4 行）；`adaptor.go` 保留本地资金/凭据合同，补回移植时丢失的 `HasExport`、`decodeRequestDescriptor`、`RawJSON`。沙箱：禁用 `eval`/`Function`，内置对象深度冻结且跨插件共享，调用深度 512；5 秒超时、中断、并发 8 不变，仍无硬内存上限。testify 1.12.1 与 yaml v3.0.5 由 moejs 最小版本选择带入，与上游一致 | `go test -timeout 60s ./pkg/jsplugin/... ./relay/channel/task/jsplugin/... ./plugins/...`（含 `TestPluginsCannotPatchSharedBuiltins`、`TestUnboundedRecursionIsARangeError`）；`go mod tidy` 无差异、`go mod verify` | moejs 仍为 alpha；跟随上游版本升级时复核沙箱测试 |
| Ren2Hub 本地合同（数值用量边界） | 资金 / P1 / 保留本地 | hook 返回安全整数，或最短往返表示不超过 15 位有效数字的非整数（转十进制文本）；`0.1+0.2`、`1/3`、超出 2^53、NaN/Inf 明确报错，不舍入。源 JSON 数字只有能原样还原时才以 JS 数字传入 hook。`utils.decimal`、`utils.json.parseNumbers` 作为本地扩展保留，已发布固定源码依赖它们 | `go test -timeout 60s ./pkg/jsplugin/ -run 'Decimal\|Usage'`；`./plugins/ -run 'TestReleasedFactorySources'` | 上游无对应合同，吸收上游用量改动时须保留边界 |
| NewAPI `pkg/jsplugin/utils.go` 的 `volcSignV4` | 协议 / P2 / 已吸收 | 照搬上游运行时，按 PascalCase 字段读取；上游 `v1.d.ts` 写 camelCase 与其运行时不符，本地 `v1.d.ts` 改为 PascalCase 并注明 | `go test -timeout 60s ./pkg/jsplugin/ -run 'Volc'`（jimeng 实际签名比对、camelCase 调用报错） | 可反馈上游修正 d.ts |
| Ren2Hub 本地 `metadata_preview.go` | 运维 / P2 / 保留本地 | Root `POST /api/plugin/task/preview` 改用 `moejs/syntax` 解析，原有测试不改即通过，owner 判断改按节点身份；hailuo 的 `Object.assign` meta 与 HEAD 一样不可预览 | `go test -timeout 60s ./pkg/jsplugin/ -run 'Preview'` | 无 |
| NewAPI `relay/helper/stream_scanner.go` 的 `NewStreamScanner(maxBytes ...int)` | 协议 / P2 / 已吸收 | 调用方上限取与全局上限的较小值，初始缓冲不超过上限；插件 SSE 提交按 1 MiB+1 读取，`submit_stream.go` 与上游一致 | `go test -timeout 60s ./relay/helper/ -run 'TestNewStreamScanner'`（移植上游 `TestNewStreamScannerCallerLimit`） | 无 |
| NewAPI `plugins/tasks/*`（1a4166d8e） | 插件 / P1 / 已吸收（带本地补丁） | 已发布版本的源码不改写：改动过的内置插件使用 `<上游版本>+ren2hub.1`（alibaba 1.4.1、doubao 1.2.1、google/vertex-ai 1.0.2、hailuo 1.2.0、kling/sora/sunoapi 1.1.0、vidu 1.0.2、jimeng 1.1.0）；sunoapi 的续跑来源校验与用量路径本就偏离上游，后缀此前漏加，现已补齐。本地补丁：10 个插件的时长/帧数输入继续走 `utils.decimal.bounded`，计费乘法走十进制守卫，kling 使用 `decimal.multiply`，doubao 帧数按整秒向下取整并保留短帧 5 秒回退；Wan3 时长精确求和、按模型声明用量字段、不把 units 换算成 tokens、google/vertex 计费请求归一化、显式 seconds 优先；省略 duration 时不再送入 `bounded`，交由上游默认值处理 | `go test -timeout 60s ./plugins/ ./pkg/jsplugin/ -run 'Kling|Doubao|Decimal|Usage|ReleasedFactorySources|VideoSubmit'`；固定 10ccd9a2f 源码仍逐字校验、旧源码在 moejs 上编译通过 | kling/sora/vidu/hailuo 新版本号按 semver 低于已发布版本（代码不比较 semver，仅影响阅读）；再吸收 doubao/hailuo 时须重放按模型用量补丁 |
| NewAPI `web/src/features/task-plugins/**`、`features/channels/lib/task-plugin-base-url.ts` | 界面 / P2 / 已吸收 | `frontend/src/features/task-plugins/` 按上游目录和组件逐一对应：总开关、已安装/市场标签、详情抽屉、上传与 URL 导入、源码差异、沙箱 dryrun、更新日志；安装前校验索引 key/version/minApiVersion 与源码元数据，市场源打开时重载且 HTTPS/失败不可保存，激活失败仍刷新列表，runtime_error 和详情关闭按钮可见；渠道表单绑定插件时填入插件默认 Base URL，并统一 type 61 的必填提示。本地差异：diverged/降级安装二次确认、市场索引与源码只接受 HTTPS、删除不提供 `force`（本地合同禁止突破占用）、不引入 CodeMirror/zod（源码纯文本查看，元数据预览走 Root `POST /preview`）。旧界面独有的源码编辑标签、运行时 generation 面板和模型定价链接随上游结构移除；`GET /api/plugin/task/runtime/status` 接口保留 | `vitest run`、`vue-tsc -b --force`、`eslint . --max-warnings 0`、`prettier . --check`、`vite build`、Playwright 全量 324 通过/8 跳过（含 task-plugins、admin-bindings、epay-wallet、pricing-partial）` | 渠道表单选插件后自动填模型和名称未吸收 |
| NewAPI `web/src/features/usage-logs/components/task-artifacts.tsx`、`lib/task-artifacts.ts` | 界面/数据隔离 / P2 / 已吸收 | 任务日志结果列按需请求 `GET /api/task/:task_id/artifacts`，只接受宿主签名地址（固定内容路径和唯一 43 位 `access`）；`result_discarded` 不发请求，`artifact_not_found` 显示空态，其他错误可重试。签名地址不缓存；旧 Suno 片段统一走 `safeExternalUrl`，坏片段单独丢弃后最多展示 64 个，媒体补可访问名称，视觉覆盖日夜主题；旧 data URI 视频返回 410 | `go test -timeout 60s -run 'TestDashboardTaskArtifacts|TestVideoContent' ./controller/`；vitest `TaskArtifactsCell`/`taskArtifacts`/`liveContracts`；Playwright `task-log-artifacts` light/dark × desktop/mobile | 无 |
| NewAPI 管理员任务日志插件列（`task-logs-columns.tsx`） | 界面 / P3 / 延期 | 本地日志页统一读取 `/self`，`/api/task/self` 与上游一样不带 `admin_info`/`root_info`；没有管理员全站任务日志页，插件列不可达，故不吸收 | — | 立项管理员全站任务日志时一并吸收插件列与筛选 |

2026-10-04 审计修复验收（Ren2Hub 候选提交 `8c947e252`，根工作区尚未更新 gitlink）：`go build ./...`、`go mod verify`、RelayKit `GOWORK=off go test ./...`、`node scripts/test-go.mjs` 全部通过；本机未设置 MySQL 5.7/PostgreSQL 15 的 `TEST_*`、`BILLING_TEST_*`、`GATEWAY_STARTUP_*` DSN，因此三库合同矩阵未执行，候选环境补跑。前端 typecheck、lint、format、build 和 Vitest 182 文件/1036 用例通过；Playwright 332 项中 324 通过、8 跳过，原先 `admin-bindings`、`epay-wallet`、`pricing-partial` 的 12 项失败已修复。任务插件计费、任务产物、渠道可服务口径、模型同步冲突、路由诊断和生命周期定向回归均通过。空库冒烟、10ccd9a2f 建库后的升级启动、真实供应商调用、常驻服务在途固定任务重启和服务器候选仍待候选/部署阶段验证。

## 2026-10-02 Codex 订阅渠道退役

本次按批准范围移除 ChatGPT Subscription (Codex) 的 type 57 链路，保留标准 OpenAI 渠道的 Codex 模型、Chat/Responses/Compact/WebSocket 及 Codex/Pi 导出。编号 57 保留为 retired/reserved，历史行可读、可显式删除，但不自动转换，不参与路由。渠道分类及迁移合同见[渠道接入类型](../channel/TYPES.md)。旧 P2 窗口和界面检查是当时功能的历史证据，不能用来证明退役后的代码已验证。

2026-10-02 15:08:54 UTC 只读核实 NewAPI main 仍为 `1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5`；发布参照沿用已核实的 `v1.0.0-rc.41`；本轮 Release API 403 继续标为采集 incomplete，不将它当作最新发布查询成功。参考 NewAPI 存在未跟踪 `grep.exe.stackdump`，比较只使用锁定 Git 对象，未修改参考目录。Sub2API main 新观察值为 `b8dece9000c68815a5b867ca5a1e6f236e173905`，AxonHub unstable 为 `89d36d610dc2ebf6d258e56bd1d051572a8c8449`；这些增量另排下轮审计，本次不扩张适配范围。

| source_project / source_ref | category / priority / status | local_scope / contract | test_commands / validation | risk / follow_up |
| --- | --- | --- | --- | --- |
| NewAPI `2a15e3b152307debf42ee1ee9d7f67dd2b0f024a`、`1292b8b2d5e90480521d45af05ff0e8b38a199f6`；当前 `1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5` | 渠道架构 / P2 / 刻意不吸收 | 上游仍保留订阅后端；本地移除 type 57 OAuth、ChatGPT 转发、模型发现、凭据刷新、Wham、provider-usage 和 UI；不删除业务订阅/付款代码 | 退役管理/历史快照零发送、隔离 SQLite/PostgreSQL、前端 950 项及 12 个 Playwright 用例通过；主模块分批与收尾增量回归通过 | OAuth JSON 不可降级为官方 API Key；只有独立批准订阅接入且重新明确认证、路由与资金边界后复评 |
| Sub2API `d6adebd22de00478cd021119ba755f37bcb94fb5` 窗口参考 | 运维 / P2 / 刻意不吸收（撤下既有实现） | 原窗口只有 Codex 订阅供应商，本轮随渠道删除；保留将来适配的认证、隔离、退避和非账务边界说明，不保留无实现的 API | `node tests/upstream-sync/smoke.mjs <本轮二进制>` 通过：退役创建不落库、7 条旧路由 404/零发送，正常 Chat/Claude/Responses 共 3 次本地上游请求且唯一结算 | 不引入新供应商认证替代被删除链路；未来窗口须独立立项 |
| Ren2Hub 标准 OpenAI 现有合同 | 协议 / P1 / 已吸收（等价保留） | SDK fixture 改用 type 1、标准 API Key 和 `/v1/responses`；客户端参数保留、Chat 成功、状态归属、凭据调序/撤销及中断合同继续保护 | `go test -p 4 -timeout 60s ./router -run '^TestGatewayOfficialCodexSDK$' -count=1 -v` 已通过 Python/Node 两语言 24 场景、0 SKIP；OpenAI SDK 3.13.0 / 7.15.0 | 不能把订阅渠道的拒绝行为迁入标准 OpenAI；`TestStandardOpenAICodexCompactAndWebSocket` 实际 HTTP/WS 与结算回归已通过 |

本批已形成 Ren2Hub 候选提交 `8c947e252`；服务器推送、远端部署和数据重建仍未执行。运行结果只按实际检查补记，历史通过、SKIP 或静态检查不能替代本次运行证据。

本轮本地验证已完成：主模块 `node scripts/test-go.mjs -p 4`（后端每次 60 秒）、最终增量回归、独立 RelayKit 测试/构建、Go 全模块构建、前端全检查、双语言 SDK、隔离两库及新二进制空库冒烟通过。未启用的其他外部集成不计作通过。执行记录、环境差异、保留内容和待提交范围见[退役验证记录](../../../docs/reviews/REN2HUB_CODEX_CHANNEL_RETIREMENT_2026-10-02.md)。

## 固定基线与范围

2026-10-02 的首批同步从 Ren2Hub `c92691f8d5254218c8d46f3de5eb20305dd33847` 开始。完整 P2 实施前 HEAD 已由用户工作推进至 `e504860da84531e995eae283e6c03257aadbcba8`；其主题/布局提交保留，154 个既有未提交适配文件与独立归档逐项比对一致后继续实施。当前交付是未提交工作树，`candidate_sha=null`，不能用上述旧 HEAD 代表本轮代码。

实施前只读核实 NewAPI 远端 `main` 仍为 `1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5`（提交时间 2026-10-01 19:50:53 +08:00），2026-10-02 审计修复开始时重新读取仍一致。它比发布标签 `v1.0.0-rc.41`（`2035a82aeb5414253a728bd937d4b8f97aa99b9b`）多 7 个提交。参考 NewAPI 工作树含本地改动，官方比较仅使用锁定 SHA 的 Git 对象，不把参考工作树视为官方源码。

参考 Sub2API `d6adebd22de00478cd021119ba755f37bcb94fb5`；AxonHub 本地和官方 `unstable` 均为 `2bf8e2c916412df72252ac1514591dc47f692d7e`，本地对象已存在，旧“对象缺失”记录不再适用。发布页核实的版本分别为 v0.2.11 与 v1.0.0-beta10。GitHub Release API 的 403 保持采集不完整，不伪装成 API 通过；源码适配依据锁定完整 SHA。参考仓库只读，不是运行依赖。

本批是安全、协议与适合现有网关的完整 P2 手工适配，不是全部上游提交的合并。原始分叉后的提交计数不能作为功能遗漏清单；已有独立实现按实际合同判断。保留 Vue、单实例统一路由、schema 5 资金事件与冻结报价。源码账本不代表提交、部署或真实供应商验收。

状态含义：**已吸收**表示已有对应本地实现；**部分吸收**表示仅落实下列范围；**刻意不吸收**表示当前结构不适用；**待验证**表示尚未证明本地需要相同改动；**延期**须列明进入下一批的条件。

## 本批处置

| 来源与提交                                                                                                                                 | 状态     | 本地落点及差异                                                                                                                                                                           | 验证合同                                                                                                                                  |
| ------------------------------------------------------------------------------------------------------------------------------------------ | -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| NewAPI `feefe09f2781429e039c3f7ba88da7f29492e98c`                                                                                          | 已吸收   | `common/init.go`、模型同步及 Relay/受保护 Fetch 独立克隆 TLS 配置；Waffo 验签后校验配置店铺，创建结账前拒绝空店铺。保留本地订单和资金事件。                                              | `TestHTTPTransportsKeepIndependentTLSSettings`、`TestModelSyncTLSConfigurationIsIndependent`、`TestWaffoPancakeWebhookBindsVerifiedStore` |
| NewAPI `2506e1b980ecf67c829ef20255a16fc61c47cb55`、`6cbb1c7edba4d634ba637a6e6f111115d6de9c9a`                                              | 已吸收   | 共用角色枚举；旧新管理入口的敏感操作绑定单次 Proof，复用 JWT/AuthFlow 和现有权限，不引入上游浏览器 session。                                                                             | 管理创建/更新/删除、无效角色、无效或复用 Proof、撤销后不写库                                                                              |
| NewAPI `caca52f8dea0436d63bacc0dd3e0a73ba7b6b094`                                                                                          | 已吸收   | `personal_access_tokens` 摘要、scope、过期、撤销、单次展示、账户安全 UI 和浏览器管理 API。立即删除旧用户行令牌路径；名称/权限 PATCH 与单次 Proof、本地动态权限求交集。                   | PAT 生命周期、权限收缩、路由/字段默认拒绝、浏览器边界、三库创建与更新                                                                     |
| NewAPI `335ebcff71b843655bf2c64f7a9a33ca21b792ea`、`1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5`                                              | 已吸收   | RelayKit custom/freeform 工具用单一字符串 `input` 包装，Claude/Gemini/Chat 往返还原。保留本地媒体、namespace、终态与转换损失保护；grammar 只能降为说明时仍由 safe 拒绝，allow 明示损失。 | Custom 工具请求/历史/结果、迟到 ID、流式累计与最终参数、重试状态清空                                                                      |
| NewAPI `d0cb7347c07bc935187c077b960435f3f3b08595`、`789c970199ea527e6a26e071915f4a4cd2c64178`                                              | 已吸收   | 共享模型能力表补 Sol/Luna；Claude 原生保留 `safeguards`，不可移植语义进入同一转换预检。                                                                                                  | 模型及快照名称、采样参数、原生字段往返、safe/allow 损失诊断                                                                               |
| NewAPI `dfd3cd893`                                                                                                                         | 已吸收   | Vue 已有系统设置 `requiresRoot` 及守卫测试，无需移植 React 页面。                                                                                                                        | 现有前端 capability guard                                                                                                                 |
| NewAPI `5fe8917be`、`56758edf9` | 刻意不吸收 / 待验证 | Vue 已有语言持久化，没有 React 的 zhTW 规范化链路；图表/字体按本地具体缺陷复评。自动禁用原因的本地缺口已另列本轮 UP-03。 | 不复制 React；图表/字体不宣称已逐页运行认证 |
| Sub2API `97bdde3135dd47f1fbb5c3bff6849d25dcd646b9`                                                                                         | 已吸收   | Claude 普通工具首帧参数作为后备值；真实 delta 替代后备，不能拼接两份 JSON。                                                                                                              | 非空首帧无 delta、空首帧、后续 delta、多工具交错                                                                                          |
| Sub2API `v0.2.11` 创建限制                                                                                                                 | 已吸收   | Token 数量与创建窗口都在现有用户锁事务内检查。最近一小时的软删除记录仍计数；不用 Redis 失败放行。                                                                                        | `TestTokenCreationLimitsAcrossDatabases`、`TestTokenCreationRollbackDoesNotSpendRateLimit`                                                |
| Sub2API `53158587a6101feab6fd4618cebf5111e367d0ba`                                                                                         | 已吸收   | Axios 固定 `1.20.0` 并更新 Bun 锁文件；公开 HTTP 使用 Fetch `credentials: omit`，修复 XHR 同源 Cookie 无法由 `withCredentials: false` 排除的问题。                                       | HTTP 会话定向用例、真实浏览器公开 Cookie/取消测试                                                                                         |
| AxonHub `b5031b0f7ddc723ec583912d9a6e841d0f16f788`                                                                                         | 已吸收   | 能力卡 TEXT JSON 与编辑器；manifest 复用授权渠道、active snapshot 和定价资格。市场、旧列表和 `/v1/models` 同源投影，Token 页导出 Codex/Pi 配置。                                         | 三库读写、未知/false/空数组、Token/价格/插件归属过滤、可信地址及无密钥导出                                                                |
| AxonHub `810294678f44ff4b1cd5f6c97e0c5aad81a0e491`、`02269f123d77acef5d73a2880cfc3421bae39909`、`0ff5b6f8e21158f369b8b2105fd839b523cfeffc` | 部分吸收 | 用严格工具、终态、流预读、refusal/audio/logprobs/reasoning 案例复核本地合同；补共享 DTO 字段和损失诊断，不引入第二个聚合管线。                                                           | RelayKit 回归、宿主流式与终态测试；不把对方 bug 当成本地必现缺陷                                                                          |

## 新安装与 API 边界

旧 `users.access_token` 格式在初始化前被只读拒绝，即使其资金 schema 标记仍为 5，也不会自动改表或清理数据。新数据库继续使用原资金 schema 与计算器；此处变化是认证格式，不改变旧报价的解释。旧库拒绝和数据原样保留由 `TestLegacyManagementTokenSchemaIsRejectedWithoutChanges` 覆盖。

PAT 后端接口和 Proof 见[鉴权合同](../authentication.md)。Token 的 `token_setting.max_user_tokens` 保持原数量语义；新增 `token_setting.max_token_creates_per_hour` 默认 60，0 明确关闭创建频率限制，负数拒绝创建。窗口只计成功创建且包含软删除，过期窗口自动不计数，不保存另一份计数器。

模型元数据的 `capabilities` 是可空 TEXT JSON；字段为 `reasoning_efforts`、`input_modalities`、`output_modalities`、`context_limit`、`output_limit`、`tool_call`、`source`、服务端 `updated_at`。null 表示未知，列表 `[]` 和布尔 false 是明确事实。旧元数据客户端省略能力卡不会清空它；显式空对象清空已知事实。能力卡只作描述，不凭空授权模型、改价或拦截未知能力。

内部 `ModelManifest` 版本为 1，按请求派生，不建独立目录表。已登录用户通过 `/api/user/model-manifest` 获取过滤后的能力摘要，`/api/token/:id/client-config` 从同一投影导出客户端配置。列表和单模型读取共用 Token、渠道、真实能力快照及价格过滤；`/v1/models` 保持兼容字段，不输出能力来源、价格配置正文或渠道身份。当前已授权渠道的真实端点替代原全局端点缓存，不能借无权渠道或无权插件的价格宣称模型可用。

## 完整 P2 本地化

本表是本轮实现队列的最终处置；每行对应来源、类别、落点、合同、风险、验证与复评条件。接口细节见[鉴权合同](../authentication.md)、[网关 API](../routing/GATEWAY_API.md)及[插件 ABI](../plugin-api/v1.md)，不在账本维护第二份 API 定义。

| 项目 / source_ref                                                                                                                              | 类别与状态                          | local_scope / contract                                                                                                                                       | test_commands                                                                                                                            | risk / follow_up                                                                 |
| ---------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| NewAPI `caca52f8dea0436d63bacc0dd3e0a73ba7b6b094`                                                                                              | 权限 / 已吸收                       | `model/personal_access_token.go`、`controller/access_token.go`、PAT 路由、账户安全 UI；改名不需 Proof，scope 绑定 ID 和规范目标，事务内重查，不延长过期时间  | `go test -timeout 60s ./controller -run '^TestPAT'`；PAT Vitest 与 Playwright                                                            | 高权限误用；保留 20 枚/30 天/一次明文及浏览器自管限制，新增 scope 时复评字段权限 |
| NewAPI `ae73ef8e25b957ea3291512b66664f418f33c180`、`87bb71e7fc83121fc995a048077e5c0e209b789f`，基线 `1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5` | 插件/计量 / 已吸收 | `plugins/tasks/doubao` 1.2.1、`pkg/jsplugin`、Images 宿主；Flash 与逐模型档位、compatibilitySchema。retainResult 仅原生路由，Images immediate 自动清理，Responses 保留 | `go test -timeout 60s ./pkg/jsplugin ./plugins ./relay/channel/task/jsplugin`；`go test -timeout 60s ./router -run '^TestGatewayDoubao'` | 旧表达式/冻结报价/重复提交；供应商变化独立验收，不自动导入价格 |
| AxonHub `2bf8e2c916412df72252ac1514591dc47f692d7e`                                                                                             | 模型身份 / 已吸收（等价实现补回归） | `router/gateway_playground_model_identity_test.go`；浏览器无持久 Token、跨渠道映射、逻辑报价和实际响应模型分离                                               | `go test -timeout 60s ./router -run '^TestGatewayPlaygroundModelIdentity'`                                                               | 错价与别名越权；后续改变 Playground 初始化或路由时复验                           |
| AxonHub `b5031b0f7ddc723ec583912d9a6e841d0f16f788` 与锁定 `2bf8e2c916412df72252ac1514591dc47f692d7e` | 目录 / 已吸收 | `service/model_manifest*.go`、目录/导出控制器、能力卡/市场/Keys UI；同一 manifest，未知保真、可信 ServerAddress、环境变量密钥 | `go test -timeout 60s ./service ./controller -run 'Test.*Manifest'`；ClientConfig 回归、目录 Vitest/Playwright | 无权模型或虚假能力；客户端格式变化仍只从 manifest 派生 |
| Sub2API `d6adebd22de00478cd021119ba755f37bcb94fb5` 用量窗口思路                                                                                | 运维 / 历史已吸收（现已撤下）       | `model/provider_usage.go`、`service/provider_usage_window.go`、渠道 API/UI；缓存读取、显式刷新、退避、身份失效、受管退出                                     | `go test -timeout 60s ./service ./controller ./model -run '^TestProviderUsage'`；三库及 provider UI                                      | 过时/错误缓存；仅 Codex，后续供应商须有现有认证边界，不进入账务/路由             |
| AxonHub `2bf8e2c916412df72252ac1514591dc47f692d7e` attempt/关联思路                                                                            | 诊断 / 已吸收（本地事实投影）       | RouteDecision、有界观察器、`request_diagnostic` 模型/API、日志 UI；实际发送/排队/首个有效输出、7 天、用户与 Token 隔离                                       | `go test -timeout 60s ./controller ./service ./model -run '^TestRequestDiagnostic'`；诊断 Vitest/Playwright、三库清理                    | 内部 ID 泄露/后台阻塞；字段权限求交集，Token 0 仅单请求，不存正文或影响资金      |
| Sub2API `fdce5b8d01ccd2dc523543fda05940288912d574`、`ad05eda15788e4eed2e4393120d58564061eeafa`                                                 | 供应商身份 / 刻意不吸收             | Grok CLI/OAuth 身份头与 billing probe；Ren2Hub xAI 使用官方 API，无对应 CLI 身份边界                                                                         | 静态合同核对，不伪造供应商运行验证                                                                                                       | 混用身份；只有独立批准 CLI/OAuth 产品能力和认证设计后复评                        |

插件 profile 收窄后仍可编辑旧价的本地合同已存在，本批补核查与兼容 schema 回归。TLS、Waffo、角色、敏感 Proof、Claude 首帧、custom tools、Axios 和 Token 创建限制沿用首批实现及匹配验证，没有引入第二份实现。

## 实施期间新增窗口

2026-10-02 14:59（UTC+08:00）收尾只读复核发现 Sub2API 主干前进到 `458b92abd4b0b09d123d4c6727dd8d59060bd883`。官方发布 `v0.2.12` 对应 `5106065716e494204fc0e8db16f68f6e9d576be0`，后一提交仅同步 VERSION。NewAPI、AxonHub 跟踪 SHA 未变。原 P2 仍绑定实施开始时的来源；新增窗口从 `d6adebd22de00478cd021119ba755f37bcb94fb5` 沿第一父链完整核对 9 个主干提交，不把该数量当作包含 PR 分支的全部提交数。本地参考仓库未 fetch，仍保留原 HEAD。

安全复评发现真实验证码/重置并发缺口，因此将该 P1 作为单独安全补丁追加；其他新产品能力只作准入处置，不无界追逐移动分支。大型 TypeSafe/充值优惠仅复核文件范围和架构适用性，未声称逐行审计。官方发布页补证不改变 Release API 403 的采集状态。

| source_ref                                 | 类别 / 状态                       | 本地结论、风险与复评条件                                                                                                                                                                                                            |
| ------------------------------------------ | --------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `34fe845f5ab1ae29d541061c175c2a397f2028e3` | TypeSafe 协议 / 延期              | 无 System One 原生适配；新供应商范围确认后先定义请求/响应、计量和转换损失，再接现有渠道，不能复制平台配额与 Composite 调度                                                                                                          |
| `5637f469fa85e30072c5635a7072087a8e25d6bf` | 充值优惠 / 部分吸收               | 本地已有金额折扣和报价快照，不等价于赠金阶梯；新增赠金必须先明确订单快照、幂等资金事件、退款与展示合同，本批不改资金实现                                                                                                            |
| `f3dc0835dfbe912e070b742b123c2ef2784698e0` | 匿名订单枚举 / 刻意不吸收         | 本地不存在该匿名查询，用户订单由 UserAuth 和订单归属保护，旧 verify 已退役；不为移植 IP 限流新增匿名入口，公开恢复流程独立立项                                                                                                      |
| `719013053ac9113b6e8b92e646ba8e894effa6f3` | Token 分组排序 / 延期             | 属于下一批列表能力；进入条件为既有查询的排序允许列表、分页前排序、稳定并列顺序、用户隔离和三库回归                                                                                                                                  |
| `43c4c882fd8dd952557ff2dd9ec54eb2cbca2eea` | Antigravity 错误脱敏 / 刻意不吸收 | 本地无该账号池或专用网关；将来引入须限制错误输出字段，不复制账号池承接补丁，不能宣称已移植无对应代码                                                                                                                                |
| `2529371bab67027e90f3f7cbe9adc04f483e38f8` | 验证码/密码重置 P1 / 已吸收       | `common/verification.go` 仅存 SHA-256、持锁累计失败 5 次失效；`controller/misc.go` 在账户写入前原子消费 token，并发仅一个成功、失败不恢复。新增 5 个 common 和 2 个控制器回归，普通/race、build/vet 通过；不引入 Redis 或迁移数据库 |
| `46f5f5e8ba8d2fbebd824022ccc4b7b5c0173660` | 错误码文案 / 刻意不吸收           | 主干净变化仅两种语言的账号池文案，本地无同设置；Axios 1.20.0 已在旧基线祖先 `53158587a6101feab6fd4618cebf5111e367d0ba`，不重复算新升级                                                                                              |
| `5106065716e494204fc0e8db16f68f6e9d576be0` | 优先级快捷编辑 / 已吸收（等价）   | 既有 `ChannelInlineNumber` 已支持桌面/移动渠道 priority/weight 原地编辑、保存/取消/失败恢复；保留本地范围与选路方向，不复制账号池范围和 debounce 规则                                                                               |
| `458b92abd4b0b09d123d4c6727dd8d59060bd883` | 发布版本号 / 刻意不吸收           | 只记录来源，不修改 Ren2Hub 的发布版本；下一周期从该审计目标继续比较                                                                                                                                                                 |

新增安全补丁验证命令为 `go test -race -timeout 60s ./common -run '^TestVerificationCode'`、`go test -race -timeout 60s ./controller -run '^TestPasswordReset'`，并执行受影响包构建/静态检查、common 全包测试和新二进制空库冒烟。该补丁不改 schema，已有三库 schema/数据合同证据继续适用；新增密码重置控制器回归实际使用隔离 SQLite，未声称再次运行 MySQL/PostgreSQL。完整逐项来源和证据哈希保存在同次归档的 `sub2api-late-delta-review.json`。

## 2026-10-02 审计修复与同步补齐

本次在独立归档 `20261002-post-iteration-audit-1e85be4f` 的 9 项复现基础上实施。写入前再次核对 266 个未提交文件与指纹 `9762168ba823ff093193c234b9eafb1ec734d51ee682dfef02e56713a4e43cbd`，保留主题提交和既有适配。新增证据归档为 `20261002-audit-fixes-1e738bd7`。锁定的 NewAPI、Sub2API、AxonHub 远端引用均未变化；Release API 403 仍为采集不完整。下表状态表示实现处置，最终检查结果另见本轮验证报告。

| ID / 优先级 / source_ref | 状态与 local_scope | behavior_contract / validation_commands | risk / follow_up |
| --- | --- | --- | --- |
| PB-1 / P1 / 本地审计，NewAPI `1a4166d8` 作为行为参照 | 已吸收；`model/task_submission.go`、`task_completion.go`、`service/task_billing.go`、`controller/relay_task.go` | immediate 创建、终态、用量、恢复意图同事务；`TestImmediateTaskAtomicCompletionAcrossDatabases`、进程退出与 Doubao HTTP 故障回归 | 不错误退款、不重发；损坏估算保留积压，未来调整资金事务时复验 |
| SEC-02 / P2 / 本地审计 | 已吸收；`model/user.go`、`controller/user.go` | 状态/角色持锁定向写入，false 不写密码；`TestManagedUserAction*`、`TestManagedUserDemotionRollsBack*`、`TestUserUpdateWithoutPassword*` | 并发资料/密码/余额保护；角色与权限删除原子回滚 |
| SEC-01 / P2 / 本地审计 | 已吸收；`controller/oauth.go` | OAuth/Telegram 二次验证接受原浏览器/PAT身份；`TestPATOAuthVerificationHTTP*` | 撤权、过期、Proof 替换和重放拒绝；账户绑定继续要求浏览器 |
| FCA-01 / P2 / 本地审计 | 已吸收；`service/model_manifest.go`、端点默认映射 | 原生与插件端点各用实际报价解析；`TestModelManifestEndpointPricingMatchesExecutingProtocol`、既有 manifest/导出回归 | 插件价格不能授权原生端点；全局按次后备仅沿用实际执行器规则，不建立新价格源 |
| PB-3 / P2 / NewAPI `1a4166d8e8ba9802d2ca56fe8ecf0ed5404e80d5` | 已吸收；插件 ABI、豆包 1.2.1、宿主结果保留 | 协议级 retainResult 加载时拒绝；原生 false 仅同步，异步保留并告警；Images immediate 一次交付，Responses JSON/SSE/background 保留 | 后续检索、取消、下载失败与升级恢复有回归；不可变旧源码不重解释 |
| PB-2 / P2 / NewAPI `1a4166d8` 差异、本地严格合同 | 已吸收（本地修复）；RelayKit Chat → Responses JSON/SSE | 无名工具返回协议错误，不能 completed 空输出；RelayKit 独立全测与官方工具 SDK | 刻意不吸收上游跳过无名调用的行为；已接受请求不可重放 |
| FCA-02 / P2 / 本地审计 | 历史已吸收；现随订阅渠道撤下 | read 可查看，operate 可刷新/设置，无 write 仍可达；Vitest 与四布局 Playwright | 不拓宽配置写权限，不复制缓存来源 |
| FCA-03 / P2 / 本地审计 | 历史已吸收；`CodexUsagePanel.vue` 现已移除 | 服务端确认值驱动；失败恢复，busy 禁重入，取消/切换/撤权忽略迟到响应 | Vitest 失败路径与真实浏览器交互；异步界面变更复验 |
| ROOT-ENV-01 / P2 / 本地审计 | 已退役；原候选验证器脚本随契约简化移除 | 原脚本承担的隔离边界（真实 DSN、Redis、vLLM、GATEWAY 端点/凭据及 opt-in 不进入子进程）不再由脚本自动强制，改由人工按[验证门槛](../../../skills/ren2hub-upstream-sync/references/validation-gates.md)执行并在报告中记录实际环境 | 退役后无自动化哨兵断言；复评条件为重新引入自动化检查，或在人工执行时发现凭据泄漏 |
| UP-01 / P2 / NewAPI `f5e54ccdb203e8d4469035e72cc48dfd25b4314d` | 已吸收；Vue 插件市场索引/源码/图标下载 | HTTP 重验证，仍保留取消、大小、credentials omit 与 SHA-256；市场定向 Vitest | 同路径重发布不命中过期字节；不绕过完整性校验 |
| UP-02 / P3 / NewAPI `6c14c0762dc7f3bc77d0c80585f974e65ac2a571` | 已吸收；Vue 比价表及纯文本差异组件 | 按字段比较，表达式词段增删，保留精度与缺失/零；Vitest、四布局 Playwright | 不执行表达式、不注入 HTML、不改变选价/保存逻辑 |
| UP-03 / P2 / NewAPI `8112120673bf593eb62e3a297dafca32a4605eb6` | 已吸收（能力本地化）；两套渠道 DTO 与 Vue 状态组件 | 只读 status_reason/time，服务端脱敏/限长；`TestChannelStatusSummary*`、键盘/移动交互 | 不保存第二份原因，不暴露原始正文；新错误结构须先定义安全投影 |

完整来源、检查输出、工具身份、最终指纹及待提交清单随本次归档保存。当前没有提交和远端候选环境授权，`candidate_sha=null`；工作树检查通过也不能改写为候选发布通过。

## 后续与刻意不吸收项

- NewAPI `2ffc59590` 的 moejs alpha 运行时、AxonHub React/GraphQL/Ent 和全文请求存储、Sub2API 完整账号池/自动兑换暂不引入。
- Redis 失败放行资金预占不吸收；只有保持现有预留/恢复不变量的证据充分时才复评，不能用可用性理由制造无担保发送。
- 供应商窗口后续须独立立项；原单凭据 Codex 窗口随 type 57 退役撤下。临时 Playground 跨请求聚合和更多客户端导出格式继续延期，进入条件分别为明确用户隔离键及官方格式合同；标准 OpenAI Codex/Pi 导出仍保留。
- 资金预留、Provider 配额、健康、模型返回名、TTFT 和 CSV 已有本地事实源，参考实现只能补合同，不能另建并行状态。
- 项目权限与预算隔离独立立项，现有用户组、渠道组不当作租户。

## 前一批 P2 验证记录

官方 Node 与 Python SDK 本轮均真实执行语义 63 场景（39 成功、24 拒绝）、媒体 8 场景，以及基础 32 文本路径、16 工具往返、16 中断流和状态归属。金额观察器保持 quota 十进制字符串/精确整数合同。基础夹具修正跨凭据重复 response/item ID，未放宽生产绑定。语义验收沿用首批 Responses SSE 生命周期与转换拒绝统一 400/no-retry 修复；案例细节见[SDK 验收说明](../../tests/gateway-sdk/README.md)。

本轮主模块按仓库脚本完成非 controller 包及 controller 分批测试，修正新增 attempt 字段后的旧断言并续跑剩余批次；后续局部改动执行对应回归。RelayKit 独立全测/构建，前端 174 个文件、933 个用例以及 typecheck、lint、format check、生产构建通过。能力卡/目录、PAT、窗口和诊断的桌面/移动、明暗主题 Playwright 通过，诊断四种布局另有截图审阅。

SQLite、隔离 MySQL 5.7 与 PostgreSQL 实例实际验证能力卡、Token 创建、窗口、诊断建表/读写/清理，以及 PAT 生命周期/PATCH/权限交集，缺环境的 SKIP 未算通过。真实编译二进制空库冒烟通过 PAT、模型元数据、manifest、可信导出、只读窗口、请求诊断、Chat、Claude tool、Responses custom tool；模拟上游共发送 3 次，三笔按次报价各结算一次，最终预留为零。

安全复审修复了仅 `ops:read` 的 PAT 借用拥有者渠道权限显示内部 ID，以及共享窗口采集脱离进程停止/Join 的问题；均有失败路径回归。执行材料保存在工作区外 `20261002-full-p2-365a4bfc` 归档，包含日志、SDK/数据库证据、视觉截图、工作树指纹及未提交清单。初次失败日志保留，不能拿历史通过报告覆盖这些修复的实际结果。

先定向测试，再检查/构建，最后做隔离空库和模拟上游冒烟。Go 每个测试命令 `-timeout 60s`；RelayKit 使用 `GOWORK=off` 独立测试和构建。三库通过环境变量 `GATEWAY_STARTUP_MYSQL_DSN`、`GATEWAY_STARTUP_POSTGRES_DSN` 指向本次可丢弃隔离库，不能用正式库；缺少环境的跳过不是通过。完整入口见[网关复验](../routing/GATEWAY_VALIDATION.md)。

本批不提交、不推送、不发布，不清空现有数据库。后续生产空库重建需要独立发布动作和对应批准；不能把本地验证等同于供应商实测或远端部署。

## 本次审计修复验证与交付

本次 9 项审计问题和 3 项上游能力均完成实现及正式回归。独立收尾复核进一步关闭了三个同合同边界：discarded 结果在 INSERT/UPDATE 前清理，允许缺价不豁免执行器资格或不兼容表达式，共享端点按请求中间件选定的首个合格插件判价。协议端点选择器现在由目录与中间件共用，泛型 task_plugin 的原生路由聚合保持原语义。

主模块 `node scripts/test-go.mjs -count=1` 全部通过；后续局部补修对相关包完整复验，最终 service/middleware 全测、受影响包 vet 和新二进制构建通过。RelayKit 独立全测/构建、插件 ABI 类型/schema、四阶段进程退出、Images 同步/异步及 Responses 三模式、无名工具真实 HTTP 都通过。每个 Go 测试入口保持 `-timeout 60s`，未将 opt-in 集成 SKIP 算作覆盖。

本轮全新隔离 SQLite/MySQL 5.7.19/PostgreSQL 15.19 实际执行权限、并发、原子完成/恢复、schema 和数据合同；最终存储边界修复再复验三库，均 0 SKIP。六套受影响官方 SDK 逐套运行（基础、Media、Python/Node Semantic、CloudToolAndInterruption、CloudParallelTool），均通过且无 SKIP。前端最终 177 文件/950 用例，typecheck、lint、format check、build 通过；35 个独立 Playwright 场景覆盖桌面/移动与明暗，最后开关修改后重跑 21 项，复用未受影响的价格 10 项、插件 4 项。构建仅保留原有大 chunk 提示，不把提示升级为功能失败，也不为此扩张拆包范围。

候选脚本 21 个正式场景通过：vLLM、数据库/Redis、故障/负载 opt-in、五类云供应商端点和凭据哨兵不能进入子进程，本地 SDK 运行时和 PYTHONPATH 保留。最终二进制空库冒烟通过权限目录、PAT、能力卡、导出、窗口、诊断和三类生成；模拟上游恰好 3 次，每次唯一结算、最终预留为零。本轮测试数据库、浏览器、Vite 和冒烟进程已退出。

完整结果见[审计修复交付报告](AUDIT_FIXES_2026-10-02.md)。最终工作树清单、指纹、日志哈希和工具身份在工作区外 `20261002-audit-fixes-1e738bd7`；旧失败和复现材料保留。此交付为已验证本地工作树，`candidate_sha=null`，没有提交、推送、远端部署或生产数据库操作。