# 原生认证与权限

浏览器采用NewAPI短期Bearer JWT、HttpOnly刷新Cookie和服务端登录会话。Vue只在内存保存access token，刷新、退出及跨标签页身份变化遵循原生会话合同。数据库会话、账户状态及鉴权版本为授权依据；缓存不能扩大已撤销权限。

| 凭据 | 用途 |
| --- | --- |
| 浏览器会话 | 登录控制台、个人工单、GMPay下单和支付配置 |
| 原生管理PAT | `user_access_tokens`、`nap_`凭据；按scope与当前用户角色/资源权限交集执行 |
| API Key | 模型中继、模型限制、分组、额度和IP限制；不能代替管理会话 |

PAT管理使用 `/api/user/access_tokens`、`/catalog`、`/scopes`，创建、变更scope和撤销按原生安全验证绑定执行。明文只在创建时返回；后端存储摘要和授权信息。不存在旧 `personal_access_tokens` / `pat_` 产品或 `/api/next` 管理层。具体字段以 `controller/access_token.go`、`service/access_token_scope.go` 为准。

每个管理路由在原生scope注册表中明确声明，未声明PAT不放行。工单管理新增 `ticket:read/reply/manage`，Token私有路由新增 `routing:read/write`；二者都注册到同一authz模型。个人工单与GMPay下单session-only，即使PAT携带wallet或ticket权限也拒绝。

刷新和退出分别为 `POST /api/user/auth/refresh`、`POST /api/user/auth/logout`。会话查询与撤销使用 `/api/user/sessions`。Secure Cookie模式要求精确HTTPS Origin，可信URL不是CORS白名单。公开部署设置稳定 `SESSION_SECRET`、`CRYPTO_SECRET`、`SESSION_COOKIE_SECURE=true` 与精确 `SESSION_COOKIE_TRUSTED_URL`；本地候选HTTP配置不得复制到公网。

`TRUSTED_PROXIES` 仅声明实际反向代理，不能信任客户端提供的任意转发头。API与模型请求限流沿用原生middleware及Redis/内存行为；不再声明旧“每逻辑请求成功名额持久租约”和独立准入合同。当前产品仅单节点，认证子系统的缓存能力不授予扩容授权。

站点OAuth、Passkey、两步验证和密码加密继续采用NewAPI。官方订阅账号渠道退役不等于删除站点OAuth登录。权限菜单由后端能力/权限投影控制，最终授权始终在服务端完成。
