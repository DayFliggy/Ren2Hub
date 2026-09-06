# Ren2Hub 路由约定

Vue 是唯一 Web 前端，从 `/` 加载，静态构建资源位于 `/assets/`。页面命名参考工作区 `reference-projects/new-api/web/src/routes` 和 `src/lib/legacy-route.ts`；API 路径与鉴权继续遵循现有 Go 接口。

| 域       | 规范路径                                                                                                                  | 访问约束                              |
| -------- | ------------------------------------------------------------------------------------------------------------------------- | ------------------------------------- |
| 公共     | `/`、`/about`、`/pricing/:modelId?`、`/rankings`、法律文档、错误页                                                        | 公开模块/法律文档配置                 |
| 认证     | `/sign-in`、`/sign-up`、`/forgot-password`、`/reset`、`/oauth/:provider`、`/setup`                                        | 当前 Bearer/refresh 会话              |
| 用户     | `/dashboard`、`/models`、`/keys/:id/routing`、`/wallet`、`/activity`、`/invite`、`/profile`、`/settings`、`/tickets/:id?` | 会话和模块 capability                 |
| 日志     | `/usage-logs`、`/usage-logs/drawing`、`/usage-logs/task`、`/usage-logs/operations`                                        | logs capability；操作日志需 Admin     |
| 管理     | `/channels`、`/users`、`/redemption-codes`、`/orders`、`/ticket-management/:id?`                                          | Admin 和已有权限规则                  |
| 模型管理 | `/models/metadata`、`/models/vendors`、`/models/prefill-groups`、`/models/deployments`                                    | Admin，admin capability               |
| 系统     | `/system-settings/:domain/:section?`、`/system-info`                                                                      | Root，admin capability                |
| 延期模块 | 市场、订阅、发票、Farm、Bigame、Lab                                                                                       | 保留源码，capability 未开放时拒绝进入 |

普通用户模型目录使用 `/models`，管理 metadata 使用 `/models/metadata`，避免将参考项目的路径含义误用于现有权限。

## 兼容边界

- `src/router/legacyRoutes.ts` 集中转换旧书签、日志 tab 和设置 section，守卫保留 query/hash。未知页面落入 404，不将未知控制台路径当作首页。
- Go 对 `/next` 做同源路径规范化，避免重复斜杠变成站外重定向。`/next` 仅为旧 Web 书签兼容入口。
- `/api`、`/api/next`、Relay 和 `/dashboard/billing` 是后端命名空间，缺失接口返回 JSON 404。绝不清理 API 尾斜杠：`/api/models` 是用户目录，`/api/models/` 是管理 metadata。
- 已退出的 Playground、Chat、Chat2Link、Chat Presets 及旧前缀路径由 Go 返回 404；Vue 内部导航也拒绝进入。
- 缺失 capability 按禁用处理；后端不可达时不挂载业务页面。服务端鉴权仍为最终权限边界。

## 空白页与交付验证

2026-09-06 的运行镜像 `bc69bf23` 引用 `_plugin-vue_export-helper-BDNMzG2s.js`，该请求实际返回 404，浏览器 `#app` 为空。Go 默认 embed 忽略下划线文件；此前的 `5d8d4a13` 已添加 `all:`，但当时运行镜像尚未包含该提交。

`prepare-frontend-embed.mjs` 校验完整 manifest 依赖和文件，`TestProductionEmbeddedAssetGraph` 检查最终 Go 嵌入资源，Docker 冷构建执行该测试。只检查 index 或入口 JS 的 HTTP 200 不构成页面恢复证据。初次导航会显示加载状态；导航失败会显示错误和重试入口。

Playwright 路由用例可在真正 Go 构建上运行：准备 embed 后启动本地独立测试实例，设置 `PLAYWRIGHT_PORT`，运行 `test:visual`。`PLAYWRIGHT_THEME=light` 选择浅色。测试夹具只模拟 API，HTML、模块与字体由实际二进制服务；生产恢复还必须用真实 API 的浏览器冒烟验证。

本次路由重构不表示所有旧认证功能已迁移：当前 OAuth 回调用于账号绑定，Passkey 登录仍受后端禁用。本轮没有实现这些额外认证流程，也没有更改支付、数据库或 Relay 业务语义。
