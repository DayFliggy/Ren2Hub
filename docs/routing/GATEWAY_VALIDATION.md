# 单实例网关验证步骤

测试仅连接明确隔离的数据库、Redis 和模拟上游；本地夹具与服务器验收实例的数据各自独立，不使用生产数据或付费模型。本文是复验方法，判定规则见[网关验收边界](SINGLE_INSTANCE_ACCEPTANCE.md)；资金与支付另见[账务复验](../billing/VALIDATION.md)和[易支付验证入口](../billing/EPAY.md#验证入口)。

## 顺序与命令

每个后端测试命令使用 `-timeout 60s`。先定向回归与 race，再三库/协议集成、静态检查和构建。检查各命令退出码；未设置环境变量而跳过的测试不能记为通过。

```powershell
go test ./service ./controller ./middleware -run 'TestLocalAdmission|TestGateway|TestRouteSendFence|TestRouteBackoff|TestRouteRetry|TestRouteQueue' -timeout 60s
go test -race ./service -run 'TestLocalAdmission|TestGatewayAdmissionCommitted|TestGatewayChannelPolicyRefresh|TestGatewayRetry|TestGatewayDrain' -timeout 60s
go test -race ./controller -run 'TestRouteSendFence|TestRouteBackoff|TestRouteRetry|TestRouteQueue' -timeout 60s
go test -race ./controller -run '^TestFinalRouteEvidenceRetainsLastSendAfterRejectedCandidates$' -timeout 60s
go test -race ./service ./controller ./router -run 'TestGatewayBatchRevocation|TestGatewayRoutingPolicy|TestGatewayChannelPolicy|TestRouteQueue|TestRouteAvailability|TestGatewayMediaCredential|TestGatewayPreSend|TestGatewayTaskExplicitRejection' -timeout 60s
go test ./model -run 'TestGatewayConfig|TestGatewayStartup' -timeout 60s
go test ./service -run 'TestBillingDatabaseIntegration|TestGatewayMultiProcessSuccessReservation' -timeout 60s
go test ./router -timeout 60s
go test ./cmd/gateway-lease-cleanup -timeout 60s
go test ./service -run 'TestGatewayProvider' -count=1 -timeout 60s
go vet -p 4 ./...
go build ./...
```

涉及 RelayKit、TokenKit 或公共契约时，进入对应模块目录设置 `GOWORK=off`，单独运行 `go test ./... -timeout 60s` 和 `go build ./...`，随后恢复环境。主模块全量入口为 `node scripts/test-go.mjs -count=1`，controller 按仓库脚本分批，单批仍为 60 秒。同代码和环境已通过的包不必因其他包结束而重复运行。

协议、原生 Responses WebSocket、逐路由透传、响应模型和 Claude effort 回归按下列命令拆分执行，避免把多个测试集塞进同一 60 秒包预算：

```powershell
go test -timeout 60s ./relay/helper ./relay/channel/openai ./relay/channel/claude ./relay/channel/ollama ./relay/channel/gemini ./relay/common -count=1
go test -timeout 60s ./service ./controller ./router -run '^(TestResponsesWS|TestGatewayResponsesWS|Test.*Compact)' -count=1
go test -timeout 60s ./router -run '^TestGateway(ResponsesWS|UnsupportedTokenCount|ResponseTerminals|ToolImageHoist|ResponsesNamespace|GeminiPartial|ReasoningAndSearch)' -count=1
go test -timeout 60s ./relay ./router ./model ./service -run '^Test(ResponseModel|UserLogResponseModel|GatewayResponsesWS|ResponsesUsage)' -count=1
go test -timeout 60s ./controller -run '^Test(VLLMStatus|SGLangStatus|ParseVLLM)' -count=1
go test -timeout 60s ./router -run '^Test(GatewayAdvancedCustomRoutePassthrough|InferenceChannelStatus|GatewayClaudeEffortProtocolAndIncomingParamBilling|GatewayClaudeMessageOutputConfigConversionBeforeBilling)' -count=1
go test -timeout 60s ./service ./relay/common -run 'Test.*(Preview|PassThrough|Passthrough|ChannelMeta)' -count=1
```

RelayKit 侧的 `TestClaudeResolveEffectiveEffort*` 与 `TestClaudeMessageOutputConfig*` 随上文 `GOWORK=off` 全量测试执行。WebSocket 的最小 race 名单须在具备 cgo 的环境运行，不能用普通并发测试代替：

```sh
go test -race -timeout 60s ./router -run '^TestGatewayResponsesWS(PreviousResponseOwnership|ReconnectPreservesOnlySameOwner|RejectsChangedConnectionIdentity|SettlesObservedUsageAndRejectsOverlappingCreate|RejectionHonorsSharedRetryBudget|ClientDisconnectSettlesDeliveredOutput|DrainLifecycle)$' -count=1
```

本地 httptest、SQLite、miniredis 和隔离子进程排空只证明相应边界；真实供应商、运行二进制及部署容器停机链路需要独立证据。协议与状态 API 字段见[网关 API](GATEWAY_API.md) 与[协议支持矩阵](GATEWAY_SUPPORT_MATRIX.md)。

## 本地依赖

复跑前准备专用隔离实例，核对数据库版本、监听端口和连接目标，不假定之前使用的进程仍在运行。测试创建临时数据库/表并按夹具清理；每次结果只证明记录的服务端版本与测试条件。所用版本和实际运行的套件只写入所属工作区服务器档案，不能由历史三库通过推导当前环境可用。

| 检查                  | 环境变量                                                                                                                    |
| --------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| 配置与启动迁移三库    | `GATEWAY_STARTUP_MYSQL_DSN`、`GATEWAY_STARTUP_POSTGRES_DSN`                                                                 |
| 模型目录与诊断三库    | `TEST_MYSQL_DSN`、`TEST_POSTGRES_DSN`；必须指向新建的可丢弃实例/数据库                                                      |
| PAT 与诊断控制器三库  | 另设 `TEST_SECURITY_DIALECT` 为 `sqlite`、`mysql` 或 `postgres`，逐方言运行；PostgreSQL DSN 使用 `postgresql://...` URL，夹具只接受回环地址并新建用例库 |
| 账务三库              | `BILLING_TEST_MYSQL_DSN`、`BILLING_TEST_POSTGRES_DSN`                                                                       |
| 跨进程成功频率预留    | `GATEWAY_MULTIPROCESS_MYSQL_DSN`、`GATEWAY_MULTIPROCESS_REDIS_ADDR`                                                         |
| 单实例及顺序重启      | `GATEWAY_APP_BINARY`、`GATEWAY_APP_MYSQL_DSN`、`GATEWAY_APP_REDIS_ADDR`；运行 `TestGatewayApplicationSingleInstanceRestart` |
| 官方 SDK              | `GATEWAY_SDK_PYTHON`、`GATEWAY_SDK_NODE`，必要时 `PYTHONPATH` 指向隔离依赖                                                  |

SDK 套件与锁定版本见 [SDK 测试说明](../../tests/gateway-sdk/README.md)，每套单独执行，避免把多个 60 秒工作合并成一个总 60 秒命令。Windows 代理环境中为回环请求设置 `NO_PROXY=127.0.0.1,localhost`。禁止关闭 TLS 校验来让 Realtime 测试通过。

## 前端

在 `frontend` 按顺序运行：

```powershell
bun run test:run -- src/api/__tests__/gateway.spec.ts src/components/console/gateway/__tests__/gateway.spec.ts
bun run typecheck
bun run lint
bun run format:check
bun run build
bun run test:visual -- tests/visual/gateway.spec.ts
```

浏览器场景覆盖桌面、移动端、明暗主题、Provider unavailable、draining、配置交互与横向溢出；截图在 `frontend/output`，须检查图像。API fixture 的浏览器测试不等同于真实后端验证。

## PAT、目录与诊断专项

先运行以下定向合同，再执行受影响包的静态检查、构建和隔离二进制冒烟。三库变量缺失而 SKIP 的子测试仍未完成；不能由 SQLite 通过推断其他方言通过。

```powershell
go test -timeout 60s ./controller -run '^Test(PAT|RequestDiagnostic)' -count=1
go test -race -timeout 60s ./common -run '^TestVerificationCode' -count=1
go test -timeout 60s ./controller -run '^TestPasswordReset' -count=1
go test -timeout 60s ./service -run '^TestRequestDiagnostic|Test.*(Manifest|ClientConfig)' -count=1
go test -race -timeout 60s ./service -run '^Test(RequestDiagnostic|LocalAdmission)' -count=1
go test -timeout 60s ./model -run '^Test(RequestDiagnostic|ModelCapabilities|TokenCreation|GatewayStartupMigrations)' -count=1
go test -timeout 60s ./pkg/jsplugin ./plugins ./relay/channel/task/jsplugin -count=1
go test -timeout 60s ./router -run '^TestGateway(Doubao|PlaygroundModelIdentity)' -count=1
node tests/upstream-sync/smoke.mjs <当前构建的可执行文件>
```

PAT 回归验证目标 ID/scopes 绑定、Proof 替换和重放、动态撤权、过期/撤销、名称及权限的局部更新、失败无部分写入。诊断回归覆盖排队与等待预算/上游/首输出的区分、用户及 Token 隔离、PAT 字段权限、队列满不阻塞请求、写库故障计数与 7 天清理。

前端覆盖 PAT、能力编辑器、目录/导出、日志时间线的 Vitest，以及 `tests/visual/pat-p2.spec.ts`、`tests/visual/catalog-p2.spec.ts`、`tests/visual/request-diagnostics.spec.ts`。按完整前端检查顺序执行，浏览器覆盖四种主题/视口以及登出后的迟到响应、只读权限、未知值和 Token 0 提示。SDK 的两种语言分别真实执行基础、媒体、语义套件，不把安装依赖算作执行成功。

空库冒烟仅启动隔离 SQLite 和回环模拟上游，验证 PAT、目录、导出、诊断、type 57 创建拒绝及三协议请求；三次生成对应三次上游发送和三笔唯一结算，最终资金预留归零。真实供应商、远端候选环境及生产数据库均不由此证明。

## 必须断言的边界

FIFO 公平、一次性释放、取消与授槽竞争、扩缩容、无限转有限、总等待上限、过期 revision、无新流量时扩容唤醒/禁用拒绝；跨 Token/协议和原任务续作共用用户池，渠道跨路由分组共用池；用户和渠道共享等待预算且不截短上游 context。

批量撤权须在首个策略查询前阻止所有受影响池继续授槽；慢策略读取须被剩余排队预算中断。验证排队中的请求可使用放宽后的 RPM/队列配置，扩容后完成 HTTP 请求，扫描期间回收的空闲池可重新获取。发送前 credential 配额拒绝须覆盖文本、Midjourney 和视频，断言只向允许的渠道发送、只产生一份账务记录且不增加供应商健康失败。

路由保持 priority/负载桶/position 硬边界，A 满 B 空时可用 B；只排一个渠道队列且不迁移。真实发送最多三次，退避前释放，A 的 Retry-After 不阻塞独立 B，接受状态未知或输出后不重放；额度预留/撤销遵守 burst 守恒，不制造供应商故障。配置省略/null/零值/CAS 和部分运行态不可用必须断言。

凭据与端点在获槽后和最终发送前复核，健康 epoch 则按每次尝试刷新；尾部候选超价或转换拒绝仍保留此前繁忙渠道的唯一排队机会。发送保护写入失败必须零发送；最终用量无法计价或持久化失败时按冻结估算结算，没有输出的失败请求释放，倍率、按次、表达式、显式零价、付费预估为零和 Realtime 累计均需验证。旧账务库与不匹配的证据密钥必须由 schema 5 守卫拒绝；篡改 request/真实 attempt/config/phase 或加密内容应阻止重放扣款。账户、Token 与资金事件保持同事务，日志失败不得重复扣款。

协议回归覆盖 Claude 全部内容块与连续工具索引、中断流输出估计及权威账务快照、Responses 缓存和模态明细与 reasoning summary、Gemini 同帧思考/正文分离、已完成图片在 EOF/取消后的单次结算。SSE 多行 data、换行变体、异常终止、大图片首帧及状态绑定失败不能仅靠基础文本成功路径证明；阶段超时须验证心跳不延长期限、关闭上游和资源释放。

性能比较须使用相同工作负载并分别记录准入延迟、首个有效输出时间、成功/超时率、额外发送比例、SQL/Redis 调用量、CPU 和内存。微基准不能代替完整 HTTP 路径压测；结论与仍缺的环境或基线必须随结果一起写入服务器档案。

受控 HTTP 测试只允许回环 Redis，使用初始为空的隔离数据库编号。测试结束通过 SCAN/UNLINK 清理本次数据库，不使用 FLUSHDB：

```powershell
$env:GATEWAY_RUN_LOAD='1'
$env:GATEWAY_LOAD_REDIS_ADDR='127.0.0.1:16379'
go test ./router -run '^TestGateway(SingleInstanceControlledLoad|ProviderFullChannelRoutesToIndependentChannel)$' -timeout 60s -v
powershell -NoProfile -File tests/admission-benchmark/compare.ps1 -RedisAddress 127.0.0.1:16379
```

HTTP 夹具明确把内存 SQLite 限为单连接，不把此结果当作多连接 SQLite 负载已通过。核心比较说明和固定旧提交见 [基准说明](../../tests/admission-benchmark/README.md)；只恢复旧核心到独立临时包，结束删除临时源码，不恢复旧生产执行路径。

## 工作区验收驱动

运行实例的 HTTP 验收由所属 RenAI 工作区 `scripts/server/` 下的驱动执行，上游均为回环模拟器，不调用真实供应商：

| 驱动                                 | 验收范围                                                         |
| ------------------------------------ | ---------------------------------------------------------------- |
| `test_ren2hub_billing.py`            | 每轮独立资源，重复验证真实 HTTP/SSE 与账务守恒                   |
| `test_ren2hub_ws.py`                 | Responses WebSocket 冒烟                                         |
| `test_ren2hub_compact.py`            | 原生 Compact HTTP 及其资金事件                                   |
| `test_ren2hub_plugin.py`             | 插件任务重启前后两阶段验收，重启由操作者完成                     |
| `test_ren2hub_preview_https.py`      | 公开预览站点的 HTTPS、Cookie 与会话撤销                          |
| `test-ren2hub-integration-matrix.py` | 在两组独立容器中验证 MySQL/PostgreSQL 三库合同，不接触常驻数据库 |

服务器拓扑、实际版本、执行报告和资源条件只查工作区[服务器档案](../../../docs/server/SERVER_PROFILE.md)。旧的负载报告与模拟器从 Git 历史读取，不作为后续变更的复验结果。

重跑须记录应用 SHA、工作区 SHA、镜像 OCI revision、本地 image ID、数据库版本、负载参数及资源配置；只有存在 registry manifest 时才记录对应 digest，本地 image ID 不等于 registry digest。不同资源条件（包括是否设置 Docker CPU/内存配额）的结果不能混算性能。模拟渠道清理、配置恢复、账务、最终路由证据和槽位守恒必须全部通过，不能仅按 HTTP 成功率验收。
