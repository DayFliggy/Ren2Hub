# Ren2Hub 单实例安装与更新

根目录 `docker-compose.yml` 从当前检出的源码构建 Go 后端和 Vue 前端，使用 PostgreSQL 15 与 Redis 7。它用于本地安装和验证，应用只监听宿主机 `127.0.0.1:3000`，数据库与 Redis 不发布端口。所有容器均不设置 CPU、vCPU 或内存配额。

RenAI 工作区的服务器部署使用工作区 `deploy/ren2hub/` 与对应部署流程；不要用本示例覆盖服务器的 Compose、密钥或数据目录。上游 `calciumion/new-api` 镜像不包含当前 Ren2Hub 改造。

## 首次启动

使用 Docker Engine 和 Docker Compose v2，在当前 Ren2Hub 源码目录执行。先确认 `relaykit/` 源码完整，构建需要访问 Docker 镜像和包依赖服务。

1. 确认目标是空库，或已有 schema 5 且 `EvidenceKeyID` 匹配的 `billing_schemas` 标记。旧账务库会被启动守卫拒绝，不会迁移或清空；首次采用当前格式应使用独立新库并保留旧库。以 `.env.example` 为模板创建本地 `.env`，分别为 `POSTGRES_PASSWORD`、`REDIS_PASSWORD`、`SESSION_SECRET`、`CRYPTO_SECRET` 填入独立随机值，例如每项执行一次 `openssl rand -hex 32`。空值会使 Compose 在启动前报错。`CRYPTO_SECRET` 用于加密账务重放，初始化后必须稳定保存并随数据库备份，不能在重启或更新时重新生成。不要把 `.env` 提交到 Git。
2. 新库默认用户名为 `root`，数据库名为 `new-api`；名称相同不代表旧账务数据兼容。可用 `POSTGRES_USER` 和 `POSTGRES_DB` 指定新库身份。密码建议使用十六进制；如需沿用含 URL 保留字符的口令，应在 Compose 的 `SQL_DSN` / `REDIS_CONN_STRING` 中填写编码后的连接字符串，而数据库与 Redis 的实际口令保持原值。
3. 校验配置，再构建和启动：

```bash
docker compose config --quiet
docker compose build new-api
docker compose up -d
docker compose ps
curl --fail http://127.0.0.1:3000/api/status
```

打开 `http://127.0.0.1:3000` 完成初始化。模型与工具价格初始为空；管理员在模型定价中手动编辑，或获取五源候选、选择单源填入草稿并保存，才会生效。不要输出完整的 `docker compose config` 或容器环境变量到公开日志，其中包含密钥。

应用使用 `./data` 和 `./logs`，PostgreSQL 使用 `pg_data` 卷，Redis 使用 `redis_data` 卷。更改目录或 Compose project name 会改变卷名；已有安装不能用空卷替代原数据库。`docker compose down -v` 会删除命名卷，不用于普通更新或停机。

## 单实例更新与回退

用户与渠道的并发额度只存在于一个网关进程中，不得使用 `--scale`、滚动更新或同时启动新旧网关接流量。Compose 给应用 130 秒停止宽限，覆盖默认 120 秒排空及清理。

1. 记录当前源码、镜像、配置、卷名与备份。构建待发布镜像时保留可回退镜像，不能只依赖可变的 `ren2hub:local` 标签。
2. 停止新入口流量，执行 `docker compose stop new-api`，确认旧进程已退出，再启动新版本。已有 PostgreSQL/Redis 需要更换容器时也先排空应用。
3. 仅应用更新使用 `docker compose up -d --no-deps new-api`。核对健康状态、前端、鉴权、并发和账务。
4. 回退同样先排空当前进程，再启动已记录且数据兼容的镜像与配置。不能把当前事件账务库交给旧格式程序；数据库恢复或切换需独立核验和授权，不能并行回退。

旧版本使用 Redis 并发租约时，还要执行[发布与回退契约](../routing/GATEWAY_RELEASE.md)中的旧进程退出确认和限定命名空间清理。不要假设旧键自动消失。

已有 PostgreSQL 卷的密码不会因为修改 `POSTGRES_PASSWORD` 自动轮换。复用旧库时必须填入其实际身份和密码；需要轮换时单独执行数据库凭据变更，再同步应用配置。

## HTTPS 与开发模式

本示例默认本地 HTTP。通过反向代理开放 HTTPS 时，在 `.env` 设置 `SESSION_COOKIE_SECURE=true`、精确的 `SESSION_COOKIE_TRUSTED_URL`，并将 `TRUSTED_PROXIES` 限定为实际代理地址。代理应关闭 SSE 缓冲并支持 WebSocket。具体鉴权边界见[登录会话](../authentication.md)。

`docker-compose.dev.yml` 是独立的前端开发配置，保留 `make dev-api` / `make dev-web` 工作流。它只构建后端，占位前端由 Vite 开发服务替代；数据库口令是本地开发默认值，不能用于服务器部署。不要让开发栈和安装栈同时使用同一端口、数据或业务流量。

`new-api.service` 的旧占位模板已移除。当前维护的安装入口是 Compose；自行使用 systemd 时，应明确服务用户、环境文件、数据路径与至少 130 秒的停止期限。

当前业务配置见[网关 API](../routing/GATEWAY_API.md)，路由行为见[统一路由](../routing/UNIFIED_ROUTING.md)，宝塔环境见[面板部署说明](BT.md)。
