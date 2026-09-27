# 计价接口

## 结构化价格

`PriceVersion` 可选 `flat_fee`（一次请求的 USD 固定费）和 `effective_from`/`effective_to`（RFC3339 窗口）。发布立即生效，拒绝未来开始或已到期的版本，没有排期切换器。创建报价须落在窗口内，结算只读冻结版本和 `FrozenAt`。

`UnitPrice` 可使用 `tier_mode:'volume'|'graduated'` 与 `tiers:[{up_to?,unit_price}]`。`volume` 按完整数量选择一个档位；`graduated` 按各档实际占用数量累加。`up_to` 必须严格递增，最后一档可省略以表示无上限；有限档位超出范围时结算失败。阶梯价不能同时填写 `price`，固定单价仍使用原有 `price` 字段。

`Usage.ReasoningTokens` 是规范化输出用量。声明 `reasoning` 单价时，该数量会从普通 completion 数量中排除后独立计费；数量必须非负且不超过 `OutputTokens`。表达式和阶梯金额在最终转为 quota 前经过有限性、范围和 Decimal 校验。
`PriceVersion` 是不可变模型价格版本，`Quote` 冻结版本、完整价格、渠道售价系数、安全属性、发送时间和违规附加费规则；`Usage` 保存规范化数量、来源和完整性，`ChargeResult` 保存分项、精确总额、最终额度和计算版本。这三类证据分别序列化，不能覆盖为同一快照。

Root 价格管理接口：

| 请求 | 结果 |
| --- | --- |
| `GET /api/billing/prices` | `{success:true,data:PriceVersion[]}` |
| `POST /api/billing/prices`，正文 `{price:PriceVersion,expected_version:number}` | 发布后返回 `{success:true,data:PriceVersion}`；版本冲突返回 409 |
| `GET /api/billing/prices/seed` | `{success:true,data:{prices:PriceVersion[],unconfigured:string[],source:string}}` |
| `POST /api/billing/prices/preview` | 只读预览，返回 `{success:true,data:{quote:Quote,charge:ChargeResult}}`；不发布、不预留、不调用供应商 |

`PriceVersion = {id:number,scope_kind:'global_model'|'channel_model',channel_id?:number,model:string,mode:'unit_price'|'expression'|'free',prices:UnitPrice[],expression?:string,created_at:number,hash:string}`。发布时 ID、创建时间、来源和哈希由服务器设置；首次发布该作用域时 `expected_version=0`，更新时传该作用域的当前 ID。没有原地修改和删除版本接口。查价先精确匹配渠道作用域，再使用已明确发布的全局作用域；渠道价格的报价系数固定为字符串 `"1"`。

安装格式为 4，不迁移旧数据库。PriceVersion 包含 `contract_version:3`、`calculation_version:'billing-v3'`、`result_unit:'micro_usd'|'usd'`、可选 `usage_schema`、`metering`、`provenance`、`support` 和 `origin/source_model/source_hash`。Quote 另冻结 `request_facts` 和时间函数的 `calendars`，均纳入哈希。新草稿可省略当前版本字段，由服务器物化；已保存价格与报价缺失或未知版本直接拒绝。

人工发布必须提供 `provenance:{kind:'retail',retail_reason:string}` 或 `{kind:'evidence',fields:[{target,source_path,raw_value,source_unit,conversion}]}`。retail 理由非空且不能混入快照 ID 或字段证据；evidence 至少一项，五个字段均是非空字符串。自动 source 证据由服务器绑定不可变快照 ID、解析器版本和字段映射，不能通过人工入口伪造。`support:{dimensions:string[],service_tiers:string[]}` 明确适用维度和档位；非默认档位须有相应表达式政策，不提供隐式倍率。

可选 `usage_scope:'embedding'` 将价格限定到已有向量请求；缺省为空且序列化省略。只接受当前合同，不兼容旧价格或报价。该范围仅接受无任务 schema 的输入 token 单价、v1 输入 token 表达式或明确免费，禁止输出 token、工具、按张和其他非 token 收费。报价前核对请求类型，原生 Gemini 向量请求仍保留原协议模式；冻结后的范围参与哈希，任何实际输出 token（包括图片和音频输出）或非零非 token 数量均拒绝结算。

预览正文为 `{price,usage,attributes,request_facts,channel_ratio,frozen_at}`；渠道倍率为明确十进制字符串，时间为非零 RFC3339，载荷上限 256 KiB。调用 `NewQuoteWithFacts` 和实际 `Calculate`，无需数据库写入。schema 数量使用 `estimated_facts` 或 `actual_facts`，不能同时在 `quantities` 再次表示。numeric facts 保持精确十进制字符串，表达式直接使用 Decimal。

固定单价结构为 `UnitPrice={quantity,unit,price}`；阶梯以 `tier_mode/tiers` 替代 `price`。token 维度为 `p,c,reasoning,cr,cc,cc1h,img,img_cr,img_o,ai,ai_cr,ao`，使用 `per_million_tokens`。`calls/tool:<工具名>` 使用 `per_call/per_thousand_calls`，`images` 使用 `per_image`，`seconds` 使用 `per_second/per_minute/per_hour`，`characters` 使用 `per_character/per_million_characters`，`credits` 使用 `per_credit`。金额及非 token 数量为 Decimal 字符串，不跨物理维度转换。表达式拥有 token 项，不得重复添加 token 单价。

物理音频计量须显式声明 `metering:{request_kind,quantity,source,method,reservation_quantity?}`。合法组合为 `speech/characters/request_fixed/unicode_codepoints`、`speech/characters/completion_fact/provider_usage` 和 `transcription/seconds/completion_fact/provider_duration`。completion_fact 必须给出正数 `reservation_quantity`；request_fixed 禁止该字段。价格仅能为无 schema 的 unit_price，单价维度限合同数量及 calls。该合同不等于所有渠道都能提供事实，当前具体适配与未支持产品见[定价边界](../../docs/billing/PRICING.md)。

v1 token 表达式结果除以一百万得到 USD，例如 `p * 2 + c * 8`。v2 schema 表达式直接返回 USD，例如 `v2:u("seconds") * 0.4`，禁止混用 token/fixed。schema 单价必须对应声明的最终数值事实。字符串属性用 `attr("service_tier")`，数值属性用 `attr_num("duration")`；禁止任意正文和请求头访问。时间函数在创建 Quote 时固化日历，重放不依赖当前时钟或时区库。详细语法见[表达式说明](../billingexpr/expr.md)。

v1 按张价格使用 `fixed(usd) * image_count`；预览/计价传 `usage.quantities.images` 十进制整数字符串，范围 1–128，缺失或零不按默认数收费。该表达式不得再带 images/per_image 附加行；服务器校验全部分支。请求固定数量仍在 Quote.Attributes.quantity，实际数写 Trace.image_count，两者分别保留。示例用量：`{"quantities":{"images":"2"},"complete":true}`。Vue 的单价转换先显示草稿预览，显式确认后才应用，保存才发布新版本。

纯计算入口 `billing.Calculate(*Quote, Usage) (*ChargeResult,error)` 只读冻结数据，不访问数据库或配置。全部项目先用 Decimal 合计，再乘渠道系数与 `500000`，只对总额四舍五入一次。正数最低收 1 quota，明确为零收 0；超过 MaxInt32 的单请求费用返回错误，交由账务对账处理。`ChargeResult.FinalQuota` 为 `common.Quota`，JSON 是十进制整数字符串。`billing.RoundCharge` 是统一计算器的舍入边界；违规附加费也通过派生 Quote 和 `Calculate` 计价。`billing.RequestQuota` 是原 relay int 接口的有界转换。

接入点为 `info.PriceData.Quote`、`.Usage`、`.Charge`。文本与音频通过 `service.NormalizedBillingUsage` 统一事实；最终预算检查在 `AnalyzeOutboundTokenBudget`、`FinalImagePriceMeta`、`FinalAudioPriceMeta` 冻结已发送参数中的安全属性。任务使用 `model.TaskBillingContext.Quote/Usage`，终态计算不得调用适配器货币倍率或重新查询价格。

主库包含 `BillingPriceVersion`、`BillingActivePrice`、`BillingPricingSource`、`BillingPricingSnapshot` 和 `BillingPricingConfig`。启动安装缺失的离线目录并发布有效全局价，系统任务默认每 30 分钟检查远端。人工价优先，同身份先 Sub2API 后 AxonHub；单源不可用不阻断其他来源，更新不改冻结证据。详细规则见[自动定价](../../docs/billing/PRICING.md)。

自动定价管理均要求 Root：`GET /api/billing/prices/automatic` 返回 `{config,sources,origins}`；`PUT` 同地址提交 `{enabled,check_interval_minutes}`（5–1440）。`POST /automatic/sync` 返回标准系统任务，按任务 API 查询结果；关闭计划不禁止手动检查。`POST /automatic/restore` 提交 `{model,expected_version}` 恢复当前目录价，版本冲突 409。`GET /prices/seed` 保留接口形状，但返回当前合并参考目录及来源哈希，不再返回旧种子。所有路径均以 `/api/billing/prices` 为基准。

`GET /api/billing/prices/automatic/snapshots?source=sub2api&limit=20` 返回归档元数据数组；省略 source 可查两个来源。`GET /api/billing/prices/automatic/snapshots/:id` 返回元数据与 `document` 原文字符串；加 `?download=1` 返回 JSON 附件及 `X-Pricing-SHA256`。三者均要求 Root，读取原文先核验 SHA；只有元数据不能重放目录。来源状态另含 `snapshot_id/parser_version/snapshot_count`。

公开 `GET /api/pricing` 每个模型返回 `{model_name,...metadata,offers:[...]}`，只含调用方可路由、当前有效的已发布价格。每份 offer 为 `{offer_id,priced_model,price_version,conditions,adjustments,display_prices,display_flat_fee?,availability}`。`price_version` 是公开投影，含版本 ID/hash、计价合同、单价、表达式、窗口、schema/metering，省略渠道 ID 和来源证据；其 hash 指向原始冻结版本，不能对投影重新验原始哈希。`conditions` 含可用的 usage_scope、dimensions、service_tiers、context_rule；`availability.status='available'`。

`adjustments=[{kind:'channel_multiplier',multiplier:string}]` 与服务端精确计算的 `display_prices/display_flat_fee` 区分原价和售价。表达式保持原文，调整另列，不伪造单一 token 价。请求模型别名不必等于 `priced_model`；同版本不同倍率保留独立 offer，同价同版本可去重。`offer_id` 是不透明哈希，禁止借其推断渠道身份。旧 `price_version/price_versions/billing_mode` 模型顶层字段已移除。

Root 对账接口 `GET /api/billing/reconciliations/:id` 返回 `audit` 和 `upstream_cost`。`audit` 校验冻结价格、报价、尝试的 request_fingerprint、完整消费指纹及最终账本事件，并重算明细；状态为 `verified|mismatch|invalid_evidence|unconfirmed`。请求指纹绑定账务请求身份，不提供跨请求 ID 的正文去重。重放不读取实时目录，不修改钱包或恢复状态。

`POST /api/billing/reconciliations/:id/cost` 正文为 `{event_id,attempt_id,amount_usd,source,reference}`。`amount_usd` 是非负 Decimal 字符串，`source` 为 `invoice` 或 `provider_receipt`，`reference` 是外部凭据引用。只允许为已 settled/refunded 请求登记属于该请求的尝试成本，每个尝试一条不可变记录；重复相同事件幂等，冲突返回 409，不影响客户余额。全部尝试的成本明确后报告 `upstream_cost_usd`；客户实收也确认后才报告 `profit_usd`，否则为 null。

`ChargeResult` 增加 `base_usd` 与 `adjustments:[{kind,multiplier,base_usd,amount_usd,result_usd}]`。渠道调整的 kind 为 `channel_multiplier`，amount_usd 可为负的折扣增减额；最终费用仍不可为负，倍率 1 也保留记录。

`GET /api/billing/metrics` 包括 `price_versions`、`quote_attempts`、`prices_by_origin:[{scope_kind,origin,count}]`，以及可选 `operational` 当前实例累计指标。来源标签有界，不使用模型名或请求 ID 作指标标签；来源健康及失败仍由自动定价接口提供。
