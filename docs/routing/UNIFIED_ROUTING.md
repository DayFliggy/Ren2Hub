# 原生自动路由与 Token 私有列表

自动模式完全采用 NewAPI：用户和 Token 分组、可用组、自动组、模型限制、渠道能力、优先级、权重、亲和与重试共同决定选择。入口是 `middleware/distributor.go`、`service/channel_select.go` 与 `model/channel_satisfy.go`。用户组参与授权与分组倍率，不能沿用旧“账号分类不影响路由或售价”的说法。

私有模式是最小扩展：每个 Token 只有一份有序渠道列表，包含 `channel_id`、`position`、`enabled`。它只缩小当前用户、Token、模型和端点已授权的候选；保存过的渠道不会获得永久授权，每次请求重新检查。无私有组、渠道倍率、独立健康分、调度器或全局 FIFO 准入。

读取及更新使用 `/api/token/:id/routing`，候选与预览分别为 `/channels`、`/preview` 子路径。`auto` 模式必须没有 entries；`manual` 无可用条目时失败，不回落到任意平台渠道。更新携带 `version`，冲突为409，跨用户Token不可访问。切回自动仍保留版本行，旧编辑器不能复用版本0。

私有列表的顺序在原生可用性条件内生效，失败候选按批准的路由合同尝试下一项；发送、错误、取消和已受理任务的重试约束仍属于 NewAPI。Responses WS、任务及原始任务续接须保持已选择身份，不能因修改偏好而跨渠道重放已接受工作。

完整字段和错误码见[网关 API](GATEWAY_API.md)。是否支持端点取决于真实路由、适配器和插件绑定，不能由渠道名称或模型字符串推断。
