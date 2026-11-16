# 计费界面与金额契约

本文描述当前 NewAPI 原生钱包与 Vue 客户端合同。用户钱包是请求消费的资金来源，Token 额度是附加限制；预扣、结算与退款由原生 `BillingSession` 及对应服务处理。前端读取服务端结果，不维护第二套余额或资金事件账本。

## 数值与响应格式

钱包 `quota`、`used_quota`，Token `remain_quota`、`used_quota`，充值到账及返利额度使用原生 JSON number。客户端在 API 边界要求安全整数，随后可转为十进制字符串供 BigInt 展示、比较和输入换算。内部字符串不是 wire contract；不能把数字额度接口改成字符串接口，也不能接受已经丢失精度的数字。

`formatQuota` 按 `/api/status` 的 `quota_display_type`、`quota_per_unit` 和汇率换算额度，默认 `500000 quota = 1 USD`；钱包余额头部用同一函数拆出的 `formatQuotaParts`，用量分布的消费指标也走 `formatQuota`。TOKENS 模式显示千分位原始 quota，不带货币符号和小数。充值输入另受 `/api/status` 的 `quota_display_type`、`quota_per_unit` 与支付配置控制。钱包入账容量上限为 `2^53 - 1`。原生结算可能产生负余额，不能静默显示为零。前端工具支持更宽的 BigInt 范围，不表示后端接受该范围的资金写入。USD 输入必须能精确换算为整数 quota，发送数值前再次检查安全整数；图表的 Number 投影不能作为资金更新输入。

普通业务接口使用 `{ success, data, message? }`。原生金额、下单及部分支付管理接口使用 `{ message: "success" | "error", data, url? }`；HTTP 200 不等于业务成功。支付适配由 `src/api/topup.ts` 处理，不能用宽松的通用客户端把失败当成功。人民币支付金额、加密资产数量与 quota 是不同单位。

## 钱包、Token 与额度管理

| 操作             | 原生接口与字段                                                                                          |
| ---------------- | ------------------------------------------------------------------------------------------------------- |
| 当前钱包         | `GET /api/user/self`：`quota`、`used_quota`、`request_count`                                            |
| 用量概览         | `GET /api/data/self/summary`：服务端范围聚合；不伪造增幅；本月超出日志范围时钱包显示 —                  |
| Token 列表及编辑 | `GET/POST/PUT /api/token/`：`remain_quota`、`used_quota`、`unlimited_quota`，以及模型、IP、有效期等限制 |
| 管理员增减额度   | `POST /api/user/manage`：`id`、`action: "add_quota"`、`mode`、数值 `value`、`operation_id`              |
| 兑换码创建       | `POST /api/redemption/`：`name`、数值 `quota`、`count`、`expired_time`                                  |
| 用户兑换         | `POST /api/user/topup`：`key`                                                                           |

无限 Token 由 `unlimited_quota: true` 表达，编辑时发送数值 `remain_quota: 0`；它仍受钱包及其他请求规则约束，不代表免费消费。Token 更新没有钱包版本号和额度操作 ID，提交的是原生 Token 预算字段。

管理员界面把 USD 增减额转换为 quota，发送 `mode: "add" | "subtract"` 与正整数 `value`。服务端也支持 `override`，当前增减对话框不使用它。相同 `operation_id` 和请求内容重试返回已记录结果，变更内容复用同一 ID 返回 409 和 `message_key: "operation_id was already used for a different request"`。权限、目标角色、容量和返利资格由后端核验；不要通过普通用户资料更新代替额度接口。该接口没有 `expected_version` 或独立调账理由字段。

兑换码表单的 USD 字符串只是输入表示，提交时使用数值 `quota`；`count` 是生成数量，不是金额。钱包按原生能力开关显示兑换入口。

兑换码 CSV 按导出开始时的筛选和排序逐页拉取全部结果（每页 100 条，上限 10000 条），超出时提示截断。兑换人列是原生 `used_user_id`，原生列表不返回邮箱。

## 在线充值与 GMPay

先读取 `GET /api/user/topup/info` 的开关、`pay_methods`、最低充值额和预设金额，只显示服务端启用的方法。选中方式的最低额取全局或渠道最低额与该方式 `min_topup` 中的较大值；`min_topup` 缺失、空串或 `"0"` 表示该方式没有额外下限，其他非整数值视为无效响应。方式级下限只在控制台生效，服务端与上游一样只校验全局或渠道最低额。EPay 与 GMPay 均通过 `POST /api/user/amount` 获取原生金额。`amount` 为整数，GMPay 方法必须保留完整标识，例如 `gmpay:usdt.tron`。

普通金额展示模式按原生充值单位提交；TOKENS 模式按 `quota_per_unit` 换算最低额和预设值，并限制为完整充值单位，避免付款数额与原生截断后的到账 quota 不一致。报价是当前配置的计算结果，带实付和按本金另赠的额度（充值加赠）。下单时把用户看到的 `expected_pay_amount`、`expected_currency` 和 `expected_bonus_quota` 一并提交，服务端重新计算，价格或加赠不一致时返回 `topup_quote_changed` 或 `topup_bonus_changed` 并附新的报价，页面展示后等用户再次确认；Stripe 和 Creem 先用 `GET /api/user/topup-bonus/preview` 取加赠。规则见[充值加赠](../../docs/billing/TOPUP_BONUS.md#在线充值的报价与确认)。请求不携带账户版本或 `amount_unit`。

EPay 使用 `POST /api/user/pay`，按成功响应的 `url` 和字符串表单字段跳转。其他提供方使用各自原生 amount/pay 路由；没有明确币种时不猜测为 USD。GMPay 使用 `POST /api/user/gmpay/pay`，以 **CNY 法币金额计价、加密资产支付**：响应给出 `trade_no`、`currency: "CNY"`、`pay_amount`、数值 `expected_quota`、`token`、`network` 和 HTTPS 收银台地址。实际转账数量、地址及网络以收银台和服务端核验后的支付证据为准。

GMPay 创建前持久化原生 TopUp 与支付快照，记录 CNY 分额、预计到账 quota、资产、网络和关联商户版本。这是单笔支付核验数据，不是请求计费的冻结账本。网关创建操作不自动重试；持久化后创建失败仍保持 `message: "error"`，可附受控 `trade_no` 及实际读到的 `creation_status`。状态可能为 `creating`、`ready`、`failed`、`unknown` 或 `settled`；读状态失败时只返回已知订单号，不猜状态。商户密钥与内部版本标识不返回用户。

前端遇到未知创建、网络中断或无法验证的创建响应时，阻止在当前界面重复创建，并尽可能跟踪已有订单；明确失败才允许手动重新发起。网关返回 `/wallet` 后，用查询参数中的订单标识或同用户会话保存的订单号读取 `GET /api/user/topup/detail/:trade_no`，不信任 URL 的成功标记。服务端确认到账后才刷新钱包，页面不自动重新下单。

GMPay 配置仅 Root 浏览器会话使用：`GET /api/option/gmpay/config`、`POST /api/option/gmpay/save`、`POST /api/option/gmpay/probe`。配置包含 `pay_address`、`merchant_id`、`enabled`、数组 `assets` 与只读 `has_secret`；保存 `secret_key: ""` 保留已有密钥。没有独立 USD 单价、业务分组倍率或每资产最低金额。个人 GMPay 下单与查询同样限定浏览器会话。

Waffo Pancake 设置的“创建店铺和商品”失败时，原生响应在 `data` 中给出 `error`；店铺已建而商品失败时另带 `orphan_store: true`、`store_id` 和 `store_name`。面板显示失败原因，孤儿店铺还会回填店铺 ID 并清空商品 ID，避免重复建店。

## TopUp 记录与订单管理

用户读取 `GET /api/user/topup/self`，管理员读取 `GET /api/user/topup`，采用原生分页字段。订单标识为 `trade_no`，状态为 `pending`、`success`、`failed`、`expired`；Vue 把 `success` 投影为“已完成”。`credited_quota` 才是实际入账额度，不能用请求 `amount`、`money` 或 `expected_quota` 冒充到账。

详情分别使用 `GET /api/user/topup/detail/:trade_no` 和 `GET /api/user/topup/admin/detail/:trade_no`，两者都带 `user_deleted:boolean`。订单所属用户被删除后详情仍可读取：`user_id` 保持原值，软删除保留用户名和邮箱，硬删除两者为空串；订单详情和管理员工单队列都显示“用户已删除”，硬删除的工单用户以 `#id` 显示。GMPay 详情区分创建时收银台信息与结算后的实际回执，请求网络不能冒充已核验的实际支付网络。管理员通过 `POST /api/user/topup/complete` 提交 `{ trade_no }` 人工补单，受权限与后端事务控制。重复回调、并发回调及回调与补单竞争不得重复入账。

`GET /api/user/topup/stats` 返回 `currencies`、`methods`、`daily`、时间范围与计数，各币种和各支付方式分别汇总，`paid_amount` 保持十进制字符串。`GET /api/user/topup/ranking` 返回按到账额度排序的前 10 名用户（`user_id`、`username`、`paid_count`、`credited_quota`）。历史记录缺少支付金额或币种时显示未知，并计入 `unknown_amount_count`，不补零、不贴 USD 标签。接口没有提供的统计不凭空生成。

订单统计、返利信息和用量概览请求都由 `viewerTzOffset()`（`src/utils/format.ts`）附带浏览器当前的 `tz_offset`，服务端按访问者所在的日历日和月份分桶。偏移在整个窗口内固定，跨夏令时切换的窗口按请求时的偏移划界。

## 邀请返利

`GET /api/user/affiliate` 返回可转额度、冻结额度、累计奖励、有效比例、规则及月度统计；`GET /api/user/affiliate/invitees` 返回脱敏名单，`GET /api/user/affiliate/records` 返回本人记录。数值额度在前端边界校验后转为展示字符串。

`policy.enabled` 或 `code_enabled` 任一为 false 时，带邀请码注册会被后端拒绝；返利相关错误都带 `message_key`，控制台按 zh-CN 服务端消息目录翻译。`useInvite` 的 `shareable` 是唯一判断点：此时不生成邀请链接，复制邀请码、复制链接和三种分享动作都不执行，卡片显示停用原因。已有返利仍可转入钱包，转入条件不受 `shareable` 影响。

`POST /api/user/affiliate/transfer` 仅提交 `{ operation_id }`，将本次可转余额全额转入原生钱包。返回 `replayed`、`transferred_quota`、`wallet_quota` 等结果；相同操作 ID 重试不再次转入。旧 `amount` 参数被拒绝，客户端不能指定任意转入金额。冻结额度不属于当前可转余额，读取或转入时由后端处理到期解冻。

成功充值的返利与钱包入账在同一事务中处理，按唯一来源记录一次奖励决定。管理员增额是否产生返利由 `admin_recharge_enabled` 等规则决定；规则变更不会自动补发旧充值奖励。管理员通过 `/api/admin/affiliate/users/:id` 读取、修改邀请规则，采用现有用户管理权限。

## 模型定价

Root 在 `/models/pricing` 维护价格，旧 `/system-settings/billing/model-pricing` 书签重定向到该页面。保留模型列表与编辑器、手机抽屉、可视化及 JSON 编辑、按 Token/按次/表达式、缓存/图像/音频价格、阶梯与请求条件、任务 usage schema 与插件变体。元数据和价格分别保存。

`GET /api/option/model_pricing` 返回 `entries`、`options`、`empty_version`；`PATCH` 同一路径提交 `changes`，每项含 `model_name`、`expected_version` 与 `pricing`；删除或清空某个模型时提交 `pricing: {}`。这里的版本用于价格配置并发编辑，不是钱包版本；保存冲突保留草稿。十一项配置包括模型价格及辅助倍率、`billing_setting.billing_mode`、`billing_setting.billing_expr`、`billing_setting.plugin_billing_expr`。

模型价格只来自管理员保存的配置，代码不内置模型或表达式价格；新安装只有上游内置音频倍率的条目，这些模型没有基础价格，仍列在“未定价”里，填写输入价并保存时，单价留空的内置音频倍率按原值保留。保存空配置后模型回到未定价，请求被拒绝，页面确认框须说明这一后果，不提“恢复默认”。缺项、显式零价及不可用分别表达；未配置的输出、缓存、图像倍率按 1 倍输入价；音频倍率与 Gemini 音频输入单价沿用上游内置值。工具价格通过通用 option `tool_price_setting.prices` 保存，沿用上游内置单价和匹配规则；保存的值覆盖内置价，内置清单以外的工具未配置时不收费。完整规则见[定价合同](../../docs/billing/PRICING.md)。

`POST /api/option/model_pricing/convert` 生成待确认的表达式草稿；`POST /api/option/model_pricing/preview` 解释草稿有效价格。二者不扣费、不保存，正常保存才使价格生效。表达式保留原生语义和文本，不添加历史版本前缀；当前没有价格 `/simulate` 接口。

“自动获取”通过 `POST /api/ratio_sync/fetch` 获取 Sub2API、AxonHub、basellm、models.dev、OpenRouter 来源，显示失败与不支持原因。选定来源和模型范围后回填草稿，保存才写入；不自动选择最低价，不混合来源覆盖。取消、失败及过期响应不丢失已有草稿。

定价编辑器有四条与上游 React 不同的本地规则，都是为了避免保存后少收：

- 改任务单价后“生成任务计价草稿”只重建能无损还原的表达式：顶层乘积里恰好一个单阶梯、无常数项的 `tier()` 价格因子，其余因子都是 `cond ? 数字 : 1` 乘数；空白或纯数字表达式直接替换。重建时保留这些乘数和 `v1:` 前缀。其他形状一律拒绝并提示直接编辑表达式，原表达式不变（`repriceTaskExpression`）。
- “配置普通 OpenAI 价格”写入的占位 0 价不能直接保存：表达式仍是占位时提交报 `ordinaryPriceRequired`，需要免费时显式改写。
- 汇率调整只换算以站点币种报价的来源，保存值始终是 USD 基础价。表达式只改 `tier()` 价格叶和 `fixed()`，阈值、零价、版本前缀、`image_count` 和请求条件倍率保持原文；无法识别的结构、非法汇率、溢出或下溢都拒绝，任一选中行出错时禁止填入。
- 保存前用确认框左右对照当前与保存后的价格；待删除行提示保存后变为未定价、请求会被拒绝。已修改的行也可以撤销回已保存的价格。

公开 `/api/pricing` 与登录用户 `/api/user/model-catalog` 使用原生价格投影，后者还提供渠道报价；前者受站点入口和权限控制。动态表达式不能伪造成固定 Token 单价，每千/每百万 Token 切换仅改变尺度。渠道倍率由服务端在原生结算链中应用一次，客户端不维护第二份售价。当前请求结算不恢复旧冻结报价或加密求值重放。

## 日志与验证边界

用户、管理员日志分别读取 `GET /api/log/self`、`GET /api/log/`，对应统计和导出接口提供汇总与下载。详情显示已记录的 `quota`、模型和原生 `other` 数据；`other` 可为对象或 JSON 字符串。它不是旧 `charge_result` 版本化证明，界面不按当前价格重算历史收费，也不要求 schema 5 字段或 Token 资金事件。前端不请求已退役的 `/api/billing/self/events`，也不维护 Token 账本或冻结报价兼容层。

前端回归覆盖安全整数、输入精确换算、原生响应格式、报价和付款失败、订单归属、回跳状态、未知创建不重复、全额返利幂等、价格版本冲突、权限及响应式布局。浏览器的真实合同模拟不能代替三库事务测试和隔离候选上的真实 HTTP 回调。验证顺序见[前端文档](../README.md)，服务端合同见[资金 API](../../docs/billing/API.md)、[定价](../../docs/billing/PRICING.md)、[GMPay](../../docs/billing/GMPAY.md)，测试方法见[按改动模块测试](../../docs/testing/MODULE_TESTING.md)。

主要维护落点：[`topup.ts`](../src/api/topup.ts)、[`adminOrders.ts`](../src/api/adminOrders.ts)、[`affiliate.ts`](../src/api/affiliate.ts)、[`pricingConfig.ts`](../src/api/pricingConfig.ts)、[`contracts.ts`](../src/api/contracts.ts)、[`WalletView.vue`](../src/views/console/WalletView.vue)、[`LogBillingDetail.vue`](../src/components/console/log-ui/LogBillingDetail.vue)。路由和状态以 [`api-router.go`](../../router/api-router.go)、[`extensions.go`](../../router/extensions.go)、[`topup.go`](../../controller/topup.go)、[`topup_query.go`](../../model/topup_query.go)、[`affiliate.go`](../../model/affiliate.go) 为准。
