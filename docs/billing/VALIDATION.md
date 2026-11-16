# 资金验证

使用独立 SQLite、MySQL、PostgreSQL 验证原生金额、钱包条件预扣、Token 限额和任务调整。所有 API 写入及假供应商、假支付调用只能进入已验证候选，不在正式实例制造充值或消费。

定向 Go 测试使用 `go test -timeout 60s`。GMPay 重点覆盖金额与 `/api/user/amount` 一致、CNY 分额、商户版本轮换、重复/并发回调与人工补单、错误签名、未知创建结果、数据库回滚和日志不泄密。三库测试 `TestGmpayAndTicketDatabaseContracts` 的外部 DSN 必须显式指向可销毁测试库；未提供 DSN 的 skip 不算三库通过。

旧 schema 5 守卫验收应证明新程序在 AutoMigrate 前拒绝旧表，且不改变旧数据；空库与普通升级、重复启动分别验证。新安装无模型默认价或内置表达式；工具单价、音频倍率和 Gemini 音频固定价保留上游。费用使用渠道倍率，充值加赠每笔入账只决定一次、只入账一次，不恢复分组倍率或旧事件账本。

候选统一入口为 `scripts/testing/run.py candidate`，完整阶段与普通升级的 `--previous-image` 参数见[本地测试方案](../testing/LOCAL_TESTING.md)。GMPay/工单合同只用本地可信 TLS 假网关，先验证候选身份，再走真实 API，并以只读 SQL 核对钱包差额、原生充值日志与订单状态。测试证据应记录源码/镜像、输入、工具版本、命令、退出码和跳过原因；源码检查、模拟测试与候选实际执行分别报告。
