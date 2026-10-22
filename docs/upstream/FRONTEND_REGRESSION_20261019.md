# 2026-10-19 前端历史覆盖与审计

本批按用户“直接使用 Git 历史版本进行覆盖并进行审计”“不动后端”执行。开发基点为 `e8bf2aa0c16f5ad39f15fdd701c66dc0e34ad063`，历史来源为 `9a63062227c31ba54a6346e07ee8b4fd8802f22f`，提交时间 **2026-10-16 13:04:14 UTC / 北京时间 21:04:14**。

先用 `git show <历史 SHA>:<文件>` 覆盖缺失页面和组件，再在前端接线到当前已有接口。保留历史布局及样式，同时保留当前原生权限、取消、数值额度、无分组路由和支付安全合同。不是对整个 frontend 目录作字节回滚；不能因恢复外观复活旧接口或已排除产品。

## 本批恢复范围

| 模块 | 恢复的历史界面 | 当前数据边界 |
| --- | --- | --- |
| 运维监控 | 状态头、网关、三资源仪表、上游/质量、趋势、存储、系统任务卡 | 使用 performance stats/routing、model-catalog 和 system-task；缺失 CPU/宿主内存/网络/数据库与 Redis 探针为不可用。Go runtime sys 不冒充堆内存。趋势仅为打开页面后的有界轮询采样，非历史监控。 |
| Key 路由 | 三项模式/倍率卡、渠道选择、排序列表、说明侧栏 | 现有自动/手动、版本冲突及最高倍率；不恢复业务分组、队列或额外权重。保存成功后刷新失败明确提示已保存和候选陈旧。 |
| Dashboard 自动路由 | 厂商分栏、折叠、3 条加“更多”、渠道状态行、环形占用和健康条位置 | 目录顺序明确不是执行顺序；只显示真实并发/倍率/资格。未提供历史健康样本时为中性不可用条，不伪造成功率。 |
| 订单 | 四项指标、曲线、方式占比和用户榜布局 | CNY/USD 等分别选择；未知金额标为已记录小计，平均额不可用，曲线注明不含未知金额。后端未提供的占比及榜单保持明确不可用。 |
| 钱包充值 | 快捷金额、支付方式、摘要及动作区域 | 保留现行报价/下单、步长、最小值和 Stripe TOKENS 限制；支付币种以真实收银台为准。 |
| GMPay 设置 | 商户卡、开放新订单、资产目录及图标/颜色编辑 | 保留现行 CNY 商户协议、目录发现、秘密留空保留和历史付款结算合同；不恢复无效结算开关。 |
| 发票 | 五列布局中的三列表单及两列记录区域、导航入口 | 当前无后端接口；明确不可提交，不发送请求、不持久化草稿、离页清除。 |
| 活动 | 历史活动/任务卡框架及签到卡 | 仅使用原生基础签到与现行邀请数据；不恢复会员、新人任务或七日阶梯。 |

## 全量页面核对

历史包含 50 个非测试 Vue 页面：47 个有当前对应文件，其中 29 个与历史页面源码字节相同。其余 18 个逐项核对布局和现行合同；3 个已排除产品页面继续退役。该计数只描述页面文件，不将组件或 API 层适配误报成完全一致。

| 页面 | 核对与处置 |
| --- | --- |
| `HomeView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `NotFoundView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `admin/ModelManagementView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `admin/ModelPricingView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `admin/OpsMonitorView.vue` | 本批 Git 覆盖：卡片、资源仪表、趋势、质量、存储与系统任务；原生数据投影 |
| `auth/OAuthCallbackView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `auth/ResetPasswordConfirmView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `auth/ResetPasswordView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `auth/SignInView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `auth/SignUpView.vue` | 保留原生注册、邀请绑定和安全校验 |
| `console/AccountCenterView.vue` | 保留账户布局与原生额度；会员等级已排除 |
| `console/AccountSettingsView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `console/ActivityView.vue` | 本批 Git 覆盖：活动卡片、原生签到；不恢复七日阶梯与新人任务 |
| `console/AdminTicketsView.vue` | 保留工单队列布局及当前原生工单、安全附件合同 |
| `console/AuditLogsView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `console/BigameView.vue` | 按既有产品边界继续退役；活动页保留不可用卡片 |
| `console/ChannelsView.vue` | 保留管理布局；采用渠道倍率和并发，不恢复业务分组、队列与旧限频 |
| `console/DashboardView.vue` | 保留概览布局；本批子组件恢复厂商折叠/更多/渠道行与健康占位条 |
| `console/DrawingLogsView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `console/FarmView.vue` | 按既有产品边界继续退役；活动页保留不可用卡片 |
| `console/InviteView.vue` | 保留邀请布局；接现行领域返利与幂等全额转入，不恢复旧最低额 |
| `console/InvoiceView.vue` | 本批 Git 覆盖：申请表及记录布局；无后端，明确不可提交，草稿只在当前页面 |
| `console/KeysView.vue` | 保留 Key 列表、Codex/Pi 配置预览/复制/下载及目录/撤权/取消保护 |
| `console/LogsView.vue` | 保留日志筛选、表格和导出；不恢复业务分组和旧账务诊断 |
| `console/MarketplaceView.vue` | 独立商城已明确排除，继续退役 |
| `console/ModelsView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `console/OrdersView.vue` | 本批 Git 覆盖：四项指标、营收曲线、占比与榜单；按币种展示，缺失聚合不可用 |
| `console/RedemptionView.vue` | 保留兑换码布局及现行四状态、双向排序 |
| `console/SecurityAccessView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `console/SystemSettingsView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `console/TaskLogsView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `console/TaskPluginsView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `console/TicketDetailView.vue` | 保留工单详情与当前原生归属、回复及附件合同 |
| `console/TicketsView.vue` | 保留用户工单布局及当前原生接口 |
| `console/TokenRoutingView.vue` | 本批 Git 覆盖：三项模式/倍率卡、渠道列表与侧栏；保留当前渠道路由接口 |
| `console/UsersView.vue` | 保留用户管理布局及当前原生额度、权限、并发和操作幂等 |
| `console/WalletView.vue` | 保留钱包记录布局；本批子组件恢复充值预设、方式、摘要与 GMPay 资产编辑 |
| `console/systemSettings/AuthSettingsView.vue` | 页面源码曾与 9a63062227 字节相同；没有路由或组件引用，2026-10-22 作为死代码删除，现行设置页由 `SystemSettingsDomainView` 承载 |
| `console/systemSettings/BillingSettingsView.vue` | 页面源码曾与 9a63062227 字节相同；没有路由或组件引用，2026-10-22 作为死代码删除，现行设置页由 `SystemSettingsDomainView` 承载 |
| `console/systemSettings/ContentSettingsView.vue` | 页面源码曾与 9a63062227 字节相同；没有路由或组件引用，2026-10-22 作为死代码删除，现行设置页由 `SystemSettingsDomainView` 承载 |
| `console/systemSettings/ModelSettingsView.vue` | 页面源码曾与 9a63062227 字节相同；没有路由或组件引用，2026-10-22 作为死代码删除，现行设置页由 `SystemSettingsDomainView` 承载 |
| `console/systemSettings/OperationsSettingsView.vue` | 页面源码曾与 9a63062227 字节相同；没有路由或组件引用，2026-10-22 作为死代码删除，现行设置页由 `SystemSettingsDomainView` 承载 |
| `console/systemSettings/SecuritySettingsView.vue` | 页面源码曾与 9a63062227 字节相同；没有路由或组件引用，2026-10-22 作为死代码删除，现行设置页由 `SystemSettingsDomainView` 承载 |
| `console/systemSettings/SiteSettingsView.vue` | 页面源码曾与 9a63062227 字节相同；没有路由或组件引用，2026-10-22 作为死代码删除，现行设置页由 `SystemSettingsDomainView` 承载 |
| `public/PricingView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `public/PublicDocumentView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `public/RankingsView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `public/StatusErrorView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `setup/SetupErrorView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |
| `setup/SetupView.vue` | 页面源码与 9a63062227 字节相同；相关子组件按本批及现行合同核对 |

## 审计及修正

- 保留用户/管理权限、功能开关、会话与撤权规则；未知 API 请求使浏览器测试失败，无 `/api/next` 兼容响应。
- 修复历史金额控件将 `10.50` 清洗为 `1050` 的风险：本页保留原始输入，仅纯数字安全整数可报价/下单；小数、负数、科学记数及符号输入均拒绝。
- 订单区间切换立即清空旧区间快照；同区间刷新失败保留并标注陈旧，401/403 清空，取消或迟到响应不覆盖当前状态。
- 原生订单不保证有 email，用户列依次使用 email、username、用户 ID；未知金额不参与虚假的平均额，曲线明确记录金额口径。
- 已恢复组件使用自有双语消息，严格检查缺少翻译，避免历史已删除的文案键以裸 key 显示。
- 运维的性能与路由轮询分别保留实际完成时刻的样本。跨秒完成不再互插空点；真实失败保留断点，取消的响应不采样，按时间保留完整 24 小时，而非按合并行数提前截断。
- 手机端钱包日期轴改为月日短标签、自动稀疏及隐藏重叠，完整日期仍保留在分类数据和 tooltip。

## 验证与状态

执行证据：`/home/work/ren2hub-frontend-regression-evidence-20261019/`。历史视图清单为 `history-view-inventory.json`。

| 检查 | 实际结果与证据 |
| --- | --- |
| Vue 单测 | 全量 191 文件 / 1116 项通过（`unit-final.log`）；后续变更另经充值 17、GMPay 15、运维 27、图表 2 项定向回归通过，不将重复运行相加成新增测试数 |
| 类型、静态与格式 | 最终全量 typecheck、ESLint、Prettier 通过（`*-release.log`） |
| Vue 生产构建 | `build-release.log` 通过，保留现有图表包超过 500 kB 的提示 |
| 嵌入 HTTP | 最终产物的资源图和 models 页面/文件两组通过（`embed-http-release.log`）；`CGO_ENABLED=0 GOWORK=off`、Go 1.26.9、单包超时 60 秒；无 Go 修改 |
| 专项浏览器 | 历史恢复中英文 × 明暗 × 桌面/手机 8 场景、网关 6 场景通过（`history-scroll-final.log`）；修正后的中文手机明暗 2 场景通过（`mobile-audit-final.log`）；原生完整流程 4 场景通过（`native-core-final.log`） |
| 完整默认浏览器 | 默认清单 518 / 518 个测试身份均有实际通过记录；完整运行中断后分批补足，无跳过。`browser-coverage-final.json` 保存逐项日志归属、清单及日志哈希 |

源码审阅分为运维、路由/发票与其他恢复页面，报告分别为 `ops-visual-audit.md`、`audit/routing-source-audit.md`、`frontend-code-audit.md`。运维实际查看 32 张分区截图；其他页面检查了首屏与下半区截图，手机钱包日期轴修正后由集成负责人实际查看最终明暗两张截图确认关闭。该范围不等于所有截图均逐张人工审阅。

原失败及中断证据全部保留：完整浏览器最初运行中断，后按测试身份补足；失去开发服务器、缺少浏览器动态库分别属于环境失败。旧页面定位和钱包交互断言已按恢复后的实际 DOM 更新；原生 17 页流程的总预算从 90 秒改为 180 秒，单断言仍为 8 秒，业务与权限断言未删除。修正后独立运行结果单独登记，不将失败、枚举或中断计作通过。

本批仅 Vue 源码、前端测试和文档；后端、数据库、正式配置及运行容器均未修改。Git 源码同步不等于生产部署。真实供应商、支付、SMTP 与正式用户操作不属于本批验证；浏览器使用严格原生接口模拟。
