# Ren2Hub 计价接口

唯一可编辑价格来源是 NewAPI 风格的 `model_pricing`。`PriceVersion` 只是该配置在请求边界的不可变执行证据，没有独立的价格发布、活动价格指针或自动发布业务。

## 管理与草稿

`GET/PATCH /api/option/model_pricing` 读取／保存模型配置，保存沿用 `expected_version` 冲突检查。`convert`、`preview` 与模拟只处理草稿。价格抓取只提供候选；管理员选取来源后填入草稿，仍由保存接口生效。初始模型价格为空；没有配置不能按免费价格执行，明确零价则有效。

`PricingValues` 包含上游 ModelRatio、ModelPrice、CompletionRatio、CacheRatio、CreateCacheRatio、ImageRatio、AudioRatio、AudioCompletionRatio、billing mode、expression 与插件表达式配置。宿主解析生效默认、模型名称及执行插件，纯计价器不查询当前设置。

## 冻结合同

`NewModelPriceVersion(model, effective, taskUsage)` 构造倍率、按次或表达式合同；`PreparePriceVersion` 深拷贝配置并生成定义哈希；`NewQuote` 冻结渠道系数和报价身份。合同版本为 4，计算版本为 `newapi-c71eefbc-v1`。旧 unit_price/free、独立单价维度和 v2 合同拒绝执行，新库不提供兼容分支。

`Quote.PreConsumeMultiplier` 仅作用于估算的 token 分支；`OtherRatios` 为宿主已验证的额外请求倍率；`AudioInputPrice` 只有明确配置时才可冻结。工具附加费使用 `PriceVersion.Prices` 的 `tool:*` 项，支持 `per_call` 和 `per_thousand_calls`。宿主的配置入口是独立上游 option `tool_price_setting.prices`，单位 USD/千次调用，初始为空；`tool:model_prefix*` 以最长前缀覆盖工具通用价格，显式零有效。声明工具缺价时拒绝发送，实际调用量缺价时拒绝计价，不能从声明补造实际次数。

模型费用和工具费用合计后应用渠道系数一次。倍率、按次、表达式分别保持上游舍入，不能统一改成 Decimal 四舍五入或所有正价最低 1 quota；具体分支见[模型定价](../../docs/billing/PRICING.md#工具渠道系数与舍入)。资金账户、预留和事件仍是精确 int64，API quota 保持十进制整数字符串。

图片按次数量来自 `Usage.Quantities.images`，DALL-E 历史尺寸／质量系数由 `LegacyImagePriceRatio` 统一提供。不能同时在 OtherRatios 再写 n/image_count 或重复的尺寸质量系数。表达式图片数量通过实际 RequestInput.ImageCount 输入。

## 求值与重放

```go
cipher, err := billing.NewEvidenceCipher(secret)
binding := billing.EvidenceBinding{
    RequestID: requestID,
    AttemptID: attemptID,
    ConfigHash: billing.PricingConfigHash(quote),
    Phase: "settlement", // 实际预扣使用 estimate；只读模拟可用 preview
}
usage, charge, err := cipher.Evaluate(quote, usage, actualRequestInput, binding)
replayed, err := cipher.Replay(quote, usage, binding)
```

`Evaluate` 返回包含 `evaluation_evidence` 密文的 Usage。AES-GCM 密钥通过 HKDF 派生，AAD 包含请求、真实尝试、配置哈希与阶段。`Replay` 校验 AAD、报价与用量哈希后重跑相同计算器，动态 `param/header/time` 使用保存的环境调用结果；不能将密文 Usage 直接交给无密钥的 Calculate，也不能仅相信历史金额。身份、内容或结果不一致保持待处理。`KeyID()` 返回公开指纹，用于安装标记，不能用于解密。请求体及头不会进入公开 Quote。

计算错误也返回包含该次实际输入的加密 Usage，ChargeResult 为空；调用方必须保存失败证据，不能沿用预扣金额。时间对象保留墙钟、单调计数和时区对象身份，重放要求与首次求值完全相同的 Go 工具链版本；升级工具链前需验证或处理在途时间证据，参见[表达式契约](../billingexpr/expr.md)。

宿主使用 `model.EvaluateBilling/ReplayBilling`，并从恢复记录和真实 BillingAttempt.ID 推导身份，不信任调用方提交的证据元信息。`PersistBillingEstimate` 在发送前验证并保存估算证据，要求已预留额度覆盖估算。BillingConsumption 必须保存对应 AttemptID，资金事务再次验证估算和实际费用。

schema 5 安装必须配置稳定的 CRYPTO_SECRET，并在数据库 marker 保存 KeyID。更换或丢失密钥会阻止旧证据的验证，不能自动生成新密钥继续扣款。大型密文快照使用跨数据库 LongText。普通 API、日志与审计报告只返回费用和验证状态，不返回解密上下文。

完整表达式和时间语义见 [表达式契约](../billingexpr/expr.md)。
