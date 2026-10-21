# GMPay 加密货币充值

GMPay 作为原生钱包的支付入口，复用 NewAPI 充值金额、最低额、折扣和额度上限。法币金额为 CNY，按人民币分精确保存；不是旧 USD 报价产品。`pkg/gmpay/` 实现 HTTPS、HMAC-SHA256、严格参数与有界响应解析，不调用真实供应商完成测试。

| 接口 | 合同 |
| --- | --- |
| `GET /api/option/gmpay/config` | Root 浏览器会话；返回地址、商户ID、`has_secret`、资产及启用状态 |
| `POST /api/option/gmpay/save` | 同上；整体保存配置，空 `secret_key` 保留已存密钥 |
| `POST /api/option/gmpay/probe` | `{pay_address}`；只探测公开资产目录，不验证商户凭据或创建订单 |
| `POST /api/user/gmpay/pay` | 用户浏览器会话；`{amount,payment_method:"gmpay:usdt.tron"}` |
| `GET /api/user/gmpay/orders/:trade_no` | 本人浏览器会话；原生订单与公开支付快照 |
| `POST /api/user/gmpay/notify` | 公开 JSON 签名回调；成功为纯文本 `success` |

GET notify 路由存在但当前协议解析器拒绝非 POST；不能将其当作有效验付接口。配置资产为 `{name,token,network,icon?,color?}`。`GET /api/user/topup/info` 增加 `enable_gmpay_topup` 和 `gmpay:token.network` 方式。下单成功返回 `{message:"success",url,data:{trade_no,payment_method,currency:"CNY",pay_amount,expected_quota,token,network}}`；与通用 success envelope 不同。

在远端创建前，同一事务保存原生订单、应到账整数额度、CNY 分额及不可变商户版本。仅向网关发送一次创建请求，网络或响应不明保存为 `unknown`。回调按订单绑定的商户版本验签，校验订单、交易号、token、精确金额和成功状态。回调不包含网络或法币字段，不能宣称独立确认了最终支付网络；快照记录请求选定的资产。

回调与 `POST /api/user/topup/complete` 共同使用原生钱包事务及订单状态检查，最多入账一次，不添加第二资金账本。商户轮换不改变旧订单验签凭据；已完成订单不会被迟到创建响应重新打开。商户密钥、版本ID及内部凭据字段不进入公开响应或日志。

本轮所有商户重新配置。旧在途支付必须由旧应用处理到终态，再批准切换独立新库；不导入旧单或新造双回调入口。生产支付动作与真实费用仍按工作区授权流程执行。
