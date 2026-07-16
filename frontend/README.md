# 前端重构工作区

本目录用于集中记录和推进前端重构任务。当前可运行前端仍位于：

- `web/default/`：主前端，React 19 + TypeScript + Rsbuild。
- `web/classic/`：兼容前端，React 18 + Semi Design + Rsbuild。

后续重构若迁移到本目录，应先明确构建入口、Go 静态资源嵌入路径和主题切换策略；在迁移完成前，不应把本目录视为生产构建源。

## 1. 当前前端架构

### 1.1 构建与交付

- Go 入口通过 `main.go` 嵌入 `web/default/dist` 与 `web/classic/dist`。
- `router/web-router.go` 根据后端主题配置选择静态文件系统，并为前端路由提供 SPA 回退。
- `/api`、`/v1` 与 `/assets` 未命中时不会回退到 HTML，而是返回中继或资源错误。
- 配置 `FRONTEND_BASE_URL` 时，非主节点可把网页请求重定向到独立前端地址。

### 1.2 前端请求链路

主前端统一使用 `web/default/src/lib/api.ts` 中的 Axios 实例：

- `baseURL` 为空，默认同源访问后端。
- `withCredentials: true`，携带登录 Session Cookie。
- 从 `localStorage.uid` 注入 `New-Api-User` 请求头；后端会校验它与 Session 或访问令牌中的用户 ID 一致。
- 并发相同 GET 请求默认去重。
- 统一处理 `{ success, message, data }` 业务响应、HTTP 错误和 401 会话失效。
- React Query 负责服务端数据缓存和变更失效，Zustand 保存登录态及前端配置。

鉴权主链路为：

```text
TanStack Router 路由守卫
  -> Axios: Cookie + New-Api-User
  -> Gin /api 路由
  -> UserAuth / AdminAuth / RootAuth / RequirePermission
  -> controller
  -> service / model / setting
```

Playground 使用用户 Session 访问 `/pg/chat/completions`，由后端执行渠道分发；外部 API 客户端则通过 Bearer Token 访问 `/v1/*`、Gemini、Midjourney、视频等中继路由。

## 2. 当前前端功能清单

### 2.1 公共访问与账号入口

- 首页：动态首页内容、产品能力展示、接入步骤、统计信息和行动入口。
- 模型广场与定价：模型筛选、价格展示、计费模式、性能与可用性信息、模型详情。
- 排行榜：按周期查看公开排行。
- 关于、用户协议、隐私政策和外部文档入口。
- 首次安装向导：检查初始化状态并完成系统初始化。
- 登录、注册、退出、邮箱验证码、忘记密码和密码重置。
- OAuth 登录与绑定：GitHub、微信、Telegram、OIDC/自定义 OAuth 等后端已启用的提供方。
- 两步验证登录、Passkey 登录、Turnstile 人机验证。
- 401、403、404、500、503 等错误页面。

### 2.2 普通用户控制台

- 控制台概览：额度、消费、请求量、Token、用户/模型分布、流量关系、公告、API 地址、FAQ 和 Uptime 状态。
- Playground：选择分组和模型，配置温度、Top P、最大 Token、惩罚项与 Seed；支持流式响应、停止生成、编辑、复制、重试和本地会话保存。
- API Keys：创建、编辑、启停、删除和批量删除；配置额度、有效期、模型限制、IP 限制、分组和跨组重试；敏感 Key 查看需要加强验证。
- 使用日志：查看普通中继日志、绘图/Midjourney 日志和异步任务日志；支持统计、筛选、分页和详情查看。
- 钱包与充值：查看余额与充值配置，发起易支付、Stripe、Creem、Waffo、Waffo Pancake 等已配置支付方式，查看充值记录和管理员补单。
- 邀请返利：查看邀请码、返利额度并转入账户额度。
- 订阅：查看可购套餐、当前订阅、扣费偏好，使用余额或已配置支付渠道购买。
- 个人资料：修改基础信息、语言、密码、邮箱/微信/自定义 OAuth 绑定和侧边栏偏好。
- 安全管理：生成访问令牌、管理 Passkey、启停 2FA、重新生成备份码、安全验证。
- 每日签到：查看月度签到状态并领取配置的签到奖励。
- 聊天入口：使用系统配置的聊天预设或外部聊天链接。

### 2.3 管理员功能

- 渠道管理：查询、搜索、新增、编辑、复制、删除、批量启停、批量删除、余额更新、连通性测试、模型拉取和缺失模型检查。
- 渠道高级能力：多 Key 管理、标签管理、模型映射、参数覆盖、自定义请求配置、状态码风险保护、Ollama 模型拉取/删除、上游配置更新和 Codex 用量查询。
- 细粒度渠道权限：读取、操作、写入、敏感写入等权限由后端 `RequirePermission` 统一校验。
- 模型元数据：模型增删改查、供应商管理、缺失模型检查、上游同步预览与同步、预填分组管理。
- 模型部署：连接测试、硬件/区域/副本查询、价格估算、创建、改名、扩容、配置更新、容器详情和日志查看。
- 用户管理：用户增删改查、搜索、启停、角色/分组/额度调整、Passkey/2FA 重置、OAuth 和第三方绑定清理、用户订阅管理。
- 兑换码：创建、编辑、启停、搜索、删除和清理失效兑换码。
- 订阅管理：套餐增删改、启停、绑定用户、创建/重置/失效/删除用户订阅。
- 系统实例：查看集群实例并删除失效实例记录。
- 日志维护：创建后台日志清理任务、查看当前任务、历史任务与执行状态。

### 2.4 Root 系统设置

- 站点与品牌：系统信息、Logo、系统名称、公告、顶部导航和侧边栏模块。
- 认证：基础登录注册、OAuth、Passkey、Turnstile、Telegram/微信及自定义 OAuth 提供方。
- 计费与支付：额度规则、货币显示、模型价格、分组倍率、支付网关和签到奖励。
- 模型与路由：全局模型配置、路由可靠性、Gemini、Claude、Grok、渠道亲和性和模型部署。
- 安全与限制：请求速率、敏感词、SSRF 防护和 Token 限制。
- 控制台内容：数据看板、公告、API 地址、FAQ、Uptime Kuma、聊天预设和绘图设置。
- 运维：系统行为、监控告警、SMTP、Worker 代理、日志维护、性能状态、磁盘缓存、GC、日志文件和更新检查。
- 上游倍率同步、渠道亲和缓存检查/清理、模型倍率重置和控制台配置迁移。

### 2.5 横向体验能力

- 多语言：主前端使用 i18next，支持英文、中文、法文、俄文、日文和越南文等现有语言包。
- 主题与外观：亮色/暗色主题、字体、方向和后端下发的主题定制。
- 响应式导航：桌面侧边栏、移动抽屉、上下文式系统设置导航和动态模块显隐。
- 表格与大列表：筛选、分页、批量操作、虚拟滚动和列配置。
- 图表：消费、流量、模型、用户和性能指标可视化。
- Markdown、代码高亮、数学公式、Mermaid、二维码、OTP 和拖拽等内容能力。

## 3. 重点前后端对接模块

| 前端模块 | 主要接口 | 后端入口与边界 |
| --- | --- | --- |
| 系统初始化与公共配置 | `/api/setup`、`/api/status`、`/api/notice`、`/api/home_page_content` | `router/api-router.go`；公开接口，状态配置还驱动品牌、导航和模块显隐 |
| 登录与账号安全 | `/api/user/*`、`/api/oauth/*`、`/api/verify` | `controller/user.go`、OAuth、Passkey、2FA 与安全验证控制器；Session Cookie + `New-Api-User` |
| 控制台数据 | `/api/data/*`、`/api/log/*`、`/api/uptime/status` | 用户与管理员接口分离；通过 `UserAuth`/`AdminAuth` 限制数据范围 |
| API Key | `/api/token/*` | Token 控制器和模型层；Key 明文读取接口带关键操作限流、禁缓存和加强验证 |
| 渠道 | `/api/channel/*` | `router/channel-router.go`；`AdminAuth` 后再按渠道权限细分，敏感 Key 仅 Root 可读 |
| 模型与部署 | `/api/models/*`、`/api/vendors/*`、`/api/prefill_group/*`、`/api/deployments/*` | 管理员接口；控制器调用模型元数据与部署服务 |
| Playground | `/pg/chat/completions` | `router/relay-router.go`；用户 Session 鉴权后进入统一分发和中继链路 |
| 外部 AI API | `/v1/*`、`/v1beta/*`、Midjourney、视频路由 | Bearer Token 鉴权、分组/模型/IP/额度校验、渠道分发、协议转换、计费与日志 |
| 钱包与订阅 | `/api/user/topup*`、支付接口、`/api/subscription/*` | 用户与管理员路由分离；支付回调为公开入口并带请求体限制 |
| 系统设置 | `/api/option/*`、`/api/performance/*`、`/api/ratio_sync/*` | Root 权限；修改配置后由 setting/service 层更新运行状态或持久化配置 |

## 4. 重构时必须保持的契约

- 保持后端统一响应结构和业务错误处理语义。
- 保持 Session Cookie 与 `New-Api-User` 双重用户校验，不以纯前端状态代替服务端鉴权。
- 保持普通用户、管理员、Root 和渠道细粒度权限的差异。
- 敏感 Key、支付、绑定、2FA、Passkey 等操作必须保留限流、禁缓存、二次确认或加强验证。
- 保持 `/api/*` 管理接口与 `/v1/*`、`/pg/*` 中继接口的职责隔离。
- 保持默认/经典双主题交付，或在移除经典前端前完成配置、构建和回退策略迁移。
- 所有用户可见文案继续接入 i18n；动态导航和模块显隐继续以后端配置为源。

