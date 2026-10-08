# 宝塔面板部署 Ren2Hub

使用宝塔的 Docker Compose 功能管理当前 Ren2Hub 源码构建。应用商店中的 New-API 和上游镜像不包含本项目的本地 FIFO、路由与前端改造，不作为 Ren2Hub 的安装或更新入口。

1. 将已确认版本的完整 Ren2Hub 源码放在独立目录，包含 `Dockerfile`、`frontend/`、`relaykit/` 和 `tokenkit/`。
2. 按[单实例安装与更新](DOCKER.md)准备本地 `.env`，校验并构建根目录 `docker-compose.yml`。面板必须使用同一工作目录和环境文件，不能只粘贴 Compose 而缺失源码。
3. 保持 PostgreSQL、Redis 的数据卷和应用数据目录固定。容器不设置 CPU 或内存配额，应用只发布 `127.0.0.1:3000`。
4. 通过面板网站反向代理配置 HTTPS，填写精确的可信 Origin 和代理地址，支持 SSE 与 WebSocket。无需把应用、PostgreSQL 或 Redis 端口直接开放到公网。
5. 更新与回退时先排空并退出旧应用，再启动新应用。不要使用面板滚动扩容、多副本或删除数据卷的重建操作。

完整的密钥、持久化、健康检查和更新步骤以[安装文档](DOCKER.md)为准；单实例切换及旧 Redis 并发键处理以[发布契约](../routing/GATEWAY_RELEASE.md)为准。

本文提供配置要求，不代表已在宝塔面板完成运行验证。RenAI 工作区服务器由工作区自己的部署流程管理，不使用宝塔应用商店。
