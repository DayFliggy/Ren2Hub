# 钱包账务架构

当前安装格式为 schema 5，价格合同版本为 4，计算版本为 `newapi-c71eefbc-v1`。模型定价配置与 Expr VM 跟随固定 NewAPI 参考版本；Ren2Hub 保留钱包、Token 预留、资金事件、恢复和终态投递合同。本文描述源码行为，复验方法见[账务复验](VALIDATION.md)；实际部署和验收结果只由所属工作区服务器档案记录。

## 配置与资金边界

钱包是唯一资金来源，Token 是附加消费限额。账户、预留、流水和统计金额使用 `common.Quota`（int64 / SQL BIGINT），固定 500000 quota/USD；API quota 是十进制整数字符串，无限 Token 的可用额度为 `null`。

`model_pricing` 是唯一模型价格编辑来源，覆盖倍率、按次和表达式三种模式。工具附加费独立来自 `tool_price_setting.prices`。两者初始均为空，明确零有效，缺价拒绝。外部价格获取只返回草稿候选，不写配置。渠道售价系数在合计费用后应用一次，没有独立渠道价格或活动版本切换器。

请求边界从已保存配置解析生效辅助倍率及任务插件，再派生不可变 `PriceVersion` 和 `Quote`。这些记录是执行证据，不是第二份可编辑价格目录。已有尝试只使用其冻结配置，后续管理员修改不追溯改变费用。纯计算与各模式舍入见[模型定价](PRICING.md)。

## 实现入口

| 入口 | 职责 |
| --- | --- |
| `model/model_pricing_config.go` | 模型配置、精确数字、乐观版本检查；首次 option 行创建后锁行重读 |
| `pkg/pricesource/`、`service/model_price_fetch.go` | 五个固定公开来源的下载和草稿转换，无价格写入 |
| `pkg/billing/model_pricing.go` | 冻结配置的三模式纯计算，不查询现行设置 |
| `pkg/billingexpr/` | 原样上游 Expr VM、动态请求探针与环境调用重放 |
| `model/billing_pricing.go` | 从唯一配置构造价格，保存按定义哈希识别的不可变执行证据 |
| `pkg/billing/evidence.go`、`model/billing_evidence.go` | 加密求值输入，绑定请求/尝试/配置/阶段，验证和重放 |
| `model/billing_event.go` | 唯一账户投影写入点；事件、钱包与 Token 同事务 |
| `model/billing_recovery.go` | 预留、补足、结算、释放及幂等恢复 |
| `model/billing_consumption.go` | 普通请求、实时观察、任务终态与最终资金事务共用的消费证据校验 |
| `model/billing_fallback.go`、`service/billing_fallback.go` | 冻结估算与 Realtime 累计快照兜底结算，以及过期在途请求的唯一结束决定 |
| `service/billing_session.go` | 协调逻辑请求、预留、发送保护与退出处理 |
| `model.TaskBillingContext` | 异步任务的实际 attempt、Quote、Usage、Charge 与原模型名 |
| `model/billing_schema.go` | 空库格式与加密密钥指纹守卫 |

充值、兑换、奖励和管理调账通过 `ApplyWalletAdjustment` / `ApplyWalletAdjustmentTx` 进入同一事件；业务凭据与入账使用同一事务。锁顺序为恢复/业务状态 → 钱包 → Token。资金事务内不调用 Redis、支付网关、日志库或通知服务。

## 求值证据

表达式可以访问真实请求正文、请求头、动态路径和时间函数。这些输入只进入用于账务重放的加密上下文，不写进公开 Quote、消费日志、路由诊断或管理详情的明文响应。不能通过参数白名单改写原表达式语义。

`EvidenceCipher` 使用 AES-GCM，密钥由稳定且显式配置的 `CRYPTO_SECRET` 经 HKDF 派生。AAD 绑定逻辑请求 ID、真实 `BillingAttempt.ID`、配置哈希和求值阶段；加密载荷另绑定报价与用量哈希。预扣为 `estimate`，实际结算为 `settlement`，只读模拟为 `preview`。预扣和实际结算分别记录真实环境，不能互换证据。

发送前 `PersistBillingEstimate` 校验并保存预估用量和费用；资金保护要求预留覆盖该估算，该估算也是无法计价时的冻结结算依据。最终消费记录携带真实 AttemptID 和加密证据。准备结算意图时解密并执行同一表达式与计算器，时间和动态读取按已保存的调用记录重放；身份、配置、用量、调用记录或金额不一致时拒绝该意图，请求改按冻结估算结算。最终资金事务只核对已保存证据的指纹与金额。

`PriceVersion`、Quote、Usage 和 Charge 分别保存，不能相互覆盖。请求体/请求头不得明文落到快照、错误或日志；密文快照使用跨数据库 `LongText`。重复完成和恢复重试复用已提交的证据与操作身份，不能靠重新求值产生另一份消费。

## 请求与恢复

所有请求，包括明确零价请求，都建立持久化发送保护。准入与 NewAPI 一致，可用额度须覆盖输入估算；更贵的重试必须先补足预留；结算超出预留时形成欠费，后续付费请求停止准入。账务先计费，不设人工待处理状态：完整用量按实际结算，不完整或未确认的用量按估算结算，无法计价时按冻结估算结算；只有没有有效输出且没有已接受异步任务的请求释放预留，规则见[结算兜底与恢复](RECOVERY.md)。

最终结算要求 `Usage.Settleable()`：未标记非法且来源非空；`Usage.Confirmed()` 只标记完整、非估算、终态 completed 的供应商证据。`validateBillingConsumption` 核对用户、Token、日志金额、真实尝试、配置/报价哈希及消费指纹，再通过加密证据重放验证费用；冻结估算必须等于该尝试自身的发送前估算，并按 estimate 阶段重放。`ValidatePublishedBillingQuote` 这一历史函数名只核对请求执行证据，不查询活动价格目录。未保存的预览不能作为实际收费依据。

协议用量的“完整”依赖原始字段是否存在：明确零是事实，缺失、null 或空对象不是。Responses、Chat SSE、音频和 Realtime 保持各自的完成观察与方向计数校验。规范化缓存、图片和音频子类仅扣除一次；实际计价失败后可以生成明确标注冻结估算依据的消费日志，不能伪造供应商实际用量已验证。

渠道测试与普通中继共用模型报价、最终发送参数、预留、证据边界和结算依据；无法计价时按冻结估算结算并向管理员报告计价错误。仅提交金额的结算接口只能确认已准备的意图，不能用金额创建缺少证据的新消费。

异步任务把供应商终态与账务分开：成功但完成用量无法计价时按提交时估算结算，明确失败、超时或缺少上游任务 ID 时释放，不重放上游。任务结果事件与 PerfMetric 在独立幂等 outbox 中投递；资金提交、消费日志、统计和缓存各自恢复。SQL 日志按事件 ID 幂等，ClickHouse 写入结果不明时保持待投递，不盲目重发。

## 安装与运行状态

schema 5 只接受空库初始化，或版本及 `EvidenceKeyID` 匹配的重复启动。程序不会自动迁移或清空旧库。重装和旧数据处置由明确授权的部署流程执行；文档更新不表示数据库已经重置。

必须保留配置好的 `CRYPTO_SECRET`，不能在重启时随机替换。更换或丢失密钥会阻止旧证据验证；备份和回退必须同时保留数据库、应用格式与对应密钥。不能只回滚镜像，也不能删除证据来绕过恢复守卫。

钱包与 Token 分别显示可用、在途预留和已结算消费。金额展示最多保留 3 位小数，编辑、提交及导出保留原始精度。Expr VM 的浮点计算语义与资金 int64 精确存储是不同边界，不能混称所有计费都是 Decimal 运算。

接口见[账务 API](API.md)，计算见[计价接口](../../pkg/billing/API.md)，故障处理见[结算兜底与恢复](RECOVERY.md)，前端见[账务说明](../../frontend/docs/BILLING.md)。
