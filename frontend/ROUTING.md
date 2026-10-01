# Ren2Hub 路由约定

Vue 是唯一 Web 前端，从 `/` 加载，静态构建资源位于 `/assets/`。路由定义以 `src/router/` 为准，控制台侧边栏菜单与访问条件统一维护在 `src/constants/navigation/consoleNav.ts`，顶部导航（含仅 Admin 可见的“运维监控”）维护在 `src/constants/navigation/topNav.ts`；API 路径与鉴权遵循现有 Go 接口。

| 域       | 规范路径                                                                                                                           | 访问约束                                                                                                                |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| 公共     | `/`、`/about`、`/pricing/:modelId?`、`/rankings`、法律文档、错误页                                                                 | 公开模块/法律文档配置                                                                                                   |
| 认证     | `/sign-in`、`/sign-up`、`/forgot-password`、`/reset`、`/oauth/:provider`、`/setup`                                                 | 当前 Bearer/refresh 会话                                                                                                |
| 用户     | `/dashboard`、`/models`、`/keys`、`/keys/:id/routing`、`/wallet`、`/activity`、`/invite`、`/profile`、`/settings`、`/tickets/:id?` | 会话和模块 capability                                                                                                   |
| 日志     | `/usage-logs`、`/usage-logs/drawing`、`/usage-logs/task`、`/usage-logs/audit`                                                      | logs capability；审计日志需 audit capability，全部账号需 `audit:read`                                                   |
| 管理     | `/channels`、`/users`、`/redemption-codes`、`/orders`、`/ticket-management/:id?`                                                   | Admin 和已有权限规则                                                                                                    |
| 模型管理 | `/models/metadata`、`/models/vendors`、`/models/prefill-groups`                                                                    | Admin，admin capability                                                                                                 |
| 系统     | `/system-settings/:domain/:section?`、`/ops`                                                                                       | 设置：Root，admin capability；`/ops`：Admin，admin capability；其中系统任务与计费健康仅 Root，上游健康需 `channel:read` |
| 账务对账 | `/billing/reconciliations`                                                                                                         | Root，billing_reconciliation capability                                                                                 |
| 延期模块 | 市场、发票、Farm、Bigame、Lab                                                                                                      | 保留源码，capability 未开放时拒绝进入                                                                                   |

普通用户模型目录使用 `/models`，管理 metadata 使用 `/models/metadata`；供应商与预填组路径跳转到模型管理页的对应视图。控制台共享布局，页面路径使用根路径，布局与语言包由 metadata 指定。Admin 为角色值 ≥10，Root 为 ≥100；渠道和工单管理还检查相应资源权限。

守卫先检查初始化状态，再检查模块能力、加载语言包并恢复登录会话。初始化检查失败进入 `/setup/error`，未完成初始化进入 `/setup`；需要登录的页面转到 `/sign-in`，回跳地址必须通过站内路由校验。

Root 模型定价入口为 `/system-settings/billing/model-pricing`；旧 `billing/pricing`、自动价格设置书签跳转到此处并保留 query/hash。模型定价与 `/models/metadata` 独立。

## 兼容边界

- `src/router/legacyRoutes.ts` 集中转换旧书签、日志 tab 和设置 section，守卫保留 query/hash。未知页面落入 404，不将未知控制台路径当作首页。
- 系统实例页已退休：`/system-info`、`/admin/system-info` 及其 `/console/` 旧前缀进入 `/ops`。
- 旧账号审计 `/audit` 进入 `/usage-logs/audit`；旧操作日志 `/usage-logs/operations`、`/logs/operations` 进入 `/usage-logs/audit?scope=all`。无 `audit:read` 权限时页面仍只读取本人记录。
- `/next` 和 `/next/*` 已退休：Go 返回 HTTP 404，Vue 内部导航进入 404 页面，不再转换旧书签。
- `/api`、`/api/next`、Relay 和 `/dashboard/billing` 是后端命名空间，缺失接口返回 JSON 404。绝不清理 API 尾斜杠：`/api/models` 是用户目录，`/api/models/` 是管理 metadata。
- 已退出的 Playground、Chat、Chat2Link、Chat Presets、模型部署及其旧前缀路径返回 404；Vue 内部导航也拒绝进入。
- 本站订阅与套餐管理已移除：`/subscription`、`/plan-management`、`/plans` 及其 `/console/` 旧前缀路径进入标准 404，不再注册功能路由或请求订阅接口。
- 缺失 capability 按禁用处理；后端不可达时不挂载业务页面。服务端鉴权仍为最终权限边界。

## 构建与交付验证

构建顺序为 `frontend/dist` → `scripts/prepare-frontend-embed.mjs` → `frontend/embed-dist` → Go 二进制。`main.go` 使用 `//go:embed all:frontend/embed-dist`，确保下划线开头的 Vite chunk 也被嵌入。

`prepare-frontend-embed.mjs` 校验完整 manifest 依赖和文件，`TestProductionEmbeddedAssetGraph` 检查最终 Go 嵌入资源，Docker 冷构建执行该测试。只检查 index 或入口 JS 的 HTTP 200 不足以验证页面可用；还需检查懒加载模块、字体和页面交互。初次导航不显示全屏加载提示，初始化检查完成后显示页面；导航失败会显示错误和重试入口。

`bun run test:visual` 默认启动或复用本地 Vite 服务。若验证 Go 产物，先准备 embed、构建并启动本地独立测试实例，再将 `PLAYWRIGHT_PORT` 设为该实例端口；Playwright 会复用该服务。`PLAYWRIGHT_THEME=light` 选择浅色。模拟 API 的用例验证界面与请求契约，不能替代使用真实 API 的浏览器冒烟验证。

Vue 登录页提供密码与 2FA 流程，OAuth 回调页面用于账号绑定，未提供 Passkey、OAuth、WeChat 或 Telegram 登录入口。后端仍保留这些登录处理，是否可用由对应设置和请求校验决定；不能把前端入口范围等同于后端接口范围。会话和绑定规则见[认证契约](../docs/authentication.md)，Relay 边界见[统一路由](../docs/routing/UNIFIED_ROUTING.md)。
