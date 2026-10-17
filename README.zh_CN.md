# Ren2Hub

Ren2Hub / DayFliggy 是基于 [NewAPI](https://github.com/QuantumNous/new-api) 的单节点 AI 网关，使用 Vue 控制台。NewAPI 是唯一上游，当前核心源码基线为 `09109ec315a5086366f94bcce00f177ec831dbe1`。

自动选路、模型与分组定价、内置默认、钱包预扣结算、协议、任务插件和权限采用 NewAPI。最小扩展为工单、每个 Token 一份有序私有渠道列表，以及 GMPay 虚拟币充值。钱包是唯一资金来源；站内订阅套餐、官方订阅账号、type57、io.net、Playground、React 和 Electron 已退役。

从[文档入口](docs/README.md)与[源码安装](docs/installation/DOCKER.md)开始。本轮使用独立新数据库，程序在迁移前拒绝旧 Ren2Hub 账务库；旧业务数据和支付商户均不导入。源码修改、候选验证与正式部署分别记录，不代表本页更新后已经发布。

保留 NewAPI、One API 的来源及版权声明，按 [GNU AGPL v3](LICENSE) 分发；第三方声明见 [NOTICE](NOTICE) 和[许可证清单](THIRD-PARTY-LICENSES.md)。上游镜像不包含此检出的 Vue 界面与扩展。

[English](README.md) · [繁體中文](README.zh_TW.md) · [Français](README.fr.md) · [日本語](README.ja.md)
