# Ren2Hub 新前端

该目录是 Ren2Hub 唯一的 Vue 3 Web 前端。

- Vue 应用直接挂载在网站根路径 `/`；旧 `/next` 和 `/next/*` 入口已移除，返回 404。
- 本地构建产物输出到 `frontend/dist/`；Docker 构建会把它复制到 `frontend/embed-dist/`，再由 Go `embed` 打入最终二进制。
- 首页、认证与已启用的 Console 模块统一调用同源真实后端 API。
- 后端通过 `/api/status.frontend_capabilities` 声明模块状态；未启用模块必须保持禁用并由路由守卫 fail-closed。

## 本地开发

```powershell
bun install
bun run dev
```

开发服务器默认监听 `5175`，可通过 `VITE_DEV_PORT` 修改。运行时不提供传输模式切换，所有 API 都使用同源 HTTP；本地开发由 `VITE_API_TARGET` 配置 Vite 代理目标，默认是 `http://localhost:3000`。外部文档和图片默认只允许同源地址；确需使用可信外部资源时，通过逗号分隔的 `VITE_TRUSTED_EXTERNAL_ORIGINS` 显式声明来源。

## 验证

```powershell
bun run test:run
bun run typecheck
bun run lint
bun run format:check
bun run build
```

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
  components/   common/home/auth/console/lab/layout
  composables/  复用状态与交互
  constants/    首页数据与导航定义
  i18n/         en/zh-CN 分域消息
  router/       路由、守卫和兼容重定向
  stores/       公开状态、真实会话与用户身份
  styles/       tokens/base/home/console
  types/        领域 contracts
  utils/        格式化与 URL 安全
  views/        路由页面
  __tests__/    全局测试设置
```

当前暂缓接入炼金室、Token 农场、小游戏、开票、市场和订阅/套餐管理。旧 Playground、Chat、Chat2Link 和 Chat Presets 路径已明确退休并返回 404。

主题和样式约束见 [docs/THEMES.md](docs/THEMES.md)。
