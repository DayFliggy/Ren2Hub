# 渠道接入

渠道类型编号和设置采用NewAPI定义，权威来源为 `constant/channel.go`、`relay/channel/` 和 `relaykit/dto/channel_settings.go`。不得重新编号或根据名称推断协议能力。

保留普通API Key的OpenAI兼容、Claude、Gemini、Azure、Vertex、AWS及其他原生适配器，以及NewAPI类型60的网关互联和任务插件绑定。AWS支持原生Bedrock/Mantle设置，Vertex API Key使用显式project设置；具体字段必须按当前DTO及管理UI提交。

官方订阅账号渠道、type57和io.net已退役，不能创建或通过旧隐藏路由继续使用。站点OAuth登录与普通供应商API Key是不同产品边界，继续保留。渠道默认值、代理、密钥池、模型映射、参数/请求头覆盖采用上游规则；没有旧channel multiplier、容量调度或供应商额度窗口产品。

实际可执行性由用户/Token分组、模型、端点、渠道状态、原生约束及插件声明共同决定。Token私有列表只缩小范围，见[路由](../routing/UNIFIED_ROUTING.md)。对余额查询、模型发现或错误处理的额外声明应以当前控制器及回归为依据，不能复用旧版本的验收结论。

控制台只对 `controller/channel-billing.go` 有实际查询分支的类型（1、8、10、12、13、20、25、40、43、58、59、60）显示余额查询，多 Key 渠道一律不显示；其他类型后端返回“Not implemented yet”，type 61 和多 Key 渠道被 `UpdateChannelBalance` 直接拒绝。判断写在前端 `supportsBalanceQuery()`，行内按钮、分组按钮和批量查询共用。
