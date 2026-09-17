> 历史归档：2026-09-16 单实例改造前的工作树材料。文中 Redis 并发、多实例和旧测试结果只适用于所记版本，不是当前实现的验收证据。现行契约见 [统一路由](../UNIFIED_ROUTING.md)。

# 网关本地验证操作说明

仅适用于隔离本地环境。数据库夹具会创建并清理随机命名的数据库，Redis 夹具会清理专用状态，不得使用生产连接。应用部署和付费供应商测试不在本次授权内。

## 检查顺序

后端统一 `-timeout 60s`，先定向测试，再静态检查、构建和冒烟。逐条检查退出码，不能用最后一条成功掩盖前面的失败。

```powershell
# Ren2Hub
go test -p 4 -timeout 60s ./relay ./relay/channel/aws ./relay/channel/coze ./relay/helper
go test -p 4 -timeout 60s ./model ./service ./controller ./router -run 'TestGateway|TestCapabilityFirstPublish'
go test -p 4 -timeout 60s ./...
go vet -p 4 ./...
go build -p 4 -o .local-tests/gateway/ren2hub-acceptance.exe .
```

RelayKit 在其独立目录中设置 `GOWORK=off`，执行 `go test -timeout 60s ./...` 和 `go build ./...`；随后恢复原环境变量。根模块通过不能替代该检查。

## 外部夹具

| 测试 | 环境变量或定义位置 |
|---|---|
| 完整迁移、并发快照、状态保留三库测试 | `GATEWAY_STARTUP_MYSQL_DSN`、`GATEWAY_STARTUP_POSTGRES_DSN`；SQLite 无需变量 |
| 任务提交、终态恢复、凭据三库测试 | 同上；`TestAcceptedTaskAndAccountingCommitTogether`、`TestTaskCompletionRecoveryAcrossDatabases`、`TestTaskCredentialBindingSurvivesKeyReorderAndTrustedRefresh` |
| `TestBillingDatabaseIntegration` | `service/billing_database_integration_test.go` 中的数据库变量 |
| `TestGatewayMultiProcessAdmission` | `service/gateway_multiprocess_test.go` 中的 MySQL/Redis 变量 |
| `TestGatewayApplicationInstances` | `GATEWAY_APP_BINARY`、`GATEWAY_APP_MYSQL_DSN`、`GATEWAY_APP_REDIS_ADDR`；使用专用 Redis DB 15 |
| 官方文本、媒体、Realtime、云适配器及 AdvancedCustom SDK 测试 | `GATEWAY_SDK_PYTHON`、`GATEWAY_SDK_NODE` 及 Python 依赖搜索路径；测试函数见 `router/gateway_*sdk_test.go` |

缺少变量时外部集成测试会跳过。普通全测通过不代表这些检查已执行。SDK 固定依赖在 `tests/gateway-sdk`，不得升级版本后直接复用旧结论。

本任务本地服务使用 MySQL `127.0.0.1:33316`、PostgreSQL `127.0.0.1:55436`、Redis `127.0.0.1:56379`；它们不是生产配置。数据库、随机测试凭据、二进制和日志位于忽略目录 `.local-tests/gateway`，不得加入提交或公开输出。

## 控制台

在 `frontend` 逐条运行定向 Vitest、`bun run typecheck`、`bun run lint`、`bun run format:check`、`bun run build`。预览回归为 `src/api/__tests__/routingApi.spec.ts` 和 `src/views/console/__tests__/TokenRoutingView.spec.ts`。

```powershell
$env:PLAYWRIGHT_PORT = '5176'
bun run test:visual -- tests/visual/gateway.spec.ts
```

`gateway.spec.ts` 使用 API fixture。`gateway-live.spec.ts` 和 `gateway-preview-live.spec.ts` 通过 `GATEWAY_LIVE_TEST=1` 启用真实本地后端，仅接受回环地址，使用独立测试数据库和随机登录凭据。后者会创建测试用户和渠道，验证普通用户样本预览、投影损失、权限和诊断保密，结束时删除该测试渠道。截图在 `frontend/output`，覆盖 1440×1000 桌面、390×844 移动端和明暗主题；还需实际检查截图内容。

样本预览默认验证 Coze，设置 `GATEWAY_PREVIEW_PROVIDER=ollama` 可验证 Ollama 的共享投影。v9 上该组合的四个 Playwright 场景、类型检查、定向 lint 和测试文件格式化均通过，四张截图已检查；预览未连接配置的无监听上游地址，普通用户看不到 Key 和内部端点。前端产品代码未改变，其余构建及 584 条 Vitest 复用之前通过的结果。

## 本轮结果

2026-09-12：状态三库保留与清理、JSON/SSE HTTP 多 Key 续轮、AWS 单次发送/Nova、Coze 明确终止/历史/轮询取消/接受状态、投影策略、公共 scanner、392 场景请求矩阵及预览 19 条定向测试通过。三组各四个 Playwright 场景分别验证 API fixture、真实准入和真实样本预览，均已通过并检查截图。

2026-09-13 v8：主模块 `go test -p 4 -timeout 60s ./...`、受影响的 relay/service/能力登记 `go vet`、后端构建，以及 RelayKit `GOWORK=off` 独立全测/build 通过。主模块日志为 `.local-tests/gateway/backend-v8.log`，独立模块为 `relaykit-v8.log`，适配器矩阵为 `adaptor-contract-v8.log`。官方 Python/TypeScript SDK 在工具语义诊断和流式终止修复后重新通过，日志为 `sdk-state-results.log`；之后仅修改宿主旧投影和图片结构诊断，未重复未受影响的 SDK 文本场景。

本批新增六个旧文本适配器精确投影、Dify 18 个策略场景、6 个实际 HTTP 上传生命周期场景及智谱 v4 字段保留回归。标准响应矩阵使用实际 `DoResponse`，旧响应矩阵包含 JSON、完整流、截断、解析失败、无效 usage、取消，共 30 场景。讯飞 8 个本地 WebSocket 场景验证最终覆盖体、取消、截断及准入拒绝时零连接。PaLM 4 个缓冲流场景覆盖正常和失败终止。模拟上游验证仍不属于真实供应商认证。

本次新增 `router/gateway_system_prompt_test.go`：16 条协议/原生透传路径验证渠道提示词、模型映射和扩展字段保留，另通过实际 AWS 适配器及模拟 HTTP 传输验证 Nova 收到渠道提示。`router/gateway_extensions_test.go` 的四协议原生/跨协议共 8 场景验证 strict 对未知顶层字段的拒绝，不发送也不产生账务预扣。RelayKit 回归验证 `0/false/null`、计划诊断传播和已拒绝计划不能在 DTO 丢字段后重新执行。

前端布尔设置标签修复后的 typecheck/lint/format/build 和四个真实后端 Playwright 场景通过，已检查桌面/移动、明暗主题的供应商设置截图。此前三库 36 个账务场景、完整迁移重复启动、TransportID 迁移、状态保留及多进程准入结果可复用。v8 二进制重新通过真实双应用检查，日志为 `application-instances-v8.log`；测试进程 30272/34104 已退出，没有把普通全测中的跳过项计为集成通过。

本地试用地址为 `http://127.0.0.1:5176`，后端 `http://127.0.0.1:3000` 使用本轮构建的 `ren2hub-contract-v8.exe`；状态返回 200，未认证指标返回 401。该运行目录使用随机测试账户和隔离 SQLite/Redis，不是发布环境。

2026-09-13 后续批次：`router/gateway_task_test.go` 通过实际本地 HTTP 验证任务 201/202、提交失败、结算恢复、原凭据 Remix、RPM 与重试边界；`relay/task_response_contract_test.go` 验证 10 个任务适配器共 30 个响应场景。`model/task_submission_test.go` 在 SQLite、MySQL 5.7 和 PostgreSQL simple protocol 通过事务回滚、重复保存、结算恢复和统计幂等检查，日志为 `task-databases.log`。Suno 同名任务归属与 Key 调序/替换回归在 `service/task_polling_owner_test.go`。

共享输出上限修复后，RelayKit 独立全测和 build 通过；官方 Python/TypeScript SDK 再次通过，日志为 `sdk-output-limit.log`。本批主模块全测记录为 `backend-task-final.log`，任务定向日志为 `task-submit-final.log`；静态检查和构建结果以本批最后一次退出码为准。前端没有新增改动，复用已通过的 584 条 Vitest 及类型/lint/格式/build/Playwright 结果。本地控制台仍运行 v8 二进制，后续任务改动尚未更新到该试用进程。

2026-09-13 v9：主模块全测、全模块 vet 和后端构建通过，记录为 `backend-v9-final.log`、`vet-v9.log`；独立 RelayKit 全测、vet/build 通过，记录为 `relaykit-v9-final.log`、`relaykit-v9-vet.log`。新增三库终态恢复 24 场景包含非空任务 JSON、终态/意图同事务、Token/统计/日志故障、钱包/订阅及零差额，记录为 `task-terminal-databases.log`。`task-terminal-recovery.log` 验证后台恢复入口，`task-terminal-integration.log` 覆盖轮询、提交与恢复集成。

官方 SDK 新增 Ollama 四协议，每语言 32 条文本 JSON/SSE、16 轮工具往返、16 条中断流通过，保留原生状态及前导失败切换验证，记录为 `sdk-ollama-v9.log`。`ollama-terminal-once.log` 验证原始 HTTP 流只有一个失败终止事件，`ollama-preview-v9.log` 验证共享投影及各入站协议的工具约束。v9 真实双应用测试通过，记录为 `application-instances-v9.log`，测试进程 22384/19192 已退出。本地试用后端改为 `ren2hub-contract-v9.exe`，仍使用隔离 SQLite 和 Redis DB 14。

## 发布边界

2026-09-14 媒体交付批次：

| 日志（相对 `.local-tests/gateway/`） | 通过的检查 |
|---|---|
| `image-final-budget.log` | 图像 handler、最终数量/品质预算、JSON/multipart、覆盖超限/余额不足零发送，OpenAI 原生模型映射与扩展保留 |
| `image-adapter-final.log` | 8 类图像适配器 JSON/转 SSE、正常/空输出共 32 场景，实际交付数量结算 |
| `replicate-upload.log` | 5 个实际 HTTP 场景：上传晚于数量校验和预扣、最终传输头、上传损坏/生成结果未知不重放、无输出退款 |
| `audio-contracts-final.log` | 音频 handler 及 11 个实际 HTTP 请求，TTS/STT 的终止、错误和计费边界 |
| `vector-final.log` / `vector-http-contracts.log` / `cloudflare-reject.log` | 26 条向量路径、100 个响应场景、2 个断开和 Gemini 映射；8 个账务 HTTP 场景及 Cloudflare 不可执行 Rerank 零发送 |
| `sse-write-contracts.log` / `sse-helper-tests.log` / `stream-output-final.log` | SSE 完整写入、短写、取消、首错保留、换行与状态绑定；主模块回归还覆盖 prelude 短写及旧 callback 错误不罚供应商 |
| `sdk-media-final.log` | 官方 Python/TypeScript SDK 文本、工具、中断、状态续轮，以及每语言 7 条正常媒体路径和 1 条图片中断路径 |
| `application-instances-media-final.log` | 使用最终媒体构建的两个真实应用进程，验证跨协议用户并发、共享渠道容量、取消、重新准入和数据库计费 |
| `relaykit-media-final.log` / `relaykit-media-vet.log` | `GOWORK=off` 独立全测、vet；独立 build 退出码 0 |
| `backend-media-final.log` | 最终主模块 `go test -p 4 -timeout 60s ./...`，外部夹具跳过项不计为执行 |

上述均为本地模拟上游。三数据库账务/迁移及多进程租约实现本批没有修改，复用其已通过结果。前端代码本批没有修改，复用 584 条 Vitest、类型/lint/格式/build 和 Playwright 证据。当前本地控制台仍为 v9，新增媒体实现由独立测试网关运行。

2026-09-13 Midjourney 与媒体补充：`backend-midjourney-final.log` 是 Midjourney 批次主模块全测；`midjourney-databases-final.log` 为三库 33 个提交/终态场景，`midjourney-startup-databases.log` 为含旧 Midjourney 表和新 `private_data` 的完整迁移两次及关闭重连。`sdk-midjourney-final.log` 为该批次官方 Python/TypeScript SDK 重跑。上述全测早于后续图像/Realtime 改动，不代表媒体最终检查。

后续定向证据：`media-contracts.log` 覆盖图像、Realtime 与 AttemptOutcome；`realtime-outcomes.log` 覆盖失败响应、限流、输入错误、取消、输出上限及 usage 无输出。OpenAI 图像的 `relay/channel/openai` 契约回归覆盖 JSON/SSE、异常终止、usage 校验、大首帧及按交付数量结算；`router/gateway_realtime_test.go`、`gateway_realtime_outcome_test.go` 使用真实本地 WebSocket，未调用供应商。

旧 Midjourney 无稳定凭据的任务仍保留在原表并可经既有控制台查看；后台和续作不会猜测 Key，需重建任务或人工对账。新图片链接采用独立 v2 签名域和本地行 ID；旧签名不能用于寻址新行。提交已经接受但数据库持续无法写入时，网关不返回成功 ID，也不自动重放；本次未证明各供应商的幂等查询确认能力。

产品源码已整理为后端提交 `02b0a180a73b9318ebf9f4eac38e78d81735df21` 和控制台提交 `638cfcc46e2890e8fedc9332a95edb257a9114f0`。按[实施记录](GATEWAY_IMPROVEMENT_STATUS_PRE_SINGLE_INSTANCE_2026_09_16.md)核对证明边界，再按工作区更新技能和同步流程准备发布。本地提交、二进制、日志或未发布 gitlink 都不是已发布版本。

能力合同 v11 需要按新指纹重建投影，保留 v10 对 Cloudflare Rerank 的撤销，并登记专有音频路径/流式边界及 Vertex Gemini 的 Claude 入站；本批未增加数据库列。此前任务终态复用既有恢复表，仅增加消费快照的 JSON 字段，没有新增任务真相来源。safe 和默认限额只迁移一次；新旧 Redis 准入不得同时接流。只增不删的结构也不能证明旧镜像可正确执行新限额和状态规则。生产仍需不可变 SHA、备份、排空、应用容器替换与上线验证，本轮没有执行这些操作。

2026-09-14 v11 定向证据：

| 日志（相对 `.local-tests/gateway/`） | 已通过范围 |
|---|---|
| `sdk-realtime-final.log` | 官方 Python/TypeScript SDK 共 8 个场景，交错响应、工具往返、上游失败/断开、客户端关闭和正常关闭码；实际类型化迭代器/事件接口 |
| `sdk-cloud-partial.log` / `sdk-cloud-vertex-final.log` | 云基础矩阵共 80 条：Azure、AWS Claude Bearer/SigV4、Vertex Claude/Gemini，四协议 JSON/SSE；后者包含 Gemini→AWS 空响应回归 |
| `sdk-custom.log` | AdvancedCustom 各语言 22 条成功及 10 条发送前拒绝；包含修复后的原生 Gemini 流地址、查询保留、映射与认证 |
| `ali-image-lifecycle.log` | Ali 有界轮询、取消传播、提交身份、错误/重定向/下载失败，以及参数序列化后的数量预算 |
| `provider-audio-final.log` | Cloudflare、MiniMax、火山专有音频共 73 场景，含错误、空/畸形数据、参数零值、取消和短写 |
| `volc-audio-gateway.log` | HTTP→火山本地 WS 共 9 场景；正常/截断/取消、额度拒绝零连接、账单、健康和租约；二进制异常可被 HTTP 客户端识别 |
| `audio-budget-ali-acceptance.log` | MiniMax/火山最终朗读文本及额度拒绝 4 个 HTTP 场景；Ali 接受/未知/明确拒绝/轮询失败/缺任务 ID 共 5 场景 |
| `sdk-audio.log` | 专有音频官方 OpenAI SDK 各语言 5 条成功、2 条发送前拒绝、1 条二进制传输异常；实际发送次数和 6 条消费记录核对 |
| `billing-source-databases.log` | SQLite/MySQL/PostgreSQL 共 36 场景，重复终结及故障恢复后消费日志保留原始用量来源 |
| `relaykit-v11.log` / `relaykit-v11-vet.log` | 音频 stream_format 要求分析改动后的独立全测、vet；该批独立 build 退出码 0，后续转换器变更须另记最终结果 |

`service/routing_audio_conversion_test.go` 的 12 个场景确认预览与执行使用同一路径/流式能力。`TestRouteProfilePreviewIgnoresRetiredAbilityGroups` 验证旧业务组不重新成为模型授权来源；账户组配置和 Token 私有路由规则仍由各自原有边界处理。本地控制台进程仍为 v9，以上均由独立夹具运行。

同日后续集成证据：

| 日志（相对 `.local-tests/gateway/`） | 已通过范围 |
|---|---|
| `adaptor-matrix-v11-final.log` | 400 个请求规划/转换场景和 226 条标准适配器 JSON/SSE 响应；修复枚举遗漏后包含 NewAPI |
| `sdk-cloud-tools-final.log` / `sdk-cloud-parallel-tools.log` | 云 40 个工具往返、40 个中断、40 个双工具往返；首帧并行调用、0/false 参数、逆序结果和唯一终态 |
| `vertex-tool-identity-rejection.log` | 四协议乘三策略共 12 个歧义历史 HTTP 拒绝；上游、预扣及账务意图均为零 |
| `sdk-legacy.log` | Baidu/Cohere/Tencent TC3/Zhipu/Dify/PaLM 两语言共 108 项，含正常、发送前拒绝和中断；角色只输出一次 |
| `compatible-sdk-python.log` / `compatible-sdk-node.log` | 28 个标准适配器配置，两语言共 340 条成功和 108 条发送前拒绝；实际传输路径、映射、认证、SDK 响应及 usage |
| `compatible-json-delivery.log` | JSON 下游短写、写失败、取消在真实控制器中不重发、不处罚供应商，退款并释放租约 |
| `stream-delivery-final.log` | 共享 scanner 与 OpenAI 全包；转换器无事件时不提前提交、无心跳、前导上限；Realtime item/delta 去重及中断估算 |
| `sdk-text-v11-final.log` | 标准文本两语言各 32 条正常、16 个工具往返和 16 条中断，及状态续轮/前导失败切换 |
| `sdk-media-v11-final.log` / `sdk-audio-v11-final.log` / `sdk-realtime-v11-final.log` | 共享 scanner 修改后重新执行三套官方 SDK；音频拒绝断言收紧为 400 |
| `veo-contracts.log` / `veo-http.log` | Veo 61 个定向场景和 34 个 HTTP 场景；参数、图片、JWT 本地换取与实际预扣一致，拒绝无上游和账务变更 |
| `video-task-lifecycle.log` / `video-task-polling.log` | 22 个下载与轮询 HTTP 场景及恢复回归；原凭据、错误任务 ID、下载失败后恢复、零错误账务变更 |
| `backend-v11-final.log` / `backend-v11-vet.log` | 当时主模块全测和 vet 已通过；此日志早于后续 Suno/Veo/兼容 JSON 修改，不能作为这些修改的最终整体验收 |
| `relaykit-v11-final.log` | Vertex 工具身份和多候选计划后的独立全测；同批 `GOWORK=off` vet/build 退出码为 0 |

SDK 测试必须逐套使用 `-timeout 60s`，不要在设置运行时变量后将全部官方 SDK 套件放入一次 router 测试。供应商夹具、固定 SDK 和真实网关链路通过只证明本地契约，不是生产供应商认证。

收尾新增证据：

| 日志（相对 `.local-tests/gateway/`） | 已通过范围 |
|---|---|
| `sdk-special.log` / `special-adapters.log` | AWS Nova、Coze、讯飞两语言共 54 场景；Coze/讯飞首个 assistant 角色，实际本地 HTTP/WS、发送前拒绝、中断及已接受不重放；三个适配器包全测 |
| `sdk-codex.log` | Codex 官方 SDK 两语言共 32 场景；Responses JSON/SSE、其他协议拒绝、未知/跨 Token、Key 调序、凭据停用/替换、上游失败和中断 |
| `google-video-download.log` / `google-video-adapters.log` | 39 个控制器场景及相关适配器包；Google 缓存/补查前稳定凭据复核、取消 OAuth/补查、HTTP 错误/原任务 ID/终态校验与零后续下载 |
| `reasoning-search-plan.log` | 11 个公共计划/执行场景；推理预算 0、开关、effort 和嵌套别名；配置型搜索的显式降级；Chat/Responses effort 保真及 summary 损失 |
| `reasoning-file-http.log` | 四种入站协议的五种推理/搜索输入乘三策略，每组原生与转换；30 个 HTTP 请求及 8 类状态回归、服务层引用提取 |
| `file-state-delivery.log` | Google Files HTTPS 与 gs:// 原生续轮、未知/跨 Token 拒绝，实际发送体包含原文件引用 |
| `sdk-vertex-open-source.log` / `vertex-auth-identity.log` / `vertex-final-packages.log` | 官方 SDK 两语言 64 场景；服务账号缓存按稳定凭据隔离，llama/-maas 实际 JWT 换取、原生/转换 JSON/SSE；API Key 不支持路径零换取/零发送/零预扣 |
| `media-relaykit-tests.log` / `media-plan-http.log` | RelayKit 独立全测、同批 vet/build；四入口媒体计划 48 个实际 HTTP 请求，24 条图片保真与正常结算、24 条发送前拒绝和零账务变更 |
| `chat-file-ownership.log` | Chat 正规嵌套 file.file_id 提取、原生身份准备及既有状态/传输边界定向回归 |
| `backend-v11-integrated.log` / `backend-v11-integrated-vet.log` | 最后文件归属、图片编码及四入口候选回归后的主模块全测和 vet；外部 SDK/数据库条件跳过项不算本轮执行 |
| `sdk-text-integrated.log` / `sdk-media-integrated.log` | 核心文本 SDK 已在 Chat 文件身份初始化后重验；媒体 SDK 在媒体转换修复后通过，最后图片编码由语义套件覆盖 |
| `semantic-sdk-python.log` / `semantic-sdk-node.log` | 两语言共 116 场景，每语言 34 成功/24 拒绝；四入口图片 JSON/SSE、结构输出、搜索/custom tools、签名和私有文件；逐请求核对发送/预扣会话/钱包/Token/账单，拒绝全部零变化 |
| `fourth-text-candidate.log` | 四种文本入口的前三个高优先级候选满载，第四个可用；仅一次发送和结算，未发送渠道不受健康处罚 |
| `application-instances-v11-integrated.log` | 最后产品代码构建后的真实双应用 43256/28992；跨协议用户并发、共享渠道容量、不发送不计费、取消传播、重新准入及数据库计费；子进程已退出 |
| `linux-amd64-build.log` | 最后产品源码在 Linux amd64、CGO_ENABLED=0、GOWORK=off、GOEXPERIMENT=greenteagc 下编译通过；Windows 验收二进制构建也退出 0 |

最终产品提交为 `638cfcc46e2890e8fedc9332a95edb257a9114f0`，包含后端提交 `02b0a180a73b9318ebf9f4eac38e78d81735df21`。主模块检查在相同产品代码上执行，文档整理不改变产品构建输入。`media-relaykit-tests.log` 已在最终 Base64URL 正规化后更新，独立 vet/build 同批通过；没有重复执行未变化的三库/前端检查，复用上文证据。

本地工作区二进制用于隔离验收，尚未获得最终发布镜像身份。发布迁移分类、Redis 排空和镜像回退边界见[发布准备](GATEWAY_RELEASE_PRE_SINGLE_INSTANCE_2026_09_16.md)；本机没有 Docker 可执行文件，镜像冷构建未执行。Linux 交叉编译不代表已运行 Linux 镜像。
