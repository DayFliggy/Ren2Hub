# Ren2Hub 前端

该目录是 Ren2Hub 唯一的 Web 前端，使用 Vue 3、TypeScript、Vite、Vue Router、Pinia、Vue I18n、Axios、Tailwind CSS 和 ECharts。依赖版本以 [package.json](package.json) 和 [bun.lock](bun.lock) 为准，使用 Bun 安装和运行脚本。

- Vue 应用直接挂载在网站根路径 `/`；旧 `/next` 和 `/next/*` 入口已移除，返回 404。
- `bun run build` 先检查类型，再输出 `frontend/dist/`。仓库根目录的 `make build-web` 和 Docker 构建均通过 `scripts/prepare-frontend-embed.mjs` 校验并复制到 `frontend/embed-dist/`，再由 Go `embed` 打入最终二进制。
- 首页、认证与已启用的 Console 模块统一调用同源真实后端 API。
- 后端通过 `/api/status.frontend_capabilities` 声明模块状态；未启用模块必须保持禁用并由路由守卫 fail-closed。

## 本地开发

```powershell
bun install
bun run dev
```

以上命令在 `frontend/` 执行。后端需单独启动；仓库根目录的 `make dev-api` 启动本地 Docker 后端、PostgreSQL 和 Redis，`make dev-web` 启动 Vite。开发服务器默认监听 `5175`，可通过 `VITE_DEV_PORT` 修改；`VITE_API_TARGET` 配置 `/api`、`/pg`、`/v1` 和 `/mj` 的代理目标，默认是 `http://localhost:3000`。

运行时所有 API 都使用同源 HTTP。Access Token 只保存在浏览器内存中，刷新依赖 HttpOnly Cookie；API 层统一校验响应、处理刷新与会话切换，Pinia 保存跨页面身份和公开状态。Vue 登录页提供密码与 2FA 流程，OAuth 回调页面用于账号绑定，未提供 Passkey 或第三方登录入口；后端相应登录接口仍受各自设置控制，详情见[认证契约](../docs/authentication.md)。外部文档和图片默认只允许同源地址，可信外部资源通过逗号分隔的 `VITE_TRUSTED_EXTERNAL_ORIGINS` 声明来源。

## 验证

```powershell
bun run test:run
bun run typecheck
bun run lint
bun run format:check
bun run build
```

Vitest 检查 `src/` 下的行为测试。`bun run test:visual` 运行本地 Playwright 页面审计，覆盖桌面/移动端、明暗主题和相关错误状态；它不替代真实后端与生产产物的验证，具体边界见[路由约定](ROUTING.md)。

## 路由

生产访问前缀为 `/`，下列路径是 Vue Router 内部路径：

- `/`：公开首页
- `/sign-in`、`/sign-up`、`/forgot-password`、`/reset`：认证与密码重置
- `/oauth/:provider`：现有 OAuth 绑定回调
- `/dashboard`、`/keys`、`/wallet`、`/usage-logs/*`、`/profile`：用户控制台
- `/channels`、`/users`、`/models/metadata`、`/system-settings/*`：管理控制台
- `/lab/*`：炼金室预留路由；后端 capability 未启用时拒绝访问

旧 `/console/*`、`/auth/*` 和 NewAPI 历史书签统一转换为根路径并保留 query/hash。`/next` 前缀不再转换或加载业务页面。匿名访问转到 `/sign-in`，登录回跳由实际路由记录校验。页面使用相同 Console 布局；布局与语言包由 metadata 指定，不依赖 URL 前缀。

`/api/next/*` 是已有业务 API 门面，保留接口契约；`/api/models` 和 `/api/models/` 的不同语义也保留。Playground、Chat、Chat2Link、Chat Presets 及旧前缀变体返回 404。详细矩阵与验证边界见 [路由约定](./ROUTING.md)。

## 源码结构

```text
src/
  api/          HTTP transport、公开 API 与 Console API contracts
  assets/       由构建器处理的资源
  canvas/       首页世界地图引擎
  charts/       ECharts 适配
  components/   common/home/auth/console/admin/lab/public/layout
  composables/  页面领域状态、请求取消与交互
  constants/    首页数据与导航定义
  i18n/         en/zh-CN 分域消息
  router/       路由、守卫和兼容重定向
  stores/       公开状态、会话、初始化与工单队列
  styles/       tokens/base/home/console
  types/        领域 contracts
  utils/        格式化与 URL 安全
  views/        路由页面
  __tests__/    全局测试设置
```

当前暂缓接入炼金室、Token 农场、小游戏、开票和市场。本站订阅与套餐管理已移除，原页面路径返回 404；钱包充值继续保留。旧 Playground、Chat、Chat2Link 和 Chat Presets 路径已明确退休并返回 404。

`common` 和 `home` 语言包随首屏加载，`auth`、`console`、`lab`、`setup` 按路由和当前语言延迟加载。`public/` 保存直接发布的资源，`src/assets/` 保存构建管理的图片和自托管字体。`dist/`、`embed-dist/` 与 `output/` 分别是构建产物、Go 嵌入产物和本地测试输出。

主题和样式约束见[主题文档](docs/THEMES.md)，金额精度、钱包与对账界面见[计费界面契约](docs/BILLING.md)。
