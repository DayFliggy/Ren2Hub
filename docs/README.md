# Ren2Hub 文档入口

这里索引当前开发契约与有效验证证据。核对功能时先看实际路由、调用链和前端能力开关；部署状态以所属工作区的服务器档案与发布记录为准。

当前源码采用钱包计费，Token 额度是附加消费限额；本站订阅、套餐购买和套餐管理已下线，前后端相关页面与接口不再开放。旧账务数据保留，非钱包来源不自动转为钱包扣退。

## 当前契约

| 主题 | 入口 | 内容 |
| --- | --- | --- |
| 开发与目录 | [项目约定](../AGENTS.md) | Go/Gin/GORM、Vue、独立 RelayKit、验证要求 |
| 安装与更新 | [单实例 Compose](installation/DOCKER.md)、[宝塔要求](installation/BT.md) | 当前源码、PostgreSQL、独立密钥、排空与数据保留 |
| Web 应用 | [前端说明](../frontend/README.md)、[页面路由](../frontend/ROUTING.md) | 根路径、同源 API、权限与功能门控 |
| 登录与会话 | [认证契约](authentication.md) | 登录令牌、刷新会话、Origin 与代理规则 |
| 模型路由 | [统一路由](routing/UNIFIED_ROUTING.md) | Token Profile、私有组、渠道容量、资格重检、计费与重试 |
| 单实例网关 | [管理 API](routing/GATEWAY_API.md)、[验收记录](routing/SINGLE_INSTANCE_ACCEPTANCE.md) | 本地 FIFO、总队列、渠道冷却、重试预算与验证边界 |
| 网关交付 | [验证步骤](routing/GATEWAY_VALIDATION.md)、[切换与回退](routing/GATEWAY_RELEASE.md) | 可重复测试、顺序排空切换、旧 Redis 键限定清理 |
| 协议转换 | [支持矩阵](routing/GATEWAY_SUPPORT_MATRIX.md)、[RelayKit](../relaykit/README.md) | 已验证协议、独立模块、转换路径及边界 |
| 历史修复证据 | [网关复评](routing/GATEWAY_REVIEW_FIXES_2026_09_18.md)、[协议与路由验收](routing/PROTOCOL_ROUTING_ACCEPTANCE_2026_09_18.md) | 已完成修复、正式回归、固定参考版本和当时的验证边界 |
| 表达式计费 | [计费表达式](../pkg/billingexpr/expr.md) | 计费表达式及实现入口；渠道倍率以统一路由为准 |

## 源码阅读顺序

普通 API 从 `router/` 进入 `controller/`、`service/` 与 `model/`。模型请求重点看 `middleware/distributor.go`、`service/request_route.go`、`controller/relay.go`，再跟进 `relay/channel/` 和 `relaykit/`。后台作业从 `main.go`、`controller/system_task_handlers.go` 进入系统任务框架。

Vue 从 `frontend/src/router/`、`frontend/src/stores/app.ts` 和 `frontend/src/api/` 核对页面、能力状态与真实 API。页面文件存在或后端 API 挂载，不单独证明产品入口已经开放。

## 辅助材料

- 工作区 `reference-projects/` 保存 AxonHub、new-api、sub2api 的本地参考源码；各次研究采用的固定提交和适用边界见对应历史记录，不代表参考目录当前 HEAD。
- `openapi/`：API 描述文件；覆盖范围与现行路由应交叉核对。
- `installation/`、`channel/`：安装和渠道辅助说明。RenAI 工作区生产发布优先使用根工作区的部署资产与技能。

## 本地文件维护

`frontend/node_modules/`、`frontend/dist/` 是可重建依赖与构建产物；清理前确认没有正在使用它们的开发或测试进程。`frontend/embed-dist/` 含受跟踪占位文件和可能的嵌入构建输出，不应整目录删除。

`.local-tests/` 是被忽略的探测工作区，可能同时包含脚本、测试数据库、可执行文件和截图，必须逐项识别。数据库、自动定价目录缓存、未合并补丁与验收证据不能只凭忽略规则或日期删除。版本文件、迁移脚本和历史契约也不是构建垃圾。

文档状态集中维护：运行契约写在统一路由和管理 API，执行方法写在验证及发布说明，当前有效结果写在验收记录和最新修复报告。被替代的阶段报告、同步台账和旧架构归档从工作树删除，历史版本由 Git 保留。服务器部署身份和资源配置由所属工作区的服务器档案维护；源码验证不能冒充容器部署，历史压测条件不能随当前配置变更而覆盖。

## 历史追溯

订阅下线的专属提交为 `a9ee8d717f66aacc394312828f6a403232eb4ecd`。原归档和计费研究已从工作区工作树移除，保留在根仓库提交 `2eb911272cc3eae95666dfd76de43f73097dea52` 中。在 RenAI 工作区的 `Ren2Hub/` 目录执行以下只读命令可查看历史，不需要恢复已删除文档：

```powershell
git show a9ee8d717f66aacc394312828f6a403232eb4ecd --stat
git -C .. show 2eb911272cc3eae95666dfd76de43f73097dea52:docs/maintenance/SUBSCRIPTION_RETIREMENT_2026_09_19.md
git -C .. show 2eb911272cc3eae95666dfd76de43f73097dea52:docs/maintenance/BILLING_ITERATION_2026_09_19.md
```

历史代码可在独立 worktree 中检查。重新引入订阅或回退运行版本仍需重新评估账务状态和迁移兼容性，不能将 Git 历史中的恢复指引视为现行部署授权。
