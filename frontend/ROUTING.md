# Ren2Hub 路由约定

Vue 是唯一 Web 前端，从 `/` 加载，静态构建资源位于 `/assets/`。路由定义以 `src/router/` 为准，控制台侧边栏菜单与访问条件统一维护在 `src/constants/navigation/consoleNav.ts`，顶部导航（含仅 Root 可见的“运维监控”）维护在 `src/constants/navigation/topNav.ts`；API 路径与鉴权遵循现有 Go 接口。

| 域       | 规范路径                                                                                                                                        | 访问约束                                                                                                                        |
| -------- | ----------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| 公共     | `/`、`/about`、`/pricing/:modelId?`、`/rankings`、法律文档、错误页                                                                              | 公开模块/法律文档配置                                                                                                           |
| 认证     | `/sign-in`、`/sign-up`、`/forgot-password`、`/reset`、`/oauth/:provider`、`/setup`                                                              | 当前 Bearer/refresh 会话                                                                                                        |
| 用户     | `/dashboard`、`/models`、`/keys`、`/keys/:id/routing`、`/wallet`、`/activity`、`/invite`、`/profile`、`/security`、`/settings`、`/tickets/:id?` | 会话和模块 capability                                                                                                           |
| 日志     | `/usage-logs`、`/usage-logs/drawing`、`/usage-logs/task`、`/usage-logs/audit`                                                                   | logs capability；审计日志需 audit capability，全部账号需 `audit:read`                                                           |
| 管理     | `/channels`、`/users`、`/redemption-codes`、`/orders`、`/ticket-management/:id?`                                                                | Admin 和已有权限规则                                                                                                            |
| 模型管理 | `/models/metadata`、`/models/vendors`、`/models/pricing`、`/models/prefill-groups`                                                              | Admin，admin capability；定价分区仅 Root                                                                                        |
| 插件     | `/task-plugins`、`/request-processing`、`/oauth/plugin/:key/callback`                                                                           | 插件管理和请求处理：Root，taskPlugins capability，请求处理另需 `plugin:write`；插件登录回调：Admin 加 `channel:sensitive_write` |
| 系统     | `/system-settings/:domain/:section?`、`/ops`                                                                                                    | 设置：Root，admin capability；`/ops`：Root，admin capability；按原生 performance 与系统任务权限读取                             |
| 排除模块 | 独立市场、发票、Farm、Bigame、炼金室（`/lab/*`）、旧 Gateway 配置与请求诊断                                                                     | 不注册功能路由，进入标准 404；没有对应的页面、导航、专属 API 和请求调用                                                         |

普通用户模型目录使用 `/models`。管理侧边栏的“模型管理”入口分为“模型”（`/models/metadata`）、“供应商”（`/models/vendors`）和 Root 可见的“定价”（`/models/pricing`），切换分区时保留 query；上游的“部署”分区已停用。模型分区的筛选参数沿用上游的 `filter/vendor/status/sync/page/pageSize`，供应商分区为 `vFilter/vPage/vPageSize`。`/models/prefill-groups` 跳转到模型分区并以 `manage=prefill-groups` 打开预填充分组弹窗。控制台共享布局，页面路径使用根路径，布局与语言包由 metadata 指定。Admin 为角色值 ≥10，Root 为 ≥100；渠道和工单管理还检查相应资源权限。

守卫先检查初始化状态，再检查模块能力、加载语言包并恢复登录会话。初始化检查失败进入 `/setup/error`，未完成初始化进入 `/setup`；需要登录的页面转到 `/sign-in`，回跳地址必须通过站内路由校验。

守卫检查模块能力时只等 `/api/status` 的结果，不等首页的公告、可用率和模型数量。应用内的导航超过 120ms 未完成时，页面顶部显示进度条（`NavigationProgress`）；初次导航不显示。

## 页面切换

- 控制台页面切换时，旧页面立即移除，新页面在同一帧挂载并以 160ms 淡入；内容区宽度不做动画。规则写在 `src/styles/console.css`，只用透明度，不用位移，固定定位的子元素不受影响。
- 路由 `meta.tabGroup` 相同的页面是同一页的页签（现有四个日志页）：切换时带 `data-page-header` 的面包屑行和其中的页签条保持不动，只有下方内容淡入。
- 同一组件内的子页面（模型管理分区、系统设置的域和分区、仪表盘页签）给面板加 `console-panel-enter` 类并按分区设 `key`，得到相同的内容淡入。
- 公共页、认证页、控制台、初始化向导各算一个外壳。只有跨外壳的跳转整页淡入，同一外壳内直接替换。
- 鼠标在站内链接上停留 80ms、触摸或键盘焦点到达链接时，预取该页面的代码和语言包；用 `router.push` 跳转的按钮通过 `data-prefetch-route="<路由名>"` 声明目标。这类预取只是猜测，浏览器开启省流量模式时关闭。进入登录页后总会预取登录后的目标页，在登录页显示完成后（400ms）开始，不与登录页的渲染争抢。

Root 模型定价是模型管理的 `pricing` 分区，路径为 `/models/pricing`；侧边栏高亮“模型管理”。切换分区时，若价格草稿未保存会先确认。旧 `/system-settings/billing/model-pricing`、`billing/pricing` 和自动价格设置书签跳转到此处并保留 query/hash。非 Root 管理员看不到定价分区，直接访问会回到控制台入口。

## 兼容边界

充值加赠位于 Root 的「系统设置 → 计费 → 充值加赠」`/system-settings/billing/topup-bonus`，侧栏和仪表盘配置入口统一指向系统设置。该编辑器使用独立的版本化加赠 API；切换设置分区前会保护未保存草稿，保存期间阻止离开。

- `src/router/legacyRoutes.ts` 集中转换旧书签、日志 tab 和设置 section，守卫保留 query/hash。未知页面落入 404，不将未知控制台路径当作首页。
- 系统实例页的旧路径 `/system-info`、`/admin/system-info` 及其 `/console/` 前缀进入 `/ops`。
- 旧账号审计 `/audit` 进入 `/usage-logs/audit`；旧操作日志 `/usage-logs/operations`、`/logs/operations` 进入 `/usage-logs/audit?scope=all`。守卫与请求均遵循当前 `audit:read` 和 capability，不能以旧书签扩大授权。
- `/next` 和 `/next/*`：Go 返回 HTTP 404，Vue 内部导航进入 404 页面，不转换为其他页面。
- `/api`、Relay 和 `/dashboard/billing` 是后端命名空间，缺失接口返回 JSON 404。`/api/next` 已退役，前端不请求也不实现兼容层。绝不统一清理 API 尾斜杠；用户授权目录为 `/api/user/model-catalog`，管理 metadata 为 `/api/models/`。
- `/market`、`/invoice`、`/farm`、`/bigame` 及旧 Gateway 配置、请求诊断页面路径不注册路由，进入标准 404；旧资金事件和 Gateway/请求诊断 API 也不保留隐藏调用。
- Playground、Chat、Chat2Link、Chat Presets、模型部署及其旧前缀路径返回 404；Vue 内部导航也拒绝进入。
- 本站没有订阅与套餐管理：`/subscription`、`/plan-management`、`/plans` 及其 `/console/` 前缀路径进入标准 404，不注册功能路由，也不请求订阅接口。
- 缺失 capability 按禁用处理；后端不可达时不挂载业务页面。服务端鉴权仍为最终权限边界。

## 构建与交付验证

构建顺序为 `frontend/dist` → `scripts/prepare-frontend-embed.mjs` → `frontend/embed-dist` → Go 二进制。`main.go` 使用 `//go:embed all:frontend/embed-dist`，确保下划线开头的 Vite chunk 也被嵌入。

`prepare-frontend-embed.mjs` 校验完整 manifest 依赖和文件，并给 js、css、svg 等文本文件写入更小的 `<文件>.br`（brotli 质量 11），客户端接受 `br` 时 Go 直接发送它，不在运行时压缩；`TestProductionEmbeddedAssetGraph` 检查最终 Go 嵌入资源，Docker 冷构建执行该测试。只检查 index 或入口 JS 的 HTTP 200 不足以验证页面可用；还需检查懒加载模块、字体和页面交互。初次导航不显示全屏加载提示，初始化检查完成后显示页面；导航失败会显示错误和重试入口。

浏览器验收使用[本地统一方案](../docs/testing/LOCAL_TESTING.md)，每轮创建真实后端和独立空库，禁止复用已有服务。

Vue 登录页按服务端配置显示密码、Passkey 和 OAuth 登录入口；需要额外验证时进入 2FA challenge。OAuth 弹窗与回调同时用于登录、账号绑定和二次验证，支持已配置的 GitHub、Discord、Linux DO、OIDC、Telegram 与自定义 provider；没有独立 WeChat 登录入口。模块 capability 表示接口已接入，不能代替供应商配置或后端请求校验。会话和绑定规则见[认证契约](../docs/authentication.md)，Relay 边界见[统一路由](../docs/routing/UNIFIED_ROUTING.md)。
