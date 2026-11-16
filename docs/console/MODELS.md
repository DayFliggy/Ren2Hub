# 模型广场

`/models` 面向登录用户，展示当前用户能用的渠道和每个渠道上的模型报价。页面读取两个接口，分别加载，互不阻塞：

- `GET /api/user/model-catalog`（`service/model_catalog.go` 的 `BuildModelCatalog`）返回渠道、模型和每个“模型 × 渠道”的报价。报价是模型基础价和该渠道的 `channel_ratio`，前端按 `frontend/src/utils/modelMarket.ts` 投影成按 Token、按次或动态表达式的价格行。可选的 `token_id` 把目录收窄到某个 Key 的路由范围。
- `GET /api/perf-metrics/summary?hours=24` 是原生的按模型聚合指标，跨全部渠道统计。它失败时只在指标位置显示错误，不隐藏价格。

页面有三种视图：按模型聚合、按渠道分货架、比价矩阵，选择保存在 localStorage 的 `ren2hub_models_view`。筛选项是关键词、渠道、计费方式、模型类型（由端点推断）和价格排序。目录刷新失败时清空旧数据再显示错误，避免展示上一个权限范围的结果。

价格只来自管理员配置，见[定价](../billing/PRICING.md)。没有配置价格的报价标为未配置并单独提示，不显示成零价；缺少指标时显示为空，不伪造延迟或成功率。模型出现在目录里不保证任意端点都可执行。

公开的价格页 `/pricing` 是另一条路径，读取原生 `/api/pricing`。Root 的模型定价编辑在 `/models/pricing`，走原生价格配置与同步接口，获取和预览候选不等于保存，见[计费界面契约](../../frontend/docs/BILLING.md)。
