# Ren2Hub 前端

该目录是 Ren2Hub 唯一的 Web 前端，使用 Vue 3、TypeScript、Vite、Vue Router、Pinia、Vue I18n、Axios、Tailwind CSS 和 ECharts。依赖版本以 [package.json](package.json) 和 [bun.lock](bun.lock) 为准，使用 Bun 安装和运行脚本。

- Vue 应用直接挂载在网站根路径 `/`；`/next` 和 `/next/*` 返回 404。
- `bun run build` 先检查类型，再输出 `frontend/dist/`。仓库根目录的 `make build-web` 和 Docker 构建均通过 `scripts/prepare-frontend-embed.mjs` 校验并复制到 `frontend/embed-dist/`，再由 Go `embed` 打入最终二进制。
- 首页、认证与已启用的 Console 模块统一调用同源真实后端 API。
- 后端通过 `/api/status.frontend_capabilities` 声明模块状态；未启用模块必须保持禁用并由路由守卫 fail-closed。

## 本地开发

```powershell
bun install
bun run dev
```

以上命令在 `frontend/` 执行。后端需单独启动；仓库根目录的 `make dev-api` 启动本地 Docker 后端、PostgreSQL 和 Redis，`make dev-web` 启动 Vite。开发服务器默认监听 `5175`，可通过 `VITE_DEV_PORT` 修改；`VITE_API_TARGET` 配置 `/api`、`/pg`、`/v1` 和 `/mj` 的代理目标，默认是 `http://localhost:3000`。

运行时所有 API 都使用同源 HTTP。Access Token 只保存在浏览器内存中，刷新依赖 HttpOnly Cookie；API 层统一校验响应、处理刷新与会话切换，Pinia 保存跨页面身份和公开状态。Vue 登录页依据服务端能力显示密码、Passkey 和已配置的 OAuth 登录，要求额外验证时进入 2FA challenge；OAuth 登录、绑定与二次验证经同一弹窗流程完成，`/oauth/:provider` 回调页把结果交回发起页。各登录方式仍受后端设置控制，详情见[认证契约](../docs/authentication.md)。外部文档和图片默认只允许同源地址，可信外部资源通过逗号分隔的 `VITE_TRUSTED_EXTERNAL_ORIGINS` 声明来源。

## 验证

```powershell
bun run test:run
bun run typecheck
bun run lint
bun run format:check
bun run build
```

浏览器采用 `python3 -B scripts/testing/run.py browser`（应用根目录），对全新真实后端执行桌面/手机与明暗主题；详见[本地方案](../docs/testing/LOCAL_TESTING.md)。 本地浏览器场景覆盖登录和刷新、权限拒绝、Key 路由（新 Key 默认自动路由 + 全平台渠道、顺序路由空列表在前端拦截、全平台保存后刷新保持）、钱包读取、工单创建/详情与退役路径 404；不创建真实支付订单。

`bun run test:visual -- tests/visual/greeting-hero.spec.ts tests/visual/models.spec.ts tests/visual/billing.spec.ts` 使用原生合同的模拟数据验证保留页面。`PLAYWRIGHT_CAPTURE_SCREENSHOTS=true` 保留临时截图到 `output/`，不会更新正式基线。设置 `REN2HUB_LOCAL_SESSION` 时，Playwright 只运行隔离后端套件；直接调用 `test:visual` 时运行已有视觉与交互测试。

依赖审计使用 `bun audit --json`，安装检查使用 `bun install --frozen-lockfile`。

## 样式构建

Tailwind 4 通过 `vite.config.ts` 里的 `@tailwindcss/vite` 构建，入口是 `src/styles/tailwind.css`，明暗主题的语义令牌在 `tokens.css`。层级顺序、兼容 utility、字体继承和动画的约定见[主题与样式结构](docs/THEMES.md#tailwind-构建与兼容约定)。

响应断点统一在 CSS `@theme` 定义为 640/768/1024/1280/1536px，与 CSS 和 JavaScript 里的像素判断一致；浏览器默认字号改为 20px 时仍按原宽度切换布局，不固定或覆盖用户的根字号。`tests/visual/responsive-breakpoints.spec.ts` 在 16/20px、明暗主题下验证首页导航间距、移动菜单 resize 与控制台侧栏切换。

最低浏览器版本为 Safari 16.4、Chrome 111、Firefox 128，不提供更低版本的兼容构建。

## 路由

生产访问前缀为 `/`，下列路径是 Vue Router 内部路径：

- `/`：公开首页
- `/sign-in`、`/sign-up`、`/forgot-password`、`/reset`：认证与密码重置
- `/oauth/:provider`：OAuth 登录、绑定与二次验证的弹窗回调
- `/dashboard`、`/keys`、`/keys/:id/routing`、`/wallet`、`/usage-logs/*`、`/profile`：用户控制台
- `/models`：模型广场，提供模型聚合、渠道货架和比价矩阵；使用用户作用域的真实渠道与平台基准价格，见[模型广场契约](../docs/console/MODELS.md)
- `/channels`、`/users`、`/models/metadata`、`/system-settings/*`：管理控制台
- `/models/pricing`：Root 模型与工具定价、五源获取候选并回填草稿；候选只回填草稿，只有保存后生效，见[计费界面契约](docs/BILLING.md)

旧 `/console/*`、`/auth/*` 和 NewAPI 历史书签统一转换为根路径并保留 query/hash。`/next` 前缀不转换也不加载业务页面。匿名访问转到 `/sign-in`，登录回跳由实际路由记录校验。页面使用相同 Console 布局；布局与语言包由 metadata 指定，不依赖 URL 前缀。

前端按领域直接调用原生 API，不使用 `/api/next/*` 兼容层。用户模型目录为 `/api/user/model-catalog`，管理元数据为 `/api/models/`，不合并其语义。旧商城、开票、农场、小游戏、Gateway 配置与请求诊断入口均已退役；其页面路径返回 404，前端不再请求专属接口。Playground、Chat、Chat2Link、Chat Presets 及旧前缀变体也返回 404。详细矩阵与验证边界见 [路由约定](./ROUTING.md)。

## 领域接口

`src/api/` 集中认证与安全、工单、充值订单、返利、模型目录、Token 路由、统计、管理和运维的请求与 DTO 校验。普通接口校验 `success/data`，支付模块单独校验原生 `message/url/data`。缺字段、未知状态、超出 JavaScript 安全整数的原生额度和业务错误不转换为零、空列表或成功；请求取消和会话版本保护防止迟到响应覆盖当前页面。

用户额度使用 `quota/used_quota`，Token 使用 `remain_quota/used_quota/unlimited_quota`。工单调用 `/api/tickets` 与 `/api/admin/tickets`，返利概览、邀请名单、流水分开读取；转入只提交稳定的 `operation_id`，由后端全额转入。GMPay 使用人民币法币金额和完整加密资产标识，未知创建结果进入订单查询，不自动重新创建。

## 用量日志导出

用量日志使用专用 NDJSON 接口，独立选择导出日期；校验成功终态、记录数量和字段后生成 CSV、Excel 或 JSON。服务端默认限制 365 天查询跨度，前端不再静默裁剪为 30 天或截断为 10000 行。取消会中止请求，普通用户导出可由系统设置关闭；文本单元格按 `src/utils/spreadsheetExport.ts` 做公式防护，金额保持精确字符串；服务端合同和浏览器侧的大小上限见[用量日志契约](../docs/console/LOGS.md)。审计日志与订单页继续遵守各自的导出限制。

## 源码结构

共享弹窗、命令面板、渠道模型输入与工单回复在输入法组合期间不截获选词按键；正常 Enter 提交、Shift+Enter 换行及 Escape 关闭沿用原行为。活动页保留基础签到，不展示新人任务和阶梯奖励。模型供应商识别将 `meta/`、`muse-` 归入 Meta，`spark-` 仍属于讯飞。

```text
src/
  api/          HTTP transport、公开 API 与 Console API contracts
  assets/       由构建器处理的资源
  canvas/       首页世界地图引擎
  charts/       ECharts 适配
  components/   common/home/auth/console/admin/public/layout
  composables/  页面领域状态、请求取消与交互
  constants/    首页数据与导航定义
  features/     按功能聚合的模块：channels、pricing、task-plugins、topup-bonus
  i18n/         en/zh-CN 分域消息
  router/       路由、守卫和兼容重定向
  stores/       公开状态、会话、初始化与工单队列
  styles/       tokens/tailwind/base/home/console
  types/        领域 contracts
  utils/        格式化与 URL 安全
  views/        路由页面
  __tests__/    全局测试设置
```

`common` 和 `home` 语言包随首屏加载，`auth`、`console`、`setup` 按路由和当前语言延迟加载。`public/` 保存直接发布的资源，`src/assets/` 保存构建管理的图片和自托管字体。`dist/`、`embed-dist/` 与 `output/` 分别是构建产物、Go 嵌入产物和本地测试输出。

主题和样式约束见[主题文档](docs/THEMES.md)，金额精度与钱包界面见[计费界面契约](docs/BILLING.md)，窄屏未解决的问题见[移动端待办](docs/MOBILE.md)。
