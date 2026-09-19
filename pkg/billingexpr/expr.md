# 计费表达式契约

Ren2Hub 的计价来源是数据库中发布的不可变 PriceVersion，模式为 unit_price、expression 或显式 free。表达式作为计价器的一种，读取与明确单价相同的规范化用量、已登记安全属性和报价冻结时间。结算绝不读取实时倍率、价格设置、请求正文或请求头。

## 表达式变量与归一化

| 变量 | 含义 |
|---|---|
| p、c | 未单独定价的输入、输出 token |
| len | 完整输入上下文长度，阶梯条件应使用此字段 |
| cr | 缓存读取 token |
| cc、cc1h | 默认/5 分钟、1 小时缓存写入 token |
| img、img_o | 图片输入、输出 token |
| ai、ao | 音频输入、输出 token |

子类别在表达式中出现时，从 p/c 排除；未出现时仍按基础输入/输出价格计费。OpenAI/Gemini 输入为包含缓存的总量；Claude 输入不包含缓存，因此未独立定价的缓存需要加回基础输入量。归一化在 pkg/billing.Usage.TokenParams 中统一执行。缓存重叠可将剩余文本降到零，不得产生负费用；原始负数或超范围数量直接拒绝计价。

表达式系数为 USD / 百万 token，例如 p * 2.5 表示输入单价每百万 token 2.5 USD。v1: 是唯一表达式版本前缀，没有前缀也按 v1。

## 函数与安全属性

支持 tier(name,value)、max/min/abs/ceil/floor、has(value,substring)。tier 记录命中档位，符合条件的倍率表达式记录 request_rules。

attr(name) 只接受登记名称的字面量：service_tier、quality、resolution、quantity、duration、tool_category。适配器从完成转换和参数覆盖后的最终发送参数提取这些属性。未知名称、动态名称、复合对象均拒绝。param() 和 header() 已删除，任意正文、原始请求头和密钥不会进入报价和恢复证据。

hour/minute/weekday/month/day 接受有效 IANA 时区的字面量，使用 Quote.FrozenAt，空值及无效时区报错；未冻结时间的求值也报错。不得回退到服务器当前时间或 UTC。

示例：

~~~text
tier("base", p * 2.5 + c * 15 + cr * 0.25)

len <= 200000
  ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)
  : tier("long_context", p * 6 + c * 22.5 + cr * 0.6 + cc * 7.5 + cc1h * 12)

tier("base", p * 2 + c * 10)
  * (attr("service_tier") == "priority" ? 2 : 1)
  * (hour("Asia/Shanghai") < 6 ? 0.8 : 1)
~~~

## 计算和持久化

发送前的 Quote 保存完整价格版本、全部单价、渠道倍率、安全属性和时间，并计算 SHA-256 哈希。每次结算校验哈希。Usage 与 ChargeResult 分别保存规范化用量和费用结果，不能覆盖原报价。

表达式引擎保持 float64 求值，结果必须有限、非负；随后转 Decimal，与报价中的工具/媒体单价费用合计，乘渠道倍率及固定 500000 quota/USD。总额只四舍五入一次；正费用最低 1 quota，零费用为零。超过 MaxInt32 单次限额直接报错并进入对账，不按饱和值扣款。

工具费用与表达式共享一次最终舍入；表达式版本的 prices 不得再次列出 token 单价。费用分项保存精确 USD 小计，总结果记录未舍入 quota、最终 quota、舍入差额和计算版本。

计算错误只向恢复记录暴露类别及冻结证据，不保存原始求值异常。未知接受状态或金额不明时保留预留，必须经过明确对账决定。

相关实现：pkg/billing/pricing.go、pkg/billingexpr、model/billing_pricing.go、relay/helper/price.go、service/billing_usage.go。
