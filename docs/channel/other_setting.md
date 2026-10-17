# 渠道设置

渠道配置沿用NewAPI的普通字段、`setting` / `other` JSON以及原生参数和请求头覆盖。以 `relaykit/dto/channel_settings.go`、`model/channel.go` 和对应适配器为权威；UI保存前应保留未编辑的原生字段。

常见设置包括网络代理、格式转换、推理内容处理、模型映射、多Key选择、Vertex认证与project、AWS API类型及认证方式。填写代理必须遵循当前解析器支持的协议和地址格式，不把私有路由偏好写进渠道JSON。AWS Mantle和Vertex字段属于原生适配器，不另建供应商配置真源。

本地已删除渠道售价系数。用户/Token分组倍率仍是NewAPI计费的一部分，不能把两者混用。密钥、代理凭据和完整上游错误不得进入面向普通用户的投影；管理权限、存储与脱敏行为以原生实现为准。
