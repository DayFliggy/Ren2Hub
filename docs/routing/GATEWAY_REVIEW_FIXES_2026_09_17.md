# 网关评审修复验收（2026-09-17）

行为基线：`f99a1bd66594476a90fb470496380ec05a2ba232`；集成基于随后仅整理文档的 `91a16c128`。本次修复参考项目评审确认的十项问题。测试使用隔离数据库、模拟供应商和固定版本官方 SDK，不请求真实模型或使用生产凭据。源代码修改不等于应用已部署。

## 缺陷与回归

| 编号 | 修复后的行为 | 主要回归证据 |
| --- | --- | --- |
| F1 | Claude 非流式响应按顺序保留全部文本、思考和工具块 | `relaykit/relayconvert/internal/claude_messages/to_oai_chat_resp_test.go` |
| F2 | Claude 中断流的本地 token 估计同步到计费快照，保留供应商权威快照并标注估计来源 | `TestClaudeStreamFinalBillingMatchesDeliveredUsage`；AWS 中断流回归 |
| F3 | Responses 已完成图片在 EOF、读取失败或客户端取消时仍结算一次；部分图片和明确失败终态不误计 | `TestResponsesImageAccountingOnStreamTermination`、`TestResponsesPartialImageEOFIsNotBillable`、`TestResponsesDeliveredImageSurvivesClientCancellation` |
| F4 | 渠道获槽后及真正发送前重新检查数据库中的端点身份、启用状态和所选凭据；过时身份返回 503 | `TestQueuedRequestRejectsChangedChannelIdentity`、`TestFinalSendRechecksCredentialAfterQualification` |
| F5 | 实际用量触发表达式错误时保留预扣，持久化待对账证据，不将估算值当作结算金额 | `TestFailedExpressionRetainsReservationForReconciliation`；三库 `pricing_reconciliation` |
| F6 | 响应头、非流式完成、首有效输出均有有限期限；心跳和元数据不能延长首输出期限，退出关闭上游 | `service/http_client_deadline_test.go`、`relay/helper/stream_deadline_test.go`、`TestGatewayPhaseDeadlineReleasesAdmissionAndReservation` |
| F7 | Responses JSON/SSE 保留缓存、图像、音频、推理 token 明细及原有计费语义 | `TestResponsesDetailedUsageJSONAndStream`；RelayKit `usage_regression_test.go` |
| F8 | Claude 内容块索引通过每流状态转换为连续的 Chat 工具索引；参数分片归属正确 | `TestClaudeChatStreamReconstructsToolsAcrossTextBlocks`、`TestClaudeRegisteredStreamUsesDenseToolIndexes` |
| F9 | 经 JSON、语义及输出验证的图片帧可以超过前导缓冲大小；非法帧和状态绑定失败不泄露前导内容 | `TestResponsesLargeImageFirstEvent`、`TestValidatedLargeSemanticFrameCommitsBeforeWrite`、`TestRejectedSemanticWriteDoesNotExposePrelude` |
| F10 | SSE 按事件空行组装多行 `data:`，支持 CRLF/LF/CR、注释、事件名和 BOM，整事件受大小上限约束 | `relay/helper/sse_decoder_test.go`、`stream_scanner_test.go`；NDJSON 独立回归 |

旧夹具中省略 SSE 事件空行的部分已改为正确帧分隔；AWS 的中断流断言改为验证实际估计输出与计费快照一致。扩大竞争检测发现并修正了并行测试写 Gin 全局模式、视频轮询夹具读取正在更新的任务对象两个测试问题；后者使用启动工作协程前保存的任务 ID。这些变更没有放宽断言或跳过失败用例。

## 运行行为

三个新环境变量均以秒为单位，默认 300：`RELAY_RESPONSE_HEADER_TIMEOUT`、`RELAY_NON_STREAM_TIMEOUT`、`STREAMING_FIRST_OUTPUT_TIMEOUT`。非正数和溢出值回到有限默认值。普通客户端、代理、AWS HTTP 调用和受保护下载采用相应期限。流式请求收到 JSON 或错误响应时，也受非流式完成期限约束。

`STREAMING_TIMEOUT` 控制有效输出后的上游空闲期限。`RELAY_TIMEOUT` 的正值继续作为 HTTP 总期限；默认 0 仅表示不设置额外总期限。正常输出的长流不受默认非流式完成期限截断。SSE 为兼容现有供应商，会在正常 EOF 时处理最后一个未以空行结束的完整数据事件；扫描错误或取消不会补交未完成事件。

F4 拒绝旧请求中的身份，不在队列中局部替换 Authorization。重新请求会按当前配置重新构造 URL、请求头、转换器和状态归属。用例覆盖 Key 替换、端点修改、所选 Key 停用、可信凭据刷新以及资格核验后再次更新；拒绝时没有发送或新账务会话，Permit 已释放。

F5 使用现有恢复记录的 `reconciliation_required` 状态，无新增数据库列。记录保存冻结表达式、倍率、模型、渠道和规范化 token 数值；不保存请求正文、请求头或可能含请求值的原始表达式错误。钱包和 Token 保留已预扣金额，不生成成功消费日志。待对账记录计入积压，但不占自动恢复批次；重复结算、退款和恢复不会自动决定最终金额。修正价格配置并不会自动解锁旧记录，必须依据该记录完成明确对账。

表达式请求在发送前持久化 `pricing_pending`，后续 Realtime 累计快照使用 `pricing_collect` 保留保护。保护写入失败时禁止发送；若实际用量的待对账记录写入失败，恢复任务仍不能推断请求未使用并自动退款。已确认的正常结算或明确失败的退款按原有路径完成。进程在发送边界崩溃、或数据库故障导致最终用量未持久化时，保留可用证据供对账；不能声称此时已保存缺失的用量数据。

## 验证记录

验证环境为 Windows amd64、Go 1.25.1。最终主模块全量测试、受影响包竞争检测、`go vet`、`go build` 及 `git diff --check` 均通过。竞争检测覆盖 `common`、`controller`、`model`、`service`、`relay/...` 与 `router`；路由测试整体在 race 下超过 60 秒，因此拆为多组，未放宽超时。初轮发现的夹具竞争已修复，失败包完整复验通过。

独立 RelayKit 已通过全模块 `-race`、`go vet` 和 `GOWORK=off go build`。15 套官方 SDK 验收均已通过、无 SKIP：基础文本、媒体、音频、Realtime、云基础、云工具与中断、云并行工具、旧协议、兼容渠道、特殊渠道、Codex、Vertex 开源模型、自定义渠道、Python 语义和 TypeScript 语义。使用仓库锁定版本的 SDK，Python 3.12.14；每套单独执行，超时均为 60 秒。

账务集成已在 SQLite、MySQL 5.7.19、PostgreSQL 16.15 各执行 25 个场景，共 75 个，全部通过，包括重复终结、各阶段故障恢复、取消、并发交付、退款与零预扣，以及新增的待对账、发送前保护、写入失败、Realtime 累计和恢复队列公平性。MySQL 与 PostgreSQL 使用本机回环的专用临时实例；夹具创建并清理独立测试数据库，两个服务均已关闭。MySQL 测试数据库显式使用 `utf8mb4`，避免临时服务器默认字符集影响既有中文日志。

本地日志保存在工作区忽略目录 `tmp/`：`gateway-fixes-root-final.log`、`gateway-fixes-relaykit-final.log`、`gateway-fixes-billing-race-final.log`、`gateway-fixes-race-fixtures.log`、`gateway-fixes-router-race-0.log`、`gateway-fixes-router-race-from-*.log`、`gateway-fixes-database-final.log` 及 `gateway-fixes-TestGatewayOfficial*.log`。初次失败日志不作为最终通过证据。

复验命令（在主模块运行；SDK 环境与逐套命令见 [说明](../../tests/gateway-sdk/README.md)）：

```powershell
go test -p 4 ./... -timeout 60s -count=1
go test -race -p 4 ./common ./controller ./model ./service ./relay/... -timeout 60s -count=1
# 设置 BILLING_TEST_MYSQL_DSN 与 BILLING_TEST_POSTGRES_DSN 为隔离数据库后执行，不能将 SKIP 算作通过
go test ./service -run '^TestBillingDatabaseIntegration$' -timeout 60s -count=1 -v
go vet -p 4 ./...
go build -p 4 ./...
```

路由竞争检测按枚举名称分组，避免包总运行时间触发 60 秒上限：

```powershell
$testNames = @(go test ./router -list '^Test' | Where-Object { $_ -match '^Test' })
if ($LASTEXITCODE -ne 0) { throw 'cannot enumerate router tests' }
for ($start = 0; $start -lt $testNames.Count; $start += 10) {
  $last = [Math]::Min($start + 9, $testNames.Count - 1)
  $pattern = '^(' + ($testNames[$start..$last] -join '|') + ')$'
  go test -race -p 4 ./router -run $pattern -timeout 60s -count=1
  if ($LASTEXITCODE -ne 0) { throw "router tests $start..$last failed" }
}
```

独立 RelayKit：在 `relaykit` 目录设置 `GOWORK=off`，执行 `go test -race -p 4 ./... -timeout 60s -count=1`、`go vet -p 4 ./...`、`go build -p 4 ./...`，结束后恢复环境。

全量命令中依赖显式环境变量的用例仍有门控，跳过不能算通过；本轮已另外启用三库账务和全部官方 SDK。未重新执行服务器容器、真实 Redis 压测及整进程重启验收。本轮没有修改前端或数据库结构，不把既有前端截图、历史负载报告或生产环境结果记为本次实测。修复不新增自动对账操作入口。
