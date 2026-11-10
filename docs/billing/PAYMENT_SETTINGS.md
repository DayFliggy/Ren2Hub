# 钱包支付设置

钱包充值保留 NewAPI 原生已启用方式（易支付、Stripe、Creem、Waffo、Waffo Pancake 等）和 GMPay 扩展。UI 根据 `/api/user/topup/info` 显示真实可用入口；支付配置还受原生合规确认与服务端校验限制。站内订阅套餐与订阅资金来源已移除。

原生 `Price`、`MinTopUp` 和展示类型决定原价与提交单位；充值分组倍率和上游 `payment_setting.amount_discount` 已删除。易支付、GMPay、Waffo、Waffo Pancake 的实付再按[充值折扣](TOPUP_DISCOUNT.md)打折一次，Stripe、Creem 不打折。前端先调用对应原生 amount API，按返回的 `quote` 展示原价、优惠和实付，下单时带回确认的金额与币种，不把模型渠道倍率用于充值。支付配置默认值以 `setting/operation_setting/` 与各支付设置源码为准，和模型定价是不同配置；模型价格只来自管理员保存，工具及音频保留上游默认值。

GMPay 使用独立 `GmpayConfigSecret` 配置项，通过专用 Root 会话接口保存；内容为 `pay_address`、`merchant_id`、`secret_key`、`assets` 和 `enabled`。通用 option 列表不得暴露其密钥，专用读取只给 `has_secret`。停用阻止新下单，已有单仍按绑定商户版本接收合法通知。

原生金额计算与 GMPay CNY 支付合同分别见[账务 API](API.md)和[GMPay](GMPAY.md)。首次采用新核心时，用户、渠道、密钥、价格和商户全部重新配置，不从旧库导入；实际外部支付与回调迁移须先满足旧订单归零门槛。
