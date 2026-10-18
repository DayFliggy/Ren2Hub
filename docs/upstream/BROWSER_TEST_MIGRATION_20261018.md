# 浏览器合同迁移记录（2026-10-18）

本轮以工作树初始 61 份 spec 为映射基线，旧原文和 SHA 保存在隔离证据 `browser-baseline-mapping.json`。本文件替代 10 月 17 日记录对本轮现状的描述，不更改历史验收结论。新增原生合同测试一并进入默认完整目录；不使用仅匹配 native、批量 skip 或通用未知接口响应。

`browser-migration-map.json` 记录每份原/现断言行号、内容 SHA256、去向和产品边界；原文保留用于逐断言复核。此映射不是运行证据，退役断言不计通过。

| 原文件（frontend/tests/visual） | 处置及现有替代 |
| --- | --- |
| account-language.spec.ts | 保留账号语言继承、设备语言切换与保存；身份改原生 user/self。 |
| account-security-integration.spec.ts | 保留旧单令牌入口不可达、历史审计可读与密钥不泄露；原生 access_tokens。 |
| admin-bindings.spec.ts | 保留绑定脱敏、解绑确认与敏感操作证明；原生 user/:id 与解绑动作。 |
| audit-fixes.spec.ts | 保留渠道停用原因、管理权限、源价差异和插件市场哈希；恢复普通渠道运行快照，插件动态元信息用真实 preview 接口。 |
| audit-logs.spec.ts | 保留本人/管理员审计、敏感字段保护、书签、失败恢复。 |
| auth-widescreen.spec.ts | 保留中英文、明暗、宽屏短屏登录注册和密码恢复布局。 |
| billing-exact.spec.ts | 旧字符串冻结钱包退役；用 NewAPI 数字额度验证 Key 预算单位、保存失败保留草稿与重试，不伪造超 JS 安全整数的 JSON 精度。 |
| billing.spec.ts | 旧资金事件和预留流水退役；保留钱包充值与失败恢复，明确旧入口不可见。 |
| catalog-p2.spec.ts | 恢复 Codex/Pi 客户端配置预览、复制与下载；只引用环境变量，Pi 显式能力。原生元数据保存及失败草稿保留；旧客户端生成 API 不恢复。 |
| channel-advanced.spec.ts | 保留模型差异预览、JSON 草稿、模型选择、WS/头透传、余额原文与多密钥；恢复 channel_ratio/max_concurrency，删除组倍率。 |
| channel-dialogs.spec.ts | 保留模型测试分页、忙状态、焦点与尺寸；旧 lab-audit/业务分组批测退役。 |
| channel-provider-status.spec.ts | 保留 vLLM/SGLang 状态、部分错误、模型上下文；公共渠道运行快照采用原生渠道字段，旧 gateway 账号凭据池退役。 |
| channel-providers.spec.ts | 保留 AWS Mantle/Vertex 配置，原生 settings/other；业务分组退役。 |
| console-lists.spec.ts | 保留列表断点、触控目标、复选框和详情；预留余额入口退役。 |
| console-shell.spec.ts | 保留普通用户/root、中英文菜单单行、滚动提示与权限导航。 |
| console-widescreen.spec.ts | 保留宽屏上限、侧栏折叠、弹窗缩放和离开控制台后恢复。 |
| dashboard.spec.ts | 保留热图所有周期、键盘触控、月份季度几何与空账户；数据统一 data/self/summary。恢复五格 KPI 和路由入口，未知 RPM 保持 --；旧聚合接口退役。 |
| epay-settings.spec.ts | 保留选项失败恢复、前导零和秘密草稿；退役无效结算开关。 |
| epay-wallet.spec.ts | 保留失败恢复、CNY 金额、真实表单导航与充值记录；退役 quote/fingerprint，使用原生 amount/pay。 |
| gateway-live.spec.ts | 退役旧 schema5、admission 队列和 gateway 聚合验收；实际候选合同移至独立 live 配置，路由页面由 gateway.spec 与 native-core.spec 覆盖。 |
| gateway-preview-live.spec.ts | 退役旧 preview schema5 验收，替换为独立 live/preflight 与 native-live；不计默认跳过或通过。 |
| gateway.spec.ts | 旧准入池/节点监控退役；验证自动/手动模式、只读预览、按保存顺序、渠道用户并发和无旧 API 调用。 |
| gmpay-orders.spec.ts | 迁移统一 TopUp 列表、币种和详情，保留人工补单确认/失败重试/成功不可重复；旧资金账本 DTO 退役。 |
| gmpay-wallet.spec.ts | 保留加载失败恢复、资产选择、准确法币金额与单次创建；原生 amount/pay，旧报价指纹退役。 |
| greeting-hero.spec.ts | 保留时段问候、显示名与长名称排版；原生身份。 |
| hero-accent-regression.spec.ts | 保留钱包标题装饰的两主题回归。 |
| home-showcase.spec.ts | 保留首页字体、地图动画、超宽/320px、时钟和可用率；恢复真实 home/metrics 请求总量与趋势，Demo 延迟仍标明演示。 |
| http-transport.spec.ts | 保留公开请求无认证 Cookie、取消不重试、中英文请求头；探针与业务 fixture 分离。 |
| input-composition.spec.ts | 保留模型选择、命令菜单、元数据抽屉、工单 IME；旧活动营销倒计时退役。 |
| log-content-parts.spec.ts | 保留服务端消息片段翻译及历史文本回退；原生 quota/use_time。 |
| log-export.spec.ts | 保留日期范围键盘可达、短屏裁切、CSV 注入保护、末尾错误和下载；group 过滤退役并验证请求无 group。 |
| log-response-model.spec.ts | 保留模型不一致标记、原始证据与窄屏；group 控件/API 退役，验证真实 log/self 请求无 group。 |
| model-distribution.spec.ts | 保留图表表格比例、明暗主题及内部横向滚动；原生 summary。 |
| model-management.spec.ts | 保留模型/供应商/预填集合/同步预览与草稿；业务组展示删除，预填模型集合不是业务组并保留。 |
| model-market-repairs.spec.ts | 历史市场投影退役；真实 model-catalog 驱动目录、渠道货架和倍率展示；保留筛选/排序/卡片/详情与布局断言。 |
| model-pricing.spec.ts | 保留原生价格、表达式、预览、版本冲突、离开确认；旧独立售价源和任务参考价退役。 |
| models.spec.ts | 复用 model-catalog 真实 DTO；保留首页卡片、筛选、比较、渠道信息及明暗/桌面手机。 |
| navigation-fonts.spec.ts | 保留字体延迟、键盘选择、当前页面揭示和手动滚动位置。 |
| navigation-retry.spec.ts | 保留 429/懒加载失败恢复与 query/hash。 |
| ops-monitor.spec.ts | 旧节点/熔断聚合退役；保留原生内存、任务列表详情、部分失败恢复、Root 门槛，新增渠道用户并发来自 performance/routing。 |
| pat-p2.spec.ts | 原生 access_tokens/catalog，保留签发/修改/撤销的敏感操作证明、一次秘密、过期与晚到响应隔离；不恢复旧 PAT 增强。 |
| pricing-partial.spec.ts | 保留部分价格、零价、表达式和不可定价模型展示；原生价格合同。 |
| pricing-reference.spec.ts | 旧 usage_reference/任务首例报价合同退役；原生源价与单次渠道倍率投影、缺价不可伪造。 |
| representative.spec.ts | 保留页面/弹窗组合、响应布局、日志用量与详情；原生 Token 路由/用户创建替代旧聚合弹窗。 |
| request-diagnostics.spec.ts | 旧 admission timeline/gateway 诊断退役；保留真实日志详情、模型和 Key 证据、Escape、溢出检查及禁止旧 API。 |
| responsive-breakpoints.spec.ts | 保留默认16/20px字体、导航断点、侧栏和抽屉转换。 |
| retired-codex-channel.spec.ts | 保留官方订阅渠道不可用及普通 API Key 模型可用；不恢复订阅池。 |
| retired-plans.spec.ts | 保留套餐/订阅页面404、禁止订阅 API 和正常钱包可达。 |
| routes.spec.ts | 保留导航、懒加载、限流恢复、未初始化屏及退役路径；活动占位卡明确禁用，账户身份卡恢复可见；每页独立原生会话并检查未知 API。 |
| settings-account-center.spec.ts | 恢复账户身份与真实创建日期；保留安全入口、菜单、偏好、三项原生额度统计及响应布局。 |
| setup.spec.ts | 保留初始化步骤、秘密显隐、已有root、错误重试与重复跳转。 |
| style-compatibility.spec.ts | 保留间距、文本行盒、菜单动画、按钮按压、字体和原生状态徽标；签到记录通过真实展开查看，当前月服务器记录同时验证日历标记。 |
| system-status.spec.ts | 无宿主 CPU/磁盘接口则明确未知；保留四块等高、长 SHA、明暗双语和窄屏；旧伪造主机数值与检查器退役。 |
| task-log-artifacts.spec.ts | 保留按需产物、图像下载、未保留与失效状态；原生 task/artifacts。 |
| task-plugins.spec.ts | 保留插件元数据、版本历史、源码与市场；原生 plugin/task。 |
| tickets.spec.ts | 保留会话归属、管理权限、附件multipart/MIME、回复草稿和状态流转；原生 tickets/admin/tickets，next 退役。 |
| token-trend.spec.ts | 保留趋势过滤、图表主题和几何；统一 summary，缓存覆盖与协议未知明确，不从不兼容口径推算命中率。 |
| token-wallet-query.spec.ts | 保留新建默认开启、旧Key回显及显式关闭；原生 Token bool，旧operation_id退役。 |
| unified-routing.spec.ts | 旧多组、权重策略与旧市场入口退役；原生 model-catalog 与自动/手动有序渠道，保留模型筛选和路由限制可见性。 |
| upstream-addresses-disable.spec.ts | 保留地址复制、插件禁用确认/级联/在途强制及失败恢复；原生插件接口。 |
| users-group-filter.spec.ts | 业务分组过滤退役；保留关键词搜索和桌面平板手机，新增用户 max_concurrency 编辑与请求不带 group。 |

新增 `affiliate-native.spec.ts` 验证低于旧最低额仍全额转入、失败后相同 operation_id 重试、成功后禁用，以及只含脱敏名称、绑定时间和累计返佣的邀请名单真实分页、切换排序复位。共享 fixture 使用真实原生数字额度、channel_ratio 字符串、渠道并发与 lab、model-catalog、summary、affiliate、TopUp。未知 API 必须导致测试失败。

默认 `bun run test:visual` 运行完整 `tests/visual`。真实候选使用 `bun run test:visual:live` 与独立配置；缺少候选身份/数据库目标/私有凭据直接失败，不静默跳过。禁止 live API mock、开发服务器、正式地址和 trace/video。测试目标文件继续复用候选隔离核验器，独立 worktree 可通过 `NATIVE_LIVE_ACCEPTANCE_DIR` 明确指定工具目录。

默认完整套件已实际通过：66 份 spec、506 / 506 项，0 失败、0 跳过、0 flaky，耗时 24.0 分钟。执行日志为隔离证据目录中的 `browser-full-final.log`，原始 JSON 为 `browser-full-final.json`，逐文件统计为 `browser-full-final-result.json`。执行前后选定的 844 份源码、资源、测试及入口配置没有变化，`frontend-source-fingerprint-before.json` 与 `frontend-source-fingerprint-after.json` 的清单哈希均为 `9f93ec94cdb176ddb4aff93c2b0ee04e52429b8386c64496877d7e2e19e16509`。本结论不包括真实候选，也不将退役断言、枚举或旧提交截图计为通过。


本轮完整默认套件为 66 份当前 spec、506 个用例（61 份原始映射中 2 份旧候选脚本移出，另纳入 7 份原生合同文件）。首轮执行时的 504 用例不含后来补充的 2 个邀请名单分页/排序用例。最终完整执行必须包含这 2 例，不能以多份定向日志相加代替一次全目录结果。

首轮实跑证据 `browser-full.log` 保留 504 项中的 417 项通过、87 项失败，耗时 37.8 分钟。`browser-full-initial-result.json` 保留逐项失败名称及 15 份文件的分布；不将该轮记为通过。后续修复分为两类：

- 合同及定位迁移：原生敏感证明 scope、桌面/手机双 DOM 的可见定位、折叠签到记录展开、恢复账户中心统计和活动占位卡、原生充值记录卡与订单币种展示、手动路由预览状态及渠道余额原文标题。保留权限、失败重试、金额、布局、未知 API 及退役路径断言。首页时序数据改为总量仍为 24 的实际起伏样例，避免零高度 SVG 被错误当成页面缺失。
- 实际界面问题：短屏日历焦点被固定操作栏裁切、KPI 文字对比不足、画布未继承自托管中文字体、订单过期状态翻译缺失，以及系统状态卡亮色下的边界溢出。修复源码并重跑原断言，不扩大容差。

真实候选入口含 2 个主题/视口用例，各访问 21 个保留页面，共 42 次页面核验，另有 1 个隔离环境可逆设置写入用例；42 是页面核验次数，不是 42 个独立用例。尚未得到候选身份前不记为候选通过。


完整套件后确认 AWS 渠道局部消息缺少 `awsKeyType` 与 `awsAccessKey`，影响表单标签及默认 AK/SK 选项。该文案增量在上述源码指纹保存后实施，需以独立消息覆盖单测、原 provider 四组合及静态构建日志记录；不得将修后树说成上述 506 项执行时的同一源码树。
