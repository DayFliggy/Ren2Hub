# 钱包支付设置

钱包充值保留 NewAPI 原生已启用方式（易支付、Stripe、Creem、Waffo、Waffo Pancake 等）和 GMPay 扩展。UI 根据 `/api/user/topup/info` 显示真实可用入口；支付配置还受原生合规确认与服务端校验限制。站内订阅套餐与订阅资金来源已移除。

原生 `Price`、`MinTopUp` 和展示类型决定原价与提交单位；充值分组倍率和上游 `payment_setting.amount_discount` 已删除。实付不受[充值加赠](TOPUP_BONUS.md)影响。易支付、GMPay、Waffo、Waffo Pancake 的前端先调用对应原生 amount API，按返回的 `quote` 展示实付、本金与加赠，下单时带回确认的金额、币种与加赠；Stripe、Creem 先取加赠预览，下单时带回确认的加赠。不把模型渠道倍率用于充值。支付配置默认值以 `setting/operation_setting/` 与各支付设置源码为准，和模型定价是不同配置；模型价格只来自管理员保存，工具及音频保留上游默认值。

GMPay 使用独立 `GmpayConfigSecret` 配置项，通过专用 Root 会话接口保存；内容为 `pay_address`、`merchant_id`、`secret_key`、`assets` 和 `enabled`。通用 option 列表不得暴露其密钥，专用读取只给 `has_secret`。停用阻止新下单，已有单仍按绑定商户版本接收合法通知。

钱包“备用支付”列出站外付款或购买兑换码的页面，在新标签页打开。来源是原生 `TopUpLink`（不带名称，显示为“外部充值”，排第一；不是 http/https 链接时不列出）和本地 `backup_payment_setting.links`（JSON 数组，每项 `name` 最多 32 字、`url` 为 http/https、可选 `icon` 最多 256 字节，最多 10 项）。保存和从数据库加载用同一套校验：保存时不合规直接拒绝；已存的值不合规（包括不是合法 JSON）时记录错误日志并保留上一份有效列表。每次保存整体替换列表，省略的 `icon` 即为空。`/api/user/topup/info` 的 `backup_payment_links` 返回合并后的列表，支付合规未确认时为空。支付方式和备用支付的图标规则相同：`icon` 是 https 图片地址时显示图片，是内置标识（如 alipay、wechat、telegram、tether、tron）时用内置图标；支付方式未配置时按类型或 GMPay 网络选图标，没有可靠标志的网络显示首字母徽标。

易支付支持 MD5/RSA：RSA 商户私钥和平台公钥先分别校验保存，切换签名方式时校验完整配置，私钥不回显。

原生金额计算与 GMPay CNY 支付合同分别见[账务 API](API.md)和[GMPay](GMPAY.md)。
