# 明确单价与冻结报价

每个模型仅一个生效价格版本，模式为 `unit_price`、`expression` 或显式 `free`。模型必须精确匹配已发布的名称；缺价、未知单位、非法金额报错，不套用其他模型价格。渠道倍率是报价中的显式售价系数。

| 数量 | 单位 | 数量来源 |
| --- | --- | --- |
| p、c | per_million_tokens | 规范化输入、输出 token |
| cr、cc、cc1h | per_million_tokens | 缓存读取、默认/5m 写入、1h 写入 |
| img、img_o、ai、ao | per_million_tokens | 图片、音频输入/输出 token |
| calls | per_call | 完成调用次数 |
| images | per_image | 已确认交付图片数；发送前按最终请求数量估计 |
| seconds | per_second | 最终参数或已确认媒体时长，批量数量计入总时长 |
| tool:类别 | per_call、per_thousand_calls | 适配器确认的工具调用次数 |

OpenAI、Claude、Gemini 的原始用量先归一化，缓存是否包含在输入量中由来源语义决定。完整上下文长度单独保存为 `ContextTokens`，表达式变量为 `len`。独立定价的子类别从基础输入/输出剔除，避免重复收费；未独立定价的 token 子类别保留在基础输入/输出中。

单价用 Decimal 计算。费用分项保存数量、单位、单价和精确 USD 小计；合计乘渠道系数与 500000 后，只对总额四舍五入一次。正金额不足 1 quota 收 1 quota；明确零金额为 0。结果包含舍入前总额、最终 quota、舍入差额、计算版本和表达式版本。超出单请求 MaxInt32 或无效结果转入对账，不截断扣款。

账务格式与价格合同版本为 2，计算器为 `billing-v2`。`v1:` token 表达式输出单位为微美元，例如 `p * 2.5 + c * 10` 对应每百万 token 输入 2.5 USD、输出 10 USD。求值结果必须有限且非负，再转 Decimal，与明确列出的非 token 项合计。表达式模式不能同时配置 token 单价。

`v2:` 插件表达式输出 USD，`u("seconds") * 0.4` 对完成事实 10 秒计 4 USD。用量字段来自冻结的版本 1 schema，估算不能补实际缺失；不允许 token 变量或 `fixed()`。固定任务价格使用 `calls/per_call`。schema、方言、结果单位及计算器版本参与价格/报价完整性哈希。表达式引用维度不得再次出现在附加单价行中。

表达式示例及安全函数见 [表达式契约](../../pkg/billingexpr/expr.md)。按秒表达式例如 `attr("duration") * attr("quantity") * 20000` 表示每秒每份 0.02 USD。复杂阶梯使用表达式；不建立第三套倍率规则。

属性仅允许已登记的 service_tier、quality、resolution、quantity、duration、tool_category、effort、thinking、thinking_budget_tokens、temperature 和 top_p。属性来自完成适配与参数覆盖的最终发送参数。时间函数使用该尝试的冻结时间，必须显式指定有效 IANA 时区；不读当前时间、任意正文或请求头。

发送前保存完整 Quote、价格版本、属性和 SHA-256 哈希，每次上游尝试有独立 ID。配置更新仅影响之后生成的报价。Usage 与 ChargeResult 单独持久化。恢复重放已确认金额与事件，不重新取价。文本没有输出上限时采用 8192 的估计并标记 Estimated，这不是费用上界；付费预估为零仍预留 1 quota，显式免费也写入发送保护。

初始目录位于 [billing_pricing_seed.json](../../model/billing_pricing_seed.json)：254 个明确价格，101 个待配置名称。来源是固定上游 `d04c118c8803f49e0c9bab74dcf5b5efeab9464a` 的已审阅快照（`pkg/billing/cmd/exportseed/upstream-rc40.json`），使用以下命令确定性导出；它不是实时供应商报价，也不会联网更新价格：

```powershell
go run ./pkg/billing/cmd/exportseed
```

历史输入倍率转换为 `倍率 × 2 USD/百万 token`，缓存、输出、媒体和工具默认值转换为显式单价。涉及适配器质量/时长规则且不能确定转换的 dall-e-3、gpt-4o-mini-tts、sora-2 系列、veo 系列，没有可审阅正式价格的插件模型，以及通配模型，留作待配置并记录原因。通配条目不能自动成为实际模型的售价。

空库安装时，在单个事务中写入目录及生效指针；重复启动不覆盖已发布价格。前端“从本地初始目录填入”只填草稿，保存后才发布。UI 最多显示 3 位小数，编辑、请求、快照和导出保留完整精度。
