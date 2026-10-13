# 用户鉴权与登录会话

面板鉴权采用短期 Access Token、HttpOnly Refresh Cookie 与服务端登录会话控制面的组合。面板请求不使用 Gin session 或 `session` Cookie，也不要求 `New-Api-User` 请求头。

## 鉴权模型

- Access Token 是有效期 15 分钟的 JWT，只保存在浏览器内存中，通过 `Authorization: Bearer <token>` 发送。
- Refresh Token 是随机不透明值，有效期最长 30 天。浏览器只通过 `HttpOnly`、`SameSite=Strict` Cookie 持有它；服务端仅保存 HMAC 摘要，并在每次刷新时轮换。
- `user_sessions` 是登录会话控制面，记录设备、IP、登录方式、最后活跃时间、到期时间和撤销状态。数据库中的 Session 状态是最终权威；撤销传播速度取决于下文所述的 Redis 拓扑。
- 用户的密码、状态、角色或安全因子发生安全相关变化时，`auth_version` 会递增并使旧登录会话失效。
- Redis 缓存保存用户鉴权快照和登录会话快照。版本栅栏和撤销 tombstone 防止旧缓存重新授权；Session 快照使用跟随 `SYNC_FREQUENCY` 的短 TTL，缓存未命中或未启用 Redis 时回退到数据库校验。

`SESSION_SECRET` 用于派生 Access Token、Security Proof、Refresh Token 摘要和 AuthFlow 摘要的不同用途密钥。生产环境及多节点部署必须在所有节点配置相同的高强度随机值；更换该值会使现有登录、临时鉴权流程和 Security Proof 全部失效。

## 多节点 Redis 拓扑

本节仅说明认证子系统的存储语义；Ren2Hub 当前采用单进程网关，不支持据此增加业务副本。应用部署仍遵守[单实例发布契约](routing/GATEWAY_RELEASE.md)。

多节点部署必须共用同一主数据库。登录 Session、账户级活跃 Session 上限和签发窗口计数都以数据库为权威，因此这些限制在应用节点间全局生效。Redis 中的 Session Hash（包含 `revoking`/`revoked` tombstone）只是缓存，其 TTL 为 Session 剩余寿命与有效 `SYNC_FREQUENCY` 中的较小值；`SYNC_FREQUENCY` 默认及非法值回退均为 `60` 秒。读取缓存不会续期，过期后会按 SID 回源数据库。延迟完成的 active 缓存回写只能使用其数据库观察窗口尚未消耗的 TTL，不能在撤销 tombstone 到期后重新启动一个完整缓存周期。

| Redis 部署方式         | Session 状态传播                                                                                                                         | 限流语义                                                 |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| 所有节点共享 Redis     | 正常撤销和版本发布通过同一缓存即时传播                                                                                                   | Redis 限流额度在所有节点间共享                           |
| 每个节点使用独立 Redis | 最迟在该节点 Session 缓存 TTL 到期后回源收敛，即不超过有效 `SYNC_FREQUENCY`；版本轮换期间，新 Token 在持有旧缓存的节点上可能短暂返回 401 | 每个节点独立计数，集群总额度最坏约为单节点阈值乘以节点数 |
| 不使用 Redis           | 每次 Session 校验直接读取数据库                                                                                                          | 使用各节点的内存限流器，额度同样按节点独立               |

`SYNC_FREQUENCY` 越大，独立 Redis 部署的陈旧窗口越长；值越小，每个活跃 SID 在每个节点上回源数据库的频率越高。默认配置下，持续活跃的 Session 每个节点最多约每 60 秒增加一次数据库主键点查。共享 Redis 时，撤销 tombstone 和版本发布仍保持即时传播。

所有节点必须使用相同的 `SESSION_SECRET`。当多个节点连接同一个 Redis 时，还必须使用相同的 `CRYPTO_SECRET`，否则节点生成的缓存键摘要不一致，无法正确共享缓存。上述保证只覆盖登录 Session 鉴权的有界陈旧语义；限流额度及其他 Redis 缓存仍会受到 Redis 拓扑影响，不能据此认为整个控制面与拓扑无关。

## 浏览器接口

Vue 登录入口依据服务端能力分别显示密码、Passkey 和已配置 OAuth。关闭密码登录不会关闭其他可用方法；要求额外验证时使用统一登录 challenge。Passkey 安全设置保持每用户一份凭据，重新注册会明确替换旧凭据；一个当前注册 RP 与兼容认证 RP 分开配置。

成功签发浏览器登录会话时，后端统一返回以下数据；各入口完成全部要求的验证后才签发：

```json
{
  "success": true,
  "data": {
    "access_token": "...",
    "token_type": "Bearer",
    "access_expires_at": 1730000000,
    "user": {},
    "session": {
      "sid": "...",
      "current": true,
      "login_method": "password",
      "ip": "...",
      "user_agent": "...",
      "created_at": 1730000000,
      "last_active_at": 1730000000,
      "expires_at": 1732592000
    }
  }
}
```

会话相关接口：

| 接口                                    | 鉴权                                                           | 用途                                                      |
| --------------------------------------- | -------------------------------------------------------------- | --------------------------------------------------------- |
| `POST /api/user/auth/refresh`           | Refresh Cookie；Secure 模式附加 Origin 校验                    | 轮换 Refresh Token 并签发新的 Access Token                |
| `POST /api/user/auth/logout`            | Refresh Cookie；Secure 模式附加 Origin 校验，可同时携带 Bearer | 撤销当前登录会话并清除 Cookie                             |
| `GET /api/user/sessions`                | Bearer                                                         | 查看当前鉴权版本的有效登录会话，当前会话优先，最多 100 条 |
| `DELETE /api/user/sessions/:sid`        | Bearer                                                         | 撤销指定登录会话，包括当前会话                            |
| `POST /api/user/sessions/revoke-others` | Bearer                                                         | 保留当前会话并撤销其他会话                                |

客户端内存中已有会话时，应在 refresh/logout 请求中发送 `X-Auth-Session: <sid>`。Refresh Cookie 与该 SID 不一致时，两个端点都返回 `409 AUTH_SESSION_MISMATCH`，且不会轮换、撤销或清除任何会话；客户端先通过 refresh 清除本标签页的旧 SID、恢复 Cookie 当前对应的会话，再重试 logout。冷启动尚无内存会话时可以省略该请求头。

并发使用同一个 Refresh Token 时，服务端通过确定性轮换恢复同一个后继 Token，多个浏览器标签页不会因丢失“胜者”响应而被迫退出。最近一代 Refresh Token 在短暂容错窗口结束后再次出现会撤销对应会话；无法识别的更早代或随机 Token 只会被拒绝，不会允许攻击者凭猜测踢掉会话。

前端在同一标签页复用刷新中的 Promise，并用会话版本防止迟到响应覆盖新身份；跨标签页的并发刷新由服务端确定性轮换和 409 会话不匹配恢复处理。BroadcastChannel（不支持时回退到 `storage` 事件）仅同步会话标识和登录/退出事件；Access Token 与 Refresh Token 都不会通过跨标签页消息传递或持久化到 Web Storage。

前端将冷启动状态与登录状态分开管理。网络或服务端临时故障允许后续导航重试 refresh；服务端确认 Refresh Cookie 无效时才进入已完成的匿名状态。内存 SID 与 Cookie SID 不一致时，客户端清除旧内存身份并在不携带旧 SID 的情况下重试一次。

## Session 签发限额与保留策略

服务端在所有登录方式的统一 Session 签发出口执行两级账户限制：

- `USER_SESSION_ACTIVE_LIMIT`（默认 `50`）：单用户未过期且状态为 active 的 Session 上限。达到上限时新登录返回 `409 AUTH_SESSION_LIMIT`。
- `USER_SESSION_ISSUANCE_LIMIT`（默认 `100`）和 `USER_SESSION_ISSUANCE_WINDOW_SECONDS`（默认 `86400`）：统计窗口内该用户创建的所有 Session，包含已撤销和旧鉴权版本的记录。达到上限时返回 `429 AUTH_SESSION_ISSUANCE_LIMIT`。
- 这两次计数与插入不加跨数据库锁；极端并发登录可能出现少量超额，但计数失败会拒绝签发，不会降级放行。

已经超过活跃上限的账户不会被自动下线或挤掉已有会话；限制只作用于新 Session 签发。

`USER_SESSION_REVOKED_RETENTION_DAYS`（默认 `7`）控制 revoked 行的审计保留期。签发计数依赖窗口内的行仍存在，因此签发窗口不得超过 revoked 保留期。如果配置超出，启动时会记录告警并将实际窗口钳制到保留期，避免提前删除 revoked 行导致限流计数被低估。

定时清理即使发现 `expires_at` 已过期，也不会删除 `created_at` 仍落在实际签发窗口内的行；尚未达到 revoked 保留期的撤销记录同样会继续保留。这样在扩大配置窗口时，过期清理不会静默削弱签发计数或审计保留。

活跃数量会计入状态仍为 active 但 `user_auth_version` 已过期的异常残留行，而设备列表只展示当前鉴权版本。因此遇到 `AUTH_SESSION_LIMIT` 时，应优先在仍已登录的设备上执行“撤销其他会话”，该操作会同时清理不可见的旧版本 active 行；没有可用设备时可使用密码重置撤销所有会话。密码重置不会清空签发窗口计数。

仅 master 节点每小时分批删除过期 Session 和超过保留期的 revoked Session。`USER_SESSION_HOURLY_ALERT_THRESHOLD`（默认 `5000`）只在最近一小时全局签发量异常时记录告警，不会形成可被滥用的全站登录拒绝开关。

## Refresh/Logout 的 Origin 校验

refresh/logout 的 Origin 防护与 Refresh Cookie 的 Secure 模式绑定：

- 未配置 `SESSION_COOKIE_SECURE` 或显式设为 `false` 时，Refresh Cookie 可用于本地 HTTP，refresh/logout 的 OriginGuard 关闭，并且不得配置 `SESSION_COOKIE_TRUSTED_URL`。这使 `http://localhost` 上 Vite 开发代理可以正常转发请求。该模式仅用于可信的本地开发环境，不应暴露到公网。
- `SESSION_COOKIE_SECURE=true` 时，Refresh Cookie 仅通过 HTTPS 发送，同时启用严格 OriginGuard。`POST /api/user/auth/refresh` 和 `POST /api/user/auth/logout` 会校验浏览器的 `Origin`；缺少 `Origin` 时只接受合法的单一 `Referer` 作为回退。允许来源包括请求自身的精确 Origin，以及 `SESSION_COOKIE_TRUSTED_URL` 中配置的精确 Origin。

Secure 模式的 Origin 校验不信任客户端直接发送的 `X-Forwarded-Proto`。TLS 在反向代理终止时，应将面板的公开 HTTPS Origin 明确写入 `SESSION_COOKIE_TRUSTED_URL`。

`SESSION_COOKIE_TRUSTED_URL` 是 refresh/logout Cookie 端点的可信 Origin 列表，不是 CORS 白名单。配置规则如下：

- 仅在 `SESSION_COOKIE_SECURE=true` 时配置；多个值用英文逗号分隔。
- 每项必须是精确的 HTTPS Origin，例如 `https://panel.example.com` 或 `https://panel.example.com:8443`。
- 不接受通配符、路径、查询参数、用户信息或域名后缀匹配。
- 不会修改 relay、旧 billing dashboard、`/api/usage/token` 或 `/api/log/token` 的 CORS 行为。浏览器使用 `sk-` key 直连 relay 的场景保持不变。

本地 HTTP 开发示例（OriginGuard 关闭）：

```env
SESSION_SECRET=<local-random-value>
SESSION_COOKIE_SECURE=false
# SESSION_COOKIE_TRUSTED_URL 不得设置
```

生产 HTTPS 示例（OriginGuard 开启）：

```env
SESSION_SECRET=<high-entropy-random-value>
SESSION_COOKIE_SECURE=true
SESSION_COOKIE_TRUSTED_URL=https://panel.example.com,https://admin.example.com
```

该开关只控制面板 Refresh Cookie 和 refresh/logout 的 OriginGuard，不会修改 relay、旧 billing dashboard、`/api/usage/token` 或 `/api/log/token` 的 CORS 行为。

## 可信代理与 IP 限流

Gin 默认会信任所有代理提供的客户端 IP 请求头。本项目使用兼顾常见反代拓扑和公网直连安全的三态配置：

- 未配置、空字符串或纯空白的 `TRUSTED_PROXIES` 默认信任 `127.0.0.0/8`、`::1`、`10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16` 和 `fc00::/7`，并输出启动告警。该默认值覆盖同机 Nginx、Docker Compose 和常见内网反代；公网直连地址不在列表中，其伪造的 `X-Forwarded-For` 会被忽略。
- `TRUSTED_PROXIES=none`（大小写不敏感且必须单独使用）启用严格直连模式，不信任任何代理，`ClientIP()` 只使用 TCP 直连地址。
- 其他非空值按英文逗号解析为代理 IP/CIDR，并完全替代默认列表。应填写反向代理自身的地址而不是客户端网段；非法 CIDR、空列表或将 `none` 与其他值混用都会阻止服务启动。

Gin 只在请求的直连来源属于可信代理时解析客户端 IP 请求头，并从转发链右侧向左寻找首个非可信地址。因此常见 Nginx `$proxy_add_x_forwarded_for` 链中的公网客户端地址会阻止更左侧的伪造前缀生效。默认信任私网的残余风险是：能够从同一私网直接访问应用的其他机器或容器仍可伪造这些请求头；需要消除此风险时应使用 `none` 或配置精确代理地址。

Redis 限流使用原子 Lua 固定窗口：窗口边界两侧可分别打满一次，极短时间内通过量最高约为配置值的两倍。例如 `20 次/20 分钟` 在边界可通过约 40 次。帐户级 Session 上限和签发窗口控制数据库增长；需要严格抑制边界突发时须改用 ZSET 滑动窗口。

登录、注册、二次验证和 OAuth 等接口共用 Critical IP 限流（每个 IP 20 次/20 分钟）。`POST /api/user/auth/refresh` 在每次整页加载时恢复面板会话，使用独立的会话刷新额度（每个 IP 60 次/分钟），不消耗登录额度；两者都由 `CRITICAL_RATE_LIMIT_ENABLE` 控制。

用户级模型请求限流通过 Redis 原子预留统一处理总次数和成功次数。每个逻辑请求只取得一个成功名额，在途请求续约，渠道重试复用该名额；请求结束后按实际输出结果确认或释放。失败流不能因 HTTP 200 被记作成功，预留维护失败会拒绝继续发送。开启该限流时 Redis 不可用返回 503，不回退到节点内计数。原生 Responses WS 对每次 `response.create` 分别执行同一合同。

开放注册仍会受 Critical IP 限流保护，但分布式 IP 多账号攻击不能仅靠 IP 限流阻止。公网开放注册的部署应同时启用 Turnstile 和邮箱验证；更强的设备或多维风控需作为独立安全项目设计。

## PAT 调用契约

管理 PAT 使用独立 `personal_access_tokens` 表，明文为随机 `pat_` 凭据，只在创建响应展示一次；数据库保存 SHA-256 摘要、用户归属、名称、scopes、创建/最后使用/过期/撤销时间。每用户最多 20 枚有效 PAT，默认 30 天过期，显式过期时间必须在未来。用户行上没有访问令牌，也没有双写或单令牌宽限期。调用使用 `Authorization: Bearer <pat>`；`New-Api-User` 不参与鉴权。

每个管理路由按 HTTP method 与注册路径明确声明 PAT scope；没有声明时即使 Root 的 PAT 也拒绝。每次调用将已授予 scope 与拥有者当前角色、资源权限求交集，用户禁用/删除、PAT 过期/撤销均拒绝。模型调用 Token 与管理 PAT 不互换。

PAT 不是浏览器登录会话，不能管理登录会话、创建或撤销其他 PAT。它可以经 `verification:write` scope 完成现有二次验证；所得 Proof 绑定 PAT ID、用户鉴权版本及操作上下文，不绑定或冒充浏览器 Session。后续业务事务会重新验证 PAT 有效性。浏览器 JWT、Refresh Cookie 和原有刷新机制保持独立。

OAuth 和 Telegram 的二次验证回调复核发起时的浏览器或 PAT 身份，不能在回调末尾退回浏览器专属链路。撤销、过期、认证版本或上下文变化均拒绝；生成的 Proof 继续绑定身份、操作与目标且只能消费一次。账户绑定流程仍要求浏览器身份。

## 临时鉴权流程与二次验证

OAuth state、2FA pending、Passkey ceremony、Telegram bind 等临时状态存放在 `auth_flows`。客户端只持有随机 `flow_token`，数据库仅保存 HMAC 摘要；流程具有用途、provider、intent、用户和登录会话绑定，并且只能原子消费一次。OAuth 注册的 affiliate code 也随登录 AuthFlow 保存。

标准 OAuth 绑定回调由 popup 通过同源 `postMessage` 交给 opener；只有 opener 使用自身内存中的 Bearer 调用后端绑定接口。Telegram 绑定先由已登录前端创建绑定 AuthFlow，再让 widget 回调携带路径中的 `flow_token`，回调时会重新确认原登录会话仍有效。Telegram 的已签名 widget assertion 也会登记为一次性凭据，重复回放会被拒绝。

敏感操作使用 `GET /api/verify/methods` 获取当前可用因子，通过对应验证流程取得有效期 1 分钟的 `X-Security-Proof`。作用域包括渠道密钥读取、PAT 生成/撤销、Passkey 注册/删除、2FA 设置/禁用/恢复码更新、账户绑定/解绑、设置/修改密码和账户删除。

Proof 绑定用户、认证身份（浏览器 Session 或 PAT）、用户鉴权版本、身份版本、操作及严格规范化的上下文。数据库一次性消费 Proof，随后各业务事务重新核对授权状态。消费后撤销认证身份会使业务写入失败；业务失败不会恢复 Proof。注册 Passkey 与 2FA 的 begin、finish 使用绑定的流程状态，不能因尚无第二因子而绕过验证。

账户密码使用 Argon2id，新密码校验 8–128 个 Unicode 字符，不裁剪空格。已有 bcrypt 哈希保留兼容校验。登录传输加密默认关闭；`GET /api/user/login/encryption-key` 只读且 `no-store`。启用后使用带 `kid` 的 v2 RSA-OAEP + AES-GCM 混合信封，拒绝明文降级、错误密钥标识和损坏密文，后续登录验证仍绑定同一流程。

PAT 管理位于账户安全页，仅允许浏览器会话。规范前缀为 `/api/user/self/personal-access-tokens`，`/api/user/personal-access-tokens` 是同处理器、同权限的别名。没有 `/api/user/token*` 单令牌路由。

| 方法与相对路径 | 请求与行为                                                                                                                                         |
| -------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /`        | 本人列表；不返回摘要或明文                                                                                                                         |
| `GET /scopes`  | 服务端当前可授予的 scope 字符串数组、有效数量上限和默认有效天数；前端不复制角色定义                                                                |
| `POST /`       | `{name, scopes, expires_at?}`，需 `pat.create` Proof；响应 `{token, personal_access_token}` 的 `token` 只显示一次                                  |
| `PATCH /:id`   | `{name?, scopes?}` 至少一项；只改名称不需 Proof。提交 scopes 必须提供 `pat.update` Proof，其 context 为 `{id, scopes}`，scope 排序、去重后绑定目标 |
| `DELETE /:id`  | 需 `pat.revoke` Proof，context 为 `{id}`                                                                                                           |

PATCH 省略字段保留原值；空名称、null/空 scopes、未知字段以及修改过期时间均拒绝。更新在现有用户锁事务内重查浏览器身份、PAT 归属/有效性和最新授权；绑定错误的 Proof 不执行更新，消费后的 Proof 不能重放。过期或撤销 PAT 不能通过改名或改权限复活，续期必须新建并撤销旧 PAT。界面关闭、退出或会话变化会丢弃未完成请求的一次性明文，不存入浏览器持久存储。

路由 scope 与字段权限分别求交集。例如 `ops:read` 允许管理员 PAT 读取请求诊断，但返回渠道和凭据内部 ID 还要求该 PAT 的 `channels:read` 及用户当前 `ChannelRead`。不能借拥有者更高权限扩大 PAT 的响应字段。个人与授权管理员审计存入独立 `audit_logs`，普通用量日志清理不会移除它；不记录密码、Proof、PAT 原文或摘要。

管理员敏感用户操作使用 `admin.user.change` Proof，context 为 `{ids: [...], action}`，目标按序规范化。创建管理员使用目标 `[0]` 与 `create_admin`；更新敏感字段、删除、状态/角色、Passkey、2FA、解绑等使用各自 action。旧 `/api/user/` 与 `/api/next/admin/users` 共用有效角色和权限校验；验证失败不进入业务写入。普通资料与额度操作仍遵守各自已有权限及账务合同，不扩大 Proof 到无关操作。

旧管理入口的状态和角色操作在持锁事务内重查当前权限，只更新目标列。降级的权限清理与角色修改原子提交；认证版本按锁内当前值提升。普通 `updatePassword=false` 更新排除密码列，不能把请求前读取的旧密码、账务或资料快照写回。

模型调用 Token 的创建总量检查与每小时创建窗口位于同一用户锁事务内。`token_setting.max_token_creates_per_hour` 默认 60，0 明确关闭，负数拒绝创建；成功创建后即使软删除也继续占用该小时次数。数量上限返回 `TOKEN_COUNT_LIMIT`，频率上限返回 HTTP 429 `TOKEN_CREATION_RATE_LIMIT`。失败事务不会留下 Token、预算事件或路由 Profile。

邮箱验证码与密码重置沿用现有单实例验证存储，条目仅保存 SHA-256 摘要。每个验证码累计失败最多 5 次，校验和失败计数在同一锁内完成，第 5 次失败使该码失效；发送接口另有限速。密码重置在任何账户写入前原子校验并消费 token，并发或重放请求最多一个进入密码修改。数据库写入失败不会恢复已消费 token，用户需要重新申请链接；成功后的鉴权版本提升和浏览器会话撤销沿用现有事务。

登录、账号安全、管理操作、访问令牌请求和系统发放（签到、邀请奖励等）统一写入 `audit_logs`，控制台在日志子页 `/usage-logs/audit` 展示。`GET /api/audit/self` 只返回本人记录并去除管理员与请求元数据；`GET /api/audit` 查看全部账号，需要 `audit:read` 权限。非 Root 查看者看不到 Root 发起的事件和 `root_info`。读取时统一去除操作参数中疑似凭据的字段（如 password、secret、token、api_key、cookie）。旧 `logs` 表的管理、系统和登录记录（type 3/4/7）在启动迁移时一次性回填到 `audit_logs`，由 Option 标记保证幂等。

## 存储与兼容边界

当前格式只接受空库初始化或匹配 `billing_schemas` 标记且已经采用 PAT 表的安装。存在 `users.access_token` 列的数据库会在任何初始化改表前被拒绝，不做数据迁移、列删除或数据库清空。资金 schema 为 5，认证格式不影响资金计算器。安装与回退以[发布契约](routing/GATEWAY_RELEASE.md)为准。

- 启动迁移创建 `user_sessions`、`auth_flows`、`external_identity_claims` 和 `users.auth_version`，为已有用户初始化鉴权版本并回填 Telegram 账号唯一归属；同一 Telegram ID 绑定多个用户时拒绝启动，需先消除歧义。
- 迁移为 Session 签发计数和分批清理建立索引；`user_sessions` 很大时应为首次启动预留维护窗口。
- `user_sessions.previous_refresh_hash` 为 `varchar(64)`，读取时去除定长列遗留的空格填充；迁移保持幂等，连续启动不反复执行列类型变更。
- 只有 master 节点定时清理过期登录会话、超过保留期的 revoked 会话和已过保留期的 AuthFlow。
- 使用公网负载均衡器、`100.64.0.0/10`、链路本地地址或自定义 CNI 网段的部署须显式配置 `TRUSTED_PROXIES`。
- 用户级模型限流及网关准入使用当前预留合同，不支持新旧版本混合滚动运行。
- 自建客户端按 AuthBundle、`flow_token` 和 Security Proof 契约接入；管理脚本使用具有最小 scope 的 PAT。
