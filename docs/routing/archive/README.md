# 路由历史归档

以下材料于 2026-09-16 从单实例本地 FIFO 改造前的工作树归档，保留当时的契约、修复依据、命令和失败证据。正文中的“当前”“未发布”和双实例验收均指其记录时点；不能作为现行部署或并发能力说明。

| 材料 | 保留用途 |
| --- | --- |
| [统一路由](UNIFIED_ROUTING_PRE_SINGLE_INSTANCE_2026_09_16.md) | 改造前 Redis 准入与路由契约 |
| [实施记录](GATEWAY_IMPROVEMENT_STATUS_PRE_SINGLE_INSTANCE_2026_09_16.md) | F01–F13 变更、提交与回归定位 |
| [2026-09-14 修复记录](GATEWAY_REVIEW_FIXES_2026_09_14_PRE_SINGLE_INSTANCE_2026_09_16.md) | R01–R05 复现、账务及转换修复边界 |
| [验证记录](GATEWAY_VALIDATION_PRE_SINGLE_INSTANCE_2026_09_16.md) | 旧版本 SDK、数据库、双应用测试与证明限制 |
| [发布准备](GATEWAY_RELEASE_PRE_SINGLE_INSTANCE_2026_09_16.md) | 旧版本持久化变更与迁移风险 |

历史记录之间直接链接到对应归档；工作区技能和协议支持矩阵的链接用于定位维护入口，不意味着历史测试已在当前版本重跑。

现行入口为 [统一路由](../UNIFIED_ROUTING.md)、[管理 API](../GATEWAY_API.md)、[验收记录](../SINGLE_INSTANCE_ACCEPTANCE.md) 和 [切换与回退](../GATEWAY_RELEASE.md)。2026-09-17 整理时删除了原目录内两份只有归档跳转的状态页，全部历史正文保留在此。
