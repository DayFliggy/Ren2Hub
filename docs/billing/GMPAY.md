# GMPay 加密货币充值

GMPay 作为原生钱包的支付入口，复用 NewAPI 充值金额、最低额、充值加赠和额度上限。法币金额为 CNY，按人民币分精确保存；不是旧 USD 报价产品。`pkg/gmpay/` 实现 HTTPS、HMAC-SHA256、严格参数与有界响应解析，不调用真实供应商完成测试。

| 接口                                   | 合同                                                            |
| -------------------------------------- | --------------------------------------------------------------- |
| `GET /api/option/gmpay/config`         | Root 浏览器会话；返回地址、商户ID、`has_secret`、资产及启用状态 |
| `POST /api/option/gmpay/save`          | 同上；整体保存配置，`secret_key` 去首尾空白，为空时保留已存密钥 |
| `POST /api/option/gmpay/probe`         | `{pay_address}`；只探测公开资产目录，不验证商户凭据或创建订单   |
| `POST /api/user/gmpay/pay`             | 用户浏览器会话；`{amount,payment_method:"gmpay:usdt.tron"}`     |
| `GET /api/user/gmpay/orders/:trade_no` | 本人浏览器会话；原生订单与公开支付快照                          |
| `POST /api/user/gmpay/notify`          | 公开 JSON 签名回调；成功为纯文本 `success`                      |

GET notify 已退役：epusdt 只对创建时声明 `payment_type=Epay` 的订单发 GET 查询串回调，本站创建订单不传该字段，回调固定为 POST JSON + HMAC。配置资产为 `{name,token,network,icon?,color?}`。`GET /api/user/topup/info` 增加 `enable_gmpay_topup` 和 `gmpay:token.network` 方式。下单成功返回 `{message:"success",url,data:{trade_no,payment_method,currency:"CNY",pay_amount,expected_quota,token,network}}`；与通用 success envelope 不同。

在远端创建前，同一事务保存原生订单、应到账整数额度、CNY 分额及不可变商户版本。仅向网关发送一次创建请求，网络或响应不明保存为 `unknown`。回调按订单绑定的商户版本验签，校验订单号、商户号、网关交易号、token、精确金额和成功状态。网关交易号 `trade_id` 始终必填，以 `(receipt_scope, gateway_trade_no)` 唯一索引保证一张网关收据只结算一张本地订单。链上交易号 `block_transaction_id` 可以为空：付款人在托管收银台改换币种或网络时，epusdt 为子订单收款，再用父订单字段发回调，其中链上交易号为空，`token`、`actual_amount`、`receive_address` 是下单时选定的那一组，不是最终到账的链上证据。首次结算记录回调的币种数量、收款地址和链上交易号（可能为空）；之后的回调必须与之完全一致，空与非空互换也被拒绝。本站验证的是网关签名、冻结的 CNY 金额和收据一致性，链上到账由网关负责。回调不包含网络或法币字段，不能宣称独立确认了最终支付网络；快照记录请求选定的资产。公开资产目录探测与解析共用 16 KiB 上限。

回调与 `POST /api/user/topup/complete` 共同使用原生钱包事务及订单状态检查，最多入账一次，不添加第二资金账本。回调金额与订单冻结的 CNY 应付值不一致时拒绝入账，控制器记录分类日志，不宣称另有统一的拒绝业务审计。请求格式解析错误返回 400 `fail`；验签或结算失败返回 200 `fail`；成功返回 200 `success`。epusdt 只把 200 + `ok`/`success` 当作完成，其余按有限次数重试。

网关须包含 epusdt `4f9cd7b` 和 `e2ae677`。前者按订单归属释放预占。旧版在过期清理、管理员关单或迟到结算时，可能释放另一张同额订单的预占，导致一笔付款记到其他订单上。网关发出的签名回调可以完全符合该订单的字段，本站无法据此发现错记。后者修正仅做人工核验的 EVM 节点的健康检查，避免可用节点被误标为 down。

创建订单返回的 `expiration_time` 是当时估计的有效期。本站不会据此在本地将订单置为过期，迟到的签名回调仍按原订单条件核验。即使网关包含上述修复，预占过期后，同一收款地址、币种和金额也可能分配给新订单。付款人超过原订单窗口才付款时，网关可能将款项匹配到新订单。

下单入口检查启用状态及支付合规确认；既有订单的回调不重新检查这两个开关。下单后停用 GMPay 或撤销合规确认，仍可按原订单的商户版本验签及冻结金额完成入账，保持已支付订单的结算路径。

管理员补单只接受 `creating`、`ready`、`unknown` 快照；`failed`（网关明确拒绝创建）和 `settled` 拒绝。epusdt 没有商户查单接口，`unknown` 订单不做自动对账，可以等待迟到的签名回调。网关满足上述版本要求时，管理员先在网关后台核对到账和订单归属：已到账待确认的订单使用 `mark-paid`，已确认但回调未送达的订单使用 `resend-callback`。优先让本站按签名回调入账，仅在网关无法回调时使用本站补单。无论网关回调与本站补单谁先完成，同一订单都只入账一次。商户轮换不改变旧订单验签凭据；已完成订单不会被迟到创建响应重新打开。商户密钥、版本ID及内部凭据字段不进入公开响应或日志。

生产支付动作与真实费用按所属工作区的授权流程执行。
