# GMPay 加密货币充值

本契约对应 2026-09-23 本地接入实现，以 `e164fc64d` 为基线。网关协议依据参考项目 `GMwalletApp/epusdt` 的 `aed4a970` 源码。GMPay 使用原生 HMAC-SHA256 接口，与现有 EPay 的 MD5 表单支付分别配置、分别下单，共同通过钱包账务事件入账。

## 接口和配置

| 接口 | 权限与用途 |
| --- | --- |
| GET `/api/next/wallet/gmpay/config` | 登录用户；返回可用资产、最低额度和报价设置，不返回商户凭据 |
| POST `/api/next/wallet/gmpay/quote` | 登录用户；按 USD 或 TOKENS 输入生成报价 |
| POST `/api/next/wallet/gmpay/topup` | 登录用户；重新计算并核对报价指纹，创建订单和返回收银台链接 |
| POST `/api/option/gmpay/probe` | Root；读取网关公开资产配置，不创建订单，也不证明密钥正确 |
| POST `/api/option/gmpay/save` | Root；原子保存配置，空密钥保留已有密钥 |
| POST `/api/user/gmpay/notify` | 匿名但必须验签；接收网关 JSON 通知 |
| GET `/api/next/admin/orders/:order_no` | 管理员订单详情；GMPay 订单附带创建状态与最终到账证据 |

旧式登录用户接口 `/api/user/gmpay/amount` 和 `/api/user/gmpay/pay` 使用同一报价与创建服务。已注册的 GET 通知入口不接受支付通知；协议解析器只接受 POST JSON。

管理配置字段为 `pay_address`、`merchant_id`、`secret_key`、`assets`、`price`、`min_topup`、`group_ratio`。网关基础地址要求 HTTPS；从基础地址追加 `/payments/gmpay/v1/config` 和 `/payments/gmpay/v1/order/create-transaction`。保存到 `GmpayPayAddress`、`GmpayId`、`GmpayKey`、`GmpayAssets`、`GmpayPrice`、`GmpayMinTopUp`、`GmpayGroupRatio`，读取设置时隐藏密钥。

资产配置示例：

```json
[{"name":"USDT / TRON","token":"usdt","network":"tron","min_topup":"1"}]
```

`group_ratio` 是 JSON 对象字符串，例如 `{"default":1}`。资产列表为空时停止创建新 GMPay 订单；EPay 和已有 GMPay 订单不受影响。`payment_setting.gmpay_settlement_enabled=false` 是独立的回调结算暂停开关，管理员核查后的补单仍遵循原权限。

网关必须实际启用对应链、币种、收款钱包和汇率。探测只读取公开配置；能探测到资产不等于商户密钥或真实收款已验收。回调 Origin 取 `CustomCallbackAddress`，为空取 `ServerAddress`，再追加 `/api/user/gmpay/notify`；网关会要求可访问的公网回调地址。

## 金额与订单

GMPay 报价固定为 USD，默认 `GmpayPrice=1`。本地 USD 购买数量乘单价、用户组倍率和预设折扣后，统一四舍五入至美分；支付金额必须大于 0.01 USD。TOKENS 输入直接表示 quota，固定 500000 quota/USD，报价下限按 USD 购买数量换算。币种不能与 EPay 的 CNY 报价混用。

请求携带 `amount`、`amount_unit`、`payment_method`、`quote_fingerprint`。方式标识支持 `gmpay:usdt.tron` 和裸 `usdt.tron`，报价返回裸标识。指纹覆盖用户、分组、支付资产、金额、费率、商户及回调地址；下单时变化返回 `QUOTE_CHANGED`，前端要求重新确认。

先在事务中保存充值订单、`gmpay_orders` 和不可变的 `gmpay_merchant_versions`，再发起一次网关请求。网络调用不进入资金事务。请求采用 form 编码保留签名中的十进制文本；接收数字 JSON 时按网关 float64 规则规范化，但法币结算仍精确比较整数美分。

创建响应必须匹配本地订单号、法币金额、USD 币种、token 和有效支付状态，并包含有效交易号、加密货币数量、收款地址、到期时间及 HTTPS 收银台地址。网关交易号先绑定本地订单，随后才返回收银台链接。不会自动重试非幂等创建请求，也不会转投 EPay。

| 快照状态 | 含义 |
| --- | --- |
| `creating` | 本地快照已创建，尚未记录网关结果 |
| `ready` | 网关创建响应已校验并保存 |
| `failed` | 网关明确拒绝，例如金额或参数不合法 |
| `unknown` | 超时、断连、系统错误、订单已存在或响应无法确认；保留本地订单待核查 |
| `settled` | 有效通知已入账或管理员已补单 |

快照状态与充值订单的 pending/success 分开保存。失败或未知创建结果不会凭空增加余额；未收到有效通知且未补单时，本地充值订单保持 pending。提前到达的成功通知和管理员补单不会被晚到的创建结果重新打开；晚到的明确失败也不能覆盖已结算状态。

## 回调、证据与幂等

回调限制为 16 KiB、32 个字段，拒绝重复 JSON 字段、非法类型和金额。签名为 HMAC-SHA256，使用订单绑定的旧商户版本验签，并核对 PID、订单号、网关交易号、法币金额、token 和 `status=2`。仅完成结算或确认一致的重复通知时返回 HTTP 200、`success`；其他状态和失败返回 `fail`。

订单、凭据、最终证据、余额事件和返利在同一事务内提交。`actual_amount` 不决定入账 quota；成功支付只入账冻结的 `expected_quota`。当前唯一凭据为 `(receipt_scope="gmpay", gateway_trade_no)`，限制一笔 GMPay 网关交易只能绑定一个本地订单。商户账户标识和版本另外保存并核对。

创建时证据保存在 `gateway_amount`、`gateway_crypto_amount`、`receive_address`、`expiration_time`。最终到账证据另存 `settled_crypto_amount`、`settled_receive_address`、`settled_transaction_id`，不覆盖创建信息。重复通知必须匹配最终证据；管理员补单后首个有效通知可以补齐证据，但不再次入账。事务失败不留下半份凭据或余额。

**当前网关回调不含 network 或 currency，收银台允许切换网络。**本地 `network` 只能表示最初请求网络，不能证明最终到账网络。实现核对本地冻结的 USD 金额和通知 token；同 token 换网络时，不机械比较创建时与到账时的数量和地址。换成另一 token 的通知拒绝自动入账，交由管理员核查。本系统不声称能独立检测网关未报告的链、汇率或少付情况。

epusdt 当前用商户数据库里的现密钥签发旧单通知；Ren2Hub 使用订单冻结密钥验签。因此轮换应新建 PID 接新单，保留旧 PID/密钥处理旧单，不能假定原地替换密钥自动兼容。epusdt 示例 `order_notice_max_retry=0` 表示首次失败后不重试，正式接入需配置合适的重试次数。

异常恢复仍由外部网关重试有效通知或管理员核查后使用现有补单入口完成；不增加用户验付、主动查单或自动退款。

## 本地验证与未完成验收

2026-09-23 已验证：协议与固定 HMAC 样例、创建响应绑定、未知结果保留、报价变化、资金幂等、提前回调、晚到结果、暂停与恢复结算、手工补单、最终证据一致性、事务回滚、SQLite 增量迁移与并发结算。后端单测统一使用 `-timeout 60s`。

`controller/gmpay_integration_test.go` 使用隔离 SQLite 和真实本地 HTTP/TLS 服务，覆盖配置保存与密钥隐藏、EPay CNY/GMPay USD 独立报价、创建订单、停止新单后旧单通知、数字 JSON 回调、重复通知及管理员证据读取。该测试使用模拟网关，不涉及区块链或真实支付。

前端验证包括 GMPay 配置保存、倍率校验、空资产停新单、资产分流、最低额度、报价变化和收银台跳转。完整 Vitest、类型、lint、格式、生产构建，以及 EPay/GMPay 桌面和手机、浅色和深色浏览器用例已执行。管理员对账详情另有定向用例。

数据库 CI 已接入 `TestGmpayMigrationsAndConcurrentSettlementAcrossDatabases`，从现有 MySQL/PostgreSQL 服务传入 `GATEWAY_STARTUP_MYSQL_DSN` / `GATEWAY_STARTUP_POSTGRES_DSN`。2026-09-23 在服务器临时隔离容器中完成 SQLite、MySQL 8.4、PostgreSQL 15 的迁移与并发结算实测，三项全部通过；测试结束后删除本次容器和网络，未连接已有业务数据库。

上线前仍需完成：

- 独立 epusdt 服务的数据卷、数据库、HTTPS 收银台和备份恢复验证；当前参考版本使用 SQLite 运行时存储，不应照旧计划假定必须部署 Redis。工作区 `deploy/epusdt/` 已提供独立部署模板，运行环境验收另行记录。
- 测试环境使用服务器 IP 和可信 HTTPS 证书；PID、收款钱包和启用资产通过测试环境安全配置注入，敏感值不写入仓库。
- 从实际 epusdt 环境验证公网回调可达、通知失败重试、旧 PID 存量订单和密钥轮换流程。
- 发布经过上述验证的 Ren2Hub 与网关版本，完成隔离的小额真实支付验收；本轮没有执行真实支付或生产部署。

已有 EPay 约定见 [EPAY.md](EPAY.md)，共同账务事件约定见 [IMPLEMENTATION.md](IMPLEMENTATION.md)。
