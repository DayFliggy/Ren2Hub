# GMPay 加密货币充值

GMPay 作为原生钱包的支付入口，复用 NewAPI 充值金额、分组倍率、最低额、折扣和额度上限。法币金额为 CNY，按人民币分精确保存；不是旧 USD 报价产品。`pkg/gmpay/` 实现 HTTPS、HMAC-SHA256、严格参数与有界响应解析，不调用真实供应商完成测试。

| 接口 | 合同 |
| --- | --- |
| `GET /api/option/gmpay/config` | Root 浏览器会话；返回地址、商户ID、`has_secret`、资产及启用状态 |
| `POST /api/option/gmpay/save` | 同上；整体保存配置，空 `secret_key` 保留已存密钥 |
| `POST /api/option/gmpay/probe` | `{pay_address}`；只探测公开资产目录，不验证商户凭据或创建订单 |
| `POST /api/user/gmpay/pay` | 用户浏览器会话；`{amount,payment_method:"gmpay:usdt.tron"}` |
| `GET /api/user/gmpay/orders/:trade_no` | 本人浏览器会话；原生订单与公开支付快照 |
| `POST /api/user/gmpay/notify` | 公开 JSON 签名回调；成功为纯文本 `success` |

GET notify 已退役：epusdt 只对创建时声明 `payment_type=Epay` 的订单发 GET 查询串回调，本站创建订单不传该字段，回调固定为 POST JSON + HMAC。配置资产为 `{name,token,network,icon?,color?}`。`GET /api/user/topup/info` 增加 `enable_gmpay_topup` 和 `gmpay:token.network` 方式。下单成功返回 `{message:"success",url,data:{trade_no,payment_method,currency:"CNY",pay_amount,expected_quota,token,network}}`；与通用 success envelope 不同。

在远端创建前，同一事务保存原生订单、应到账整数额度、CNY 分额及不可变商户版本。仅向网关发送一次创建请求，网络或响应不明保存为 `unknown`。回调按订单绑定的商户版本验签，校验订单、交易号、token、精确金额和成功状态。回调不包含网络或法币字段，不能宣称独立确认了最终支付网络；快照记录请求选定的资产。

回调与 `POST /api/user/topup/complete` 共同使用原生钱包事务及订单状态检查，最多入账一次，不添加第二资金账本。回调金额与订单冻结的 CNY 应付值不一致时拒绝入账并记审计。回调处理失败一律回 `fail`：epusdt 只把 200 + `ok`/`success` 当作完成，其余按有限次数重试。

管理员补单只接受 `creating`、`ready`、`unknown` 快照；`failed`（网关明确拒绝创建）和 `settled` 拒绝。epusdt 没有商户查单接口，`unknown` 订单不做自动对账，只能等迟到的已验签回调，或由管理员在 GMPay 后台核实到账后补单。商户轮换不改变旧订单验签凭据；已完成订单不会被迟到创建响应重新打开。商户密钥、版本ID及内部凭据字段不进入公开响应或日志。

本轮所有商户重新配置。旧在途支付必须由旧应用处理到终态，再批准切换独立新库；不导入旧单或新造双回调入口。生产支付动作与真实费用仍按工作区授权流程执行。
