# 等待上游与本地合同边界

下表的问题与冻结同步目标（见[同步账本](SYNC_LEDGER.md)）的上游源码相同，没有在本地修复。精确诊断把它们冻结为 12 个已知失败指纹和 30 个后续阻断，集合不扩大，规则见[本地测试方案](../testing/LOCAL_TESTING.md#已接受的原生差异)。本表不自动创建 issue、PR 或定时任务；上游 issue 的状态在每次同步时复核。

## 共有问题

| 问题                                   | 源码位置与触发                                                                                                                                    | 上游关联及处置                                                                                                                                                                  |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| N1 remix 使用公开任务 ID               | `relay/relay_task.go` 的 `ResolveOriginTask`；公开 ID 与供应商 ID 不同                                                                            | [#4120](https://github.com/QuantumNous/new-api/issues/4120)、[#4946](https://github.com/QuantumNous/new-api/pull/4946) 未合并，等待上游。真实入口先被 N2 阻断，分层 Go 诊断保留 |
| N2 原生 remix 空渠道上下文 panic       | 同函数使用尚未初始化的 `ChannelMeta`；原生 `/v1/videos/:task_id/remix`                                                                            | [#4121](https://github.com/QuantumNous/new-api/issues/4121)、[#7676](https://github.com/QuantumNous/new-api/issues/7676)。必须以具体的 nil pointer 栈识别，不能接受任意 500     |
| N3 remix 动作变成新生成                | `relay/channel/task/jsplugin/adaptor.go` 原生验证覆盖动作                                                                                         | 等待上游。真实入口的后续动作和不支持 remix 的供应商行为受 N2 阻断，不能算通过                                                                                                   |
| A5 原任务模型与错误顺序                | `middleware/distributor.go`、`relay/relay_task.go`；受限 Key remix、任务不存在或存储失败                                                          | [#5327](https://github.com/QuantumNous/new-api/pull/5327) 已关闭未合并；不吸收其跳过模型限制的方案                                                                              |
| A7 继承倍率及表达式用量                | `relay/relay_task.go` 和提交估算；续作时长与尺寸、缺少 `UsageFacts`                                                                               | 等待上游；原生 HTTP 的后续计费断言受 N2 阻断                                                                                                                                    |
| 原生与插件的实际平台归属               | `relay/relay_task.go`；`middleware/task_plugin.go` 准备阶段使用首个候选的元数据                                                                   | 多个候选与实际执行插件不一致时仍待上游；准备阶段的校验不等于实际执行的保证                                                                                                      |
| draft_task 原任务声明                  | `plugins/tasks/doubao/plugin.js` 的协议解码                                                                                                       | [#8127](https://github.com/QuantumNous/new-api/pull/8127) 未合并；它只解决豆包 OpenAI 视频协议的原任务声明，不能代替通用的归属和续作修复                                        |
| MJ 与插件的存储错误分类                | `relay/mjproxy_handler.go` image-seed 的 `GetByMJId` 吞错；`middleware/task_plugin.go` 存储失败仍用 not_found code；`getTaskOriginModelName` 吞错 | 保留错误诊断并等待。image-seed 非满载的准入错误仍为 400 `code=4`，不声称统一 503                                                                                                |
| MJ simple-change、模型限制和 HTTP 配置 | 只有 content 的 simple-change 映射为 change；image-seed 模型限制；全局 MJ HTTP 客户端                                                             | 与上游相同；本地只保留原渠道准入接点，不扩展协议和错误语义                                                                                                                      |

## 本地合同，不属于等待上游

续作倍率上限、自动与有序手动路由、即时并发、路由观测、预览范围、路由表锁序是 Ren2Hub 定制，行为见[统一路由](../routing/UNIFIED_ROUTING.md)。续作只豁免手动列表，超出当前 Key 上限须在占槽和预扣前拒绝。插件错误净化仅放行宿主生成的 `channel_ratio_exceeded`，不放行任意供应商错误。锁序验收必须跑真实的 MySQL 和 PostgreSQL，SQLite 不能证明行锁安全。

普通 HTTP 成功路径里，客户端断开后供应商的在途尝试继续，结束后结算一次、释放一次；不做立即取消传播和退款。

上游 #8218 给 fence/version 键加了同槽标记。生产是单实例 Redis，这个标记没有功能收益，只保持键结构与上游一致，不表示应用支持 Redis Cluster；旧的 `auth:user:version:<id>` 键成为无 TTL 残留，清理属于生产 Redis 写操作，没有授权。
