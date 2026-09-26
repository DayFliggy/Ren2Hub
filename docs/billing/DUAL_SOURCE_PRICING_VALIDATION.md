# 双源模型定价重构验证（2026-09-26）

本轮在 `0c503f4e561a0db1ec6f32a302491adc08082f52` 上替换自动价格获取、解析、匹配和发布编排。活动来源只有 Sub2API 和 AxonHub：精确身份优先，同一身份采用 Sub2API 主源、AxonHub 补缺，每个模型选择一个来源的完整价格。models.dev、OpenRouter 只保留历史归属和任务读取兼容，不能重新写入或启用。规则见[定价说明](PRICING.md)、[解析器说明](../../pkg/autopricing/README.md)和 [API 合同](API.md)。

重构止于发布不可变 `PriceVersion`。请求锁价、资金预留、Decimal 金额协议、幂等结算、异步任务和恢复机制保持原合同；不涉及订阅计费。本轮验证使用本机隔离进程和数据库，未部署生产应用、恢复生产数据或调用付费模型。

## 获取、匹配与发布

| 检查 | 实际结果 |
| --- | --- |
| 来源获取 | Sub2API SHA256 无变化与不匹配、AxonHub 条件请求／304、响应大小上限、取消／超时、损坏／空／全部不可用目录的缓存保护通过 |
| 离线启动 | 两个内嵌目录均可解析并发布；已有持久化快照不会被内嵌内容替换；首次检查失败且没有有效缓存时，不将旧价格批量停用 |
| 数值与收费语义 | Decimal 精度、明确零与缺失、缓存不重复收费、长上下文边界、服务档位、embedding 限制、现有媒体／工具费用及无法表达的非零收费项拒绝通过 |
| 模型身份 | 精确身份先于别名、主源优先和补源接管、Google 资源名／日期／reasoning 单向别名、禁止日期兄弟猜价、同价供应商歧义、缺价供应商引起的裸名歧义、重复身份冲突不受数组顺序影响通过 |
| 价格发布 | 同价不新增版本、人工价格保护、恢复自动的版本冲突、来源快照变化阻止旧批次发布、慢下载不能覆盖新快照、发布失败后同哈希重试通过 |
| 失价与冻结报价 | 有效目录缺价时持久化具体身份不可用标记，阻止宽泛别名回退；网络或整份目录失败沿用有效快照；旧价格和冻结 Quote 不被改写通过 |
| 配置与诊断 | 默认启用／30 分钟，5–1440 分钟校验，原禁用配置保留，禁用后手动任务仍可运行；双源状态、逐模型原因、错误和有界任务结果通过 |

普通 Go 回归执行以下命令，均通过；每次后端测试均设置 60 秒超时，不代表运行了全仓测试。

```text
go test ./pkg/autopricing ./pkg/billing ./pkg/billingexpr ./model ./service ./relay/helper ./router -timeout 60s
go test ./controller -run 'Test(AutomaticBillingPricing|RestoreAutomaticBillingPrice|Billing|Preview|Publish)' -timeout 60s
```

最后的空价格数组哈希修复后，再次运行 `go test ./pkg/autopricing ./service -timeout 60s` 通过；最终审查补充 `TestAutomaticPricingHTTPTimeoutRetainsSnapshot`，哈希请求和正文请求超时以及主动取消的定向回归通过。两个内嵌原始目录的离线解析也实际执行：Sub2API 接受 189 个身份、拒绝 66 个；AxonHub 接受 10,741 个、拒绝 4,374 个。数量包含供应商限定名和别名，不代表不同基础模型数量，也不作为上游更新后的固定断言。

## 三种数据库

使用 SQLite 和本机全新 MySQL 5.7.19、PostgreSQL 16.4 隔离实例，依次执行下表测试；三种数据库分支均实际运行，没有把跳过项算作通过。

| 测试 | SQLite | MySQL | PostgreSQL |
| --- | --- | --- | --- |
| `TestAutomaticBillingPriceContractsAcrossDatabases` | 通过 | 通过 | 通过 |
| `TestGatewayStartupMigrations` | 通过 | 通过 | 通过 |
| `TestBillingDatabaseIntegration` | 通过 | 通过 | 通过 |

自动价格合同覆盖新安装、历史种子／人工／四源旧价格归属、关闭自动时迁移、重复迁移、退休来源写入拒绝、自动接管或停用、来源与人工并发冲突、历史价格和冻结 Quote 保留。启动迁移验证实际数据库初始化路径，账务集成每库覆盖 33 项场景，包括预留、重试、重复结算、持久化 Quote 恢复、断写和发送保护。

测试实例已正常关闭，进程和监听端口确认消失。执行器与日志留在忽略目录 `.local-tests/pricing-rebuild-validation-20260926/`；最近两份日志为 `20260926-153140-506-__model-tests.log` 和 `20260926-153210-387-__service-tests.log`。连接信息未进入跟踪文件。

## 前端与实际程序

前端 6 个相关 Vitest 文件共 48 项通过，`typecheck`、`lint`、`format:check` 和生产构建通过。Playwright 模型管理的 12 项受影响场景通过，另完成 2 项手机截图复核；覆盖中英文、桌面／手机、明暗主题、缓存状态与错误、任务结果、恢复自动及 409 后保留编辑内容。截图已查看，无横向溢出。浏览器场景使用模拟 API；真实后端合同另由以下实际进程验证。

在 `frontend/` 执行的定向命令如下；最后两项手机复核属于原用例重跑，不另计新增用例。

```text
bun run test:run -- src/api/__tests__/automaticPricing.spec.ts src/api/__tests__/systemManagement.spec.ts src/components/admin/__tests__/AutomaticPricingPanel.spec.ts src/components/admin/__tests__/ModelPricingEditor.spec.ts src/components/admin/__tests__/ModelManagementView.spec.ts src/components/admin/__tests__/ModelMetadataDrawer.spec.ts
bun run test:visual -- tests/visual/model-management.spec.ts --grep 'automatic price controls|model management'
bun run test:visual -- tests/visual/model-management.spec.ts --grep 'automatic price controls .* mobile' --output output/playwright-pricing-mobile
```

将最新 Vue 构建嵌入程序，`TestProductionEmbeddedAssetGraph` 和可执行程序构建通过。在全新 SQLite 文件库启动该程序，实际 HTTP 检查通过：

- Vue 入口及其实际哈希资源返回成功，匿名定价管理接口返回 401，初始化和管理员登录成功。
- 初始双源价格发布 10,902 个身份，默认启用且周期为 30 分钟。
- 关闭周期任务后手动联网同步成功，任务状态 `succeeded`，两个活动来源均为 `current`，没有来源错误。
- Sub2API 在线目录接受 189 个身份、拒绝 66 个；AxonHub 接受 10,738 个、拒绝 4,370 个。在线内容相较内嵌目录有变化，正常进入持久化和发布链路。
- 人工发布的 `gpt-5.4` 免费价格在同步后保持原版本；错误 `expected_version` 的恢复请求返回 409，正确版本恢复成功。
- 对原价格重新执行同一冻结预览，Quote 哈希与收费金额保持一致。
- `/prices/seed` 返回 10,899 个目录草稿，修订标识只含两个活动来源。

实际程序结果位于忽略目录 `.local-tests/pricing-dual-source-02b3530e/smoke-result.json`。进程已关闭并确认无残留监听，构建用的跟踪占位入口已原样恢复。未等待真实 30 分钟周期；调度、任务去重和配置行为由自动定价 API 及既有系统任务回归覆盖。

## 静态检查与交付边界

受影响 Go 包的 `go vet`、`go build ./...`、`gofmt` 和 `git diff --check` 通过。前端构建只有既有的图表分块超过 500 kB 提示。旧四源适配和合并路径已删除，`sync-models-dev` 厂商元数据脚本保留；历史价格、冻结报价、任务和资金记录不删除。

以上验证不代表已验证每个上游模型的真实用量格式，也不代表生产应用已升级。源码按根仓库同步工作流发布，具体源码提交与服务器同步状态见[服务器档案](../../../docs/server/SERVER_PROFILE.md)。此前四源实现的测试证据保留在[历史验证记录](AUTOMATIC_PRICING_VALIDATION.md)，不能当作本轮双源验证结果。
