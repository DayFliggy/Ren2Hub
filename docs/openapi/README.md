# API 合同入口

不再维护与当前实现不一致的 OpenAPI JSON。旧版描述保留在 Git 历史，不用于生成当前客户端或判定接口是否开放。

现行路由以 `router/` 注册和对应 `controller/` 的参数、响应及错误处理为准，权限以原生 authz/PAT 注册表为准。Vue 客户端同时遵循这些实际接口；新增或修改 API 时应同步控制器、客户端和有意义的合同测试。

- [原生认证与权限](../authentication.md)
- [网关与最小扩展 API](../routing/GATEWAY_API.md)
- [钱包、定价与充值 API](../billing/API.md)
- [GMPay 支付合同](../billing/GMPAY.md)
- [任务插件 ABI](../plugin-api/README.md)
