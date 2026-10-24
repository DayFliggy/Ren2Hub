# 前端审计中的上游共同问题

本台账按“造成缺陷的具体路径是否被本地改造”判断责任。共同问题等待 NewAPI 修复，不因 Vue 重写或文件其他位置存在差异而认定需要本地修复。以下复现来自静态代码对照，不代表已在生产执行。

固定对照：Ren2Hub `79e237455262fcb652379e13965ce86fb1a4eefa`；NewAPI `206e0fa5c600127a314a97f88592c067f784f4a6`。本轮参考 Sub2API `a34f373973bf547ccec301c7405eb45194e1f28a`、AxonHub `cbdf9a6d4a3dab1e4bfe5168ce5d80205c805913`，均不作为上游，也不用于绕开共同缺陷。表中两侧路径均针对上述固定提交；行号会随本地修复变化。

| 审计编号 | 两侧致缺陷路径与本地判断 | 最小复现与影响 | 处置与重新评估条件 |
| --- | --- | --- | --- |
| channel-1 | 两侧 `controller/channel.go` 的 Vertex `other` 校验要求含 `default` 的 JSON，relay 使用同一 JSON 合同和字符串断言；NewAPI React 区域输入仍提示普通区域名。本地额外增加“留空使用 global”并放宽必填，属于可拆分的本地问题。 | 提交 `other=us-central1` 被拒绝；嵌套值为非字符串时可能在运行解析处触发断言错误。输入提示与接受合同不一致。 | 本地只恢复空白校验和正确提示，合法 JSON 原样提交。普通字符串与 JSON 矛盾、运行时断言等待上游；上游统一表单、校验与 relay 类型后重评。 |
| channel-2 | 两侧 `model/channel.go` 的 `Update` 使用 GORM `Updates(channel)` 忽略空字符串，再读取旧模型同步 abilities。上游表单至少一个模型，本地编辑状态豁免属于本地问题。 | 直接更新 API 提交 `models: ""`，成功响应但旧模型仍在。 | 本地新增、编辑统一拦截零模型；直接 API 假成功仍待上游明确空值更新合同。 |
| settings-1 | 两侧 `model/option.go` 对整数用 `strconv.Atoi` 并忽略错误；`controller/option.go` 可接受小数文本。Vue 本地五项控件缺少整数约束，上游对应表单已有边界。 | 直接保存 `ModelRequestRateLimitCount=1.5`，数据库保留文本，运行值解析为 0，限流可能被关闭。SMTP 端口、导出间隔等同类。LinuxDO 信任等级、Stripe/Waffo 最低充值额也存在共同解析边界缺口。 | 本地五项表单恢复上游边界并拒绝非有限/非安全整数。后端解析、加载、保存顺序及其余共同字段不改；上游返回解析错误并明确边界后吸收。Pancake 面板不自动扩入本轮。 |
| settings-2 | 两侧 `setting/config/config.go` 的 `UpdateConfigFromMap` 嵌套值反序列化失败后继续，相关路径未改造。 | 保存 `gemini.version_settings={"default":123}`：持久化文本与运行值不一致，调用方仍可能提示成功。 | 仅记录。上游校验嵌套类型并传播失败、统一保存和运行状态后重评。 |
| settings-3 | 两侧 `model/option.go` 处理 `DisplayInCurrencyEnabled` 时重写运行时 `quota_display_type`（true→USD，false→TOKENS），未改造。Vue 额外无条件展示旧控件。 | 先后修改旧布尔与新枚举，运行显示随提交顺序变化，重载还受加载顺序影响。 | 本地恢复 NewAPI 的旧控件显示条件，仅在值为 false 时显示，继续只保存修改项；两套后端来源互相覆盖等待上游收敛。 |
| status-2 | 两侧 status 输出 `default_collapse_sidebar`，并提供 `DefaultCollapseSidebar` 设置，但前端均未消费；没有本地新增致缺陷路径。 | 修改默认折叠侧栏，首次进入页面仍不受该值影响。 | 仅记录，保留现有设置与用户本地折叠状态；上游明确站点默认和用户偏好优先级并接通后重评。 |
| catalog-1 | 两侧 `common/endpoint_type.go` 定义 OpenAIVideo，而 `common/endpoint_defaults.go` 缺对应默认方法与路径，相关映射未改造。 | 类型 55 Sora 模型详情找不到默认请求方法和路径。 | 仅记录，不自行添加视频路径；上游补齐映射和视频协议合同后重评。 |
| settings-11（后端部分） | 两侧亲和缓存统计读取路径可能将缓存 `Keys` 错误转成空结果，调用方得到成功零统计；该错误处理未改造。Vue 将明确失败也显示零属于本地问题。 | 缓存读取失败时，后端可能响应成功的 0/0。 | 本地修复可观察的请求失败与刷新失败状态；服务端伪成功无法由客户端识别，待上游传播缓存读取错误后重评。 |

## 无业务消费者的配置（E 类）

固定提交同上。两侧 `common/constants.go`、`setting/sensitive.go`、`model/option.go` 等仅定义、保存或读取以下选项，未找到实际业务消费者；`controller/misc.go` 的相关 status 输出也不证明选项参与业务控制。

| 选项 | Vue 与 NewAPI 对照 | 最小复现、影响与本轮处置 |
| --- | --- | --- |
| `FileUploadPermission`、`FileDownloadPermission`、`ImageUploadPermission`、`ImageDownloadPermission` | Vue 新增四个权限控件；NewAPI React 无对应控件。后端选项继承。 | 修改值不会改变文件/图片权限。删除四个无效 Vue 控件和孤立分区/helper；保留 Go 变量、默认值、解析和数据库行。真实角色鉴权不改。 |
| `StopOnSensitiveEnabled`、`StreamCacheQueueLength` | Vue 新增控件；上游无对应控件。后端映射继承。 | 修改值不影响敏感词拦截或流处理。删除两个无效控件和独占文案；保留真实敏感词策略与全部后端兼容内容。 |
| `TaskEnabled` | Vue 新增通用任务开关；上游 React 无此控件。继承的 status 输出没有消费者。 | 修改开关不阻止任务 API。删除无效 Vue 控件，保留真正的 `TaskPluginEnabled` 和任务插件配置、后端选项及 status 字段。 |
| `DrawingEnabled`、`DataExportDefaultTime` | 上游也展示，双方均无业务消费者。 | 修改不会禁用绘图或改变默认导出时间。保留 Vue 和 Go，仅记录；真实导出周期与导出总开关保留。 |

九项的后端删除、数据库清理与 API 移除不在本轮。上游实现消费者、明确退役或另行批准本地偏离时再评估；不得按名称批量删除角色常量、敏感词策略、任务插件或导出设置。

本轮本地修复和验收见[实施记录](FRONTEND_FIXES_20261024.md)。上游出现修复提交后按同步技能评估吸收，不自动追平两个参考项目。
