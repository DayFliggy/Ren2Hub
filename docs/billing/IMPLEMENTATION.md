# 原生钱包与计费

Ren2Hub 使用 NewAPI 的计费实现，资金来源仅保留钱包。账户 `quota`、Token `remain_quota` / `unlimited_quota` 及 `used_quota` 沿用上游整数与 JSON number 合同。标准单位为 `500000 quota = 1 USD`；展示类型不会改变记账单位。

| 入口 | 职责 |
| --- | --- |
| `service/billing_session.go` | 单次请求预扣、实际结算和退款；维护请求内状态 |
| `service/funding_source.go` | 钱包预扣及差额调整；无订阅资金来源 |
| `model/quota_reserve.go` | 钱包余额充足条件下原子预扣 |
| `service/text_quota.go`、`service/tiered_settle.go` | 原生用量、倍率、表达式及工具费用 |
| `model/model_pricing_config.go`、`setting/ratio_setting/` | 模型编辑配置与上游默认值 |
| `service/task_billing.go`、`service/task_polling.go` | 任务计费与终态轮询 |
| `model/topup.go`、`model/gmpay.go` | 原生充值与 GMPay 最多一次入账 |

预扣结果按原生 BillingSession 保存于请求生命周期内，成功后按实际费用与预扣差额调整。符合上游信任条件的请求可以跳过实际预扣；不能把“每次请求必须持久化预留”当成本地合同。资金来源与 Token 的结算是分步操作，消费日志和统计也不是与余额同事务的第二账本。

本地没有旧 schema 5 资金事件、冻结报价、加密求值重放、独立准入和投递 outbox。日志是运营与用量记录，不能称为不可变钱包流水。进程中断、异步退款与日志失败的边界遵从原生实现；发布前须排空并按[恢复说明](RECOVERY.md)验证，不能宣称旧系统的崩溃恢复保证仍存在。

价格默认值、缺项继承、分组倍率、表达式及舍入均采用上游。不存在渠道售价系数，也不要求空模型价或空工具价。详见[定价](PRICING.md)。新库使用原生初始化；旧库在迁移前被拒绝，不能借 AutoMigrate 转换旧余额。
