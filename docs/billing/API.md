# 账务与模型定价 API

当前安装格式为 schema 5，价格合同版本为 4，计算版本为 `newapi-c71eefbc-v1`。钱包、Token、奖励、统计和事件中的 quota 使用 `common.Quota`（int64 / SQL BIGINT），固定 `500000 quota = 1 USD`。API quota 为十进制整数字符串，例如 `"9007199254740993"`，不能先转 JavaScript Number。无限 Token 的 `available_quota` 为 `null`，仍返回预留和消费。价格编辑、精确小计与舍入差额使用十进制字符串；时间、数量、记录 ID 沿用各端点的整数协议。

响应沿用 `{ "success": true, "data": ... }`。调用方须同时检查 HTTP 状态与 `success`；部分只读配置业务错误沿用上游 HTTP 200、`success:false`。用户端点依据认证用户过滤，`token_id` 仅进一步缩小范围。Root 权限由服务端检查，前端能力开关只控制入口。

## 模型配置与草稿

关闭模型广场时，`GET /api/pricing` 仍允许有效 Root 浏览器会话用于管理。服务端继续核对会话有效期、撤销状态及账户当前角色/状态；不扩大 PAT 的端点范围，也不开放其他关闭模块。普通用户和匿名访问仍按模块配置拒绝。

`model_pricing` 是唯一模型价格编辑来源。初始配置为空；缺价不能执行，显式零价有效。请求边界派生的 `PriceVersion` 是不可变执行证据，没有独立发布或活动价格管理接口。

| 方法与路径 | 权限 | 请求与结果 |
| --- | --- | --- |
| GET `/api/option/model_pricing` | Root | 可用重复 `model` 参数过滤；返回 `entries`、`options`、`empty_version` |
| PATCH `/api/option/model_pricing` | Root | `{changes:[{model_name,expected_version,pricing,reset?}]}`；原子校验并保存，版本冲突 409 |
| POST `/api/option/model_pricing/preview` | Root | `{model_name,pricing}`；返回草稿生效值、缓存模式和定价详情，不保存 |
| POST `/api/option/model_pricing/convert` | Root | 相同草稿输入；返回转换表达式或 `unsupported_reason`，不保存 |
| POST `/api/option/model_pricing/simulate` | Root | 模拟草稿，返回精确 `amount_usd` 字符串及计算详情，不预留、不保存 |
| POST `/api/ratio_sync/fetch` | Root | 并行获取固定来源，逐源返回候选、状态和错误，不写价格 |
| PUT `/api/option/` | Root | 以 `key:tool_price_setting.prices` 保存独立工具价格映射 |
| GET `/api/pricing` | 公开/当前用户权限 | NewAPI 模型行：元数据、倍率或按次价格、表达式、插件变体和 `pricing_version` |

每项 entry 包含 `model_name`、配置内容哈希 `version`、`configured`、`effective`、`cache_write_mode`、`billing_details`、`usage_schema` 和 `plugin_variants`。`options` 的值为 JSON 文本，数字以十进制字符串投影，避免浏览器丢失编辑精度。首次保存使用读取到的 `empty_version`；后续使用对应 entry 的 `version`。

```json
{
  "changes": [
    {
      "model_name": "example-model",
      "expected_version": "配置读取接口返回的内容哈希",
      "pricing": { "ModelRatio": "1.25", "CompletionRatio": "4" }
    }
  ]
}
```

`pricing` 是该模型的完整草稿，缺失字段不会自动补零；`reset:true` 移除该模型配置。支持八个数值键 `ModelPrice`、`ModelRatio`、`CompletionRatio`、`CacheRatio`、`CreateCacheRatio`、`ImageRatio`、`AudioRatio`、`AudioCompletionRatio`，以及 `billing_setting.billing_mode`、`billing_setting.billing_expr`、`billing_setting.plugin_billing_expr`。数字接受非负、有限数字或十进制文本，表达式保留原文。三种模式、单位和辅助默认见[模型定价](PRICING.md)。

模拟请求形状如下；共享任务模型需指定 `plugin_key`，普通模型可以省略：

```json
{
  "model_name": "example-model",
  "pricing": {
    "billing_setting.billing_mode": "tiered_expr",
    "billing_setting.billing_expr": "tier(\"base\", p * 2.5 + c * 10)"
  },
  "usage": { "p": 1000, "c": 200, "len": 1000 },
  "request": { "params": {}, "headers": {} },
  "time": "2026-10-01T00:00:00Z"
}
```

Token 用量支持 `p/c/len/cr/cc/cc1h/img/img_cr/img_o/ai/ai_cr/ao/reasoning` 和 `image_count`；计数必须是有界非负整数，图片数量为 1–128。`p/c` 接收完整输入/输出计数，宿主按表达式使用的子类规范化一次。任务 `usage` 允许插件声明的数字、十进制文本、枚举文本及布尔事实。Token 模式的 `request.params` 和 `headers` 是表达式模拟输入；任务模式与实际运行一致，只读取插件用量事实，忽略请求参数和头。`time` 可选且为 RFC3339，不传时使用本次模拟时刻。模拟使用相同报价、计算器和临时 `preview` 证据绑定，响应只返回金额与必要详情，不返回请求正文、头或加密证据。

## 自动获取

请求可省略 `sources`，或使用空数组取得全部五源；指定时只接受 `sub2api`、`axonhub`、`basellm`、`models_dev`、`openrouter`，未知或重复 ID 返回 400。响应合同：

```typescript
{
  success: true,
  data: {
    sources: Array<{
      id: string
      name: string
      status: 'success' | 'partial' | 'error'
      models: Array<{
        model_name: string
        provider: string
        pricing: Record<string, unknown>
        unsupported_reason?: string
        warnings?: string[]
      }>
      errors: string[]
    }>
  }
}
```

不可支持条目的 `pricing` 为空。总体 `success:true` 表示已返回各源结果，不代表全部来源成功。单源下载或解析失败保留明确 `error`，没有历史快照回退。固定地址、超时、体积限制与语义边界见[来源说明](PRICING.md#自动获取只填入草稿)。

管理员在页面选择单一来源及模型范围，同名不同 provider 只能选一个，确认后填入前端草稿。获取不保存，不混合字段，不择最低价；最终仍通过 PATCH 模型配置生效。没有后台自动发布、定时同步任务或来源优先级。

工具价格独立保存为完整 JSON 映射，例如 `{ "key":"tool_price_setting.prices", "value":"{\"web_search\":10}" }`，单位 USD/千次调用。映射内部使用非负有限 JSON 数字。`tool_name:model_prefix*` 按最长前缀覆盖 `tool_name`，显式零有效。初始工具价格为空，声明工具缺价时拒绝发送；实际调用量随冻结工具价格结算。

## 预留、事件与结算观察

任务实际结算、估算结算及失败释放日志的 `other.is_task` 为 `true`。已保存的结算详情读取 `charge_result`；历史日志不按当前模型定价重算。

| 方法与路径 | 权限 | 请求与结果 |
| --- | --- | --- |
| GET `/api/billing/self/reservations` | 登录用户 | 当前预留，支持 offset、limit、token_id |
| GET `/api/billing/self/events` | 登录用户 | 不可变流水，支持 offset、limit、token_id |
| GET `/api/log/` | 管理员 | 消费日志；`other.charge_result` 为结算费用，`other.admin_info.usage_billing_path` 标明实际、估算或 `frozen_estimate` 依据 |
| GET `/api/gateway/metrics` | Root | `billing.settlements_total` 按依据计数，`billing_recovery` 为超时未结束记录积压 |

列表返回 `{items,total}`；limit 默认 20、最大 100。预留项包含 `request_id`、`user_id`、`token_id`、`state`、`funding_amount`、`token_amount`、`version`、时间和 `last_error`。账务不提供人工结算、释放或成本登记接口，每个请求按[结算兜底与恢复](RECOVERY.md)自动结束。

求值输入使用 AES-GCM 与 HKDF 加密，绑定请求 ID、真实 attempt、配置哈希及 `estimate/settlement` 阶段。准备结算意图时解密重跑同一计算器，验证报价、用量与费用；不一致时改按冻结估算结算，不根据外部金额补造证据。日志和公开 API 不返回解密请求正文、原始头、密钥或证据密文。稳定 `CRYPTO_SECRET` 与 schema 5 的 `EvidenceKeyID` 必须匹配，见[账务架构](IMPLEMENTATION.md)。

资金事务成功后，即使日志库不可用也不会再次扣款；记录保留待投递状态，由恢复任务完成投递。管理员设置余额必须携带操作 ID、预期账户版本，通过差额事件记账。支付、兑换和奖励继续维护资格和业务凭据，并与钱包事件使用同一事务。返利额度仍归返利权益账本管理，转入钱包时建立关联事件。

易支付报价、下单、回调和管理员补单遵循[易支付契约](EPAY.md)。不提供用户验付或后台主动查单；异常由外部网关重试通知或管理员核查补单。预期 quota 冻结于订单快照，实际 `credited_quota` 由钱包事件生成；支付金额以人民币分保存和比较，不能从展示设置反推入账额度。
