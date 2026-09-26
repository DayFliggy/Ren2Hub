# 自动定价验证（2026-09-26）

本文件保留此前四源实现的历史证据。当前双源实现及本轮验证见[双源模型定价重构验证](DUAL_SOURCE_PRICING_VALIDATION.md)。

本记录对应基于 `a04750a3b7491a487fa28060a316ef6c8c06c8cb` 的自动定价重构及后续规则完善。优先级为整模型人工价、Sub2API、AxonHub、models.dev、OpenRouter；默认每 30 分钟检查。规则见[定价说明](PRICING.md)，内嵌目录的下载日期与原始哈希见[快照说明](../../service/pricingdata/README.md)。本轮没有部署应用、恢复生产数据库或调用付费模型。

## 后端与数据库

| 检查 | 结果与边界 |
| --- | --- |
| 受影响 Go 包 | `model`、`service`、`relay`、`relay/helper`、`relay/common`、`router`、`pkg/autopricing`、`pkg/billing`、`setting/reasoning`、`setting/operation_setting` 全包通过；后续边界修改再跑相关定向。所有后端测试使用 60 秒超时，未宣称全仓测试 |
| Controller | 自动开关与周期、关闭计划后手动任务去重、恢复自动成功／400／409、价格预览与发布、退休设置拒绝及模型列表定向通过 |
| 来源和发布 | 四源整模型优先级、非法主源转备用、全部不可用停用、网络／哈希／目录失败保留快照、条件请求、哈希不变、发布失败重试、人工保护、别名与日期误匹配通过 |
| 自动计价 | 明确零价、部分 priority/flex 覆写、priority/fast/ultrafast 的 2 倍默认、flex 的 0.5 倍默认、上下文边界、缓存、工具／按张费用不叠倍率通过 |
| 输入型 embedding | 两个默认源的类型证据、可选 scope 的旧序列化／哈希兼容、存储与 Quote 冻结、输出价格与异常输出拒绝、错请求类型预发送拒绝通过 |
| SQLite、MySQL 5.7.19、PostgreSQL | 隔离实例实际执行自动价格合同和启动迁移，包括重复启动、大小写模型身份、并发发布、来源哈希保护、批次事务回滚、人工归属、历史 Quote 和 embedding scope 持久化，全部通过 |
| 静态与构建 | 受影响包 `go vet`、Go 构建、`gofmt` 和 `git diff --check` 通过；Vue 资源图检查后构建实际嵌入前端的可执行程序 |

三库执行命令为 `go test ./model -run 'TestAutomaticBillingPriceContractsAcrossDatabases|TestGatewayStartupMigrations' -timeout 60s -v`，MySQL 与 PostgreSQL 均配置隔离 DSN，未将跳过算作通过。两个临时实例已正常关闭。后续没有 DSN 的普通测试会跳过这两库分支；不重复把它们计为三库运行。

真实本地 HTTP 网关回归验证：正常 embedding 在发送前预留并按输入结算；`completion_tokens`、`output_tokens` 和输出的 text、reasoning、audio、image 明细等异常保留预留并进入对账，不能当作免费输出。直接 DTO、规范化 DTO 及内嵌 OpenAI 原始使用量的冲突证据也有定向覆盖。聊天请求使用输入型价格时零上游发送、零预留。服务档在自动 PriceVersion 内冻结，之后人工改价不影响旧 Quote 回放。

具体模型失效时持久化不可用标记，覆盖从未有价与曾经生效两种情况。三库回归验证占位创建／回滚、人工并发保护与恢复；真实 HTTP 回归确认 reasoning 具体身份不能越过标记读取基础价，已发送请求仍按冻结报价结算。自动档位使用规范小写值，手动表达式的大小写敏感属性原样冻结和执行。

错误诊断按 UTF-8 字节有界保存：来源 `last_error` 最多 512 字节，最终任务结果最多 60 KiB，最多展示 100 个模型而完整总数不变。回归使用超长来源标识与 JSON 转义字符，验证失败来源不阻断后备同步，也不使 MySQL `TEXT` 结果列溢出。

## 最终构建与界面

在全新隔离 SQLite 文件库启动实际可执行程序，完成初始化、Root 登录、默认启用／30 分钟、关闭定时后手动同步、人工价保护、embedding scope 保存与预览、异常输出预览拒绝、最近任务回载、恢复自动版本冲突及成功恢复、匿名接口 401。

最终构建实测初始有效价格身份 11,265 个，包含供应商限定名和别名，不代表互不重复的基础模型。联网任务四个来源均成功，任务为 `succeeded`，`checked=4`、`changed=3`、`published=921`、`deactivated=9`、`unconfigured_count=3233`、`errors=[]`，返回 100 个待配置详情。这些数字是本轮数据快照的实测值，不作为未来来源变化时的固定断言。没有等待真实 30 分钟周期；周期与首次调度、持久化调度、任务锁和去重由现有统一系统任务测试及定向配置测试覆盖。

| 前端检查 | 结果 |
| --- | --- |
| Vitest | 10 个定向文件首轮 86 例通过，新增父页面刷新测试后相关 2 文件 10 例通过，共覆盖 87 个不同用例 |
| 静态与构建 | `bun run typecheck`、`lint`、`format:check`、`build` 通过；文档更新另做格式检查。已有图表分块超过 500 kB 的构建提示不阻断产物 |
| Playwright | 模型管理 21 例通过；最后刷新入口调整后自动定价 4 例再次通过 |
| 浏览器范围 | 中英文、桌面／手机、明暗主题，任务回载与轮询、统计与逐源原因、关闭计划后手动检查、恢复自动成功／409、embedding 编辑和横向溢出；使用模拟 API，截图已查看 |

截图位于本地忽略目录 `frontend/output/playwright/`，最后保留自动定价四组场景；真实进程的临时库与结果位于 `.local-tests/pricing-refactor-d495dbf4/`。这些测试数据不属于发布物。

## 覆盖边界

无法可靠表达的收费项拒绝该来源条目，继续尝试下一个完整来源。例如缺少账务映射的 Google Maps、音频缓存、按字符收费及 OpenRouter 条件 overrides，不会被悄悄当作零价。四个来源全部不可用才停用自动价，人工完整价格仍可优先生效。

保留旧种子用于识别历史归属，不再参与新安装灌价或请求兜底；旧模型、缓存、媒体和工具硬编码价格表及失效设置入口已清理。权限和路由仍需要的名称匹配独立保留。验证覆盖上述合同，不代表已经验证每个上游模型的实际用量格式或完成生产部署。
