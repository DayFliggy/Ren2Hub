# NewAPI 定价合同

模型价格、分组倍率、默认值、预扣和结算均跟随固定 NewAPI 基线。`setting/ratio_setting/` 提供内置模型与辅助倍率；`setting/billing_setting/`、`setting/task_pricing_setting/` 和 `pkg/billingexpr/` 提供表达式与任务定价。缺项按原生规则继承默认，显式零与缺项不同。重置模型配置恢复原生默认行为，不是强制变为未定价。

| 模式 | 原生语义 |
| --- | --- |
| 模型倍率 | 输入及输出、缓存、图片、音频倍率；叠加有效用户/Token 分组倍率 |
| 按次价格 | `ModelPrice` 与对应端点数量或费用规则 |
| 表达式 | 原生 `tiered_expr`、请求上下文、用量变量、任务 `u(...)` 与插件覆盖 |

模型编辑使用 `GET/PATCH /api/option/model_pricing`，通过 `expected_version` 拒绝覆盖并发更新；预览和转换接口只读草稿。有效值和公开 `/api/pricing` 均投影原生配置。不存在旧 PriceVersion 发布目录、独立 Quote/Charge 证据和渠道售价系数。

价格同步使用原生 `controller/ratio_sync.go`：读取上游渠道或内置预设，保留原生预览与应用流程。当前预设包括 basellm 与 models.dev；不能继续使用已退役的五源固定下载合同。以实际返回的来源与差异为准，不假定外部价格就是供应商结算账单。

工具价格使用原生 `tool_price_setting.prices` 及其默认规则。分组倍率包括原生分组与用户专属配置；Token 自动组选择同时影响授权和有效倍率。私有渠道列表只缩小授权候选，不创建新的价格或组。金额 API 和前端字段采用原生数值合同，不补造 `available_quota`、`reserved_quota` 或账户版本。

预扣与最终结算的公式、用量规范化和舍入以 `relay/helper/price.go`、`service/text_quota.go`、`service/tiered_settle.go`、`service/task_billing.go` 为准。不要用一套自定义 Decimal 公式替换所有分支，也不要为统一展示改变实际计费。插件 ABI 与用量声明见[插件文档](../plugin-api/README.md)。
