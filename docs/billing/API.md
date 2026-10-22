# 钱包、定价与充值 API

管理接口通常返回 `{success,data,message?}`；原生金额及下单接口可能使用 `{message:"success",data,...}`。客户端必须按各接口合同检查业务成功，不能只看 HTTP 200。quota 使用原生 JSON number，时间与 ID 使用原生字段，不能发送旧账户版本或资金事件指纹。

| 接口 | 用途 |
| --- | --- |
| `GET /api/user/self` | 当前用户 `quota`、`used_quota`、权限与设置 |
| `GET/POST /api/token/`、`PUT /api/token/` | API Key 原生预算、模型及 IP 限制；路由由 `/routing` 子接口管理 |
| `GET /api/user/topup/info` | 当前启用支付方式、最低金额和折扣；含 GMPay 开关及资产 |
| `POST /api/user/amount` | 原生充值金额计算，结果为格式化金额字符串 |
| `GET /api/user/topup/self` | 本人原生充值记录；`start_time` 早于 30 天截止点时截到截止点 |
| `GET /api/user/topup`、`POST /api/user/topup/complete` | 管理员查询与人工补单 |
| `GET /api/user/topup/stats` | 管理员，PAT 需要 `billing:read`；按币种、日期和 `methods[]`（支付方式×币种）汇总，不跨币种合并 |
| `GET /api/user/topup/ranking` | 管理员，PAT 需要 `billing:read`；只统计成功订单，按到账额度降序、同额按 `user_id` 升序，`limit` 取 1–50、默认 10；不返回支付凭据 |
| `GET /api/user/affiliate`、`GET /api/user/affiliate/invitees` | 返利余额、规则及脱敏邀请名单 |
| `POST /api/user/affiliate/transfer`、`POST /api/user/aff_transfer` | 带 `operation_id` 的全额幂等转入；拒绝旧金额参数。两条路由都只接受浏览器会话，PAT（含 legacy PAT）返回 403 `AUTH_SESSION_REQUIRED` |
| `GET /api/pricing` | 原生模型价格投影 |
| `GET/PATCH /api/option/model_pricing` | 模型配置读取、版本校验保存 |
| `POST /api/option/model_pricing/{preview,convert}` | 草稿辅助操作，不创建请求预留 |
| `GET /api/log/self`、`GET /api/log/` | 用户与管理员用量日志 |

订单查询的 `keyword` 使用原生 `sanitizeLikePattern`：不带 `%` 时按订单号精确匹配；最多两个 `%`，不允许 `%%`。列表与统计共用同一套筛选，`total` 最多为 10000。30 天截止点只作用于本人查询 `/api/user/topup/self`；管理员列表、`stats` 和 `ranking`（包括带 `user_id` 的查询）与上游 `GetAllTopUps` 一致，未传 `start_time` 时不设下限。`stats` 和 `ranking` 接受与列表相同的 `start_time`、`end_time`、`user_id`、`keyword`、`method`、`currency`。`method` 按有效支付方式筛选：`payment_provider` 非空时取它，否则取 `payment_method`；不属于 `epay`、`gmpay`、`stripe`、`creem`、`waffo`、`waffo_pancake`、`balance` 的归为 `other`，`methods[]` 按同一规则分组。没有 provider 的遗留订单不按 `payment_method` 推断渠道。`method` 只接受上述值和 `other`，其余返回错误。

支付回调入账前由 `settleTopUp` 统一核对已验签的实付金额和币种，金额按十进制值比较，订单没有冻结 Currency/PayAmount 时同样拒绝。不符时订单保持 pending，写 `payment.callback_rejected` 审计。Stripe、Creem、Waffo、Waffo Pancake 对这类永久错误返回不触发重试的响应；Epay 和 GMPay 沿用上游，入账失败一律回 `fail`，两者收到 success 才停止重投，失败留在商户后台可见，epusdt 的重投次数有上限（见 [GMPAY.md](GMPAY.md)），Epay 的重投上限由所接平台决定。验签失败保留上游的 4xx。已启用核对的渠道：Epay（`money`，CNY 元）、Stripe（`amount_subtotal`，不含折扣和税；Adaptive Pricing 取 `currency_conversion` 的原币金额）、GMPay（签名内的 `amount`）。Creem、Waffo、Waffo Pancake 的实付字段语义无法从代码和 SDK 确认，未启用核对，登记为待验证。数据库不可用等暂时性错误返回可重试的响应。

模型、充值与管理员操作仍须满足原生角色、scope、业务校验及适用的安全验证。GMPay 与个人工单额外限定浏览器会话；管理员工单使用原生 ticket scopes，见[扩展接口](../routing/GATEWAY_API.md)。

旧 `/api/next`、`/api/billing/self/reservations`、`/api/billing/self/events` 与独立网关账务指标不是现行 API。不能根据历史 OpenAPI 文件恢复这些端点。金额计算见[定价](PRICING.md)，GMPay 响应例外和回调见[支付合同](GMPAY.md)。
