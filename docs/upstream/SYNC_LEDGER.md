# 上游同步账本

NewAPI（`QuantumNous/new-api`）是 Ren2Hub 的唯一上游。Sub2API（`Wei-Shaw/sub2api`）与 AxonHub（`looplj/axonhub`）仅为参考项目，不是并列上游，不要求追平。历史引用和作者归属保留在 Git 历史中。

## 2026-10-17 审计后的上游依据修复

`01098b738…` 全量变更审计发现8项代码缺陷和1项验收遗漏。按用户“有上游处理方式直接参考修复、没有则等待授权”的限定，逐项结论、具体来源、修复及待授权边界见[审计修复记录](AUDIT_FIXES_20261017.md)。2026-10-17 07:59 UTC 官方main为`3e2ce2c8856e32ca7054d673f8cd059ae466e80c`；新增4提交不涉及这些问题，相关27份源文件未变化，核心基线保持`09109ec315…`。

本轮采用上游的创建用户字段分流、EPay无独立结算开关的设置合同及Telegram先存凭据后启用的顺序。A2完整修复，A3/A4部分修复，其余6项无可直接采用的完整方案，保持待授权。GitHub等OAuth与Turnstile的先启用问题在观察到的上游中同样存在，不能宣称已有上游修复。

既有“代码完成/候选通过”只描述原提交的实施和当时已执行检查；新的审计结论仍有8个未完全解决条目，正式切换继续未执行。

## 2026-10-17 核心回归

本轮以 NewAPI `09109ec315a5086366f94bcce00f177ec831dbe1` 为冻结核心，Ren2Hub 开发基点为 `ab9fa349c75b39b53cf8f783c5c8be44e8ca3a9f`。这次重建取代此前逐项维护本地账务、协议和准入合同的方案。源码仍在原 Ren2Hub Git 历史内；正式旧系统尚未切换。

标准来源采集于 `2026-10-17T04:48:58.9793113Z` 完整通过，使用工作区外只读参考仓库，无 fetch、无截断。NewAPI main 与冻结 SHA 相同；最新发布为 `v1.0.0-rc.44`（标签提交 `633b4c5b190dc1af99a2814391354d86948e7943`）。官方 release 的 prerelease=false 不等于将 rc 名称认定为稳定版。此次没有推进两个参考项目的追踪点。

| 模块 | 当前处置 | 明确差异 |
| --- | --- | --- |
| 用户、权限、PAT、登录及 Passkey | 采用冻结上游实现与资源目录 | 保留站点 OAuth；新增 routing、ticket 权限；不恢复旧独立权限模型 |
| 钱包、Token 额度、默认价格、分组倍率、预扣及结算 | 采用冻结上游数值额度和计算 | 删除资金事件、独立恢复/准入、渠道倍率及旧价格投影；不提供旧 schema 兼容层 |
| 协议、重试、自动选路、任务 | 采用冻结上游实现 | 私有路由仅缩小候选集合；既有任务维持原渠道约束；WS 每次 create 校验当前列表 |
| 私有路由（Ren2Hub 独有） | 每 Key 单份有序列表，版本冲突 409、空列表失败 | GET/PUT `/api/token/:id/routing`，只读候选和 preview；无额外权重、分组或健康策略 |
| 工单（Ren2Hub 独有） | 用户提交/回复/关闭/重开、管理队列/分派/回复/状态/附件 | `/api/tickets`、`/api/admin/tickets`；每次最多 5 个、每个 10 MiB；实际 MIME 与归属校验；用户入口要求会话 |
| GMPay（Ren2Hub 独有） | 原生 TopUp 事务入账，固定订单应入额度与商户版本 | CNY 十进制金额；网关资产目录；回调/补单共享幂等事务；验签不宣称验证回调未携带的字段 |
| 官方订阅账号、套餐、io.net、Playground | 删除 | type 57 不可创建/选用；普通 API Key、普通充值和钱包额度兼容路径保留 |
| Vue Web | 重接上游接口及实际类型 | 移除 React、Electron、旧聚合后端、任务样例价及模型市场投影；保留 Vue 布局和品牌 |
| 单节点部署 | 只允许 master 单应用实例 | 保留上游 Redis、任务轮询、日志及 WS 功能；旧库守卫在任何迁移前拒绝 schema 5 |

NewAPI `09109ec` 的 cgroup CPU/内存采样与连续阈值熔断随核心整体采用，不再保留旧宿主口径采样或自建运维聚合接口。原生插件按当前激活版本执行，并携带任务的 producerVersion；本轮没有恢复旧版固定源码运行机制。插件重启验收必须据此判断，不能将已退役断言记作通过。

冻结上游的 Stripe 在 TOKENS 展示模式下存在充值数量合同不一致：最低值乘 quota_per_unit，但商品数量/入账及最大值沿用原始数量，报价又除以 quota_per_unit。本轮保持其原生后端，Vue 在该组合下明确停用 Stripe 入口；USD/CNY/CUSTOM 入口保留。EPay/GMPay 的 TOKENS 预设及最低值按真实接口换算，Waffo/Pancake 保留各自最低值语义；四类支付的 TOKENS 手动输入须为 quota_per_unit 的整数倍，避免原生向下取整入账与付款数量不一致。不能把这些界面约束记为上游后端缺陷已修复。

价格编辑采用 `/api/option/model_pricing`，公开展示采用 `/api/pricing`；原生数值的显示转换不构成结算精度升级。静态表达式首档只用于说明；任务表达式不推导样例总额或不存在的 token 单价。设置多项保存逐项执行，不承诺后端原子事务。

## 证据与发布状态

实施副本：`/home/work/ren2hub-core-20261017`。证据目录：`/home/work/ren2hub-core-evidence-20261017`。`source-state.json` 记录标准来源采集，`source-manifest.json` 记录实施前基线及上游文件哈希；[源码差异清单](CORE_SOURCE_DELTA.json)逐文件区分一致、适配、扩展与删除。真实候选 SHA 在提交形成后绑定到根工作区候选报告，不使用工作树指纹冒充提交。

当前已完成的运行证据：

- `go-full-final.log`：主模块全部包与 controller/router 分批测试，单包 60 秒；测试夹具使用原生 PAT，保留权限和审计断言。
- `go-vet-2.log`、`relaykit-1.log`、`tokenkit-1.log` 及对应 vet/build 日志：根静态检查、两个独立模块测试及构建。
- `startup-databases-1.log`：SQLite/MySQL/PostgreSQL 空库初始化、重复启动保留数据、旧库标记拒绝。
- `routing-databases-final.log`、`extensions/evidence-extensions-databases-1.log`、`audit-databases-1.log`：三库路由版本并发、GMPay 回调/补单及工单、原生 PAT 审计。
- `core-race-3.log`：路由、工单、支付、WS 定向 race 通过。此前缺编译器和临时目录空间不足的失败日志保留；通过结果来自固定 Go 1.26.9 编译镜像。
- `frontend-embedded-assets-release.log`、`pgo-build-final.log`、`pgo-binary-info.txt` 与 `pgo-sha256.txt`：真实 Vue 嵌入资源图检查及 moejs profile 的实际 PGO 构建通过，二进制工具链为 Go 1.26.9。
- `sdk-preflight.json`：官方 Python/Node SDK 的 18 项本地协议预检通过；此结果不代替真实候选的扣费验证。
- `vue/vue-vitest.log` 与 `vue/vue-vitest-exit.json`：167 文件、909 项 Vue 测试通过并确认 exit 0。类型、lint、格式和生产构建通过；工单、渠道、原生价格管理及公开页桌面/手机、明暗主题浏览器检查已通过。`vue/vue-topup-units-*` 另记录后续 36 项充值单位定向测试、四个 USD 浏览器组合及 TOKENS 组合通过，最新静态检查和构建均 exit 0。真实候选结果另行绑定。
- `vue/vue-models-http-final.log`：真实嵌入资源 HTTP 回归通过；修复同名 `/models` 目录截获 Vue 页面导致的循环重定向，图标文件与 API 鉴权保持正常。
- `vue/vue-live-contract-tests.log`：后续 22 项定向回归覆盖原生签到开关、任务 list/current/detail 与导航状态。删除不存在的任务 summary API 和旧价格同步结果投影；关闭签到时显示实际状态，不请求禁用接口。相应类型、lint、格式及生产构建通过，旧摘要断言单独登记退役。
- `licenses-1.log`：实际 Go/Vue 的 100 项直接依赖、110 份许可源文件已核对，生成完整许可及 NOTICE。
- `release/archive-preflight-2/manifest.json`：在线只读归档及隔离恢复演练通过；检查时旧任务、未完成恢复、支付订单、附件均为 0，Redis db1 为空。这是切换准备，不能替代停旧应用后的最终归档。

这些是提交前代码验证结果；源码发布、不可变镜像候选和正式空库切换由根工作区[路线图](../../../docs/workflows/REN2HUB_UPSTREAM_ROADMAP.md)及绑定实际 SHA 的候选报告分别记录，不能由本段推定完成。旧核心删除文件及消失的 Go 测试函数在 `legacy-test-retirements.json` 全量登记，裁剪上游订阅用例及旧候选断言另在 `retired-test-cases.json`、`retired-candidate-tests.json` 登记退役，不计作通过。失败、重跑和缺环境的原始日志保留。实际供应商和支付请求仅使用本地模拟服务。

正式切换必须使用新库 `ren2hub_core_<提交前12位>` 和核实为空的 Redis db1，保留旧库、db0、镜像、配置和密钥，不迁移旧业务数据。准备完成后按[更新技能](../../../skills/update-ren2hub-preview/SKILL.md)请求本次空库切换批准。新系统有新增业务数据后停止自动回退，不恢复旧库覆盖新数据。

## 历史来源及等待项

2026-10-16 的完整 NewAPI 19 项、Sub2API 48 项、AxonHub 15 项清单，以及此前历次处置，保存在 Ren2Hub 提交 `ab9fa349c75b39b53cf8f783c5c8be44e8ca3a9f` 的同路径账本中：

```sh
git show ab9fa349c75b39b53cf8f783c5c8be44e8ca3a9f:docs/upstream/SYNC_LEDGER.md
git log -- docs/upstream/SYNC_LEDGER.md
```

历史追踪点为 NewAPI `d032204733db31208f10bac118b6e683aab3bf29`、Sub2API `b0a02a3304a0a78cdd003ddda1dd23ba02c03084`、AxonHub `a946fca495c52344ed073abb6c3562b9ff728b12`。旧账本中的“保留本地合同”仅描述当时实现，不能覆盖本轮已批准的上游回归边界。历史等待 NewAPI 或实际供应商验证的事项不因重建自动变成完成；例如 `SYNC1008-06` 的百炼实际 reasoning_effort 仍未通过真实供应商验证。已删除产品的原测试及合同按退役记录，不据此声称修复其历史问题。
