# 渠道接入

渠道类型编号和设置采用NewAPI定义，权威来源为 `constant/channel.go`、`relay/channel/` 和 `relaykit/dto/channel_settings.go`。不得重新编号或根据名称推断协议能力。

保留普通API Key的OpenAI兼容、Claude、Gemini、Azure、Vertex、AWS及其他原生适配器，以及NewAPI类型60的网关互联和任务插件绑定。AWS支持原生Bedrock/Mantle设置，Vertex API Key使用显式project设置；具体字段必须按当前DTO及管理UI提交。

官方订阅账号渠道、type57和io.net已退役，不能创建或通过旧隐藏路由继续使用。站点OAuth登录与普通供应商API Key是不同产品边界，继续保留。渠道默认值、代理、密钥池、模型映射、参数/请求头覆盖采用上游规则；没有旧channel multiplier、容量调度或供应商额度窗口产品。

实际可执行性由用户/Token分组、模型、端点、渠道状态、原生约束及插件声明共同决定。Token私有列表只缩小范围，见[路由](../routing/UNIFIED_ROUTING.md)。对余额查询、模型发现或错误处理的额外声明应以当前控制器及回归为依据，不能复用旧版本的验收结论。

控制台只对 `controller/channel-billing.go` 有实际查询分支的类型（1、8、10、12、13、20、25、40、43、58、59、60）显示余额查询，多 Key 渠道一律不显示；其他类型后端返回“Not implemented yet”，type 61 和多 Key 渠道被 `UpdateChannelBalance` 直接拒绝。判断写在前端 `supportsBalanceQuery()`，行内按钮、分组按钮和批量查询共用。

任务插件绑定需要 `channel.sensitive_write` 和 `task_plugin.bind` 两项权限，缺任一项时表单不显示绑定选择，缺 bind 时类型列表不提供 type 61。`UpdateChannel` 拒绝任何不带 bind 却携带 type 61 的请求，所以没有 bind 的管理员编辑 type 61 渠道时，表单不发送类型、`setting` 等敏感字段，存储值保持不变；有完整权限时，type 61 每次都重发完整 `setting`，因为 `validateChannel` 在读取库内渠道前就检查 `task_plugin_key`。type 60 的绑定集合是 `task_plugin_key` 与 `task_extend_plugin_keys` 的并集（`ChannelSettings.TaskPluginBindings`），表单按并集回填，只有集合变化时才改写这两个字段，并保留 `setting` 的其他键。

前端类型目录 `ADMIN_CHANNEL_TYPE_META` 覆盖 `constant/channel.go` 的全部已知类型，名称取上游 web `CHANNEL_TYPES`（上游注释掉的 6、9–13、21 也保留名称），由 `channel-type-ids.json` 契约夹具防止遗漏。新建只提供带 `creatable` 的 19 种类型（1、3、4、14、17、20、24、25、33、40、41、43、48、58–63）；其他已知类型可以查看并编辑通用字段，表单锁定类型选择，不发送 `other`、`settings` 等类型专属字段。前端不认识的类型显示“未知类型”，表单禁止保存。

渠道列表按后端 `lab.id`（`model/channel_lab.go`）分组，前端不再派生 `supplier` 或 `lab_*` 字段；`labDisplayName()` 把 `unknown`、`mixed` 两个分类显示为“未识别厂商”“多个厂商”，渠道列表、自动路由面板和运行详情共用它。`lab.source` 为 `invalid_mapping` 的渠道归入“未识别厂商”，行内另标“模型映射无效”。契约夹具 `channel-list.get.json` 由真实 `/api/channel/` 处理器生成。

多 Key 渠道在状态列和移动端卡片上显示“可用 Key 数 / 总数”，运行详情逐个列出被禁用的 Key（编号从 1 开始，区分手动禁用和自动禁用）。原生 `channel_info.multi_key_status_list` 只记录被禁用的 Key，`null` 和空对象都表示全部可用，可用数按 `multi_key_size` 减去条目数计算，写在 `multiKeyAvailability()`。通过 `/status` 手动启用渠道不会清除 Key 状态，所以已启用的渠道也可能显示 0 个可用 Key。前端只读，不提供多 Key 管理操作；契约夹具 `channel-multi-key-list.get.json` 覆盖新建、全部恢复、部分禁用和全部禁用四种状态。
