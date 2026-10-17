# 2026-10-17 审计问题与 NewAPI 处理方式

本记录覆盖 `01098b7387fb67a79ea01b4192d71e96a62a78ad` 的审计总报告 `REVIEW.md`、后端、Vue、工单/GMPay、部署分报告，以及此前 `final-core-audit.md`。审计证据保存在 `/home/work/ren2hub-core-evidence-20261017/audit-20261017/`；本轮上游核对与修复证据在同级 `upstream-audit-fixes/`。历史报告保持原样，不将其中的旧版本通过结果改写成当前版本通过。

用户本次限定：上游已有相关处理方式的直接参考修复；没有的退后等待授权。因此下表中的本地扩展设计和上游尚未处理的问题继续开放，本轮不能标记整轮审计修复完成。

## 来源与版本

2026-10-17 07:59 UTC 标准来源采集为 `complete`。NewAPI main 为 [`3e2ce2c8856e32ca7054d673f8cd059ae466e80c`](https://github.com/QuantumNous/new-api/commit/3e2ce2c8856e32ca7054d673f8cd059ae466e80c)，最新发布仍为 `v1.0.0-rc.44`（tag提交 `633b4c5b190dc1af99a2814391354d86948e7943`）。本地只读参考保持冻结的 `09109ec315a5086366f94bcce00f177ec831dbe1`，没有 fetch 或修改引用。

[冻结基线至观察 main 的4个提交](https://github.com/QuantumNous/new-api/compare/09109ec315a5086366f94bcce00f177ec831dbe1...3e2ce2c8856e32ca7054d673f8cd059ae466e80c)分别处理图片 detail、模型供应商分类、首屏主题和指标卡片统计时段。13个变化路径均未涉及下列问题；逐项核对的27份上游文件在该窗口中未变化，完整列表和SHA256见 `source-evidence.json`。本轮只修复审计中有明确来源的适配，没有推进核心基线或吸收无关提交。NewAPI仍是唯一上游，Sub2API/AxonHub不参与本轮判定。

## 逐项判定

| 审计项 | NewAPI 的实际方式 | 本轮处置 |
| --- | --- | --- |
| A1 工单邮件HTML | [account_security.go](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/service/account_security.go#L34)对安全事件做转义；通用[user_notify.go](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/service/user_notify.go#L110)仍原样输出HTML正文，没有工单或通知按类型转义合同。 | 待授权。局部转义惯例可供设计参考，但不是现成工单处理流程。 |
| A2 创建用户字段 | [user-form.ts](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/web/src/features/users/lib/user-form.ts#L79)在创建时仅提交用户名、显示名、密码、角色及适用权限；分组只编辑时提交，邮箱由已有账户绑定管理，状态走独立管理操作。 | 直接采用创建/编辑分流：Vue创建不再展示或提交email/group/status，不增加创建后补写。现有编辑行为不在本项扩大修改。 |
| A3 EPay/GMPay结算开关 | [payment-settings-section.tsx](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/web/src/features/system-settings/integrations/payment-settings-section.tsx#L421)的EPay字段集合及[PaymentSetting](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/setting/operation_setting/payment_setting.go#L5)没有独立回调结算开关；[可用性检查](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/controller/payment_webhook_availability.go#L95)按合规、商户配置和支付方式判断。上游没有GMPay。 | 部分处理：移除EPay无效开关、专用类型/默认值/文案。GMPay开关及其虚假暂停承诺仍未解决，待本地扩展修复授权；不能用EPay可用性检查替代GMPay历史订单商户快照。 |
| A4 OAuth/Turnstile保存顺序 | [oauth-section.tsx](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/web/src/features/system-settings/auth/oauth-section.tsx#L212)对GitHub等依旧先enabled再凭据并逐项保存；[Turnstile](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/web/src/features/system-settings/auth/bot-protection-section.tsx#L73)同样未处理。仅Telegram明确排列client_id、client_secret、enabled。 | 部分处理：仅按上游顺序保存Telegram两项凭据后启用；其他提供商及Turnstile待授权。不新增通用依赖排序或假装原子保存。 |
| A5 普通用户routing PAT | [个人scope静态目录](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/service/access_token_scope.go#L60)有api_key，但[Casbin枚举](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/service/access_token_scope.go#L134)仅面向管理员；上游没有普通用户可撤销routing资源。 | 待授权。直接加静态routing条目会与本地Casbin资源重复；改用api_key又会改变权限合同。 |
| A6 归一化模型候选 | 原生[请求鉴权](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/middleware/distributor.go#L509)处理实际请求模型；[模型目录](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/controller/model.go#L251)仍从ability名称检查限制，没有具体Token模型与渠道规范化名称反向求交实现。 | 待授权。已有规范化工具不等于有完整私有候选求交方案，不能放宽模型白名单代替修复。 |
| A7 管理员pin+manual WS | [首次pin](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/service/channel_select.go#L285)和[续次pin](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/relay/responses_websocket.go#L582)都按管理员指定渠道处理；上游没有manual列表约束。 | 待授权。缺陷来自本地首次增加privatePinnedGroup而续次没有，需要本地接合修复；普通manual WS及auto管理员pin不应被扩大变更。 |
| A8 DB名绑定应用SHA | [compose](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/docker-compose.yml#L29)使用固定DSN和状态健康检查，没有本地release.env、镜像SHA和持久数据目标验证器。 | 待授权。普通固定DSN不足以提供完整稳定数据身份核验方案。 |
| A9 旧Playwright合同 | 上游[测试入口](https://github.com/QuantumNous/new-api/blob/09109ec315a5086366f94bcce00f177ec831dbe1/web/package.json#L17)为React/Vitest，没有本地Vue/Playwright旧钱包、渠道与运维用例的迁移或退役清单。 | 待授权。不通过通用testIgnore隐藏问题；本次只给实际修复增加定向回归。 |

A2关闭后，A3/A4仍为部分修复，A1/A5/A6/A7/A8/A9保持开放；共8个审计条目仍需完整处理。其中工单HTML、GMPay虚假结算开关和私有路由边界不能因本次其他修复通过而忽略。

## 验证与发布

本次验证使用 Go `1.26.9`、Bun `1.4.0`，所有接口交互为本地测试或模拟服务：

- Vue 全量单测 171 个文件、937 项通过；随后 EPay 中英文文案补清，受影响的 2 个测试文件、16 项通过。两组有重叠，不相加为独立用例总数。Telegram 新增 3 项先在旧顺序下实际失败，修复后通过；覆盖凭据完成前不启用及任一凭据失败时保留草稿。
- 完整类型、ESLint、Prettier 和生产构建通过。首次类型检查暴露创建/编辑类型拆分后父页面的联合类型不再自动兼容，已在既有编辑分支明确参数类型并复验；保留首次失败日志。构建仍有 ECharts 分块超过 500 kB 的提示，本次没有调整分包。
- 定向 Playwright 4 个场景通过：1440/390 宽度 × 明/暗主题，验证创建请求仅含有效字段、EPay 旧无效配置不重现、有效设置保存及重新加载，无未预期 API、页面错误或横向溢出。截图复核发现的 EPay 残留承诺已清理，最终重新执行 4 项并检查截图。首次浏览器因宿主缺少动态库在启动前失败；使用已有隔离浏览器库后通过，没有安装系统包。
- 新生产资源经 `prepare-frontend-embed.mjs` 进入 Go 嵌入目录；`GOWORK=off CGO_ENABLED=0 go test -timeout=60s -count=1 -v -run '^TestProductionEmbedded(AssetGraph|ModelPagesAndFiles)$' .` 的两个顶层测试及 8 个 HTTP 子场景全部通过、无跳过。资源图及产物 SHA256 已归档；验证后恢复原跟踪占位文件，不提交生成资源。
- 核心差异清单全部 1145 路径哈希仍匹配；未修改 Go 后端、数据库、依赖、锁文件或候选驱动。未重复未受影响的三库、race 和全套后端测试。5 份修改文档的 23 个相对链接与 diff 空白检查通过。

原始日志、退出码、截图、来源报告及生产资源哈希均在 `upstream-audit-fixes/`。源码经 Ren2Hub 子模块先发布、根 gitlink 与文档后发布；完整应用/根 SHA 及 `VERIFY_OK`、`SYNC_OK` 记录在该目录的发布摘要。没有新镜像或新候选报告；本次修复完成不等于整批审计通过。

旧不可变镜像及45项后端/38次页面候选报告只绑定`01098b738…`，不是本轮前端修复的候选结果。正式仍运行旧核心，本次没有更新应用容器、正式配置、数据库或Redis。待授权项处理完成前，不把旧候选或本轮前端测试当作整批审计通过。
