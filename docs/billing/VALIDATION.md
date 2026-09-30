# 账务复验

本页列出当前账务合同的复验入口和证明边界，现行规则见[账务架构](IMPLEMENTATION.md)和[定价说明](PRICING.md)。本页不保存执行结果；源码发布、部署版本和服务器验收结果只由所属工作区服务器档案 `docs/server/SERVER_PROFILE.md` 记录，旧的验证报告和性能数据从 Git 历史读取。

## 本地入口

后端测试遵循[项目约定](../../AGENTS.md)，每条命令保持 60 秒超时。schema 5 的数据库或进程测试须显式设置隔离测试用的稳定 `CRYPTO_SECRET`，同一测试数据库重开和子进程恢复使用同一个值；不得读取或打印生产密钥。缺少所需配置必须明确失败，不能把相关用例跳过当作通过。

| 检查 | 入口 | 边界 |
| --- | --- | --- |
| 价格与计算 | `go test -timeout 60s ./pkg/billing ./pkg/billingunits ./pkg/billingexpr -count=1` | 纯计算，不涉及数据库 |
| 外部来源 | `go test -timeout 60s ./pkg/pricesource -count=1`；`./service` 的 `TestModelPriceFetch` 定向测试 | 固定样本与模拟 HTTP 验证单位、零值、缓存/阶梯、重复模型、部分失败、体积与超时；不证明公网当时可用 |
| 模型配置 API | `./model` 的 `TestModelPricing` 与 `./controller` 的 `TestModelPricing`、`TestFetchUpstreamRatios` 定向测试 | 空默认、完整草稿保存、并发版本、精确数字、三模式转换、插件变体与只读模拟；不写生产配置 |
| 资金、恢复与投递 | `go test -timeout 60s ./model ./service ./controller ./router -count=1` | 默认只跑 SQLite |
| 三数据库 | 设置 `BILLING_TEST_MYSQL_DSN`、`BILLING_TEST_POSTGRES_DSN` 后运行 `TestBillingDatabaseIntegration`（`./service`）；设置 `GATEWAY_STARTUP_MYSQL_DSN`、`GATEWAY_STARTUP_POSTGRES_DSN` 后运行 `TestBillingAccountConcurrencyAcrossDatabases` 和启动测试（`./model`） | DSN 只能指向可丢弃的隔离库；缺 DSN 时子测试跳过，跳过不算通过 |
| 进程退出 | `go test -timeout 60s ./service -run '^TestBillingProcessExitRecovery$' -count=1` | 子进程在预留事务中、预留后、投递保护后、结算意图后、资金提交后、日志写入后退出，恢复执行两次检查幂等；只证明 SQLite 文件库 |
| ClickHouse 日志 | `go test -timeout 60s ./model -run '^TestBillingAmbiguousClickHouseWriteIsConfirmedBeforeRetry$' -count=1` | 用隔离 SQL 存储模拟该分支，证明未知写入不会盲目重发；不证明真实 ClickHouse 驱动或服务 |
| RelayKit | 在 `relaykit/` 以 `GOWORK=off` 运行 `go test -timeout 60s ./...` 和 `go build ./...` | 独立模块，改动 RelayKit 时才需要重跑 |
| 前端 | 在 `frontend/` 运行 `bun run test:run`、`bun run typecheck`、`bun run lint`、`bun run build`；界面检查用 `bun run test:visual` | 浏览器用例使用模拟 API，只证明界面与请求契约；账务页面说明见[前端账务说明](../../frontend/docs/BILLING.md) |

计价与证据验收须覆盖 NewAPI 倍率、按次、表达式各自的舍入规则，显式零与缺价、工具缺价与最长前缀零覆盖、渠道系数仅一次、动态 `param/header/time` 的真实输入及重放。修改请求、真实 attempt、配置、阶段、用量或密文应验证失败且不扣款；相同费用但证据不同也不得通过。公开 API、管理详情及日志不得返回输入明文或证据密文。前端获取五源、选择单源填草稿与明确保存分别验收，抓取和预览不能写配置。

## 性能

性能测试默认跳过，需要显式开启：

```powershell
$env:BILLING_PERFORMANCE='1'
go test ./model -run '^TestBillingPerformance$' -count=3 -timeout=60s -v
```

入口是 `model/billing_performance_test.go`：同机内存 SQLite、单写连接、8 个账户、每轮 160 请求，并发度 1 和 8，完整执行预留、结算、统计与日志，Redis 关闭。对比基线需要在重构前提交的独立 worktree 中运行同一负载，旧基线测试和旧数据从 Git 历史读取。结果只覆盖本地账务与投递，不含上游网络、价格求值、跨主机数据库、缓存命中和长期存储增长，不能推导 PostgreSQL 生产容量。

## 服务器复验

服务器账务复验使用根工作区 `scripts/server/test_ren2hub_billing.py`，执行版本、数据库状态与报告由服务器档案关联。本地测试不调用真实付费供应商或真实支付；供应商实际账单一致性和支付回调只能由对应的服务器或人工验收证明。整理文档不算重新执行测试，保留的日志也不能代替重新执行所需的验证。
