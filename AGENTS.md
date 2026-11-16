# Ren2Hub 项目规则

遵循上层工作区 AGENTS.md。默认使用简体中文；用户明确批准的本轮合同优先于历史文档。

## 产品与来源

- NewAPI（QuantumNous/new-api）是唯一上游；Sub2API 和 AxonHub 仅为参考。本轮冻结同步目标为 `b388140acfd80369ec6243b16869daff1b4051f4`（固定 NewAPI main，rc.51 后两个提交），处置与验证状态见同步账本。
- 单节点核心网关，仅交付 Go 服务和 Vue Web。保留 Ren2Hub / DayFliggy 品牌及上游许可。
- 后端钱包、预扣结算、协议和权限采用 NewAPI。按 2026-10-18 已批准合同实施渠道级自动/手动路由、无等待并发及模型基础价 × 渠道倍率；删除业务分组。不要恢复旧 schema 5 账本、冻结报价、加密重放、等待队列或第二套权限模型。
- 扩展包括工单、每个 Token 的自动/有序手动路由、GMPay、邀请返利、统一模型目录与统计。平台渠道由管理员维护，用户选择不等于 BYOK。Vue 保留 9a63062227 视觉，按真实原生接口接通；排除历史协议诊断、权限增强、JS 插件增强、任务产物增强、独立商城、会员等级、新人任务、七日签到阶梯。当前原生对应能力继续保留。
- 2026-11-16 批准充值加赠（替换 2026-11-10 的充值折扣，旧配置不保留，档位比例数值沿用）：单笔入账阶梯、固定加赠、全站阶梯与个人日/周/月阶梯相加封顶，实付不变，按本金另赠额度；覆盖六个在线充值渠道、兑换码和管理员加额，跟随同一启用设置；调用计价与上游一致，见 docs/billing/TOPUP_BONUS.md。它不是会员等级，不附带权益，不复制基础价，不建资金账务。
- 旧 Codex/type 57 专用账号接入、站内订阅套餐、io.net 部署、Playground、React、Electron 均已退役。通用提供商插件登录、刷新和凭据管理开放；普通 API Key 接入、站点 OAuth 和钱包充值继续保留。请求处理插件仅有全局、渠道与模型作用域，无业务分组。
- 新核心首次安装不迁移旧独立账本或业务分组数据，迁移前守卫继续保留。已初始化实例的普通更新保持现有数据库、Redis、附件、配置与在途任务，不按首次安装流程重建数据。
- 模型默认价格与内置计费表达式已按明确要求删除；工具单价、音频倍率表和 Gemini 音频输入固定价继续沿用上游。

## 实现

- Go 1.26；独立 relaykit/ 和 tokenkit/ 不得依赖根模块，受影响时以 GOWORK=off 单独验证。
- 保持 Router → Controller → Service → Model 分层；核心代码优先保持上游一致，必要差异记入 docs/upstream/SYNC_LEDGER.md。
- 2026-11-14 后端原生回归：从 `2382fbdd55a6d76d73d8e5b314c7765c98cd8da6` 对齐固定 b388。仅保留批准产品合同及 Vue 实际对接的最小差异；历史 `verified_fix` 不构成保留理由。混合文件逐段拆分，来源及裁定见 `docs/upstream/NATIVE_B388_REVIEW.json`。
- JSON 使用 common.Marshal / Unmarshal 等共享封装。数据库支持 SQLite、MySQL、PostgreSQL；保持上游锁和事务习惯。
- 不吞掉异常、不添加假成功或无依据回退；支付回调和人工补单必须保证最多入账一次。
- 严格预扣、渠道倍率与即时并发保留；管理员拉取或删除模型不占中继并发槽。保留充值订单冻结的本金与加赠（`ExpectedQuota` 为合计）、GMPay 核验和支付单次入账，已使用的兑换码不可修改；Stripe 的 Money 仍表示原生数量含义，不能改写成实付金额或重算历史额度。
- Vue 使用 Composition API、TypeScript、Pinia、Vue Router、Vue I18n 和 Bun。保留真实后端合同，不能伪造旧预留余额或版本字段。
- 所有 UI 文本使用现有 i18n。翻译工作先读 .agents/skills/i18n-translate/SKILL.md。
- Vue 是唯一前端，Go 嵌入 frontend/embed-dist，所有启用页面使用实际接口。权限和导航共享来源；失效请求应取消或防止覆盖新结果。
- 不引入 /api/next 兼容后端。工单使用 /api/tickets 和 /api/admin/tickets，私有路由使用 /api/token/:id/routing。

## 验证

先定向测试，再类型和静态检查、受影响构建、最小冒烟。Go 单包测试超时 60 秒。新测试应保护真实行为，不以文件存在或实现镜像断言替代回归。

默认按改动模块测试，方法和模块对照见 docs/testing/MODULE_TESTING.md，各层的验证内容见 docs/testing/LOCAL_TESTING.md：每个任务和实施计划先写测试计划，格式和定向不扩大的规则见工作区的 task-testing 技能；迭代用 `run.py targeted` 点名运行，提交前用 `affected --plan` 对照影响面，只在影响面说不清时运行 `affected`，自动选择等于整层时按全量档位处理；部署用 `candidate --scope auto`，按改动追加部分。`quick`、`all`、完整 candidate 和整套 ui-contracts 是全量档位，只在用户明确要求全量测试时运行。Vue 改动对涉及的文件执行相关用例、typecheck、lint、format:check，并验证桌面/手机、明暗主题及权限变化。新数据库和支付改动需验证三种数据库。仅使用本地供应商和支付模拟服务。

独立候选必须隔离数据库、Redis、网络、密钥与存储。旧合同测试记录退役，不算通过。源码、候选和正式部署分别记录证据；未提交工作 candidate_sha=null。范围候选只证明所选部分，未运行的 routing、诊断和 race 不计通过。

恢复原生造成的已知行为差异独立进入 `known_upstream_restored`：保留正确历史断言和原始失败日志，改动前冻结旧版/原生对照及精确指纹。不扩大既有 12 项上游诊断和 30 项阻断；新增、缺失、超时、编译错误、意外跳过或行为变化阻断交付。完整候选里已接受诊断仍为退出 2、candidate_success=false，不能称全绿。改动冻结对象时 `run.py registry` 会指出要重跑的档位。正式切换另行授权。
