# 原生易支付充值

易支付使用 NewAPI 原生 `controller/topup.go`、`model/topup.go` 和支付配置。充值金额通过 `service.QuoteTopUp` 生成一次报价并确认实付、币种与加赠；额度换算复用原生 `getTopUpQuota`。金额展示类型为 TOKENS 时，客户端按原生单位提交。

`POST /api/user/amount` 计算金额，`POST /api/user/pay` 创建订单；`GET/POST /api/user/epay/notify` 接收原生签名通知。订单在 `top_ups` 中记录金额、方式、状态和交易号；成功回调与人工补单由原生订单锁、状态检查及数据库事务约束。人工补单入口为 `POST /api/user/topup/complete`，只允许已授权管理员操作。

不要恢复旧冻结报价、fingerprint、账户版本、商户版本表或 schema 5 事件；当前展示报价及下单确认按[充值加赠](TOPUP_BONUS.md)合同处理。新建与通知是否启用由原生支付配置和合规状态决定，不增设本地结算开关或追加账本。其他原生支付方式按各自控制器合同运行；GMPay 是单独的最小扩展，见[其合同](GMPAY.md)。

普通升级保留已有的商户配置和订单。退款、对账和真实付款不由文档或候选测试自动授权。

签名方式为 `payment_setting.epay_sign_type`，默认 `MD5`，使用 `EpayKey`；选择 `RSA` 时先保存商户私钥和平台公钥，再切换方式。商户私钥签署下单请求，平台公钥验证回调，不能混用；RSA 回调在验签通过后还须校验 `pid` 与配置的商户 ID 一致，缺失或不匹配时返回 `fail` 且不入账。RSA 启用时两份密钥必须能解析；option 列表不返回私钥。错误签名、错误金额、重复回调与人工补单竞争不得多次入账。
