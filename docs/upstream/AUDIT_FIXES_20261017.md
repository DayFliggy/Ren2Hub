# 2026-10-17 审计修复与来源边界

本记录覆盖 `01098b7387fb67a79ea01b4192d71e96a62a78ad` 的原始审计与后续两阶段修复。原始报告在 `/home/work/ren2hub-core-evidence-20261017/audit-20261017/`；历史首阶段证据在同级 `upstream-audit-fixes/`，保持原样。

当前批次已获用户批准：先参考 NewAPI，再核对 Sub2API/AxonHub；三者没有完整对应实现时，实施最小本地修复。工作副本为 `/home/work/ren2hub-audit-repair-20261017`，应用基点 `29a1d1c78ad576feda3e280dd359e37219b49df2`，证据为 `/home/work/ren2hub-audit-repair-evidence-20261017/`。源码尚未提交，`candidate_sha=null`。本地代码及适用验证已完成；发布和绑定新 SHA 的不可变候选仍未完成，整批审计尚不关闭。

## 来源与版本

本批标准采集 `source-state.json` 于 `2026-10-17T08:36:51.9100792Z` 为 complete；全部参考副本干净，无 fetch 或 Git 引用修改。下表是该时刻观察值，不代表持续跟踪的实时结果。

| 项目/角色 | 本地冻结 SHA | 官方默认/跟踪 HEAD | 官方 latest |
| --- | --- | --- | --- |
| NewAPI，唯一上游 | `09109ec315a5086366f94bcce00f177ec831dbe1` | main `3e2ce2c8856e32ca7054d673f8cd059ae466e80c` | `v1.0.0-rc.44`，tag commit `633b4c5b190dc1af99a2814391354d86948e7943`，2026-10-12 13:42:01 UTC |
| Sub2API，参考 | `b0a02a3304a0a78cdd003ddda1dd23ba02c03084` | main `b0a02a3304a0a78cdd003ddda1dd23ba02c03084` | `v0.2.17`，tag commit `060d0160417782cd962a0c7f460f6cd4565d4916`，2026-10-16 04:41:43 UTC |
| AxonHub，参考 | `a946fca495c52344ed073abb6c3562b9ff728b12` | unstable `a946fca495c52344ed073abb6c3562b9ff728b12` | `v1.0.0-beta11`，tag commit `b8bb3421e1fe946cfc4f0d86e8412098738f2793`，2026-10-08 05:43:47 UTC |

三份 GitHub releases/latest 的原始标志均为 draft=false、prerelease=false；rc/beta 名称不因此变成稳定版。NewAPI [冻结核心到观察 main 的四个提交](https://github.com/QuantumNous/new-api/compare/09109ec315a5086366f94bcce00f177ec831dbe1...3e2ce2c8856e32ca7054d673f8cd059ae466e80c)处理图片 detail、模型供应商分类、首屏主题和指标时段，不涉及本批问题；历史核对的 27 份相关文件未变化。本批不吸收该窗口，不推进核心基线。

参考范围只提供惯例：Sub2API 的 [BeforeRequest/BeforeTurn](https://github.com/Wei-Shaw/sub2api/blob/b0a02a3304a0a78cdd003ddda1dd23ba02c03084/backend/internal/handler/openai_gateway_handler.go#L2976)及[续次模型白名单测试](https://github.com/Wei-Shaw/sub2api/blob/b0a02a3304a0a78cdd003ddda1dd23ba02c03084/backend/internal/handler/openai_gateway_ws_model_allowlist_test.go#L51)支持“发送前逐次校验”；AxonHub [CanGrantScopes](https://github.com/looplj/axonhub/blob/a946fca495c52344ed073abb6c3562b9ff728b12/internal/server/biz/permission_validator.go#L94)支持授权范围受当前权限限制。它们没有 Ren2Hub 的 Casbin routing 资源、每 Token 手动列表和 NewAPI pin/额度组合，不能整段移植或声称已具备完整修复。既有规范化工具、HTML 转义或普通 compose DSN 同样不等于相应本地合同。

## 当前逐项状态

| 审计项 | NewAPI 的实际方式 | 本批处置与边界 |
| --- | --- | --- |
| A1 工单邮件HTML | [account_security.go](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/service/account_security.go#L34)对安全事件做转义；通用[user_notify.go](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/service/user_notify.go#L110)仍原样输出HTML正文，没有工单或通知按类型转义合同。 | 本批源码及定向验证完成。`service/user_notify.go` 在最终邮件正文、占位符替换之后仅转义工单文本；不改变非邮件通道或原生 HTML 模板。属于批准的本地扩展修复。 |
| A2 创建用户字段 | [user-form.ts](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/web/src/features/users/lib/user-form.ts#L79)在创建时仅提交用户名、显示名、密码、角色及适用权限；分组只编辑时提交，邮箱由已有账户绑定管理，状态走独立管理操作。 | 已在历史阶段 `29a1d1c78…` 完成并发布；创建/编辑分流沿用 NewAPI，无新增创建后补写。 |
| A3 EPay/GMPay结算开关 | [payment-settings-section.tsx](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/web/src/features/system-settings/integrations/payment-settings-section.tsx#L421)的EPay字段集合及[PaymentSetting](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/setting/operation_setting/payment_setting.go#L5)没有独立回调结算开关；[可用性检查](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/controller/payment_webhook_availability.go#L95)按合规、商户配置和支付方式判断。上游没有GMPay。 | 本批补齐 GMPay：移除无效结算开关及承诺，真实 enabled 只表示接受新订单；旧订单仍使用商户快照核验并幂等入账。源码及定向回归完成；EPay 部分已在历史阶段完成。 |
| A4 OAuth/Turnstile保存顺序 | [oauth-section.tsx](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/web/src/features/system-settings/auth/oauth-section.tsx#L212)对GitHub等依旧先enabled再凭据并逐项保存；[Turnstile](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/web/src/features/system-settings/auth/bot-protection-section.tsx#L73)同样未处理。仅Telegram明确排列client_id、client_secret、enabled。 | 本批源码及定向验证完成。共享保存器按停用→依赖配置→启用排序，逐项维护已成功基线与失败草稿，不宣称事务；OIDC 所需 discovery 在任何写入前完成，复用原生 root-only 接口。超出 Telegram 的通用顺序和 Vue 前置整合属于批准的本地修复。 |
| A5 普通用户routing PAT | [个人scope静态目录](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/service/access_token_scope.go#L60)有api_key，但[Casbin枚举](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/service/access_token_scope.go#L134)仅面向管理员；上游没有普通用户可撤销routing资源。 | 本批源码及真实 PAT 定向验证完成。由现有 authz ResourceDefinition.DefaultRoles 标识个人动作，与实时权限求交；不复制静态 scope 目录。普通用户仅获得当前 routing read/write，跨用户、缺 scope、撤权均拒绝。 |
| A6 归一化模型候选 | 原生[请求鉴权](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/middleware/distributor.go#L509)处理实际请求模型；[模型目录](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/controller/model.go#L251)仍从ability名称检查限制，没有具体Token模型与渠道规范化名称反向求交实现。 | 本批源码及定向验证完成。候选按实际请求名→精确/规范化 ability 求交；具体 Token 名命中规范化 ability 时返回实际获准请求名，不使同族不同具体名互相授权。GET/PUT/preview、选择器及缓存开关回归通过。 |
| A7 管理员pin+manual WS | [首次pin](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/service/channel_select.go#L285)和[续次pin](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/relay/responses_websocket.go#L582)都按管理员指定渠道处理；上游没有manual列表约束。 | 本批源码及真实 WS 定向/race 验证完成。manual+pin 每次 create 共用列表/组/模型资格校验；保留锁定计价组。撤销资格为 403/1008；配置和资格查询技术故障为 500/1011，发送和预扣前失败且不重试。唯一 SQL 查询实现立即传播错误，避免精确查询错误被规范化结果覆盖；auto 管理员 pin 与原任务续接边界保留。 |
| A8 DB名绑定应用SHA | [compose](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/docker-compose.yml#L29)使用固定DSN和状态健康检查，没有本地release.env、镜像SHA和持久数据目标验证器。 | 本批验证器源码及 19 项本地替身执行/安装器测试完成。数据目标来自 release.env，数据库名保留首次安装身份；普通镜像更新不要求重命名数据库，实际 DSN、Redis、目录、镜像绑定继续严格校验。没有执行正式更新。 |
| A9 旧Playwright合同 | 上游[测试入口](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/web/package.json#L17)为React/Vitest，没有本地Vue/Playwright旧钱包、渠道与运维用例的迁移或退役清单。 | 本地代码及验证完成。55 份历史浏览器文件已有逐项迁移/保留/退役清单，真实目标测试独立且缺目标明确失败；不得用 testIgnore 隐藏旧断言。检查另修复元数据预览 candidates、提交 selections 的原生接口；插件不存在 preview 端点的问题已按 NewAPI 本地静态解析合同处理，不执行插件代码；无法静态确定的声明不再阻断服务器权威上传。完整默认浏览器 416/416 通过，无跳过或重试通过；新候选仍待执行，不提前关闭整批。 |

## 本批验证与剩余工作

- A1/A3：`a1-before-fix.log` 保留真实 SMTP 失败，`a1-a3-go-targeted-2.log` 验证 SMTP/HTTP 通知边界和 GMPay 新订单关闭、历史回调继续处理；`a3-vue-targeted.log` 验证无效开关退役。非邮件正文与原生通知模板未改变。
- A4：`a4-settings-tests-final.log` 验证依赖顺序、部分成功基线、失败草稿及 OIDC 前置；相应类型和 lint 结果已保存。最终共享前端检查仍由集成批次确认。
- A5/A6/A7：`a5-before.log`、`a6-before.log`、`a7-before.log` 和 `a7-ability-error-before.log` 保存先红证据；`a5-a7-final-tests.log`、`a5-authz-baseline.log`、`a7-ability-error-after.log`、`a7-model-after.log` 通过。真实 WS 覆盖缓存开关下逐次撤权、正常连续结算、auto pin 和锁定计价组；首次/续次资格数据库故障均无额外发送或扣费。`a7-ability-error-race.log` 是最后受影响的 model/service/relay/controller 定向 race，官方 Go 1.26.9 容器、单包 60 秒，全部通过。
- A8：`a8-baseline-repro.log` 在旧验证器上实际复现“B 镜像不能保留 A 首次安装数据库”；`a8-after.log` 通过 4 组真实验证器替身执行及 15 项安装器回归，共 19 项。`a8-before.log` 含测试夹具错误，不能作为旧行为失败证据。相关技能静态校验通过。
- 三库：`go-databases-1.log` 验证 SQLite/MySQL/PostgreSQL 的 TokenRouting 与 APITokenAudit；未改的旧初始化测试可按源码一致性复用。`go-race-2.log` 与后续 A7 定向 race 分别绑定各自代码阶段，不重复相加。主模块完整分批 `go-full-2.log`、`go-vet-final.log` 均 exit 0；A7 后续补充变化以上述最终定向和 race 结果覆盖。
- Vue：最终 `vue-unit-full-final.log` 的 174 文件/1012 项通过；`vue-typecheck-final-2.log`、`vue-lint-final-2.log`、`vue-format-final-2.log`、`vue-build-final.log` 与 `go-embedded-final.log` 均通过，包含元数据及插件修复。早期类型、格式和接口失败证据保留。`a9-default-full.log`/`a9-default-results.json` 的完整默认 55 文件、416 场景全部通过，unexpected/flaky/skipped 均为 0；这是无过滤的独立完整运行，不以分组求和替代。55 原文件逐一映射核对无遗漏，见 [浏览器迁移记录](BROWSER_TEST_MIGRATION_20261017.md)及 `a9-mapping-validation.json`。
- 候选扩展驱动新增普通用户 PAT、具体模型候选交集、已锁 WS 资格撤销与收费断言，`routing-acceptance-preflight.log` 仅为本地驱动预检，不能代替真实候选。发布、新不可变镜像及绑定实际 SHA 的候选验收均待完成；本批未修改正式业务数据。

`audit-production-review.md` 记录 A1/A3/A4/A5/A6/A7 独立复审；唯一补充发现为 A7 资格 SQL 错误吞没，已在共享查询路径修复并有先红/后绿及 race 证据。[CORE_SOURCE_DELTA.json](CORE_SOURCE_DELTA.json) 记录当前源码范围、分类和内容哈希，既不代表已提交 SHA，也不覆盖 Vue 和根部署脚本。`audit-a9-production-review.md` 另核对元数据、插件解析及 live 隔离；发现的 Playwright 密码失败日志风险已改用不打印参数的真实表单输入，并用假凭据浏览器验证。许可清单更新到 101 项直接依赖、111 份许可源文件。

## 历史阶段：截至 29a1d1c78 的授权与验证

首阶段用户限定“上游已有相关处理方式直接参考修复，否则等待授权”。该阶段只完成 A2、A3 的 EPay 部分、A4 的 Telegram 顺序，源码已发布为 `29a1d1c78ad576feda3e280dd359e37219b49df2`；当时其余条目保持开放。本批后续授权已替代“等待授权”的执行限制，但不追溯改写历史报告，也不将当时的检查扩张为本批通过。

该阶段验证使用 Go `1.26.9`、Bun `1.4.0`，所有接口交互为本地测试或模拟服务：

- Vue 全量单测 171 个文件、937 项通过；随后 EPay 中英文文案补清，受影响的 2 个测试文件、16 项通过。两组有重叠，不相加为独立用例总数。Telegram 新增 3 项先在旧顺序下实际失败，修复后通过；覆盖凭据完成前不启用及任一凭据失败时保留草稿。
- 完整类型、ESLint、Prettier 和生产构建通过。首次类型检查暴露创建/编辑类型拆分后父页面的联合类型不再自动兼容，已在既有编辑分支明确参数类型并复验；保留首次失败日志。构建仍有 ECharts 分块超过 500 kB 的提示，该阶段没有调整分包。
- 定向 Playwright 4 个场景通过：1440/390 宽度 × 明/暗主题，验证创建请求仅含有效字段、EPay 旧无效配置不重现、有效设置保存及重新加载，无未预期 API、页面错误或横向溢出。截图复核发现的 EPay 残留承诺已清理，最终重新执行 4 项并检查截图。首次浏览器因宿主缺少动态库在启动前失败；使用已有隔离浏览器库后通过，没有安装系统包。
- 新生产资源经 `prepare-frontend-embed.mjs` 进入 Go 嵌入目录；`GOWORK=off CGO_ENABLED=0 go test -timeout=60s -count=1 -v -run '^TestProductionEmbedded(AssetGraph|ModelPagesAndFiles)$' .` 的两个顶层测试及 8 个 HTTP 子场景全部通过、无跳过。资源图及产物 SHA256 已归档；验证后恢复原跟踪占位文件，不提交生成资源。
- 核心差异清单全部 1145 路径哈希仍匹配；未修改 Go 后端、数据库、依赖、锁文件或候选驱动。未重复未受影响的三库、race 和全套后端测试。5 份修改文档的 23 个相对链接与 diff 空白检查通过。

原始日志、退出码、截图、来源报告及生产资源哈希均在 `upstream-audit-fixes/`。源码经 Ren2Hub 子模块先发布、根 gitlink 与文档后发布；完整应用/根 SHA 及 `VERIFY_OK`、`SYNC_OK` 记录在该目录的发布摘要。没有新镜像或新候选报告；该阶段修复完成不等于整批审计通过。

旧不可变镜像及45项后端/38次页面候选报告只绑定`01098b738…`，不是该阶段前端修复的候选结果。正式仍运行旧核心，该阶段没有更新应用容器、正式配置、数据库或Redis。当时未获授权的项目现已进入上述本地修复批次；这些历史结果仍不能代替本批集成和新候选验收。
