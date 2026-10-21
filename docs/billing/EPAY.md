# 原生易支付充值

易支付使用 NewAPI 原生 `controller/topup.go`、`model/topup.go` 和支付配置。充值金额复用 `getPayMoney`、最低充值额及预设折扣；额度换算复用原生 `getTopUpQuota`。金额展示类型为 TOKENS 时，客户端按原生单位提交。

`POST /api/user/amount` 计算金额，`POST /api/user/pay` 创建订单；`GET/POST /api/user/epay/notify` 接收原生签名通知。订单在 `top_ups` 中记录金额、方式、状态和交易号；成功回调与人工补单由原生订单锁、状态检查及数据库事务约束。人工补单入口为 `POST /api/user/topup/complete`，只允许已授权管理员操作。

不要携带旧充值 quote、fingerprint、账户版本、商户版本表或 schema 5 事件。新建与通知是否启用由原生支付配置和合规状态决定，不增设本地结算开关或追加账本。其他原生支付方式按各自控制器合同运行；GMPay 是单独的最小扩展，见[其合同](GMPAY.md)。

本轮新库重新配置支付商户，不导入旧订单或密钥。旧在途支付未完成时阻塞正式切换，不将旧回调导入新库。退款、对账和真实付款不由文档或候选测试自动授权。
