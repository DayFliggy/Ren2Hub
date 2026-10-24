# 前端审计中的上游共同问题

本台账按“造成缺陷的具体路径是否被本地改造”判断责任。共同问题等待 NewAPI 修复，不因 Vue 重写或文件其他位置存在差异而认定需要本地修复。以下最小复现默认来自静态代码对照；明确标注的隔离复现也不代表已在生产执行。登记、既有 Vue 防护和等待条件均不等于服务端问题已经修复。

初次对照为 Ren2Hub `79e237455262fcb652379e13965ce86fb1a4eefa`；本次从 `6e4539904d649f892cb7d2f61ea5fedc8c5198bb` 复核。唯一上游 NewAPI 核心仍冻结在 `206e0fa5c600127a314a97f88592c067f784f4a6`，另审阅固定快照 `33c61a24234aa20cb10e8a644304322c476361de` 相对基线的增量，未找到下列共同致缺陷路径的修复。历史参考为 Ren2Hub `9a63062227c31ba54a6346e07ee8b4fd8802f22f`；Sub2API `a34f373973bf547ccec301c7405eb45194e1f28a`、AxonHub `cbdf9a6d4a3dab1e4bfe5168ce5d80205c805913` 仅为参考，不用于绕开共同问题的等待决定。来源和本地实施见[本轮记录](LOCAL_REPAIRS_20261024.md)，两侧文件行号会随修复变化。

## 当前等待 NewAPI 的 10 组问题

| 审计编号 | 两侧致缺陷路径与本地判断 | 最小复现与影响 | 处置与重新评估条件 |
| --- | --- | --- | --- |
| channel-1 | 两侧 `controller/channel.go` 的 Vertex `other` 校验要求含 `default` 的 JSON，`relay/channel/vertex/relay-vertex.go` 将区域值断言为字符串；NewAPI React 同样提示普通区域名。 | 提交 `other=us-central1` 被拒绝；`{"default":123}` 等非字符串区域可通过保存校验，转发时触发断言并返回请求 500。 | 前次 Vue 空白校验保留；普通区域名提示与 JSON 合同矛盾、区域值类型和 relay 错误传播一起等待上游统一。本轮不另写本地兼容格式。 |
| channel-2 | 两侧 `model/channel.go` 的 `Update` 使用 GORM `Updates(channel)` 忽略空字符串，再读取旧模型同步 abilities。上游表单至少一个模型，本地编辑状态豁免属于本地问题。 | 直接更新 API 提交 `models: ""`，成功响应但旧模型仍在。 | 本地新增、编辑统一拦截零模型；直接 API 假成功仍待上游明确空值更新合同。 |
| settings-1 | 两侧 `model/option.go` 对整数用 `strconv.Atoi` 并忽略错误；`controller/option.go` 可接受小数文本。NewAPI 的 LinuxDO 输入、Stripe 数字 schema/step、Waffo schema 也没有完整整数合同，不能把这三项定成本地独有。 | 直接保存 `ModelRequestRateLimitCount=1.5`，数据库保留文本，运行值解析为 0，限流可能被关闭。SMTP 端口、导出间隔、LinuxDO 信任等级、Stripe/Waffo 最低充值额同类。 | 前次五项 Vue 安全整数防护保留，直接 API 和其余共同字段继续等待。上游同时明确类型、边界、加载/保存错误并保证持久化与运行值一致后吸收；本轮不扩入 Pancake 面板。 |
| settings-2 | 两侧 `setting/config/config.go` 的 `UpdateConfigFromMap` 嵌套值反序列化失败后继续，相关路径未改造。 | 保存 `gemini.version_settings={"default":123}`：持久化文本与运行值不一致，调用方仍可能提示成功。 | 仅记录。上游校验嵌套类型并传播失败、统一保存和运行状态后重评。 |
| settings-3 | 两侧 `model/option.go` 处理 `DisplayInCurrencyEnabled` 时重写运行时 `quota_display_type`（true→USD，false→TOKENS），未改造。Vue 额外无条件展示旧控件。 | 先后修改旧布尔与新枚举，运行显示随提交顺序变化，重载还受加载顺序影响。 | 本地恢复 NewAPI 的旧控件显示条件，仅在值为 false 时显示，继续只保存修改项；两套后端来源互相覆盖等待上游收敛。 |
| status-2 | 两侧 status 输出 `default_collapse_sidebar`，并提供 `DefaultCollapseSidebar` 设置，但前端均未消费；没有本地新增致缺陷路径。 | 修改默认折叠侧栏，首次进入页面仍不受该值影响。 | 仅记录，保留现有设置与用户本地折叠状态；上游明确站点默认和用户偏好优先级并接通后重评。 |
| catalog-1 | 两侧 `common/endpoint_type.go` 定义 OpenAIVideo，而 `common/endpoint_defaults.go` 缺对应默认方法与路径，相关映射未改造。 | 类型 55 Sora 模型详情找不到默认请求方法和路径。 | 仅记录，不自行添加视频路径；上游补齐映射和视频协议合同后重评。 |
| settings-11（后端部分） | 两侧 `service/channel_affinity.go` 将 `Keys` 错误变成空结果；全部清理路径还忽略扫描、`DeleteMany` 错误，按待删除条数返回；controller 仍给成功响应。 | 统计失败显示成功 0/0；全部清理可报告删除 1 条而键仍存在。后一项已在上一轮隔离 Go/Redis 模拟中复现，非生产测试。 | 前次 Vue 可观察失败/刷新失败状态保留。服务端查询假零和清理假成功等待上游一起传播扫描、删除错误，并报告实际结果；客户端无法识别伪成功。 |
| channel-6 | 两侧 `controller/channel-test.go` 对任务类渠道固定返回不支持；NewAPI React 行操作和详情测试同样没有统一能力屏蔽，Vue 也显示通用测试。 | MJ、Suno、Kling 等渠道点普通测试会固定失败，容易被误解为渠道故障。 | 新增等待记录，不把它当成本地独有缺陷；上游定义可测试能力和入口行为后吸收，避免本地再维护一套渠道类型字典。 |
| E 类：`DrawingEnabled`、`DataExportDefaultTime` | NewAPI React 与 Vue 都展示控件；后端定义、保存和 status 输出没有接到实际业务控制。 | 关闭绘图开关不阻止绘图 API；修改导出默认时间不改变业务默认。上游 dashboard 同名 localStorage 键也只是用户偏好，不是 status 消费者。 | 合并为共同死配置问题，继续等待上游接入真实行为或明确退役；本轮不删除这两个控件，也不从历史版本恢复同样无效的消费者。 |

## 无业务消费者的配置（E 类）

以下保留前次处置历史，不增加当前等待组数。两侧 `common/constants.go`、`setting/sensitive.go`、`model/option.go` 等仅定义、保存或读取这些选项；`controller/misc.go` 的相关 status 输出不证明选项参与业务控制。

| 选项 | Vue 与 NewAPI 对照 | 最小复现、影响与本轮处置 |
| --- | --- | --- |
| `FileUploadPermission`、`FileDownloadPermission`、`ImageUploadPermission`、`ImageDownloadPermission` | Vue 曾新增四个权限控件；NewAPI React 无对应控件。后端选项继承。 | 前次已删除四个无效 Vue 控件和孤立分区/helper；保留 Go 变量、默认值、解析和数据库行。真实角色鉴权不改，不重复列作本轮待修。 |
| `StopOnSensitiveEnabled`、`StreamCacheQueueLength` | Vue 曾新增控件；上游无对应控件。后端映射继承。 | 前次已删除两个无效控件和独占文案；保留真实敏感词策略与全部后端兼容内容，不重复实施。 |
| `TaskEnabled` | Vue 曾新增通用任务开关；上游 React 无此控件。继承的 status 输出没有消费者。 | 前次已删除无效 Vue 控件；真正的 `TaskPluginEnabled`、任务插件配置、后端选项及 status 字段保留。 |
| `DrawingEnabled`、`DataExportDefaultTime` | 上游也展示，双方均无业务消费者。 | 仍保留 Vue 和 Go，当前处置见上表 E 类共同问题。真实导出周期与导出总开关保留。 |

九项的后端删除、数据库清理与 API 移除不在本轮。上游实现消费者、明确退役或另行批准本地偏离时再评估；不得按名称批量删除角色常量、敏感词策略、任务插件或导出设置。

前次七项无效控件删除及验收见[前端实施记录](FRONTEND_FIXES_20261024.md)；本轮日志、观测、Dashboard 与原生计数见[本地修复记录](LOCAL_REPAIRS_20261024.md)。原生计数已独立立项实施，图片变体仅候选，其他协议缺口等待 NewAPI，不混入上述 10 组共同缺陷。上游出现针对性修复后按同步技能评估吸收，不自动追平两个参考项目。
