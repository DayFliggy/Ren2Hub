# 渠道接入

渠道类型编号和设置采用NewAPI定义，权威来源为 `constant/channel.go`、`relay/channel/` 和 `relaykit/dto/channel_settings.go`。不得重新编号或根据名称推断协议能力。

保留普通API Key的OpenAI兼容、Claude、Gemini、Azure、Vertex、AWS及其他原生适配器，以及NewAPI类型60的网关互联和任务插件绑定。AWS支持原生Bedrock/Mantle设置，Vertex API Key使用显式project设置；具体字段必须按当前DTO及管理UI提交。

旧 Codex/type 57 专用渠道和io.net已退役，不能创建或通过旧隐藏路由继续使用。站点OAuth登录与普通供应商API Key是不同产品边界，继续保留。渠道默认值、代理、密钥池、模型映射、参数/请求头覆盖采用上游规则；保留渠道倍率和无等待并发上限，不恢复供应商订阅额度窗口。

实际可执行性由 Token 自动/有序手动路由、模型、端点、渠道状态、倍率上限、原生约束及插件声明共同决定，不再使用业务分组。Token私有列表只缩小范围，见[路由](../routing/UNIFIED_ROUTING.md)。对余额查询、模型发现或错误处理的额外声明应以当前控制器及回归为依据，不能复用旧版本的验收结论。

创建渠道成功响应保留 `success` 等原字段，并返回 `data.ids` 和 `data.results[{input_index, channel_id}]`；只有一个创建结果时同时返回 `data.id`。批量输入按原输入位置关联结果，合并多 Key 只产生一条渠道。校验失败、数据库错误或事务 panic 不返回成功或半套 ID。

余额能力由原生适配器和提供商插件声明共同决定。type 61 插件导出余额能力时可查询，插件多 Key 返回逐项摘要；普通不支持余额查询的类型仍拒绝。更新全部余额创建系统后台任务，通过 `/api/channel/update_balance/task/:task_id` 查询进度。通用插件支持账号登录、凭据摘要、刷新和后台维护；旧 Codex 专用渠道不恢复。

任务插件绑定需要 `channel.sensitive_write` 和 `task_plugin.bind` 两项权限，缺任一项时表单不显示绑定选择，缺 bind 时类型列表不提供 type 61。`UpdateChannel` 拒绝任何不带 bind 却携带 type 61 的请求，所以没有 bind 的管理员编辑 type 61 渠道时，表单不发送类型、`setting` 等敏感字段，存储值保持不变；有完整权限时，type 61 每次都重发完整 `setting`，因为 `validateChannel` 在读取库内渠道前就检查 `task_plugin_key`。type 60 的绑定集合是 `task_plugin_key` 与 `task_extend_plugin_keys` 的并集（`ChannelSettings.TaskPluginBindings`），表单按并集回填，只有集合变化时才改写这两个字段，并保留 `setting` 的其他键。

前端类型目录 `ADMIN_CHANNEL_TYPE_META` 覆盖 `constant/channel.go` 的全部已知类型，名称取上游 web `CHANNEL_TYPES`（上游注释掉的 6、9–13、21 也保留名称），由 `channel-type-ids.json` 契约夹具防止遗漏。新建只提供带 `creatable` 的 19 种类型（1、3、4、14、17、20、24、25、33、40、41、43、48、58–63）；其他已知类型可以查看并编辑通用字段，表单锁定类型选择，不发送 `other`、`settings` 等类型专属字段。前端不认识的类型显示“未知类型”，表单禁止保存。

渠道列表按后端 `lab.id`（`model/channel_lab.go`）分组，前端不再派生 `supplier` 或 `lab_*` 字段；`labDisplayName()` 把 `unknown`、`mixed` 两个分类显示为“未识别厂商”“多个厂商”，渠道列表、工具栏厂商筛选、自动路由面板和运行详情共用它。工具栏的关键词或模型筛选任一非空时改用 `/api/channel/search`，与上游一致；原生 `/api/channel/` 不读取 `model`。`lab.source` 为 `invalid_mapping` 的渠道归入“未识别厂商”，行内另标“模型映射无效”。契约夹具 `channel-list.get.json` 由真实 `/api/channel/` 处理器生成。

多 Key 渠道在状态列和移动端卡片上显示“可用 Key 数 / 总数”，运行详情逐个列出被禁用的 Key（编号从 1 开始，区分手动禁用和自动禁用）。原生 `channel_info.multi_key_status_list` 只记录被禁用的 Key，`null` 和空对象都表示全部可用，可用数按 `multi_key_size` 减去条目数计算，写在 `multiKeyAvailability()`。通过 `/status` 手动启用渠道不会清除 Key 状态，所以已启用的渠道也可能显示 0 个可用 Key。前端只读，不提供多 Key 管理操作；契约夹具 `channel-multi-key-list.get.json` 覆盖新建、全部恢复、部分禁用和全部禁用四种状态。
