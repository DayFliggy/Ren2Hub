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

## 宿主网卡流量

默认 bridge 部署不挂载宿主文件，运维监控的流量读数取自应用主监听器上的连接字节（`scope=application_listener`）。bridge 容器自己的 `/proc/net/dev` 只有容器 veth，不代表宿主机，任何情况下都不作为宿主来源。要改读宿主上联网卡的收发计数，先在宿主机执行 `ip route show default`，取输出中 `dev` 后的网卡名（如 `eth0`），再在 `docker-compose.override.yml` 中为 `new-api` 追加：

```yaml
services:
  new-api:
    environment:
      TRAFFIC_HOST_INTERFACE: eth0
    volumes:
      - type: bind
        source: /proc/1/net/dev
        target: /host/proc/net/dev
        read_only: true
```

应用只读取这一个网卡，不汇总多网卡，也不按默认路由自动选择。`TRAFFIC_HOST_NET_DEV` 默认就是 `/host/proc/net/dev`，bridge 部署不需要设置，尤其不能设成 `/proc/net/dev`。挂载必须用长格式：短格式 `:ro` 经 `docker compose config` 解析后带 `create_host_path: true`。两种写法在挂载源不存在时都会让容器创建失败，应用根本不会启动，所以只在已确认存在 `/proc/1/net/dev` 的 Linux Docker 主机上启用。修改后用 `docker compose up -d --no-deps new-api` 重建容器，单纯 restart 不会带上新挂载。

应用启动时判定一次来源：两个变量都为空时使用监听器；任一变量非空即为宿主模式，之后读取失败只显示不可用并给出原因，不改用监听器，也不阻止业务启动。第一次采样只建立基线，约 10 秒后的下一次采样才有速率。Root 账号在“运维监控”页或 `GET /api/performance/stats` 的 `observability.traffic` 查看：`scope` 应为 `host_interface`，`interface` 为配置的网卡，`reason` 为空。`reason` 可能是 `host_config_invalid`（网卡名或路径非法）、`host_counters_unreadable`（文件打不开、读不了或不是普通文件，如 SELinux 拒绝、指向 FIFO 或 `/dev/zero`）、`host_counters_invalid`（文件格式不符或超过 1 MiB）或 `host_interface_missing`（文件中没有该网卡）。撤回时同时删除挂载和两个变量；只删网卡名而留下 `TRAFFIC_HOST_NET_DEV`，应用仍是宿主模式并报 `host_config_invalid`。

裸机运行或 `network_mode: host` 的容器本身就在宿主网络命名空间，不需要挂载，设 `TRAFFIC_HOST_INTERFACE` 和 `TRAFFIC_HOST_NET_DEV=/proc/net/dev`；host 网络容器这一形态尚未实测。Kubernetes（Pod Security Standards Baseline 禁止 hostPath）、托管平台和 Docker Desktop（读到的是虚拟机网卡）不要启用，保持两个变量为空，使用监听器计数。SELinux enforcing、rootless 运行时和纯 IPv6 主机尚未实测，启用后先在 Root 页确认 `reason` 为空。
