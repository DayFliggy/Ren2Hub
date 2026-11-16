# 宝塔环境部署

宝塔只作为Docker Compose和HTTPS反代的管理入口。使用完整Ren2Hub源码（含Vue、RelayKit、TokenKit）构建，遵循[单节点安装](DOCKER.md)；应用商店的上游镜像不包含本地扩展。

仅应用发布回环端口，数据库和Redis不对公网开放。保持数据卷、私有配置及凭据稳定，代理支持SSE和WebSocket。更新前备份并停止旧应用，不使用面板扩容或删除卷重建。

新安装使用空数据库，更新和回退的边界见[安装与更新的边界](../routing/GATEWAY_RELEASE.md)。本文是配置要求，不代表已在宝塔实际验收。
