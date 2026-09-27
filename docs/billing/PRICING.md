# 明确单价与冻结报价

价格身份为 `(scope_kind, channel_id, model)`，每个作用域有独立的活动版本，模式为 `unit_price`、`expression` 或显式 `free`。`channel_model` 要求正整数渠道 ID，`global_model` 的渠道 ID 为 0。查价先读取精确渠道价格，再读取明确发布的全局模型价格；缺价、未知单位、非法金额在预留前报错。渠道独立价格的报价系数必须为 1；全局价格可使用冻结的渠道售价系数。渠道系数在配置、API 和报价中均为 Decimal 字符串，最多 18 位小数，范围为 `(0,1000]`。

| 数量 | 单位 | 数量来源 |
| --- | --- | --- |
| p、c | per_million_tokens | 规范化输入、输出 token |
| reasoning | per_million_tokens | 输出 token 中的 reasoning 数量；独立定价时从普通输出扣除 |
| cr、cc、cc1h | per_million_tokens | 缓存读取、默认/5m 写入、1h 写入 |
| img、img_o、ai、ao | per_million_tokens | 图片、音频输入/输出 token |
| calls | per_call | 完成调用次数 |
| images | per_image | 已确认交付图片数；发送前按最终请求数量估计 |
| seconds | per_second | 最终参数或已确认媒体时长，批量数量计入总时长 |
| tool:类别 | per_call、per_thousand_calls | 适配器确认的工具调用次数 |

OpenAI、Claude、Gemini 的原始用量先归一化，缓存是否包含在输入量中由来源语义决定。完整上下文长度单独保存为 `ContextTokens`，表达式变量为 `len`。独立定价的子类别从基础输入/输出剔除，避免重复收费；未独立定价的 token 子类别保留在基础输入/输出中。

单价用 Decimal 计算。费用分项保存数量、单位、单价和精确 USD 小计；合计乘渠道系数与 500000 后，只对总额四舍五入一次。正金额不足 1 quota 收 1 quota；明确零金额为 0。结果包含舍入前总额、最终 quota、舍入差额、计算版本和表达式版本。超出单请求 MaxInt32 或无效结果转入对账，不截断扣款。

新安装账务格式为 3，价格合同版本仍为 2，计算器为 `billing-v2`。本次不迁移旧账务数据库；启动会拒绝旧格式，不能直接对已有生产数据库运行新版本。`v1:` token 表达式输出单位为微美元，例如 `p * 2.5 + c * 10` 对应每百万 token 输入 2.5 USD、输出 10 USD。VM 保留浮点求值，仅在 `billing.Calculate` 的统一边界检查有限性、非负和金额范围后转换 Decimal，与非 token 项合计。表达式模式不能同时配置 token 单价。

`v2:` 插件表达式输出 USD，`u("seconds") * 0.4` 对完成事实 10 秒计 4 USD。用量字段来自冻结的版本 1 schema，估算不能补实际缺失；不允许 token 变量或 `fixed()`。固定任务价格使用 `calls/per_call`。schema、方言、结果单位及计算器版本参与价格/报价完整性哈希。表达式引用维度不得再次出现在附加单价行中。

价格可组合 `flat_fee`、固定单价和结构化阶梯。`volume` 用完整数量选择一个档位，`graduated` 分档累计；有限末档不允许超量。价格可指定 `effective_from`、`effective_to` 生效窗口，报价创建时校验，结算按冻结时间重放。表达式示例及安全函数见 [表达式契约](../../pkg/billingexpr/expr.md)。按秒表达式例如 `attr("duration") * attr("quantity") * 20000` 表示每秒每份 0.02 USD。

属性仅允许已登记的 service_tier、quality、resolution、quantity、duration、tool_category、effort、thinking、thinking_budget_tokens、temperature 和 top_p。属性来自完成适配与参数覆盖的最终发送参数。时间函数使用该尝试的冻结时间，必须显式指定有效 IANA 时区；不读当前时间、任意正文或请求头。

发送前保存完整 Quote、作用域、版本 ID、版本哈希、来源、属性和报价 SHA-256。每次上游尝试有独立 ID；同一请求首次选中某渠道时冻结该渠道价格，切换渠道使用另一份冻结报价，回到已尝试渠道仍复用原价格。预留累计目标由恢复快照管理。Usage 与 ChargeResult 单独持久化，请求指纹绑定请求 ID、渠道和报价，消费指纹覆盖完整用量及账单；相同舍入金额不能掩盖不同结算证据。恢复重放已确认金额与事件，不重新取价。文本没有输出上限时采用 8192 的估计并标记 Estimated，这不是费用上界；付费预估为零仍预留 1 quota，显式免费也写入发送保护。

自动定价仅接入 Sub2API 主源与 AxonHub 补缺源。流程为下载、校验并持久化快照、规范化目录、模型匹配、发布不可变价格版本。两个独立来源适配器输出相同的规范化价格结构，由公共转换器生成 `PriceVersion`；同步发布、目录草稿和恢复自动管理共用同一个匹配器。

精确模型身份优先，同一候选身份按 `Sub2API → AxonHub` 选择完整价格；主源缺价或拒绝该模型时，允许补源提供完整价格。手动发布的 `manual` 价格始终优先，恢复自动必须由 Root 显式操作且携带当前版本。来源内不同供应商同名，即使金额相同，也只保留供应商限定名，裸名标为待配置；不猜模型家族，也不拼接不同供应商的价格字段。

| 来源 | 地址 | 计量单位 |
| --- | --- | --- |
| Sub2API 默认 | `https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.json` | USD/token 转 USD/百万 token |
| AxonHub 默认 | `https://raw.githubusercontent.com/ThinkInAIXYZ/PublicProviderConf/refs/heads/dev/dist/all.json` | USD/百万 token |

启动仅在来源记录不存在时安装两个默认源的[离线快照](../../service/pricingdata/README.md)，已有数据库快照优先，不进行网络请求。默认启用自动刷新，由 `billing_pricing_sync` 系统任务每 30 分钟检查；Root 可设置 5–1440 分钟，关闭计划后仍可手动检查。Sub2API 先比较独立 SHA-256 文件，不变则不下载，下载后必须与远端哈希一致；AxonHub 通过 ETag/Last-Modified 条件请求及正文 SHA-256 判断变化。正文最多 16 MiB，每源超时 40 秒，哈希请求超时 10 秒。哈希不符、HTTP 错误、空/坏目录或全部条目不可用不替换上次有效数据，错误会进入来源状态及任务结果。

快照持久化于 `BillingPricingSource`；`origin`、`source_model`、`source_hash` 同时冻结到 PriceVersion 并参与版本和报价哈希。因此来源变化即使单价不变，也会产生新版本。来源与价格均有并发校验，完整双源哈希参与发布事务，防止慢节点用旧主源视图覆盖刚生效的价格。来源检查完成后也重试之前尚未发布完的有效目录，因此一次数据库故障不会使相同哈希永远跳过发布。自动发布仅管理全局作用域，不覆盖人工渠道价格。

解析直接从 JSON 数字或数字字符串构造 Decimal，保留明确零价，区分缺失，拒绝负值、非有限值及不能可靠表达的非零收费维度。单模型缺失、价格非法、费用不完整或不满足任务插件发布 schema 时，继续尝试下一来源的完整价格，不拼接字段。有效目录中两个来源均无法提供价格时，停用该模型的自动生效指针，阻止新请求沿用旧价；历史版本和冻结报价仍完整保留。停用受完整双源哈希、当前版本和人工所有权保护。整源下载、哈希或目录结构校验失败则保留上次有效快照，记录错误；没有有效缓存的来源标为 `unavailable`，继续读取其他来源。全部来源都没有有效快照时目录解析失败，不能据此清空既有发布价格，也不启用硬编码默认价。

来源状态包括 `bundled`（尚未远端检查的离线快照）、`current`（最近检查成功）、`cached`（检查失败，沿用有效缓存）和 `unavailable`（没有有效缓存）。公开价格和请求报价只读取已发布版本，不能直接使用参考目录。

自动 token 价格在发布时把上下文阶梯与服务档组合成冻结表达式。`priority`、`fast` 共用显式 priority 价；没有显式分项时按普通 token 费用的 2 倍。`ultrafast` 始终为普通 token 费用的 2 倍；`flex` 优先显式价，否则为 0.5 倍。部分显式档位只覆盖对应分项，其余继承普通价，不再套默认倍率；明确零也算显式价。服务档缺少长上下文绝对价时，按来源中的基础与长上下文比例组合。默认倍率涵盖 token 与缓存，不影响工具按次、图片按张等独立费用。结算只使用冻结表达式；手动价格原样执行。

模型匹配复用宿主 reasoning 规则，精确目录条目优先。明确的资源包装及合法日期后缀可以匹配已有基础型号；不能从一个日期版本猜另一个日期版本，不做家族兜底，也不任意删除供应商命名空间。

所有来源均无法提供价格的具体身份，会在活动价格表保留 `version_id=0`、`origin=unavailable` 的不可用标记。请求查价遇到该标记立即停止，不能通过 reasoning 基础名继续兜价；有效来源恢复或显式人工发布后才解除。标记不修改历史版本，已冻结请求仍按原 Quote 结算。

Sub2API 的 `mode=embedding`、AxonHub 的 `type=embedding` 是允许缺少输出单价的明确证据；其他来源不推测输入型模型。此类价格附带 `usage_scope: "embedding"`，与 PriceVersion 和 Quote 一起冻结，仅允许 embedding 请求，错误请求类型在发送前拒绝。实际输出或其他不适用用量拒绝计价并进入现有异常处理，不能按输出免费结算。省略该字段时保留旧行为及原有哈希序列化。

同步任务结果包含检查、变化、发布、停用和待配置总数。`unconfigured_details` 最多返回 100 个模型及各来源原因，并限制结果体积以适配三种数据库；`unconfigured_count` 始终表示完整总数。即使正文哈希不变，也重新解析和核对待发布结果，允许失败重试与解析规则升级生效。

启动迁移将未标记的历史归属持久化，关闭自动同步也执行归属迁移，但不换价。旧 `billing_pricing_seed.json` 仅供这次幂等识别：必须由 actor 0 创建、模型与定义哈希匹配并通过内容验真才标为 `legacy_seed`；其他未标记旧价按人工价保护。正常查价、发布和来源读取不再加载旧种子。原 `models_dev`、`openrouter` 仅保留历史归属，在首次双源重建时由有效双源价格接管，无法匹配则停用；不允许再抓取、写入缓存或发布这两个来源。

新安装不再播种旧 New API 倍率目录。前端“从参考目录填入”只生成草稿，保存后成为人工价格；“恢复自动定价”经版本校验后重新交由双源管理。UI 最多显示 3 位小数，编辑、请求、快照和导出保留完整精度。


## 审计与成本

`GET /api/billing/reconciliations/:id` 的 `audit` 重放冻结的 Quote 和 Usage，校验版本、报价、尝试指纹、消费指纹及账本事件证据。结果为 `verified`、`mismatch`、`invalid_evidence` 或 `unconfirmed`，不查询实时价格。违规附加费使用保留真实价格版本的派生报价，`charge_kind` 和 `source_quote_hash` 绑定原报价，仍由统一计算器计算。

客户实际收费来自最终账本事件的 `UsedDelta / 500000`，与舍入前的 `ChargeResult.TotalUSD` 分开。Root 可通过 `POST /api/billing/reconciliations/:id/cost` 记录绑定尝试的供应商回执或账单成本；记录不可变且幂等，不影响客户钱包。所有尝试均有明确成本记录（包括零成本）后才报告完整上游成本。利润以已确认客户收费减成本计算，退款或人工调整仍保留真实收支；证据不足时返回 `null`，不从售价或 token 数量猜测成本。

`GET /api/billing/metrics` 提供价格版本数、报价尝试数及按作用域和来源统计的活动价格数。`/api/billing/prices/automatic` 提供逐来源状态和失败原因。日志使用作用域、版本 ID、版本哈希、报价哈希和 ChargeResult 明细；金额及调整项使用 Decimal 字符串，quota 使用整数字符串。旧模型倍率写入入口、可变 PriceData 价格字段和独立浮点结算器已移除。
