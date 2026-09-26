# 计价接口

`PriceVersion` 是不可变模型价格版本，`Quote` 冻结版本、完整价格、渠道售价系数、安全属性、发送时间和违规附加费规则；`Usage` 保存规范化数量、来源和完整性，`ChargeResult` 保存分项、精确总额、最终额度和计算版本。这三类证据分别序列化，不能覆盖为同一快照。

Root 价格管理接口：

| 请求 | 结果 |
| --- | --- |
| `GET /api/billing/prices` | `{success:true,data:PriceVersion[]}` |
| `POST /api/billing/prices`，正文 `{price:PriceVersion,expected_version:number}` | 发布后返回 `{success:true,data:PriceVersion}`；版本冲突返回 409 |
| `GET /api/billing/prices/seed` | `{success:true,data:{prices:PriceVersion[],unconfigured:string[],source:string}}` |
| `POST /api/billing/prices/preview` | 只读预览，返回 `{success:true,data:{quote:Quote,charge:ChargeResult}}`；不发布、不预留、不调用供应商 |

`PriceVersion = {id:number,model:string,mode:'unit_price'|'expression'|'free',prices:UnitPrice[],expression?:string,created_at:number,hash:string}`。发布时 ID、创建时间、哈希由服务器设置；新模型 `expected_version=0`，更新时传当前 ID。没有原地修改和删除版本接口。

安装格式 2 的 PriceVersion 还包含 `contract_version:2`、`calculation_version:'billing-v2'`、`result_unit:'micro_usd'|'usd'`、可选 `usage_schema`；Quote 同样包含合同和计算器版本，以及可选 `request_facts`。全部进入哈希。新草稿可省略当前版本字段，由服务器物化；已保存价格与报价缺失或未知版本直接拒绝。

可选 `usage_scope:'embedding'` 将价格限定到已有向量请求；缺省为空且序列化省略，旧价格和报价的哈希保持不变。该范围仅接受无任务 schema 的输入 token 单价、v1 输入 token 表达式或明确免费，禁止输出 token、工具、按张和其他非 token 收费。报价前核对请求类型，原生 Gemini 向量请求仍保留原协议模式；冻结后的范围参与哈希，任何实际输出 token（包括图片和音频输出）或非零非 token 数量均拒绝结算。手动价格未声明该范围时保留既有合同。

预览正文为 `{price,usage,attributes,request_facts,channel_ratio,frozen_at}`；渠道倍率为明确十进制字符串，时间为非零 RFC3339，载荷上限 256 KiB。调用 `NewQuoteWithFacts` 和实际 `Calculate`，无需数据库写入。schema 数量使用 `estimated_facts` 或 `actual_facts`，不能同时在旧 `quantities` 再次表示。numeric facts 以规范十进制字符串保存，表达式求值时才转换浮点。

`UnitPrice = {quantity:string,unit:'per_million_tokens'|'per_thousand_calls'|'per_call'|'per_image'|'per_second',price:string}`。`price` 是 USD 十进制字符串。quantity 为 `p,c,cr,cc,cc1h,img,img_o,ai,ao,calls,images,seconds,tool:<工具名>`，单位必须与数量匹配。表达式模式的 prices 只用于额外工具、按张、按次、按秒费用，token 项由表达式负责。

v1 token 表达式沿用 USD/百万 token 的计算约定，结果除以一百万得到 USD；例如 `p * 2 + c * 8`。v2 schema 表达式直接返回 USD，例如 `v2:u("seconds") * 0.4`，禁止混用 token/fixed。schema 单价 quantity 必须声明为最终数值事实，unit 为 `per_` 加声明单位。属性用 `attr("service_tier")` 访问，只允许已登记安全属性；禁止 `param`、`header`。时间函数只读取 Quote.FrozenAt，时区必须有效。详细语法见 [表达式说明](../billingexpr/expr.md)。

v1 按张价格使用 `fixed(usd) * image_count`；预览/计价传 `usage.quantities.images` 十进制整数字符串，范围 1–128，缺失或零不按默认数收费。该表达式不得再带 images/per_image 附加行；服务器校验全部分支。请求固定数量仍在 Quote.Attributes.quantity，实际数写 Trace.image_count，两者分别保留。示例用量：`{"quantities":{"images":"2"},"complete":true}`。Vue 的单价转换先显示草稿预览，显式确认后才应用，保存才发布新版本。

纯计算入口 `billing.Calculate(*Quote, Usage) (*ChargeResult,error)` 只读冻结数据，不访问数据库或配置。全部项目先用 Decimal 合计，再乘渠道系数与 `500000`，只对总额四舍五入一次。正数最低收 1 quota，明确为零收 0；超过 MaxInt32 的单请求费用返回错误，交由账务对账处理。`ChargeResult.FinalQuota` 为 `common.Quota`，JSON 是十进制整数字符串。`billing.RoundCharge` 供独立附加费使用；`billing.RequestQuota` 是原 relay int 接口的有界转换。

接入点为 `info.PriceData.Quote`、`.Usage`、`.Charge`。文本与音频通过 `service.NormalizedBillingUsage` 统一事实；最终预算检查在 `AnalyzeOutboundTokenBudget`、`FinalImagePriceMeta`、`FinalAudioPriceMeta` 冻结已发送参数中的安全属性。任务使用 `model.TaskBillingContext.Quote/Usage`，终态计算不得调用适配器货币倍率或重新查询价格。

主库建表包含 `model.BillingPriceVersion`、`model.BillingActivePrice`、`model.BillingPricingSource`、`model.BillingPricingConfig`。主节点启动调用 `service.InitializeAutomaticPricing` 安装缺失的双源离线目录，并发布有效自动价；随后由系统任务默认每 30 分钟检查远程哈希。来源优先级为人工价 > Sub2API > AxonHub > models.dev > OpenRouter。快照更新不能改写已冻结的 PriceVersion/Quote；旧种子仅用于升级识别，详细规则见[自动定价](../../docs/billing/PRICING.md)。

自动定价管理均要求 Root：`GET /api/billing/prices/automatic` 返回 `{config,sources,origins}`；`PUT` 同地址提交 `{enabled,check_interval_minutes}`（5–1440）。`POST /automatic/sync` 返回标准系统任务，按任务 API 查询结果；关闭计划不禁止手动检查。`POST /automatic/restore` 提交 `{model,expected_version}` 恢复当前目录价，版本冲突 409。`GET /prices/seed` 保留接口形状，但返回当前合并参考目录及来源哈希，不再返回旧种子。所有路径均以 `/api/billing/prices` 为基准。

公开 `GET /api/pricing` 只返回已发布价格且当前路由可用的模型，保留描述与 channel_ratio_min/max，提供完整 `price_version`，`billing_mode` 使用新模式。旧 model_ratio/model_price/completion_ratio 字段不再作为公开计价契约。
