# 计费界面与金额契约

本界面对应账务格式 5。额度字段使用有符号 int64 范围内的十进制整数字符串，换算固定为 `500000 quota = 1 USD`。接口不会同时读取旧余额字段；用户使用 `available_quota`、`reserved_quota`、`used_quota`，Token 使用相同字段，其中无限 Token 的 `available_quota` 必须为 `null`。账户版本字段为 `quota_version`。

展示数字最多保留 3 位小数。`formatQuota` 与 Decimal 展示函数统一限制显示精度；原始 quota 整数、计价配置、快照与编辑输入保留原始精度。比如额度 `9007199254740993` 在列表显示 `$18,014,398,509.482`，编辑时仍是 `18014398509.481986` USD，保存后仍是原始整数。只读证据 JSON 使用展示投影，不会改写原始报价、用量或结果；表达式文本保持原样。

金额输入、加总与比较使用 BigInt，明确单价及渠道系数展示使用 Decimal 字符串运算。`quotaForChart` 只在图表呈现边界转换为 Number；不能将图表数据作为资金更新的输入。金额溢出、数字类型的 quota、指数写法、非规范字符串及转换后并非整数 quota 的输入均明确拒绝。

## 钱包与 Token

钱包显示可用额度、在途预留和累计已结算消费，并同时保留原始 quota 整数，方便核对低于显示精度的金额。资金流水和预留列表独立分页，加载失败显示“账务数据暂不可用”。Token 明细以 `token_id` 限定查询，显示 Token 的预算变动字段；无限 Token 仍展示预留和消费。

`GET /api/billing/self/reservations` 与 `GET /api/billing/self/events` 使用 `offset`、`limit` 和可选 `token_id`，返回 `{ items, total }`。金额字段只接受字符串。Token 预算修改提交 `available_quota`、`unlimited_quota`、`operation_id` 与 `expected_version`；无限额度输入提交 `available_quota: "0"`，服务端响应返回 `null`。同一界面内重试相同修改保留相同操作 ID。

管理员调账提交增减差额、原因、稳定操作 ID 和预期版本；界面不会直接设置钱包投影。兑换码创建中的 `amount` 是十进制 USD 字符串。返利转入中的 `amount` 则是 quota 整数字符串，沿用该业务 API 的单位约定。

## 模型定价

Root 在 /system-settings/billing/model-pricing 维护 NewAPI 模型定价。页面沿用模型列表与右侧编辑器布局，手机使用抽屉；支持模型与未定价模型、可视化与 JSON、按 Token、按次、表达式、币种显示、缓存/图像/音频价格、阶梯与请求条件、任务 usage schema 与插件变体。元数据编辑只保存元数据。

GET /api/option/model_pricing 返回 entries、options、empty_version；PATCH 同一路径提交 changes，每项为 model_name、expected_version、pricing 和可选 reset。唯一价格源为管理员保存的 11 项模型定价配置；保存冲突保留草稿。空安装无价格，清空不恢复内置目录或默认模型主价，空表达式不能当作免费；显式填写 0 才表示免费。实际模式为 ratio、per_call、expression；对外 billing_mode 使用 ratio 或 tiered_expr。

按次/倍率转表达式通过 POST /api/option/model_pricing/convert 生成待确认草稿，现有保存操作才生效。POST /api/option/model_pricing/preview 解释有效价格，/simulate 使用草稿、用量、原始请求参数、请求头、时间和可选 plugin_key 计算费用，不调用网关或扣费。表达式沿用上游 param/header/time 语义；任务表达式直接使用 u 字段，USD 单位由任务计价上下文确定，不添加 v2 前缀。

“自动获取”通过 POST /api/ratio_sync/fetch 拉取 Sub2API、AxonHub、basellm、models.dev、OpenRouter，展示来源状态与失败原因。管理员选择一个来源及模型范围，核对覆盖项后只回填当前草稿；点击“保存模型价格”才写入。来源没有默认优先级、不自动选择最低价格、不拼接来源；不支持的条目禁选，同一模型 ID 的多供应商候选必须消歧。大目录分页展示，取消、失败和过期响应均不得丢失既有草稿。

工具价格仍由通用 option 的 tool_price_setting.prices 保存，格式为工具名到 USD/千次调用单价的数值映射，支持表格与 JSON。初始为空，未填写的单价不能隐式成为 0；显式 0 保留。未定价收费工具由后端拒绝。

公开 /api/pricing 使用上游扁平模型定价字段，Vue 公开价格页和模型目录共享解释；表达式显示原式，不伪造固定 Token 单价。每千/每百万 Token 切换只改变显示尺度。渠道售价倍率由后端应用一次；前端不再管理独立渠道价格、活动 PriceVersion 指针、来源证据发布或定时自动发布。账务执行仍保存冻结计价配置和加密求值证据，用于实际结算与恢复。

## 人工对账

`/billing/reconciliations` 同时要求 Root 和 `billing_reconciliation: "live"` 能力。普通管理员与能力关闭时的 Root 均不能进入页面，服务端权限仍是最终边界。

列表默认显示待处理账务。打开详情后核对请求 ID、账户、Token、冻结报价、各次上游尝试的报价及哈希、规范用量、费用项目、舍入差额与处理历史。详情的 `attempts` 数组保留原始证据，界面只对数字显示进行三位小数投影。历史显示操作者、原因及各钱包字段的变化。费用快照不可用时明确提示，不将其显示为零费用。

重试发送 `POST /api/billing/reconciliations/:id/retry`，请求为 `{ operation_id, expected_version }`，用于重放已经保存的处理意图。结算或释放发送 `POST /api/billing/reconciliations/:id/resolve`，包含操作 ID、预期版本、`settle/release`、quota 整数字符串及理由。实际费用范围为 0 至 `2147483647` quota；释放提交 `"0"`。同一内容重试保留操作 ID，409 提示重新加载核对，不自动覆盖新状态。已结束账务隐藏人工结算／释放按钮。

页面通过 `GET /api/billing/metrics` 展示待恢复、待对账、预留金额、最长等待秒数、幂等冲突、恢复失败和欠费事件。查询失败时指标显示“不可用”。

## 验证边界

前端金额测试应覆盖 int64 边界、超过 JavaScript 安全整数的输入、溢出、舍入、严格 API 字段、不可变价格发布和路由权限。可按[前端文档](../README.md)的顺序运行 Vitest、类型检查、lint、格式检查和生产构建；涉及页面时再运行 Playwright 桌面/移动端与明暗主题用例。

浏览器用例可以拦截符合真实 wire contract 的 API 形状，用于验证展示、请求参数、错误状态、409 重试身份、权限和布局；它们不替代主数据库事务、真实支付回调或三库集成验证。主数据库与计费接口见[计费 API](../../docs/billing/API.md)，服务端验证范围见[计费验证](../../docs/billing/VALIDATION.md)。
