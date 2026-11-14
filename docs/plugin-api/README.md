# NewAPI 插件

Ren2Hub沿用 NewAPI `b388140acfd80369ec6243b16869daff1b4051f4` 的 JS 任务/提供商/请求处理插件运行时、元数据、用量表达式、协议绑定及管理 API。插件能力不是本地独有。ABI 正文、类型和 schema 按该适配基线校验；来源差异见[同步账本](../upstream/SYNC_LEDGER.md)：

- [v1文档](v1.md)
- [TypeScript类型](v1.d.ts)
- [元数据schema](v1.schema.json)

实际运行入口为 `pkg/jsplugin/`、`relay/channel/task/jsplugin/`、`controller/task_plugin.go`、`service/task_polling.go`。原生生产版本、任务私有数据、用量与表达式快照遵从上游，不携带旧 schema 5 执行归档、资金事件、加密重放或结果 outbox。

首次从已退役账务核心换库需旧任务归零；当前原生核心普通升级保留数据，需核对任务引用版本及实际恢复行为，不能把某次无在途任务的启动等同于任务恢复已验证。候选验收只用本地模拟服务，运行结果另存证据。

本地产品删去了旧 Codex/type 57 专用接入、io.net与Playground；插件文档中的通用宿主机制不重新开放这些产品。界面由Vue实现，上游文档中的管理页面描述应映射到实际Vue入口。

通用提供商插件登录、凭据刷新与管理开放，普通 API Key 的检查/签名/临时 token 交换也保留。请求处理实例仅有全局、渠道和模型范围；配置与试运行仅 Root 可写，Admin 绑定接口不返回实例配置。Vue 入口为插件、请求处理与渠道配置页，登录回调位于 `/oauth/plugin/:key/callback`。
