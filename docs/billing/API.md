# 钱包、定价与充值 API

管理接口通常返回 `{success,data,message?}`；原生金额及下单接口可能使用 `{message:"success",data,...}`。客户端必须按各接口合同检查业务成功，不能只看 HTTP 200。quota 使用原生 JSON number，时间与 ID 使用原生字段，不能发送旧账户版本或资金事件指纹。

| 接口 | 用途 |
| --- | --- |
| `GET /api/user/self` | 当前用户 `quota`、`used_quota`、权限与设置 |
| `GET/POST /api/token/`、`PUT /api/token/` | API Key 原生预算、模型及 IP 限制；路由由 `/routing` 子接口管理 |
| `GET /api/user/topup/info` | 当前启用支付方式和最低金额；含 GMPay 开关及资产 |
| `POST /api/user/amount` | 原生充值金额计算，结果为格式化金额字符串 |
| `GET /api/user/topup/self` | 本人原生充值记录；`start_time` 早于 30 天截止点时截到截止点 |
| `GET /api/user/topup`、`POST /api/user/topup/complete` | 管理员查询与人工补单 |
| `GET /api/user/topup/stats` | 管理员，PAT 需要 `billing:read`；按币种、日期和 `methods[]`（支付方式×币种）汇总，不跨币种合并 |
| `GET /api/user/topup/ranking` | 管理员，PAT 需要 `billing:read`；只统计成功订单，按到账额度降序、同额按 `user_id` 升序，`limit` 取 1–50、默认 10；不返回支付凭据 |
| `GET /api/user/affiliate`、`GET /api/user/affiliate/invitees` | 返利余额、规则及脱敏邀请名单 |
| `GET /api/user/affiliate/records` | 本人的原始返利记录；包含 `invitee_id`、`source_id` 和 `base_quota`，不属于脱敏邀请名单 |
| `POST /api/user/affiliate/transfer`、`POST /api/user/aff_transfer` | 带 `operation_id` 的全额幂等转入；拒绝旧金额参数。两条路由都只接受浏览器会话，PAT（含 legacy PAT）返回 403 `AUTH_SESSION_REQUIRED` |
| `GET /api/user/topup/admin/detail/:trade_no` | 管理员订单详情；`affiliate` 为订单发出的返利 `{inviter_id, reward_quota, reversed}`，没有发出返利时为 `null`。本人的订单详情 `/api/user/topup/detail/:trade_no` 没有这个字段 |
| `POST /api/user/topup/admin/affiliate-reversal` | 管理员按订单回收返利，见下文“回收返利”；PAT 需要 `billing:write` |
| `POST /api/admin/affiliate/users/:id/deduct` | 管理员扣减用户未归因的可用返利，见下文“返利扣减”；只有接口，管理界面不提供入口；PAT 需要 `user:write` |
| `GET /api/pricing` | 原生模型价格投影 |
| `GET/PATCH /api/option/model_pricing` | 模型配置读取、版本校验保存 |
| `POST /api/option/model_pricing/{preview,convert}` | 草稿辅助操作，不创建请求预留 |
| `GET /api/log/self`、`GET /api/log/` | 用户与管理员用量日志 |

订单查询的 `keyword` 使用原生 `sanitizeLikePattern`：不带 `%` 时按订单号精确匹配；最多两个 `%`，不允许 `%%`。列表与统计共用同一套筛选；列表只在 `keyword` 非空时沿用原生的 `COUNT` 查询上限参数 10000，空关键词不设置该参数，不能视为所有查询的结果数量或扫描成本上限。管理员统计仍读取全部匹配记录。30 天截止点只作用于本人查询 `/api/user/topup/self`；管理员列表、`stats` 和 `ranking`（包括带 `user_id` 的查询）与上游 `GetAllTopUps` 一致，未传 `start_time` 时不设下限。`stats` 和 `ranking` 接受与列表相同的 `start_time`、`end_time`、`user_id`、`keyword`、`method`、`currency`。`method` 按有效支付方式筛选：`payment_provider` 非空时取它，否则取 `payment_method`；不属于 `epay`、`gmpay`、`stripe`、`creem`、`waffo`、`waffo_pancake`、`balance` 的归为 `other`，`methods[]` 按同一规则分组。没有 provider 的遗留订单不按 `payment_method` 推断渠道。`method` 只接受上述值和 `other`，其余返回错误。

`stats` 的 `daily`、`GET /api/user/affiliate` 的 `monthly_series` 和 `GET /api/data/self/summary` 的日界按 `tz_offset` 划分。参数沿用浏览器 `Date.getTimezoneOffset()` 约定：分钟数，UTC 以西为正，取值 -840 到 840；未传时按 UTC。非整数或超出范围返回 400 和 `message_key: "Invalid timezone offset"`，返利接口在解冻前拒绝。偏移在整个窗口内固定，窗口跨越夏令时切换时，切换后的日期或月份边界按切换前的偏移计算。

支付回调和人工补单通过订单行锁、状态校验与事务保证最多入账一次。该保证不等于所有支付通道都核对回调实付金额：只有 Epay 的报价合同和 GMPay 向统一结算传入 `TopUpReceipt`，按十进制值核对冻结的 `PayAmount` 和 `Currency`；这些路径遇到冻结值缺失或不符时拒绝入账。GMPay 的 CNY 币种来自订单与本地合同，回调本身没有独立的法币或网络字段。Stripe、Creem、Waffo 和 Waffo Pancake 沿用各自原生通知与订单规则，不传入这层金额收据。

回调响应也遵循各渠道合同，不再提供统一的永久错误 ACK 分类或 `payment.callback_rejected` 审计。Epay 验签或入账失败返回 `fail`；Stripe 保留验签失败 400，并在原生事件处理完成后返回 200，不能据此保证每种入账错误都会触发重试。GMPay 请求格式错误返回 400 `fail`，验签或结算失败返回 200 `fail`，只有成功返回 200 `success`；其余渠道以各自原生实现为准。日志中记录错误不等于持久化的业务审计。GMPay 的有限重试及旧订单处理见 [GMPAY.md](GMPAY.md)。

注册时传入邀请码，就必须成功归因；无效邀请码、邀请人停用或邀请码停用会使整笔注册回滚。返利活动关闭时，任何携带邀请码的注册都会返回 `AFFILIATE_DISABLED`，默认配置也关闭活动；此时需要移除邀请码或由 Root 开启活动。该规则同样作用于密码、OAuth 和微信注册，没有恢复上游忽略无效邀请码的行为。

“脱敏”仅指邀请名单的显示字段。返利记录接口和 `/api/user/affiliate` 的 `records` 继续返回原始 `invitee_id`、`source_id` 和 `base_quota`；充值来源的 `source_id` 是充值记录 ID，管理员加额来源是操作标识，并非统一的商户订单号。现有接口允许邀请人看到这些来源标识及额度基数，本轮保留该响应形状，不宣称返利记录已匿名化。

每个受邀人的返利上限按历史 `accrue` 毛额累计，冲回不释放上限；邀请名单的奖励显示也是毛额。名单排除软删除用户，而累计邀请人数和月度序列包含软删除记录，软删除后两种人数可能不同。这些是现有统计与返利口径，不改变充值到账额度。

充值订单的返利在拒付（付款成功后被银行或支付平台撤回）或退款后不自动回收。管理员在订单详情执行“回收返利”（`POST /api/user/topup/admin/affiliate-reversal`，请求体只接受 `{"trade_no":"..."}` 一个 JSON 对象，未知字段或多余内容被拒），每个订单只回收一次，按回收时的状态处理这笔返利：

- 仍在冻结：从邀请人的冻结返利扣回，到期后不再解冻出这笔额度。
- 已解冻：先解冻其他到期的返利，再从未归因的可用返利扣回。充值返利不记划转归因，扣不到的部分（已划转到钱包或已被扣减）记为欠款 `aff_offset_debt_quota`，由以后的返利先行抵扣。
- 这笔返利当初抵掉的旧欠款重新记为欠款。

仍归因到管理员加额的返利不动，它随扣减受邀人额度冲回。回收不改变累计返利、每个受邀人的上限（按毛额累计，回收不释放）、受邀人的钱包和订单状态；受邀人的额度由管理员另行扣减。成功返回 `record`（`kind` 为 `reversal`、`source_type` 为 `topup_reversal` 的返利记录，`credited_quota` 为扣回的额度，`debt_added_quota` 为新增欠款，字段含义与管理员加额冲回的记录相同）、`replayed`、`reversed_quota`（这笔返利的毛额）、`revoked_quota`（扣回的额度）、`debt_added_quota`。再次回收同一订单返回原记录、`replayed: true`，不再改动余额。订单不存在返回 `Top-up order does not exist`；订单未付款、没有邀请人、返利被跳过或邀请人已不存在返回 `this order has no referral reward to reverse`；管理员不能回收同级或更高角色邀请人的返利，Root 不受此限；已软删除的邀请人仍可回收。首次成功写一条审计 `topup.affiliate_reverse`，重放不再写。回收不受返利活动开关和支付合规确认影响。

返利扣减（`POST /api/admin/affiliate/users/:id/deduct`）只有接口，管理界面不提供入口：请求体只接受 `{"quota":正整数,"operation_id":"..."}` 一个 JSON 对象，`operation_id` 为 8–96 位字母、数字、`_` 或 `-`。它同样只扣未归因的可用返利，扣前先解冻到期的冻结返利，未到期的冻结返利不能扣。金额非正或超过钱包上限返回 `referral deduction amount must be a positive quota`，超过可扣部分返回 `amount exceeds the deductible referral quota`；角色限制与回收相同。成功返回 `record`（`kind` 为 `deduction`）、`replayed`、`deducted_quota`、`available_quota`、`frozen_quota`；同一用户的同一 `operation_id` 重放返回原记录，金额不同返回 HTTP 409；首次成功写一条审计 `user.affiliate_deduct`。扣减和回收都从同一部分余额扣，同一笔拒付只用其中一种：对同一笔既扣减又回收会扣两次，第二次记为欠款。

回收和扣减记录出现在管理员和本人的返利记录里，可按 `kind=reversal`、`kind=deduction` 筛选；两者都不改变累计返利、每个受邀人的上限、邀请名单奖励和月度序列。

模型、充值与管理员操作仍须满足原生角色、scope、业务校验及适用的安全验证。GMPay 与个人工单额外限定浏览器会话；管理员工单使用原生 ticket scopes，见[扩展接口](../routing/GATEWAY_API.md)。

旧 `/api/next`、`/api/billing/self/reservations`、`/api/billing/self/events` 与独立网关账务指标不是现行 API。不能根据历史 OpenAPI 文件恢复这些端点。金额计算见[定价](PRICING.md)，GMPay 响应例外和回调见[支付合同](GMPAY.md)。
