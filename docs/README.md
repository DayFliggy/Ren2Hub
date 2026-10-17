# Ren2Hub 文档入口

Ren2Hub / DayFliggy 是采用 NewAPI 核心的单节点 AI 网关，使用 Vue 控制台。唯一上游为 `QuantumNous/new-api`，本轮源码基线是 `09109ec315a5086366f94bcce00f177ec831dbe1`。来源、必要差异和验证状态见[同步账本](upstream/SYNC_LEDGER.md)。本文说明源码合同，不代表当前候选或正式实例已经发布。

自动选路、分组倍率、默认价格、预扣、结算、金额 API、协议和权限采用 NewAPI。钱包是唯一资金来源，Token 额度是附加消费限制。最小扩展为工单、每个 Token 的有序私有渠道列表及 GMPay。官方订阅账号、站内订阅套餐、type 57、io.net 部署、Playground、React 和 Electron 已退役；普通 API Key、站点 OAuth 和原生钱包支付仍保留。

| 主题 | 入口 |
| --- | --- |
| 安装与发布 | [安装](installation/DOCKER.md)、[宝塔](installation/BT.md)、[切换与回退](routing/GATEWAY_RELEASE.md) |
| 登录与权限 | [认证](authentication.md)、[扩展接口](routing/GATEWAY_API.md) |
| 路由与协议 | [自动及私有路由](routing/UNIFIED_ROUTING.md)、[协议边界](routing/GATEWAY_SUPPORT_MATRIX.md)、[渠道](channel/TYPES.md) |
| 钱包与定价 | [实现](billing/IMPLEMENTATION.md)、[API](billing/API.md)、[定价](billing/PRICING.md)、[恢复](billing/RECOVERY.md) |
| 充值 | [原生易支付](billing/EPAY.md)、[GMPay](billing/GMPAY.md)、[支付设置](billing/PAYMENT_SETTINGS.md) |
| 异步任务 | [插件入口](plugin-api/README.md)、[上游 v1 ABI](plugin-api/v1.md) |
| Vue | [前端](../frontend/README.md)、[模型](console/MODELS.md)、[日志](console/LOGS.md)、[术语](translation-glossary.md) |
| 验证 | [网关](routing/GATEWAY_VALIDATION.md)、[资金](billing/VALIDATION.md)、[单节点边界](routing/SINGLE_INSTANCE_ACCEPTANCE.md)、[依赖](dependencies.md) |

启动从 `main.go`、`model/main.go` 阅读；请求沿 `router/` → `middleware/` → `controller/` → `service/` / `model/` 进入 `relay/`。RelayKit 与 TokenKit 是独立 Go 模块。Vue 从 `frontend/src/router/`、`stores/` 和 `api/` 核对入口与真实接口；菜单可见不代替服务端授权。

本轮采用全新数据库，不迁移旧用户、渠道、密钥、价格、商户、任务或支付记录。程序在 AutoMigrate 前拒绝包含旧 `billing_schemas` 或 `billing_events` 的数据库。旧库归档、独立新库及 Redis db1 切换需要所属工作区部署流程的单独批准。旧任务、在途支付未归零时阻塞切换，不新增双回调入口。

旧架构与执行报告保留在 Git 历史；现行接口以路由注册和源码为准。[API 合同入口](openapi/README.md) 指向当前路由与控制器；旧 OpenAPI JSON 已退役。外部参考仓库只作研究，不是运行依赖。测试证据、源码发布、候选验收和正式部署分别记录，不互相替代。
