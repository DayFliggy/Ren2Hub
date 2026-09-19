# 计费界面与金额契约

本界面只支持新计费库。额度字段使用有符号 int64 范围内的十进制整数字符串，换算固定为 `500000 quota = 1 USD`。接口不会同时读取旧余额字段；用户使用 `available_quota`、`reserved_quota`、`used_quota`，Token 使用相同字段，其中无限 Token 的 `available_quota` 必须为 `null`。账户版本字段为 `quota_version`。

展示数字最多保留 3 位小数。`formatQuota` 与 Decimal 展示函数统一限制显示精度；原始 quota 整数、价格版本、快照与编辑输入保留原始精度。比如额度 `9007199254740993` 在列表显示 `$18,014,398,509.482`，编辑时仍是 `18014398509.481986` USD，保存后仍是原始整数。只读证据 JSON 使用展示投影，不会改写原始报价、用量或结果；表达式文本保持原样。

金额输入、加总与比较使用 BigInt，明确单价及渠道系数展示使用 Decimal 字符串运算。`quotaForChart` 只在图表呈现边界转换为 Number；不能将图表数据作为资金更新的输入。金额溢出、数字类型的 quota、指数写法、非规范字符串及转换后并非整数 quota 的输入均明确拒绝。

## 钱包与 Token

钱包显示可用额度、在途预留和累计已结算消费，并同时保留原始 quota 整数，方便核对低于显示精度的金额。资金流水和预留列表独立分页，加载失败显示“账务数据暂不可用”。Token 明细以 `token_id` 限定查询，显示 Token 的预算变动字段；无限 Token 仍展示预留和消费。

`GET /api/billing/self/reservations` 与 `GET /api/billing/self/events` 使用 `offset`、`limit` 和可选 `token_id`，返回 `{ items, total }`。金额字段只接受字符串。Token 预算修改提交 `available_quota`、`unlimited_quota`、`operation_id` 与 `expected_version`；无限额度输入提交 `available_quota: "0"`，服务端响应返回 `null`。同一界面内重试相同修改保留相同操作 ID。

管理员调账提交增减差额、原因、稳定操作 ID 和预期版本；界面不会直接设置钱包投影。兑换码创建中的 `amount` 是十进制 USD 字符串。返利转入中的 `amount` 则是 quota 整数字符串，沿用该业务 API 的单位约定。

## 价格发布

Root 在模型元数据编辑器中维护唯一生效价格版本：明确单价、高级表达式或明确免费。单价项选择规范用量和对应单位，未知单位、重复维度、无效价格、缺少表达式均阻止保存。表达式负责 token 定价时，不再同时加入 token 单价项；非 token 附加项仍可使用明确单价。

`GET /api/billing/prices` 返回生效版本数组；发布使用 `POST /api/billing/prices`，请求为 `{ price, expected_version }`，冲突返回 409。保存发布新版本，已发送请求继续使用冻结报价。关闭编辑器取消尚未完成的读取。

“从本地初始目录填入”调用 `GET /api/billing/prices/seed`，只填入当前模型草稿，检查后保存才发布。未能确定性转换的模型明确提示待配置。公开价格页展示所有费用项目及计价单位；切换每千／每百万 Token 仅改变 token 单价的展示尺度，图片、时长和调用费用不受影响。

## 人工对账

`/billing/reconciliations` 同时要求 Root 和 `billing_reconciliation: "live"` 能力。普通管理员与能力关闭时的 Root 均不能进入页面，服务端权限仍是最终边界。

列表默认显示待处理账务。打开详情后核对请求 ID、账户、Token、冻结报价、各次上游尝试的报价及哈希、规范用量、费用项目、舍入差额与处理历史。详情的 `attempts` 数组保留原始证据，界面只对数字显示进行三位小数投影。历史显示操作者、原因及各钱包字段的变化。费用快照不可用时明确提示，不将其显示为零费用。

重试发送 `POST /api/billing/reconciliations/:id/retry`，请求为 `{ operation_id, expected_version }`，用于重放已经保存的处理意图。结算或释放发送 `POST /api/billing/reconciliations/:id/resolve`，包含操作 ID、预期版本、`settle/release`、quota 整数字符串及理由。实际费用范围为 0 至 `2147483647` quota；释放提交 `"0"`。同一内容重试保留操作 ID，409 提示重新加载核对，不自动覆盖新状态。已结束账务隐藏人工结算／释放按钮。

页面通过 `GET /api/billing/metrics` 展示待恢复、待对账、预留金额、最长等待秒数、幂等冲突、恢复失败和欠费事件。查询失败时指标显示“不可用”。

## 验证记录

验证日期：2026-09-20。使用 Bun、Vitest、vue-tsc、ESLint、Prettier、Vite 与 Playwright Chromium。

- `bun run test:run`：126 个测试文件、602 项测试通过，覆盖 int64 和超过 JavaScript 安全整数的金额、溢出、舍入、严格 API 字段、不可变价格发布与路由权限。
- 最后补充充值预览溢出与大额余额占比回归：3 个相关文件、10 项定向测试通过，其中新增 4 项。充值金额不合法或充值后余额超出 int64 时，界面明确提示并禁止提交。
- `bun run typecheck`、`bun run lint`、`bun run format:check`、`bun run build` 均通过。构建仍有现有图表依赖块超过 500 kB 的体积提示，无构建错误。
- Playwright 定向验收 68 项通过，覆盖钱包、对账、精确 Token 编辑、公开单价、模型目录与编辑器，以及受影响的仪表盘、日志、活动、邀请和用户界面；桌面／移动端及明暗主题均覆盖。人工检查了对账移动端、Token 精确输入及公开单价截图；最后的充值与余额改动又通过 8 项钱包／仪表盘烟测。
- 上游尝试证据加入后，7 个文件的 31 项定向单测、16 项账务与精度浏览器测试通过，类型、lint、格式及构建重新通过。验证了非空尝试证据、三位小数显示、精确 Token 输入与提交，以及日志／订单导出和 CSV／Excel 文件中的完整额度与十进制字符串；显示格式化不会改写输入、快照或导出数据。
- 浏览器测试使用拦截的真实 API 形状，验证界面行为、请求参数、409 重试身份、错误状态、权限及布局。它们不替代主数据库事务、真实支付回调或三库集成验证。
- 本任务没有运行真实支付、线上部署或数据库迁移。构建产物没有发布。
