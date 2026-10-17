# Ren2Hub 项目规则

遵循上层工作区 AGENTS.md。默认使用简体中文；用户明确批准的本轮合同优先于历史文档。

## 产品与来源

- NewAPI（QuantumNous/new-api）是唯一上游；Sub2API 和 AxonHub 仅为参考。当前核心基线为 `09109ec315a5086366f94bcce00f177ec831dbe1`。
- 单节点核心网关，仅交付 Go 服务和 Vue Web。保留 Ren2Hub / DayFliggy 品牌及上游许可。
- 后端计价、余额、预扣结算、协议、自动选路和权限采用 NewAPI。不要恢复旧 schema 5 账本、冻结报价、加密重放、独立准入、渠道倍率或第二套权限模型。
- 最小扩展仅为工单、每个 Token 一份有序私有渠道列表、GMPay 支付。自动路由完全采用上游；私有路由仅缩小授权候选范围。
- 官方订阅账号、站内订阅套餐、io.net 部署、Playground、React、Electron 均已退役。普通 API Key 接入、站点 OAuth 和钱包充值继续保留。
- 本轮全新安装，不迁移旧业务数据。新程序在迁移前拒绝旧 schema 5 数据库；正式空库切换按已批准的准备与单独批准流程执行。

## 实现

- Go 1.26；独立 relaykit/ 和 tokenkit/ 不得依赖根模块，受影响时以 GOWORK=off 单独验证。
- 保持 Router → Controller → Service → Model 分层；核心代码优先保持上游一致，必要差异记入 docs/upstream/SYNC_LEDGER.md。
- JSON 使用 common.Marshal / Unmarshal 等共享封装。数据库支持 SQLite、MySQL、PostgreSQL；保持上游锁和事务习惯。
- 不吞掉异常、不添加假成功或无依据回退；支付回调和人工补单必须保证最多入账一次。
- Vue 使用 Composition API、TypeScript、Pinia、Vue Router、Vue I18n 和 Bun。保留真实后端合同，不能伪造旧预留余额或版本字段。
- 所有 UI 文本使用现有 i18n。翻译工作先读 .agents/skills/i18n-translate/SKILL.md。
- Vue 是唯一前端，Go 嵌入 frontend/embed-dist，所有启用页面使用实际接口。权限和导航共享来源；失效请求应取消或防止覆盖新结果。
- 不引入 /api/next 兼容后端。工单使用 /api/tickets 和 /api/admin/tickets，私有路由使用 /api/token/:id/routing。

## 验证

先定向测试，再类型和静态检查、受影响构建、最小冒烟。Go 单包测试超时 60 秒。新测试应保护真实行为，不以文件存在或实现镜像断言替代回归。

Vue 执行适用的 test:run、typecheck、lint、format:check、build，并验证桌面/手机、明暗主题及权限变化。新数据库和支付改动需验证三种数据库。仅使用本地供应商和支付模拟服务。

独立候选必须隔离数据库、Redis、网络、密钥与存储。旧合同测试记录退役，不算通过。源码、候选和正式部署分别记录证据；未提交工作 candidate_sha=null。
