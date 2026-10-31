# Ren2Hub 单节点安装

从当前Ren2Hub源码构建Go服务和Vue应用；上游镜像不包含本项目的最小扩展和Vue品牌。根 `docker-compose.yml` 为本地安装示例，包含PostgreSQL和Redis，仅应用绑定 `127.0.0.1:3000`。RenAI服务器使用所属工作区独立部署资产，不以本示例覆盖正式配置。

首次采用新核心必须使用独立空数据库。程序在AutoMigrate前拒绝旧业务分组字段及 `billing_schemas` / `billing_events`；不会转换或清空旧库。按 `.env.example` 配置数据库、Redis及稳定的 `SESSION_SECRET`、`CRYPTO_SECRET`，将口令保存在未跟踪私有环境文件，不输出完整Compose配置或容器环境。

```sh
docker compose config --quiet
docker compose build new-api
docker compose up -d
docker compose ps
curl --fail http://127.0.0.1:3000/api/status
```

完成被授权的管理员初始化，再重新配置渠道、Key和支付商户。代码不内置模型价格：在“模型定价”页录入或用“自动获取”回填并保存后模型才可调用；工具调用沿用上游内置单价；选中渠道倍率在原生计费中应用一次。旧用户、价格、订单和任务不导入。

普通更新先保存配置/备份，构建固定SHA镜像，停止旧应用再用 `up -d --no-deps new-api` 替换。只运行一个应用实例；不使用滚动双活或删除卷的方式更新。PostgreSQL已有卷的口令不会因改环境变量自动轮换。

本地HTTP保持非Secure Cookie；公开HTTPS必须启用 `SESSION_COOKIE_SECURE`、精确 `SESSION_COOKIE_TRUSTED_URL` 和实际 `TRUSTED_PROXIES`，反代支持SSE和WebSocket。认证见[会话合同](../authentication.md)。独立开发栈与正式栈不得共用数据、端口或凭据。

正式新核心切换另行批准独立数据库与另一个核实为空的 Redis 目标，保留全部旧数据目标；旧在途任务/支付归零后才能执行。回退需匹配应用和数据，不是只换镜像，详见[发布契约](../routing/GATEWAY_RELEASE.md)。
