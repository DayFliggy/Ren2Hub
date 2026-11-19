# 渠道设置

渠道配置沿用NewAPI的普通字段、`setting` / `other` JSON以及原生参数和请求头覆盖。以 `relaykit/dto/channel_settings.go`、`model/channel.go` 和对应适配器为权威；UI保存前应保留未编辑的原生字段。

常见设置包括网络代理、格式转换、推理内容处理、模型映射、Vertex认证与project、AWS API类型及认证方式。填写代理必须遵循当前解析器支持的协议和地址格式，不把私有路由偏好写进渠道JSON。AWS Mantle和Vertex字段属于原生适配器，不另建供应商配置真源。

Ren2Hub 使用渠道 `channel_ratio` 及 Token 渠道倍率上限，业务分组和用户/Token 分组倍率已删除。渠道上游倍率只作只读展示，不参与计费或选路。密钥、代理凭据和完整上游错误不得进入面向普通用户的投影；管理权限、存储与脱敏行为以原生实现为准。

Vertex 的 `settings.vertex_api_version` 只接受空值、`v1`、`v1beta1`；空值维持适配器原来的默认版本。该选项只影响 Google Gemini/Imagen 请求，不改变 Claude/OpenSource 请求路径。Vue 编辑器保留未编辑的未知设置字段，并沿用现有敏感字段权限。
