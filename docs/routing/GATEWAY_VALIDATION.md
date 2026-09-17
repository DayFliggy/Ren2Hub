# 单实例网关验证步骤

更新：2026-09-17。测试仅连接明确隔离的数据库、Redis 和模拟上游；本地夹具与服务器测试栈的数据各自独立，不使用生产数据或付费模型。此前多版本结果已保留于 [历史验证归档](archive/GATEWAY_VALIDATION_PRE_SINGLE_INSTANCE_2026_09_16.md)，已执行结果见 [验收记录](SINGLE_INSTANCE_ACCEPTANCE.md)。

参考项目评审发现的十项网关缺陷、修复边界和复验结果见 [2026-09-17 修复验收](GATEWAY_REVIEW_FIXES_2026_09_17.md)。该记录单独说明排队凭据复核、表达式计费待对账、流式阶段期限及协议回归。

后续六项复评修复、全部计价模式的发送保护、异步任务接受后的退款边界，以及三个参考项目的设计取舍见 [2026-09-18 修复验收](GATEWAY_REVIEW_FIXES_2026_09_18.md)。

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
go vet -p 4 ./...
go build ./...
```

涉及 RelayKit 或公共契约时，进入 `relaykit` 设置 `GOWORK=off`，单独运行 `go test ./... -timeout 60s` 和 `go build ./...`，随后恢复环境。最终全模块测试可使用 `go test -p 4 ./... -timeout 60s`。同代码和环境已通过的包不必因其他包结束而重复运行。

## 本地依赖

2026-09-16 本地验证的数据位于 `.local-tests/single-instance/{mysql,postgres,redis}`，相关进程已在验证后关闭，数据和日志保留。记录版本为 MySQL 5.7.19（回环 13306）、PostgreSQL 16.4（15432）、Redis 5.0.14.1（16379）；复跑前核对隔离服务和连接，不假定旧端口仍在监听。测试创建临时数据库/表并按夹具清理；这些版本的结果不能冒充其他服务端版本实测。

| 检查 | 环境变量 |
| --- | --- |
| 配置与启动迁移三库 | `GATEWAY_STARTUP_MYSQL_DSN`、`GATEWAY_STARTUP_POSTGRES_DSN` |
| 账务三库 | `BILLING_TEST_MYSQL_DSN`、`BILLING_TEST_POSTGRES_DSN` |
| 跨进程成功频率预留 | `GATEWAY_MULTIPROCESS_MYSQL_DSN`、`GATEWAY_MULTIPROCESS_REDIS_ADDR` |
| 单实例及顺序重启 | `GATEWAY_APP_BINARY`、`GATEWAY_APP_MYSQL_DSN`、`GATEWAY_APP_REDIS_ADDR`；运行 `TestGatewayApplicationSingleInstanceRestart` |
| 官方 SDK | `GATEWAY_SDK_PYTHON`、`GATEWAY_SDK_NODE`，必要时 `PYTHONPATH` 指向隔离依赖 |

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

## 必须断言的边界

FIFO 公平、一次性释放、取消与授槽竞争、扩缩容、无限转有限、总等待上限、过期 revision、无新流量时扩容唤醒/禁用拒绝；跨 Token/协议和原任务续作共用用户池，渠道跨路由分组共用池；用户和渠道共享等待预算且不截短上游 context。

批量撤权须在首个策略查询前阻止所有受影响池继续授槽；慢策略读取须被剩余排队预算中断。验证排队中的请求可使用放宽后的 RPM/队列配置，扩容后完成 HTTP 请求，扫描期间回收的空闲池可重新获取。发送前 credential 配额拒绝须覆盖文本、Midjourney 和视频，断言只向允许的渠道发送、只产生一份账务记录且不增加供应商健康失败。

路由保持 priority/负载桶/position 硬边界，A 满 B 空时可用 B；只排一个渠道队列且不迁移。真实发送最多三次，退避前释放，A 的 Retry-After 不阻塞独立 B，接受状态未知或输出后不重放；额度预留/撤销遵守 burst 守恒，不制造供应商故障。配置省略/null/零值/CAS 和部分运行态不可用必须断言。

性能比较须使用相同工作负载并分别记录准入延迟、首个有效输出时间、成功/超时率、额外发送比例、SQL/Redis 调用量、CPU 和内存。微基准不能代替完整 HTTP 路径压测；本轮结论与仍缺的环境或基线均在验收记录中明确列出。

受控 HTTP 测试只允许回环 Redis，使用初始为空的隔离数据库编号。测试结束通过 SCAN/UNLINK 清理本次数据库，不使用 FLUSHDB：

```powershell
$env:GATEWAY_RUN_LOAD='1'
$env:GATEWAY_LOAD_REDIS_ADDR='127.0.0.1:16379'
go test ./router -run '^TestGateway(SingleInstanceControlledLoad|ProviderFullChannelRoutesToIndependentChannel)$' -timeout 60s -v
powershell -NoProfile -File tests/admission-benchmark/compare.ps1 -RedisAddress 127.0.0.1:16379
```

HTTP 夹具明确把内存 SQLite 限为单连接，不把此结果当作多连接 SQLite 负载已通过。核心比较说明和固定旧提交见 [基准说明](../../tests/admission-benchmark/README.md)；只恢复旧核心到独立临时包，结束删除临时源码，不恢复旧生产执行路径。

## 服务器 PostgreSQL 验证

RenAI 工作区提供 `scripts/server/build-ren2hub-test.sh`、`prepare-ren2hub-test.sh` 和 `run-ren2hub-test.sh`。它们使用已发布 Git 提交、独立 PostgreSQL/Redis 数据目录和内部 Docker 网络，验证前端烟测，并运行 11 个模拟渠道场景；执行入口和精确命令见工作区 `routing-simulator/ADMISSION_LAB.md`，最新无配额结果见 `docs/server/REN2HUB_GATEWAY_FIXES_LOAD_20260917.md`。

重跑须记录应用 SHA、工作区 SHA、镜像 OCI revision、本地 image ID、数据库版本、负载参数及资源配置；只有存在 registry manifest 时才记录对应 digest，本地 image ID 不等于 registry digest。现行部署不设置 Docker CPU/内存配额；旧 `REN2HUB_POSTGRES_TEST_20260917.md` 保留受限环境证据，不与无配额复测混算性能。模拟渠道清理、配置恢复、账务、最终路由证据和槽位守恒必须全部通过，不能仅按 HTTP 成功率验收。
