# Ren2Hub 文档入口

这里索引当前开发契约和历史材料。核对功能时先看实际路由、调用链和前端能力开关；部署状态以所属工作区的服务器档案与发布记录为准。

## 当前契约

| 主题 | 入口 | 内容 |
| --- | --- | --- |
| 开发与目录 | [项目约定](../AGENTS.md) | Go/Gin/GORM、Vue、独立 RelayKit、验证要求 |
| Web 应用 | [前端说明](../frontend/README.md)、[页面路由](../frontend/ROUTING.md) | 根路径、同源 API、权限与功能门控 |
| 登录与会话 | [认证契约](authentication.md) | 登录令牌、刷新会话、Origin 与代理规则 |
| 模型路由 | [统一路由](routing/UNIFIED_ROUTING.md) | Token Profile、私有组、渠道容量、资格重检、计费与重试 |
| 单实例网关 | [管理 API](routing/GATEWAY_API.md)、[验收记录](routing/SINGLE_INSTANCE_ACCEPTANCE.md) | 本地 FIFO、总队列、渠道冷却、重试预算与验证边界 |
| 网关交付 | [验证步骤](routing/GATEWAY_VALIDATION.md)、[切换与回退](routing/GATEWAY_RELEASE.md) | 可重复测试、顺序排空切换、旧 Redis 键限定清理 |
| 协议转换 | [RelayKit](../relaykit/README.md) | 独立模块、转换路径及协议边界 |
| 表达式计费 | [计费表达式](../pkg/billingexpr/expr.md) | 计费表达式及实现入口；渠道倍率以统一路由为准 |

## 源码阅读顺序

普通 API 从 `router/` 进入 `controller/`、`service/` 与 `model/`。模型请求重点看 `middleware/distributor.go`、`service/request_route.go`、`controller/relay.go`，再跟进 `relay/channel/` 和 `relaykit/`。后台作业从 `main.go`、`controller/system_task_handlers.go` 进入系统任务框架。

Vue 从 `frontend/src/router/`、`frontend/src/stores/app.ts` 和 `frontend/src/api/` 核对页面、能力状态与真实 API。页面文件存在或后端 API 挂载，不单独证明产品入口已经开放。

## 历史与辅助材料

- [改造前路由归档](routing/archive/UNIFIED_ROUTING_PRE_SINGLE_INSTANCE_2026_09_16.md)：适用于 2026-09-16 本地 FIFO 改造前工作树；旧 Redis 并发与双网关结果不代表现行支持。
- 工作区 `reference-projects/` 保留 AxonHub、sub2api 和研究报告；现行单实例方案替代 Redis-only 用户准入以及配置变化新建活动池的旧建议。

- [模型管理前端验收](frontend-model-management-acceptance.md)：对应记录版本的验收，不能当作当前重跑结果。
- [上游 RC24 同步记录](upstream-sync/NEWAPI_RC24_E2C7AA7B.md)：固定提交的历史同步材料。
- `openapi/`：API 描述文件；覆盖范围与现行路由应交叉核对。
- `installation/`、`channel/`：安装和渠道辅助说明。RenAI 工作区生产发布优先使用根工作区的部署资产与技能。

## 本地文件维护

`frontend/node_modules/`、`frontend/dist/` 是可重建依赖与构建产物；清理前确认没有正在使用它们的开发或测试进程。`frontend/embed-dist/` 含受跟踪占位文件和可能的嵌入构建输出，不应整目录删除。

`.local-tests/` 是被忽略的探测工作区，可能同时包含脚本、测试数据库、可执行文件和截图，必须逐项识别。数据库、自动定价目录缓存、未合并补丁与验收证据不能只凭忽略规则或日期删除。版本文件、迁移脚本和历史契约也不是构建垃圾。
