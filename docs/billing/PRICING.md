# 模型定价与价格获取

当前安装格式为 schema 5，价格合同版本为 4，计算版本为 `newapi-c71eefbc-v1`。模型定价采用 NewAPI `c71eefbc` 的配置和计算语义；钱包、预留、资金事件及恢复仍由 Ren2Hub 的持久化账务处理。初始模型价格与工具价格均为空，启动不下载或安装默认价格。旧库不能直接升级，安装边界见[账务架构](IMPLEMENTATION.md)。

## 模型配置

`model_pricing` 是模型价格的唯一编辑来源。管理员通过 `GET/PATCH /api/option/model_pricing` 读取和保存，`expected_version` 是配置内容哈希。首次保存以读取结果中的 `empty_version` 为基准；发生并发修改返回 409，不能覆盖更新后的价格。

| 模式 | 配置 | 单位与行为 |
| --- | --- | --- |
| 按 Token 倍率 | `ModelRatio`，可附输出、缓存、图片和音频倍率 | 基础输入每百万 token 的 USD 单价为 `ModelRatio × 2`；辅助倍率按上游规则解析并随请求冻结 |
| 按次 | `ModelPrice` | USD/次；图片入口使用已验证的数量及适用的历史尺寸、质量规则 |
| 表达式 | `billing_setting.billing_mode=tiered_expr` 与 `billing_setting.billing_expr` | Token 系数为 USD/百万 token；任务表达式返回 USD，单位由任务执行合同确定 |

八项数值价格键为 `ModelPrice`、`ModelRatio`、`CompletionRatio`、`CacheRatio`、`CreateCacheRatio`、`ImageRatio`、`AudioRatio`、`AudioCompletionRatio`；加上表达式 mode、expression 和 `billing_setting.plugin_billing_expr`，构成 11 项配置。插件覆盖在模型草稿中以插件 key 为键，存储时使用 `<pluginKey>::<model>`。

缺失主价表示未定价，不补零、不使用内置绝对价格；显式数字 `0` 是有效配置。输出及缓存等辅助默认只有主价已配置后才生效。新表达式草稿为空，保存前必须由管理员填写。清空定价会移除配置，不恢复默认价格。旧倍率到表达式的转换只修改草稿，保存后才生效；转换按实际生效倍率生成价格，不能无损转换时返回具体原因。

数值输入接受有限、非负数字或精确十进制文本，数据库保存数字 JSON，编辑 API 返回十进制字符串保留精度。表达式原文保留。不能先将钱包、Token 或事件中的 int64 quota 转成 JavaScript Number；金额协议见[账务 API](API.md)。

## 自动获取只填入草稿

模型定价页面的“自动获取”调用 `POST /api/ratio_sync/fetch`，并行获取下面五个固定公开来源。管理员选择一个来源及模型范围，确认覆盖提示后填入前端草稿；只有“保存模型价格”会改变服务器配置。获取、重试、取消和页面刷新不会发布价格，也不会修改渠道或模型清单。

| 来源 ID | 固定地址 | 价格单位 |
| --- | --- | --- |
| `sub2api` | `https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.json` | Token 字段为 USD/token，转换为表达式系数时乘 1000000 |
| `axonhub` | `https://raw.githubusercontent.com/ThinkInAIXYZ/PublicProviderConf/refs/heads/dev/dist/all.json` | `cost` 的 Token 字段为 USD/百万 token |
| `basellm` | `https://basellm.github.io/llm-metadata/api/newapi/ratio_config-v1-base.json` | NewAPI 倍率、按次或现成表达式 |
| `models_dev` | `https://models.dev/api.json` | `cost` 为 USD/百万 token，保留 provider 身份 |
| `openrouter` | `https://openrouter.ai/api/v1/models` | Token 价格为 USD/token 字符串，无需渠道密钥 |

每源最多读取 16 MiB，超时 30 秒；不接受自定义 URL，不携带渠道凭据，不跟随重定向。结果逐源返回 `success/partial/error`、候选及原因。一个来源失败不阻断其他来源，但不会用旧缓存伪装本次获取成功。

每个模型只采用所选来源的完整价格，不按字段混源、不自动择最低价、不建立来源优先级。保留原始模型 ID、provider、明确零价、缓存和可表示的阶梯。相同来源中多个 provider 的同名模型分别展示，只能为一个本地模型选择一项；同一 provider 与模型 ID 重复使该来源报错。不能无损映射的模型带 `unsupported_reason`，不得导入。

当前网关不提供公开 Batch 模型端点，独立 Batch 产品的 `_batches` 价格字段通过 `warnings` 明确提示未导入。工具费用可能叠加在同一模型请求上，不能仅提示后丢弃：OpenRouter 的 `web_search`、Sub2API 的 `search_context_cost_per_query` 或 `google_maps_grounding_cost_per_query` 只要存在非零或非法值，整个模型就拒绝导入。嵌套对象必须逐项证明全部为明确零价，否则同样拒绝；明确全零时仅提示工具价格未同步，不会写入工具价格配置。

来源解析生成 NewAPI 草稿，不创建 `PriceVersion`、定时同步任务、活动价格指针或历史来源归档。模型目录只是参考数据，不是官方账单，也不证明渠道支持某种协议或计费维度。

## 表达式与任务

表达式使用上游 Expr VM，支持原样 `param()`、`header()`、动态字段路径及 `hour/minute/weekday/month/day`。不将表达式翻译成有限属性语言，也不为它增加另一套计算逻辑。`p/c` 表示未单独计价的输入/输出，`len` 保持完整输入上下文，引用的缓存、图片、音频子类由规范化用量剔除一次。推理 token 没有独立的 NewAPI 价格变量，按普通输出计价；不同推理单价的外部条目不能直接导入。

Token 表达式以 `tier("base", p * 2.5 + c * 10 + cr * 0.25)` 为例，`fixed(0.01)` 表示一次成功请求的 USD 价格。任务通过 `u("seconds") * 0.4` 等表达式读取当前插件声明的用量，结果直接为 USD；不再使用单独的 v2 方言。共享模型可以配置插件覆盖，实际渠道确定执行插件，客户端不能选择计费提供者。当前插件元数据与所选表达式一起冻结；缺失、非法或超界的完成事实不能从预估补齐。任务求值只传入插件用量事实，不提供提交正文或请求头；模拟保持同一输入边界。

预扣与实际结算分别使用各自真实参数、用量和求值时刻。报价时间不是所有时间函数的固定替代值。求值记录经过加密，恢复重放读取当时记录的环境调用结果，再执行同一个表达式；不会读取当前请求、系统时钟或现行价格。语法和函数细节见[表达式合同](../../pkg/billingexpr/expr.md)，证据边界见[计价接口](../../pkg/billing/API.md)。

## 工具、渠道系数与舍入

辅助工具价格单独保存在上游 `tool_price_setting.prices` option，单位为 USD/千次调用，初始为空。键 `tool_name` 表示工具通用价格，`tool_name:model_prefix*` 表示模型前缀覆盖，最长前缀优先，显式零也优先。管理员通过设置接口保存该完整映射；它不是第二份模型价格来源。

声明使用工具但缺少适用价格时，在预留与发送前拒绝；明确零价允许执行。声明工具不等于实际调用。完成回执中的调用次数才用于实际附加费，正调用量缺价也不能当作零。工具费用随模型配置一起进入请求快照，后续设置变化不修改旧请求。

部分渠道按供应商自己的计数和价格键计费，渠道适配器在初始化时声明读取器和对应价格键，声明相应托管工具时这些键同样须在发送前定价：Azure Responses 的网页搜索按 `tool_usage.web_search.num_requests` 计 `bing_web_search`；xAI Responses 按 `server_side_tool_usage_details` 计 `web_search`（用 `web_search:grok*` 覆盖定价）、`x_search_posts`、`x_search_profiles`；百炼 Responses 与 Anthropic 兼容端点计 `search_strategy_agent`。供应商报告的次数替换同一键下按协议条目统计的次数，报告为零即不收该键。Claude `server_tool_use` 中的 `web_fetch`、`code_execution`、`tool_search` 仅在冻结报价为该键定价时计入，Claude 线格式声明这些工具时须定价；动态过滤网页搜索内部使用的代码执行在未定价时不计费。上述键及协议内置工具键是保留名，客户端同名函数不按其计费。结算时单个工具的调用次数封顶 10,000 并记录警告；Responses 图片数量超过 128 仍视为无效用量，改按冻结估算结算。已完成的 `image_generation` 输出在 failed、incomplete、cancelled 或 error 终态同样计费。

Gemini Google 搜索 grounding 按一次响应内去重后的非空查询数（网页与图片搜索，跨流式帧和全部候选）计 `google_search`；响应带任一查询时另记一次 `google_search_grounded_prompt`，但只在冻结报价为该键定价时计入。Google 对 Gemini 3 按查询、对 Gemini 2.5 及更早按 grounding 次数计费：运营可为 `google_search_grounded_prompt:gemini-2.5*` 等前缀定价，并把同前缀的 `google_search` 设为 0。Google 不在响应中报告计费次数，去重查询数是可得的最佳证据，不保证与账单一致；客户端同名函数不按这两个键计费。

渠道售价系数仍为 Decimal 字符串，在模型费用与工具附加费合计后应用一次。没有独立渠道模型价格。表达式和 `OtherRatios` 不得再次叠加相同的渠道系数、图片数量或尺寸质量系数。预扣倍率只影响 token 预估分支，不改变实际结算或按次分支。

| 计算分支 | 转为 quota 的规则 |
| --- | --- |
| 倍率 | 按冻结辅助倍率计算并截断整数；正费用截断为零时最低 1 quota |
| 按次 | 单价、数量及适用倍率计算后截断整数 |
| 无非零工具附加费的表达式 | Expr VM 的浮点结果按上游公式转 quota，使用 `math.Round` |
| 带非零工具附加费的表达式 | 按上游路径合并工具费用后截断整数 |

资金金额仍为 int64，固定 `500000 quota = 1 USD`。不能用“所有费用 Decimal 一次四舍五入”概括这些模式。负数、非有限结果和超过单请求 MaxInt32 的费用不能作为实际费用，请求改按发送前冻结估算结算，不截断成允许的最大费用。`ChargeResult` 保留基础 USD、调整后金额、最终 quota 和舍入差额。

## 公开展示与审计

`GET /api/pricing` 恢复 NewAPI 模型行合同：模型元数据、倍率或按次价格、`billing_mode`、表达式、插件变体及 `pricing_version`。不返回旧 offers、活动价格版本或来源原文；网关模型可用性仍受用户、Token、渠道资格与实际已配置价格约束。

请求中的 `PriceVersion` 是从当前配置派生的不可变执行证据，不能由独立管理页面发布或修改。准备结算意图时比较请求、真实尝试、报价、用量与加密证据；相同最终 quota 不能替代证据一致性。结算依据与兜底见[结算兜底与恢复](RECOVERY.md)。
