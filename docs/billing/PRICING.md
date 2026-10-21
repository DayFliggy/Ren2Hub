# 模型价格与渠道倍率

模型基础价格、缺项继承、默认值、用量规范化和舍入沿固定 NewAPI 核心。`setting/ratio_setting/` 提供模型及辅助倍率，`setting/billing_setting/`、`setting/task_pricing_setting/` 和 `pkg/billingexpr/` 提供表达式与原生任务定价。缺项与显式零不同；重置配置恢复原生默认。

每个渠道的 `channel_ratio` 必须大于 0、最多 18 位小数、最大 1000，默认 `"1"`。每 Key 的 `max_channel_ratio` 可为空；它仅限制候选，不创建第二份售价。业务分组、组间倍率及充值分组倍率已移除。

| 模式 | 接入位置 |
| --- | --- |
| 按量 | 原生输入、输出、缓存、图像、音频、工具费用合成后，最终取整前应用一次渠道倍率 |
| 按次 | `ModelPrice` 与原生数量规则应用渠道倍率 |
| 表达式及任务 | 保留原生表达式和用量语义，独立绑定渠道倍率，不能放进会被适配器替换的 OtherRatios |

模型编辑仍使用 `GET/PATCH /api/option/model_pricing` 和 `expected_version`。目录 `/api/user/model-catalog` 对基础价与各渠道报价提供同一展示/排序投影，区分 configured、default、zero、unavailable。只投影能静态确定的表达式单价；动态条件、上下文倍率不能猜成 1。目录十进制字符串不构成结算精度升级。

价格同步在原生 `/api/ratio_sync/fetch` 增量支持固定来源纯解析，`/api/ratio_sync/sources` 列出来源。本地来源只有 `sub2api` 和 `axonhub` 两种格式到表达式的转换；basellm、models.dev 走原生 `ratio_sync` 预设上游，OpenRouter 走已配置 Key 的 OpenRouter 渠道。草稿给出下载内容的 SHA-256，与来源发布的 `.sha256` 比对结果作为来源级警告，不改变状态。sub2api 中已知但本地不计费的字段（如 `cache_storage_cost_per_token_per_hour`）跳过并在草稿中警告，其他未知计价字段仍然拒收。结果只进入草稿，保存后才生效；渠道上游抓取路径继续保留。抓取失败、部分不可解析和明确零价分别表达，不自动覆盖售价。

渠道倍率无效时，`HandleChannelRatio` 返回错误，`ModelPriceHelper`、`ModelPriceHelperPerCall` 和任务表达式分支都拒绝请求（任务返回 400 `model_price_error`），不预扣、不发上游，也不按 0 计费。

所有金额 API 沿原生数值 quota，不补造旧 `available_quota`、`reserved_quota` 或钱包版本。配置生成、公开展示和排序不能反向决定路由。实际结算以 `relay/helper/price.go`、`service/text_quota.go`、`service/tiered_settle.go`、`service/task_billing.go` 为准。
