# 钱包账务架构

当前账务格式为 4，价格合同为 3，计算器为 `billing-v3`，表达式与单价均使用 Decimal。只支持空库初始化和匹配 `billing_schemas` 标记的重复启动，不自动迁移或清空旧库。本文描述源码契约，复验方法见[账务复验](VALIDATION.md)；实际部署和验收结果只由所属工作区服务器档案记录。

## 资金与计价边界

钱包是唯一资金来源，Token 是附加消费限额。账户、预留、流水和统计金额使用 `common.Quota`（int64 / SQL BIGINT），固定 500000 quota/USD；API 金额是十进制整数字符串，无限 Token 的可用额度为 `null`。

模型价格只能来自已发布的不可变 `PriceVersion`：明确单价、高级表达式或显式免费。活动版本以 `(scope_kind, channel_id, model)` 为身份；先取精确渠道价格，再取明确发布的全局模型价格。渠道价格的报价系数固定为 1，全局价格可使用冻结的 Decimal 渠道售价系数。每次真实发送前冻结 `Quote`、作用域、版本及哈希、最终安全属性与时间；实际规范用量保存为 `Usage`，费用结果保存为 `ChargeResult`。这些证据各自持久化，不能相互覆盖。全部费用用 Decimal 合计后只舍入一次，正费用最低 1 quota；无效或超过单请求上限的金额进入对账，不按饱和值结算。

## 实现入口

| 入口 | 职责 |
| --- | --- |
| `pkg/billing/` | 规范用量、不可变报价和纯计算；不查询实时配置 |
| `pkg/billingexpr/` | 仅读取规范用量、已登记属性及冻结时间的表达式 |
| `model/billing_pricing.go` | 按价格作用域发布不可变版本、维护活动指针及并发保护 |
| `service/billing_pricing_catalog.go` | 逐来源解析有效快照，供自动发布、参考草稿与恢复自动共用 |
| `model/billing_event.go` | 唯一账户投影写入点；事件、钱包与 Token 同事务 |
| `model/billing_recovery.go` | Reserve、IncreaseReservation、Settle、Release 及幂等恢复 |
| `model/billing_consumption.go` | `validateBillingConsumption`：普通请求、实时观察、任务终态与最终资金事务共用的消费证据校验 |
| `model/billing_quote.go` | `ValidatePublishedBillingQuote`：冻结 Quote 与数据库不可变版本的核对 |
| `service/billing_session.go` | 协调逻辑请求、预留、发送保护与退出处理 |
| `model.TaskBillingContext` | 异步任务的 Quote、Usage、Charge 与原模型名 |
| `model/billing_schema.go` | 空库格式标记与重复启动守卫 |

充值、兑换、奖励和管理调账通过 `ApplyWalletAdjustment` / `ApplyWalletAdjustmentTx` 进入同一事件；业务凭据与入账使用同一事务。锁顺序为恢复/业务状态 → 钱包 → Token。资金事务内不调用 Redis、支付网关、日志库或通知服务。

## 请求与恢复

付费请求发送前持久化预留与消费保护，更贵的重试先补足预留。已确认实际费用超出预估时可形成欠费，后续付费请求停止准入；未知接受或缺少最终用量时保留预留。显式免费请求也写发送保护。已交付内容、断流、超时或进程退出都不能单独成为退款依据。

最终结算只接受 `Usage.ReadyForSettlement()` 为真的用量：`Complete` 为真、非估算、未标记非法、`Source` 非空，且 `Terminal` 为 `completed`。普通请求、实时观察、任务终态和最终资金事务共用 `validateBillingConsumption`：要求冻结的 Usage 与 Charge 同时存在、用户与 Token 一致、日志金额等于 `Charge.FinalQuota`，再核对已发布报价和同一请求、渠道、报价哈希的不可变尝试及其指纹，按冻结 Quote 重新计算 Charge 并比较哈希。任务失败释放不保留消费费用，成功任务同样经过该校验。`ValidatePublishedBillingQuote` 按版本 ID 读取数据库中的不可变版本并比较冻结定义哈希，不读取当前活动指针，之后发布的新版本不会使已接受的尝试失效；伪造版本 ID 或未发布预览不能成为收费依据。

只提交金额的结算入口只能幂等确认已准备好的结算意图，不能新建结算意图。人工结算要求已保存 Quote、Usage、Charge 和消费指纹，并在最终资金事务中按冻结证据复核金额；管理员金额和说明不能把未知用量变成可扣款事实，明确放弃收费的释放仍可执行。

同一请求首次使用某渠道时冻结该渠道的价格，切换渠道建立另一份报价，返回已尝试渠道时复用原价格。每次尝试独立保存报价证据，恢复快照管理累计预留目标。审计按冻结 Quote、Usage 和账本事件重放；请求指纹绑定请求 ID、渠道和报价，消费指纹覆盖完整用量与账单，不用舍入后的相同金额代替证据一致性。

消费结果以恢复记录作为持久化待投递记录。资金事件与账户投影提交后，日志、统计和缓存独立恢复；SQL 日志按事件 ID 幂等，ClickHouse 写入结果不明时保持保护。任务终态与账务意图保持关联，Root 人工决定继续驱动同一任务费用及消费结果。

当前结算路径不读取旧模型倍率或实时目录，不使用无持久化资金来源、拆分钱包/Token 退款或内存资金批量写入。目录检查仅发布新不可变价格，保存源原文历史和字段转换证据；之后的请求才使用新版本。旧种子和归属迁移路径已删除。易支付依赖冻结订单和有效通知，不主动查单。

来源只声明有据的支持维度和服务档，请求最终参数在预留前核对。人工发布必须有来源证据或明确零售价理由；未来生效版本直接拒绝，未实现计划调度。字符、时长及任务完成事实各自冻结计量合同，不能用请求估算补缺。媒体、Realtime 和工具的具体边界见[定价说明](PRICING.md)。

UI 展示最多保留 3 位小数，编辑、提交、导出和内部计算保留完整精度。钱包与 Token 分别展示可用、在途预留和已结算消费。

接口见[账务 API](API.md)，定价见[单价与目录](PRICING.md)和[表达式契约](../../pkg/billingexpr/expr.md)，故障处理见[恢复与对账](RECONCILIATION.md)，前端见[账务说明](../../frontend/docs/BILLING.md)，复验方法见[账务复验](VALIDATION.md)。
