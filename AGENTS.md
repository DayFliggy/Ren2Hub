# Ren2Hub 项目规则

遵循上层工作区 AGENTS.md。默认使用简体中文；用户明确批准的合同优先于文档里的旧描述。文档只写现行状态，历史从 Git 读取。

## 产品与来源

- NewAPI（QuantumNous/new-api）是唯一上游；Sub2API 和 AxonHub 仅为参考。冻结同步目标为 `3a407cc9b064fe79a4ffac4146b2690c465dc929`（固定 NewAPI main，rc.51 后第 16 个提交），本地差异和未修复的已知问题见 docs/upstream/SYNC_LEDGER.md。
- 单节点核心网关，仅交付 Go 服务和 Vue Web。保留 Ren2Hub / DayFliggy 品牌及上游许可。
- 后端钱包、预扣结算、协议和权限采用 NewAPI。本地合同是渠道级自动/顺序路由、无等待并发及模型基础价 × 渠道倍率；一条渠道一把 Key，提供商插件渠道一份凭据；业务分组已删除。不要恢复旧 schema 5 账本、冻结报价、加密重放、等待队列或第二套权限模型。
- 扩展包括工单、每个 Token 的自动/顺序路由、GMPay、邀请返利、统一模型目录与统计。平台渠道由管理员维护，用户选择不等于 BYOK。Vue 日间保留原视觉，夜间为 B1 Studio（frontend/docs/THEMES.md），按真实原生接口接通；排除历史协议诊断、权限增强、JS 插件增强、任务产物增强、独立商城、会员等级、新人任务、七日签到阶梯。当前原生对应能力继续保留。
- 充值加赠：单笔入账阶梯、固定加赠、全站阶梯与个人日/周/月阶梯相加封顶，实付不变，按本金另赠额度；覆盖六个在线充值渠道、兑换码和管理员加额，跟随同一启用设置；调用计价与上游一致，见 docs/billing/TOPUP_BONUS.md。它不是会员等级，不附带权益，不复制基础价，不建资金账务。
- 旧 Codex/type 57 专用账号接入、站内订阅套餐、io.net 部署、Playground、React、Electron 均已退役。通用提供商插件登录（再次登录替换渠道凭据）、刷新和凭据管理开放；普通 API Key 接入、站点 OAuth 和钱包充值继续保留。请求处理插件有全局、渠道、模型作用域和插件声明的作用范围，无业务分组。
- 新安装使用空数据库，不迁移旧独立账本或业务分组数据，迁移前守卫继续保留。已初始化实例的普通更新保持现有数据库、Redis、附件、配置与在途任务，不重建数据。
- 代码不内置模型默认价格与计费表达式；工具单价、音频倍率表和 Gemini 音频输入固定价沿用上游。

## 实现

- Go 1.26；独立 relaykit/ 和 tokenkit/ 不得依赖根模块，受影响时以 GOWORK=off 单独验证。
- 保持 Router → Controller → Service → Model 分层。后端对齐冻结目标 3a407cc9b：原生文件保持上游原文，本地逻辑写在扩展文件里，只保留批准的产品合同及 Vue 实际对接的最小接点，历史 `verified_fix` 不构成保留理由。混合文件逐段拆分，来源及裁定见 `docs/upstream/NATIVE_B388_REVIEW.json`；新的必要差异记入 docs/upstream/SYNC_LEDGER.md。
- 与上游相同的已知缺陷不在本地修复，登记在同步账本或 docs/upstream/WAITING_UPSTREAM.md。
- JSON 使用 common.Marshal / Unmarshal 等共享封装。数据库支持 SQLite、MySQL、PostgreSQL；保持上游锁和事务习惯。
- 不吞掉异常、不添加假成功或无依据回退；支付回调和人工补单必须保证最多入账一次。
- 严格预扣、渠道倍率与即时并发保留；管理员拉取或删除模型不占中继并发槽。保留充值订单冻结的本金与加赠（`ExpectedQuota` 为合计）、GMPay 核验和支付单次入账，已使用的兑换码不可修改；Stripe 的 Money 仍表示原生数量含义，不能改写成实付金额或重算历史额度。
- Vue 使用 Composition API、TypeScript、Pinia、Vue Router、Vue I18n 和 Bun。保留真实后端合同，不能伪造旧预留余额或版本字段。
- 所有 UI 文本使用现有 i18n。翻译工作先读 .agents/skills/i18n-translate/SKILL.md。
- Vue 是唯一前端，Go 嵌入 frontend/embed-dist，所有启用页面使用实际接口。权限和导航共享来源；失效请求应取消或防止覆盖新结果。上游 `web/` 的改动按 frontend/UPSTREAM_MAPPING.md 找 Vue 落点。
- 不引入 /api/next 兼容后端。工单使用 /api/tickets 和 /api/admin/tickets，私有路由使用 /api/token/:id/routing。

## 验证

先定向测试，再类型和静态检查、受影响构建、最小冒烟。Go 单包测试超时 60 秒。新测试应保护真实行为，不以文件存在或实现镜像断言替代回归。

按改动模块测试：每个任务和实施计划先写测试计划，方法和定向不扩大的规则见工作区的 task-testing 技能，模块对照见 docs/testing/MODULE_TESTING.md，各层内容见 docs/testing/LOCAL_TESTING.md，功能链路的保护测试和缺口见 docs/testing/FUNCTION_CHAINS.md。`quick`、`all`、完整 candidate 和整套 ui-contracts 是全量档位，只在用户明确要求全量测试时运行；自动选择达到整层（某一层的八成以上）时同样按全量档位处理。Vue 改动对涉及的文件执行相关用例、typecheck、lint、format:check，并验证桌面/手机、明暗主题及权限变化。新数据库和支付改动需验证三种数据库。仅使用本地供应商和支付模拟服务。

独立候选必须隔离数据库、Redis、网络、密钥与存储。源码、候选和正式部署分别记录证据；未提交工作 candidate_sha=null。范围候选只证明所选部分，未运行的 routing、诊断和 race 不计通过。

已接受的原生差异冻结成诊断，集合不扩大：12 项上游诊断和 30 项阻断、`known_upstream_restored`、`known_upstream_race`、`known_upstream_logger_race`。新增的原生差异独立进入 `known_upstream_restored`，保留正确的历史断言和原始失败日志，改动前冻结旧版与原生的对照及精确指纹。新增、缺失、超时、编译错误、意外跳过或行为变化阻断交付。完整候选里已接受的诊断仍为退出 2、candidate_success=false，不能称全绿。改动冻结对象时 `run.py registry` 会指出要重跑的档位。正式切换另行授权。
