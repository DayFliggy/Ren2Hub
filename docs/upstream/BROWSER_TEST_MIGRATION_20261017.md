# 浏览器测试合同迁移记录（2026-10-17）

本表记录旧 55 份浏览器 spec 的行为去向。退役表示相关产品或接口已删除，不计作测试通过；合并必须保留对应行为。定向与完整运行结果由验收证据记录，不能以本表替代实际执行。下列路径以 `frontend/tests/visual/` 为基准。

## 设置、认证、渠道、日志与运维批次

| 原文件 | 保留、迁移与退役边界 | 当前去向 |
| --- | --- | --- |
| account-security-integration.spec.ts | 保留旧单令牌入口不可达、历史审计记录可读与不请求旧生成接口 | 原文件；原生 audit/self |
| admin-bindings.spec.ts | 保留用户绑定脱敏、确认、二次验证、解绑和编辑页同步；删除旧 gateway 用户运行态 mock | 原文件；原生 user/:id 与 admin.user.binding.clear |
| audit-logs.spec.ts | 保留管理员/本人范围、详情、敏感字段保护、历史书签、加载/空/失败重试 | 原文件；原生 audit 与 audit/self |
| pat-p2.spec.ts | 保留创建/改名/改权限/撤销、每次敏感操作 proof、过期禁止操作、秘密一次展示与退出后晚到响应隔离；退役 revoked_at 留存行语义 | 原文件；access_tokens/catalog、nap_ 格式、原生 token_id proof context；撤销后从列表删除 |
| token-wallet-query.spec.ts | 保留新建默认开、手工关闭、旧 Token 编辑回显与开关保存；退役 operation_id | 原文件；原生 token/ DTO |
| users-group-filter.spec.ts | 保留分组筛选/恢复全部及桌面平板手机布局；退役旧计数下拉、邀请隐藏列断言 | 原文件；user/search group 参数和原生整数 quota |
| channel-advanced.spec.ts | 保留原始余额响应纯文本、模型变更预览确认、模型批量选择、WS/透传/参数/头覆盖、非法 JSON 草稿、多密钥创建；删除渠道倍率和旧字段显示入口断言 | 原文件；channel/、update_balance、fetch_models、upstream_updates |
| channel-dialogs.spec.ts | 保留单渠道模型测试分页、忙状态、焦点、对话框约束；退役 Anthropic Lab 分组测试及 lab-audit | 原文件；channel/test/:id 顶层 time，响应时间为整数毫秒 |
| channel-provider-status.spec.ts | 保留 vLLM/SGLang 版本、部分端点失败及模型上下文显示；退役 gateway runtime/credentials | 原文件；channel/:id/{vllm,sglang}/status |
| channel-providers.spec.ts | 保留 AWS Mantle 与 Vertex 项目/地域编辑；退役 setting 中 provider 字段及 gateway mock | 原文件；原生 settings 与 other |
| retired-codex-channel.spec.ts | 退役旧库 type57 行只读/删除兼容；保留无订阅渠道选项、普通 OpenAI 渠道可用 Codex 模型且不请求订阅 API | 原文件重写为负向回归 |
| input-composition.spec.ts | 保留模型输入/分组选择、命令菜单、供应商抽屉、工单回复的 IME 与键盘行为；退役已删除的活动营销倒计时（现行签到由其他原生用例覆盖） | 原文件；原生 fetch_models 和真实工单 detail |
| log-content-parts.spec.ts | 保留服务器消息片段翻译与历史文本回退；修正原生 quota/use_time 类型和单位 | 原文件；log/self |
| log-response-model.spec.ts | 保留日志分组筛选、列表模型不一致标记与详情原始 JSON 证据；退役旧详情徽标，修正原生 quota/use_time | 原文件；log/self/groups 与 log/self |
| log-export.spec.ts | 保留筛选透传、日期键盘/小窗口裁切、CSV 注入防护、流式末尾错误与下载；退役旧十进制字符串超安全整数输入，按原生安全整数验证 | 原文件；log/self/export 原生 NDJSON |
| task-log-artifacts.spec.ts | 保留未保留结果、按需制品读取、图像/下载、制品缺失；quota 改原生整数 | 原文件；task/self、task/:id/artifacts |
| task-plugins.spec.ts | 保留插件详情、版本、变更说明、源码预览及市场读取 | 原文件；plugin/task 系列 |
| upstream-addresses-disable.spec.ts | 保留 API 地址复制/回退、插件禁用确认/级联/在途强制与错误恢复 | 原文件；status、plugin/task 系列 |
| ops-monitor.spec.ts | 退役旧 ops 概览、上游熔断、节点/资源/窗口摘要与 system-task/summary；保留现行运行态、活动/历史任务、过滤、详情、部分失败与旧数据可见、空态、非 Root 拒绝 | 原文件重写；performance/stats 与 system-task/list；current/detail API 的既有单元合同不伪装为页面请求 |
| native-audit-fixes.spec.ts | 保留原生创建用户及支付设置回归 | 原文件 |
| native-channels.spec.ts | 保留原生渠道组、provider、当前权限变化与倍率/容量不存在 | 原文件 |
| native-core.spec.ts | 保留原生用户/管理员页面、私有路由预览与切自动、GMPay 创建；日志从空态改为真实 fixture 消费行 | 原文件 |
| native-pricing.spec.ts | 保留原生管理价格保存/预览 | 原文件 |
| native-public.spec.ts | 保留公开价目、归属、充值单位与 Stripe TOKENS 禁用 | 原文件 |
| native-live.spec.ts | 保留原镜像真实会话两组合×19路由与公开归属；与 mock 默认集分离，不计为默认跳过通过 | 主线迁至独立 live 入口 |

新增 `auth-settings-save.spec.ts` 验证 A4 的凭据先写、已确认秘密不重发、部分成功提示和 OIDC discovery 后启用的桌面/手机、明暗组合。组件测试 `AuthSettingsSave.spec.ts` 覆盖完整八组、显式关闭优先、网络确认不明、发现失败零写入与手填端点/其他 provider 隔离。

## 布局、账户、公开页面与仪表盘批次

| 原文件 | 保留、迁移与退役边界 | 当前去向 |
| --- | --- | --- |
| account-language.spec.ts | 保留账号语言继承、设备选择及保存；用原生 user/self | 原文件，共享 nativeUser |
| auth-widescreen.spec.ts | 保留中英文、明暗、宽屏及短屏的登录/注册/密码恢复可达性 | 原文件 |
| console-lists.spec.ts | 保留列表断点、24px 触控、复选框可点区域和日志详情；退役预留额度按钮，以原生编辑操作验证触控 | 原文件 |
| console-shell.spec.ts | 保留普通用户/root、中英文菜单单行、滚动提示、字体变更及真实导航链接 | 原文件 |
| console-widescreen.spec.ts | 保留宽屏增长上限、折叠侧栏、对话框缩放与退出控制台后的尺寸恢复 | 原文件 |
| greeting-hero.spec.ts | 保留问候时段、账号显示名、长名字换行与句号不悬挂；身份来自 nativeUser | 原文件 |
| hero-accent-regression.spec.ts | 保留钱包标题装饰的主题回归 | 原文件 |
| http-transport.spec.ts | 保留公开请求不带认证 Cookie、取消不重试、中英文请求头 | 原文件；专用 transport 探针不是旧业务兼容接口 |
| model-distribution.spec.ts | 保留图表/表格比例、主题、手机内部滚动及页面不溢出；改原生 data/self | 原文件 |
| navigation-fonts.spec.ts | 保留字体延迟、键盘选中、当前页揭示及用户手动滚动位置 | 原文件 |
| navigation-retry.spec.ts | 保留429可恢复、懒加载失败重试、query/hash目的地 | 原文件 |
| responsive-breakpoints.spec.ts | 保留16/20px浏览器默认字体、导航断点、抽屉与侧栏切换 | 原文件 |
| retired-plans.spec.ts | 保留订阅/套餐页面404、不请求订阅接口、钱包仍可访问 | 原文件；删除项负向检查 |
| settings-account-center.spec.ts | 保留安全入口、菜单/Escape、偏好与响应式；加入原生 quota/used_quota/request_count；退役聚合入会日期/天数、身份卡与旧标题布局 | 原文件；显示真实账号名称及原生三项数据 |
| setup.spec.ts | 保留初始化四步、秘密显隐、已有root、检查失败重试、重复初始化跳转 | 原文件；原生 setup 合同 |
| style-compatibility.spec.ts | 保留容器边距、文本行盒、菜单动画、按钮按压和导航字体；退役签到印章、旧待上线徽标，改原生签到表和 Token 状态徽标 | 原文件 |
| dashboard.spec.ts | 保留热图周期、键盘/触控、月份/季度布局、对比度、趋势与空账户；数据改由原生 data/self 聚合；退役独立分布接口、旧RPM/折扣卡、五格KPI及自动路由实验室 | 原文件；三格原生KPI和已删除投影负断言 |
| home-showcase.spec.ts | 保留Hero、运行时钟、字体、动画、导航点、可用率有/无数据、320px至4K布局；退役请求数聚合/趋势与超长计数断言 | 原文件；明确断言不请求 home/metrics、不伪造请求总量，运行时间与uptime来自原生接口 |
| representative.spec.ts | 保留各页面/弹窗明暗与桌面手机组合、日志用量/详情、宽屏尺寸；退役自动路由实验室、邀请码配置弹窗和独立计费证据 | 原文件；替换为 Token 私有路由、原生新建用户和数值用量详情 |
| routes.spec.ts | 保留页面导航、懒加载、429、无初始化加载屏、退役路径负断言；每条路径用独立原生会话，guest页面不再由假认证自动重定向掩盖 | 原文件；已退役的元数据预填分组弹窗入口从 routes.ts 列表移除 |

共享 `native-core-fixture.ts` 提供原生身份及 API 数据；`fixtures.ts` 只保留固定时钟和几何辅助。未知 API 在测试结束时失败，单独创建的 route-smoke 页面逐页核对未知请求。默认入口执行完整 mock/视觉目录；live 使用独立目录和配置，缺少隔离目标、完整源码 SHA 或私有凭据即失败，复用候选网络/数据库检查并拒绝开发资源，关闭 trace/video。

## 支付、工单、模型与定价批次

| 原文件 | 保留、迁移与退役边界 | 当前去向 |
| --- | --- | --- |
| epay-settings.spec.ts | 保留配置加载失败重试、商户前导零、秘密保存失败保留及成功清除；退役 EPay/GMPay 独立结算开关，真实 GMPay enabled 仅开放新订单 | 原文件；option/ 与 gmpay/config，旧选项存在也不显示死开关 |
| epay-wallet.spec.ts | 保留报价失败恢复、付款方式、CNY 金额与真实表单跳转、订单状态；退役字符串冻结报价和个人补单入口 | 原文件；amount/pay 数值请求、原生 topup/self；本地表单目标无真实支付 |
| gmpay-orders.spec.ts | 保留管理员人工补单确认、失败保留弹窗、重试成功与禁止重复补单；退役旧 GMPay 专属管理列表与冻结账本 DTO | 原文件；统一 topup 列表与 trade_no 补单 |
| gmpay-wallet.spec.ts | 保留加载失败恢复、资产选择、金额、跳转及并发点击仅一次下单；退役旧 quote/fingerprint | 原文件；原生充值信息与数值 GMPay 请求，等待期间禁用数量和资产切换 |
| tickets.spec.ts | 保留个人/管理员列表与详情、真实 multipart 附件、鉴权图片、回复失败草稿及重试、关闭/重开、管理员指派；退役 next 路由和旧工单 DTO | 原文件；tickets 与 admin/tickets，桌面/手机及明暗组合 |
| audit-fixes.spec.ts | 保留渠道停用原因与操作/编辑权限、源价格差异、市场资产重验证及原生上传；退役 gateway runtime 与不存在的插件 preview API | 原文件；channel status_reason/status_time、数值 ModelPrice、静态源码元信息预览与 plugin/task 上传 |
| catalog-p2.spec.ts | 退役客户端配置下载与不存在的模型 capabilities 编辑入口；保留现行元数据字段更新、保存失败草稿及重试 | 原文件；原生 model PUT 与已删除入口负向断言 |
| model-management.spec.ts | 保留双语/明暗/响应布局、供应商与模型草稿、忙状态、同步预览取消与手工配置；旧 missing/conflicts 合同改原生 candidates/selections | 原文件；同步显式选择创建/更新，携带 source_version 与 record_version，核对原生结果数组 |
| model-pricing.spec.ts | 保留表达式档位、外部价格草稿、手动保存、版本冲突和离开确认；退役已删除任务模拟器及 usage_reference | 原文件；原生 model_pricing 数值价格、表达式字符串与 expected_version |
| pricing-partial.spec.ts | 保留部分可定价模型、显式零价格、表达式和不可计价模型的公开列表行为；原文件已采用原生合同，无需改写 | 原文件；pricing 数值倍率；本轮定向运行确认 |

元数据同步和插件上传在迁移中暴露真实 Vue 合同缺口，已分别按冻结 NewAPI 的同步选择流程及静态源码解析方式修复。静态预览无法确定的动态元信息只显示提示，实际安装由原生上传接口校验；市场文件仍先核验 SHA256。浏览器中的支付与上传使用本地 mock，不代表真实供应商付款或正式部署验证。

## 本批完整运行结果

`a9-default-full.log` 与 `a9-default-results.json` 记录完整默认 `bun run test:visual --workers=2 --reporter=list,json`：55 文件、416 场景全部通过，耗时 22.1 分钟，unexpected/flaky/skipped 均为 0。旧 55 文件逐一映射验证无遗漏；旧 live 移出、新增 A4 spec 后默认仍为 55 文件。退役断言不计入通过，早期失败记录保留。live 入口缺目标会失败，新的不可变候选 live 尚待执行。
