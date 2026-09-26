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
| img_cr | 明确报告的图片缓存读取 token，单独定价时从 cr 与 img 中移出 |
| ai、ao | 音频输入、输出 token |
| image_count | 图片实际数量，整数 1–128；发送前使用最终请求的已验证数量 |

子类别在表达式中出现时，从 p/c 排除；未出现时仍按基础输入/输出价格计费。OpenAI/Gemini 输入为包含缓存的总量；Claude 输入不包含缓存，因此未独立定价的缓存需要加回基础输入量。归一化在 pkg/billing.Usage.TokenParams 中统一执行。缓存重叠可将剩余文本降到零，不得产生负费用；原始负数或超范围数量直接拒绝计价。

图片缓存遵循目标上游的显式拆分：仅在 img_cr 被定价且 OpenAI 包含缓存语义下，将实际图片缓存从 cr 和 img 同时移出，再从 p 扣除互不重叠的已定价类别。缺少图片缓存细分时不估算其比例，img_cr=0，保留上游通用缓存语义；显式零与缺失仍分别持久化。负数、超出缓存/图片总量或无法组成合法集合的细分拒绝计算并待核对，不按降级猜测扣款。

`v1:` 表达式系数为 USD / 百万 token，例如 p * 2.5 表示输入单价每百万 token 2.5 USD；表达式结果为微美元。没有前缀的 token 草稿按 v1。`fixed(usd)` 返回 `usd × 1000000`，仍由同一结算器转换到美元。

按张表达式可写 `v1:tier("images", fixed(0.04)) * image_count`。数量来自同一 `Usage.Quantities["images"]`，不是新价格或请求体来源；只在表达式使用该变量时必需，缺失、零、负数、小数和超过 128 均拒绝。正常图片请求省略 n 时，由协议验证器先解析默认值 1；纯计算器不从缺失用量猜 1。发送前按最终转换和覆盖后的 n 预留；成功终态按有效图片载荷确认实际数量，缺失/非法/断流保持待核对。实际数不会改写 `Quote.Attributes.quantity` 的冻结请求条件。token 表达式不额外乘数量；使用 image_count 时禁止附加 images/per_image 行重复收费，包括未执行分支中的引用。Trace.image_count 保存实际求值数量，所有费用仍一次最终舍入。Vue 转换只更新待确认草稿，原已发布价格保持不变。

`v2:` 为任务 schema 表达式，结果直接为 USD，不再除以一百万。`u("seconds") * 0.4` 对实际 10 秒得到 4 USD。此方言没有 token 变量和 fixed；固定任务采用 calls/per_call。未知版本拒绝。

v2 必须附带冻结 UsageSchema（版本 1、字段定义及 SHA-256 哈希）。字段声明 number/string/boolean、数值单位、范围、整数约束、可选枚举、必需性及来源 request_fixed / estimate_only / completion_fact。u 只能直接使用已声明的字面量键；全部 AST 分支验证，不允许动态名称、函数别名或未声明字段。编译缓存按表达式哈希与 schema 哈希隔离。

Quote.RequestFacts 仅存请求固定值；Usage.EstimatedFacts 与 ActualFacts 分开。实际结算不从估算补缺；缺失、null、非法和明确零分别处理。完成事实不得覆盖冻结条件，只有完整实际事实才能结算；价格引用的事实缺失则待核对。estimate_only 字段不能定义最终收费。表达式与单价行不得同时收取同一维度。

Vidu 工厂插件仅在完成时提供 credits，提交阶段没有可信的估算或预留上界。新工厂价格草稿和发布中，只要 schema 将 credits 标为必需、表达式任意分支引用 credits 或单价行按 credits 收费，就拒绝该方案；不能把示例中的 0 当预留依据。未引用 credits 的 `v2:0` 仍可用于 schema 发现。已发布价格与在途 Quote 不改写，再次发布则必须通过当前校验。

固定任务价使用不带 UsageSchema 的 unit_price，且仅含一条明确的 calls/per_call 单价；发送前和成功完成均按 calls=1 计价。供应商完成事实保存在 CompletionUsage 中，不作为这类价格的 ActualFacts，也不追加收费；hook 失败或非法完成事实仍进入待核对。插件升级不替换在途任务固定的 key/version/hash，新工厂解析修正不会追溯改写旧执行引用。

## 函数与安全属性

支持 tier(name,value)、max/min/abs/ceil/floor、has(value,substring)。tier 记录命中档位，符合条件的倍率表达式记录 request_rules。

attr(name) 只接受登记名称的字面量：service_tier、quality、resolution、quantity、duration、tool_category、effort、thinking、thinking_budget_tokens、temperature、top_p。适配器从完成转换和参数覆盖后的最终发送参数提取这些属性；缺失参数不补默认值，明确零值保留。thinking 为 on/off/adaptive，Gemini 自动预算 -1 记录为 adaptive 而不记录负用量。未知名称、动态名称、复合对象均拒绝。param() 和 header() 已删除，任意正文、原始请求头和密钥不会进入报价和恢复证据。

Claude 的 effort 取最终请求当前回合的有效值：顶层作为初始设置，消息级 system 设置从其后第一个 user 开始生效并持续；尾部未遇到 user 的配置不覆盖当前值。该只读解析由 RelayKit 统一提供，计价与最终日志共用结果；配置缺省不猜模型默认值，非法结构在预留和发送前拒绝。跨协议采用实际目标参数，不用已丢失的源消息配置计价。参数覆盖和重试仅影响新尝试，历史 Quote 的属性、哈希及恢复结算不追溯更改。

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

发送前的 Quote 保存完整价格版本、全部单价、渠道倍率、安全属性、固定事实和时间，并计算 SHA-256 哈希。价格与报价均冻结合同版本 2 和计算器 billing-v2；价格额外冻结表达式结果单位及用量 schema。每次结算严格检查版本和哈希，旧报价不能自动补版本。新草稿由发布入口补齐当前合同，数据库解码必须验证已保存合同。Usage 与 ChargeResult 分别保存规范化用量和费用结果，不能覆盖原报价。

表达式引擎保持 float64 求值，结果必须有限、非负；随后转 Decimal，与报价中的工具/媒体单价费用合计，乘渠道倍率及固定 500000 quota/USD。总额只四舍五入一次；正费用最低 1 quota，零费用为零。超过 MaxInt32 单次限额直接报错并进入对账，不按饱和值扣款。

工具费用与表达式共享一次最终舍入；表达式版本的 prices 不得再次列出 token 单价。费用分项保存精确 USD 小计，总结果记录未舍入 quota、最终 quota、舍入差额和计算版本。

自动目录在生成 PriceVersion 时组合上下文和服务档位。priority/fast 共用显式 priority 分项，缺少整个档位时普通 token 与缓存费用乘 2；ultrafast 乘 2；flex 显式价优先，否则乘 0.5。部分显式档位的未覆盖分项继承普通价，明确零视为覆盖。独立工具和媒体单价不参加默认倍率。结算不读取实时目录，也不额外应用档位倍率；手动表达式完全按原定义执行。

可选 usage_scope=embedding 参与价格及 Quote 的冻结哈希，省略时保留旧序列化。输入型价格不得引用输出变量、图片数量、fixed 或任务 schema；全部 AST 分支都检查，即使零系数或不可达分支也不能隐藏输出引用。实际结算拒绝输出用量，发送前拒绝非 embedding 请求。

计算错误只向恢复记录暴露类别及冻结证据，不保存原始求值异常。未知接受状态或金额不明时保留预留，必须经过明确对账决定。

相关实现：pkg/billing/pricing.go、pkg/billingexpr、model/billing_pricing.go、relay/helper/price.go、service/billing_usage.go。
