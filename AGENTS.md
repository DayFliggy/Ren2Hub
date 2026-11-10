# Ren2Hub 项目规则

遵循上层工作区 AGENTS.md。默认使用简体中文；用户明确批准的本轮合同优先于历史文档。

## 产品与来源

- NewAPI（QuantumNous/new-api）是唯一上游；Sub2API 和 AxonHub 仅为参考。本轮冻结同步目标为 `48a6c87f9e7757fdc58e5be9bcb4f8281ad0dfd0`（NewAPI `v1.0.0-rc.49`），处置与验证状态见同步账本。
- 单节点核心网关，仅交付 Go 服务和 Vue Web。保留 Ren2Hub / DayFliggy 品牌及上游许可。
- 后端钱包、预扣结算、协议和权限采用 NewAPI。按 2026-10-18 已批准合同实施渠道级自动/手动路由、无等待并发及模型基础价 × 渠道倍率；删除业务分组。不要恢复旧 schema 5 账本、冻结报价、加密重放、等待队列或第二套权限模型。
- 扩展包括工单、每个 Token 的自动/有序手动路由、GMPay、邀请返利、统一模型目录与统计。平台渠道由管理员维护，用户选择不等于 BYOK。Vue 保留 9a63062227 视觉，按真实原生接口接通；排除历史协议诊断、权限增强、JS 插件增强、任务产物增强、独立商城、会员等级、新人任务、七日签到阶梯。当前原生对应能力继续保留。
- 2026-11-10 批准充值折扣：单笔充值阶梯、固定折扣、全站阶梯与个人日/周/月阶梯相加封顶，只在易支付、GMPay、Waffo、Waffo Pancake 下单时降低实付，到账足额；调用计价与上游一致，见 docs/billing/TOPUP_DISCOUNT.md。它不是会员等级，不附带权益，不复制基础价，不建资金账务。
- 官方订阅账号、站内订阅套餐、io.net 部署、Playground、React、Electron 均已退役。普通 API Key 接入、站点 OAuth 和钱包充值继续保留。
- 新核心首次安装不迁移旧独立账本或业务分组数据，迁移前守卫继续保留。已初始化实例的普通更新保持现有数据库、Redis、附件、配置与在途任务，不按首次安装流程重建数据。
- 模型默认价格与内置计费表达式已按明确要求删除；工具单价、音频倍率表和 Gemini 音频输入固定价继续沿用上游。

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

本地统一入口和分层门槛见 docs/testing/LOCAL_TESTING.md。Vue 执行适用的 test:run、typecheck、lint、format:check、build，并验证桌面/手机、明暗主题及权限变化。新数据库和支付改动需验证三种数据库。仅使用本地供应商和支付模拟服务。

独立候选必须隔离数据库、Redis、网络、密钥与存储。旧合同测试记录退役，不算通过。源码、候选和正式部署分别记录证据；未提交工作 candidate_sha=null。
