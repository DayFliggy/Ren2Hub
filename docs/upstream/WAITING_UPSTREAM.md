# 等待上游与本地待办

最后检查：2026-11-05 15:42 UTC。实际适配基线仍为 NewAPI `706144769e5bed5a47b78721e3bb11da3cf4c0b9`（含 #8200）；本轮检查的 HEAD 是 `2de51a19e35a602a0041d380b29be7c9bcd1e0e5`，不能把检查过等同于全量吸收。本表不自动创建 issue、PR 或定时任务。

## 共有问题

| 问题 | 当前源码位置与触发 | 上游关联及处置 |
| --- | --- | --- |
| N1 remix 使用公开任务 ID | `relay/relay_task.go` 的 `ResolveOriginTask`；公开 ID 与供应商 ID 不同 | [#4120](https://github.com/QuantumNous/new-api/issues/4120)、[#4946](https://github.com/QuantumNous/new-api/pull/4946) 未合并；撤回今天提前吸收的一行，等待上游。真实入口先被 N2 阻断，分层 Go 诊断仍保留 |
| N2 原生 remix 空渠道上下文 panic | 同函数使用尚未初始化的 `ChannelMeta`；原生 `/v1/videos/:task_id/remix` | [#4121](https://github.com/QuantumNous/new-api/issues/4121)、[#7676](https://github.com/QuantumNous/new-api/issues/7676)；撤回本日共有修复。必须以具体 nil pointer 栈识别，不能接受任意 500 |
| N3 remix 动作变成新生成 | `relay/channel/task/jsplugin/adaptor.go` 原生验证覆盖动作 | 冻结上游同实现；等待。真实入口的后续动作/不支持 remix 的供应商行为受 N2 阻断，不能算通过 |
| A5 原任务模型与错误顺序 | `middleware/distributor.go`、`relay/relay_task.go`；受限 Key remix、任务不存在/存储失败 | [#5327](https://github.com/QuantumNous/new-api/pull/5327) 已关闭未合并；不吸收其跳过模型限制方案。撤回今天抽出的统一模型推导，不扩展历史差异 |
| A7 继承倍率及表达式用量 | `relay/relay_task.go` 和提交估算；续作时长/尺寸、缺少 `UsageFacts` | 冻结上游同路径；撤回新增 `OriginOtherRatios`，等待；原生 HTTP 后续计费断言受 N2 阻断 |
| 原生/插件实际平台归属 | `relay/relay_task.go`；`middleware/task_plugin.go` 准备阶段使用首个候选元数据 | 撤回新增原生平台字段/守卫；多个候选与实际执行插件不一致仍待上游；不能把准备阶段校验描述为实际执行保证 |
| draft_task 原任务声明 | `plugins/tasks/doubao/plugin.js` 的协议解码 | [#8127](https://github.com/QuantumNous/new-api/pull/8127) 未合并；只解决豆包 OpenAI 视频协议的原任务声明，不能代替通用归属/续作修复 |
| MJ/插件存储错误分类 | `relay/mjproxy_handler.go` image-seed 的 `GetByMJId` 吞错；`middleware/task_plugin.go` 存储失败仍用 not_found code；`getTaskOriginModelName` 吞错 | 冻结上游共有语义，保留错误诊断并等待。image-seed 非满载准入错误仍为 400 `code=4`，不声称统一 503 |
| MJ simple-change、模型限制和 HTTP 配置 | 只有 content 的 simple-change 映射 change；image-seed 模型限制；全局 MJ HTTP 客户端 | 冻结上游相同行为；本轮只保留原渠道准入接点，不扩展协议/错误语义 |

## 本地合同与未解决项

续作倍率上限、自动/有序手动路由、即时并发、路由观测、预览范围、路由表锁序是 Ren2Hub 定制，不能归为等待上游。续作只豁免手动列表，超出当前 Key 上限须在占槽和预扣前拒绝。插件错误净化仅放行宿主生成的 `channel_ratio_exceeded`，不放行任意供应商错误。

2026-11-05 修订计划已批准修复四项本地缺陷：六类多行渠道写与保存统一锁序；非锁定任务重试的全部选路失败保留上次实际发送后的 TaskError（含本地包装的响应错误，排除未发送的预扣/构造失败）；WS 和插件预览复用实际资格规则。A4 已移入默认回归，不再使用 routing_diagnostic 标签或等待上游分类。锁序验收必须跑真实 MySQL/PostgreSQL，SQLite 不能证明行锁安全；旧 PostgreSQL 40P01 和预览红灯证据保留，不以新实现覆盖旧记录。

普通 HTTP 成功路径采用现行共享合同：客户端断开后供应商在途尝试继续，结束后一次结算和释放。立即取消传播和退款不在本轮范围，也不再作为未决定的测试期望。实际验证以同一源码指纹的最新报告为准；任何本地失败仍为 fail，缺项为 not_run。

## 已合并、下个同步窗口评估

NewAPI #8218 已在检查的 HEAD 中合并，为关联缓存键添加 Redis 同槽标记。本轮不吸收：本地仍为单实例 Redis 客户端及部署，无跨槽触发条件，改名会增加缓存版本下限和 fence 的过渡成本。该项不是等待上游修复，也不表示应用已支持 Redis Cluster。将来启用 Cluster 时还须评估客户端、数据库编号、Pipeline 和真实集群验证。

## 已吸收

[#8200](https://github.com/QuantumNous/new-api/pull/8200) 已合并进上述完整 SHA；吸收缓存签名、旧缓存修复、撤销会话保护及有效额度保留，适配本地 `MaxConcurrency`。三库/缓存断言和真实 Redis 验证分别记账，不把 miniredis 说成实际 Redis。
