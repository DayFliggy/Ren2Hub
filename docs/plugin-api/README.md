# NewAPI 任务插件

Ren2Hub沿用NewAPI `09109ec315a5086366f94bcce00f177ec831dbe1` 的JS任务插件运行时、元数据、用量表达式、协议绑定及管理API。插件能力不是本地独有。ABI正文、类型和schema从该上游版本同步：

- [v1文档](v1.md)
- [TypeScript类型](v1.d.ts)
- [元数据schema](v1.schema.json)

实际运行入口为 `pkg/jsplugin/`、`relay/channel/task/jsplugin/`、`controller/task_plugin.go`、`service/task_polling.go`。原生生产版本、任务私有数据、用量与表达式快照遵从上游，不携带旧schema5执行归档、资金事件、加密重放或结果outbox。

本轮空库重装要求旧任务归零；不存在跨旧/新核心的任务迁移。之后的原生升级仍需核对任务引用版本及实际恢复行为，不能把某次无在途任务的启动等同于任务恢复已验证。候选验收只用本地模拟服务，运行结果另存证据。

本地产品删去了官方订阅账号、type57、io.net与Playground；插件文档中的通用宿主机制不重新开放这些产品。界面由Vue实现，上游文档中的管理页面描述应映射到实际Vue入口。
