# 计价接口

`PriceVersion` 是不可变模型价格版本，`Quote` 冻结版本、完整价格、渠道售价系数、安全属性、发送时间和违规附加费规则；`Usage` 保存规范化数量、来源和完整性，`ChargeResult` 保存分项、精确总额、最终额度和计算版本。这三类证据分别序列化，不能覆盖为同一快照。

Root 价格管理接口：

| 请求 | 结果 |
| --- | --- |
| `GET /api/billing/prices` | `{success:true,data:PriceVersion[]}` |
| `POST /api/billing/prices`，正文 `{price:PriceVersion,expected_version:number}` | 发布后返回 `{success:true,data:PriceVersion}`；版本冲突返回 409 |
| `GET /api/billing/prices/seed` | `{success:true,data:{prices:PriceVersion[],unconfigured:string[],source:string}}` |

`PriceVersion = {id:number,model:string,mode:'unit_price'|'expression'|'free',prices:UnitPrice[],expression?:string,created_at:number,hash:string}`。发布时 ID、创建时间、哈希由服务器设置；新模型 `expected_version=0`，更新时传当前 ID。没有原地修改和删除版本接口。

`UnitPrice = {quantity:string,unit:'per_million_tokens'|'per_thousand_calls'|'per_call'|'per_image'|'per_second',price:string}`。`price` 是 USD 十进制字符串。quantity 为 `p,c,cr,cc,cc1h,img,img_o,ai,ao,calls,images,seconds,tool:<工具名>`，单位必须与数量匹配。表达式模式的 prices 只用于额外工具、按张、按次、按秒费用，token 项由表达式负责。

表达式沿用 USD/百万 token 的计算约定，结果除以一百万得到 USD；例如 `p * 2 + c * 8`。属性用 `attr("service_tier")` 访问，只允许已登记安全属性；禁止 `param`、`header`。时间函数只读取 Quote.FrozenAt，时区必须有效。详细语法见 [表达式说明](../billingexpr/expr.md)。

纯计算入口 `billing.Calculate(*Quote, Usage) (*ChargeResult,error)` 只读冻结数据，不访问数据库或配置。全部项目先用 Decimal 合计，再乘渠道系数与 `500000`，只对总额四舍五入一次。正数最低收 1 quota，明确为零收 0；超过 MaxInt32 的单请求费用返回错误，交由账务对账处理。`ChargeResult.FinalQuota` 为 `common.Quota`，JSON 是十进制整数字符串。`billing.RoundCharge` 供独立附加费使用；`billing.RequestQuota` 是原 relay int 接口的有界转换。

接入点为 `info.PriceData.Quote`、`.Usage`、`.Charge`。文本与音频通过 `service.NormalizedBillingUsage` 统一事实；最终预算检查在 `AnalyzeOutboundTokenBudget`、`FinalImagePriceMeta`、`FinalAudioPriceMeta` 冻结已发送参数中的安全属性。任务使用 `model.TaskBillingContext.Quote/Usage`，终态计算不得调用适配器货币倍率或重新查询价格。

主库建表包含 `model.BillingPriceVersion` 和 `model.BillingActivePrice`，空库初始化调用 `model.SeedBillingPriceVersions(ctx)`。种子来自已检视本地默认配置的确定性导出，固定在 `model/billing_pricing_seed.json`，运行时不读取旧倍率或联网取价。目录有 254 个可转换模型和 13 个待配置项目；后者没有自动价格回退。离线重新生成工具为 `go run ./pkg/billing/cmd/exportseed`。

公开 `GET /api/pricing` 只返回已发布价格且当前路由可用的模型，保留描述与 channel_ratio_min/max，提供完整 `price_version`，`billing_mode` 使用新模式。旧 model_ratio/model_price/completion_ratio 字段不再作为公开计价契约。
