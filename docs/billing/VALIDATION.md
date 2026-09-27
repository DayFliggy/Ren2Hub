# 账务验证记录

本页保留 2026-09-20 本地实现与审计后的有效证据，源码范围为 `83bd7c23d` → `325c4ce40` → `a09a7c4c8`。2026-09-26 的双源重构见[双源定价验证](DUAL_SOURCE_PRICING_VALIDATION.md)，后续易支付变更另见[易支付验证](EPAY_VALIDATION.md)。这些历史结果不覆盖 `7ef87f9e3` 引入的渠道独立价格、结构化计价与账务格式 3；现行合同见[账务架构](IMPLEMENTATION.md)和[定价说明](PRICING.md)。文档整理不算重新执行应用测试，源码发布和服务器实际 HTTP/SSE、部署结果以所属工作区 `docs/server/SERVER_PROFILE.md` 为准。

## 本地验证范围

| 检查 | 已执行结果与限制 |
| --- | --- |
| Go 测试 | 根模块 `go test -p 4 -timeout 60s ./... -count 1` 通过；后续 Token 幂等与退款保护变更重跑 model/service/controller/router，最后 Token HTTP 指纹修正重跑 model/controller/router 和相关 vet，通过 |
| 三数据库 | 使用隔离 SQLite、MySQL 5.7.19、PostgreSQL 16.15，实际执行空库初始化、重复启动、拒绝旧格式、并发资金、任务终态、独立日志及故障重放；缺 DSN 跳过不算通过 |
| 进程退出 | 6 个 SQLite 文件库子进程场景通过：预留事务中、预留提交后、发送保护后、结算意图后、资金提交后、日志写入后确认前；恢复两次检查事件、投影和日志幂等 |
| ClickHouse 保护逻辑 | 隔离 SQL 存储模拟该分支并注入故障，验证未知写入不盲目重发；未验证真实 ClickHouse 驱动/服务 |
| RelayKit | 实现阶段在独立目录 `GOWORK=off` 完成全量测试和构建；后续未改模块的部分复用有效结果 |
| 静态与构建 | 根模块 vet、Go 构建、格式及 diff 检查通过；Vue 产物嵌入资源图检查通过 |
| Vue | 审计后 128 文件、618 项测试通过，后续 Token 表单 3 项定向通过；typecheck、lint、production build 通过 |
| 浏览器 | 审计后 22 项通过，价格标签调整后相关 4 项补测通过；覆盖桌面/移动、明暗、预留与对账错误状态，使用模拟 API |
| 初始价格目录 | 本地导出与检入 JSON 按结构一致：254 个价格、13 个待配置项目；没有联网更新价格 |

回归涉及混合按次与 Token 计价预留、附加费证据原子准备、未成功预留的投递状态、待投递账户删除保护、初始 Root 赠额事件、Token 欠费资料编辑与操作重放、已接受任务的矛盾拒绝信号、十进制价格及冻结时间。没有执行真实付费供应商或真实支付。

后续易支付验证包含隔离进程 HTTP 检查，结果见对应验证页。服务器账务复验使用根工作区 `scripts/server/test_ren2hub_billing.py`，版本、数据库与报告由服务器档案关联。

## 性能证据

审计后重跑相同账务负载：同机内存 SQLite、单写连接、8 个账户，每轮 160 请求，每种并发度 3 轮，完整执行预留、结算、统计与日志；Redis 两版均关闭。下表只保留最后一组中位数，早期调优结果由 Git 历史追溯。

| 指标 | 基线 1 并发 | 新版 1 并发 | 基线 8 并发 | 新版 8 并发 |
| --- | ---: | ---: | ---: | ---: |
| 请求/秒 | 976.86 | 898.52 | 837.95 | 790.37 |
| p95 毫秒 | 2.000 | 2.017 | 12.343 | 16.225 |
| SQL/请求 | 34 | 32 | 34 | 32 |
| Redis/请求 | 0 | 0 | 0 | 0 |
| 累计连接池等待毫秒/轮 | — | — | 725.409 | 1406.097 |

8 并发 p95 增加约 31%，不能表述为无性能退化。该结果只覆盖本地账务与投递，不含上游网络、价格求值、跨主机数据库、缓存命中或长期存储增长，不能推导 PostgreSQL 生产容量。基线为重构前独立 worktree，复现附件为[基线测试](fixtures/billing_performance_baseline_test.go.txt)，当前入口是 `model/billing_performance_test.go`。

```powershell
$env:BILLING_PERFORMANCE='1'
go test ./model -run '^TestBillingPerformance$' -count=3 -timeout=60s -v
```

其余复验根据[项目约定](../../AGENTS.md)选择定向测试、三库集成、静态与构建检查，每条后端测试保持 60 秒超时。工作区保留的账务原始日志归入 `tmp/evidence/billing/`，前端账务日志位于 `frontend/output/`；它们不是源码发布物。基准 worktree 可从对应提交与附件重建，无需常驻保留。日志是否仍保留不能代替重新执行所需验证。
