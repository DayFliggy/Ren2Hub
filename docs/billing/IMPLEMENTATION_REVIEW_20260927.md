# 2026-09-27 计费核心实现复审

本次复审覆盖 Decimal 表达式、冻结 Quote、计量单位、自动价格原文与字段证据、候选身份与完整性、普通请求与任务的资金结算边界，以及插件用量和 Realtime 的精度、准入边界。审计比较起点为 `c178744028`，包含当前实现的已提交和未提交修改。该报告不代表全项目部署验收完成。

## 已复现并修复的问题

| 优先级 | 修复前行为与触发条件 | 修复 |
| --- | --- | --- |
| 高 | `UpdateBillingRecoverySettlement` 可把只有预留金额的记录直接改为待结算；`Settle` 随后生成没有 Quote、Usage、Charge 和指纹的消费事件。隔离 SQLite 复现为预留 40、最终扣款 30，全部计价证据为空。 | 金额接口只能确认已经准备好的结算意图；实际资金事务必须校验已保存的完整事实、费用分项、报价和证据指纹，并重新计算金额。 |
| 高 | `createTaskCompletionBilling` 另有一套结算准备逻辑，允许空 Usage/Charge，存在任务绕过普通消费校验的入口。 | 任务、普通请求及最终扣款共用 `validateBillingConsumption`；失败退款明确不产生消费费用，成功任务必须有完整实际事实。 |
| 高 | 自洽的 Quote 哈希不证明价格已发布，伪造版本 ID 或未发布预览可被内部调用方当成收费依据。 | `ValidatePublishedBillingQuote` 核验真实数据库版本、冻结定义及哈希；最终收费还要求同一请求和渠道的不可变尝试与报价相符。活动版本切换不影响旧报价。 |
| 中 | 纯 `flat_fee` 放过所有额外数量；带有 7 次未定价搜索工具调用的用量仍只收固定 3 quota。 | 固定费用只允许忽略其覆盖的常规产品计量，不能吞掉未定价 `tool:*` 或未知数量。表达式加固定费用也不获得此豁免。 |
| 中 | schema 只检查 `price.Unit == "per_" + field.Unit`，任意 `banana` 单位也能参与结算。 | 价格及 schema 统一使用 `pkg/billingunits` 注册表；未知单位在发布前拒绝，秒、分钟和小时仅在同一物理维度内换算。 |
| 中 | 输入总量为 100、图像输入 80、音频输入 80 时，归一化将负的剩余文本量夹到零并收取 160 token。 | 拒绝超过总量的互斥媒体子类别；缓存与媒体本来允许重叠，合法缓存集合继续可用。 |
| 中 | 插件计量入口对整个上下文执行精确金额验证，Suno 的普通 `continue_at: 12.5` 参数导致请求在预留前失败，即使计费只使用作品数。 | 请求校验只检查声明或明确约定的计量字段。计量 hook 读取未经保真的数字时立即报错；未读取的普通参数不阻断请求。返回事实仍执行严格精度检查，不能把不可信小数乘成整数后计费。 |
| 高 | 自动发布只核对来源快照指针与哈希，没有验证原文确实产生该价格。隔离复现把来源的输入价 1 USD/百万 token 改成 999，并填入不存在的字段路径，仍可发布并计算出 999 USD。 | `model/billing_price_evidence.go` 重新解析归档原文并核验正文 SHA，比较完整适配器结果、字段证据和计划哈希。批量发布使用内部生成且不可由调用方填写的证据对象；事务仍校验来源当前指针，不能仅凭哈希字符串发布伪造计划。 |
| 中 | 未启用的地图工具、无法支持的搜索上下文价会使整个模型基础价格被拒绝，普通请求也失去有效来源。 | 已识别的可选能力保留字段级证据，并明确标记为未定价能力；普通请求可用，实际启用地图或未覆盖搜索能力时仍在发送前拒绝。不放宽任意未知价格字段。 |
| 中 | 不同来源中的同名裸模型可能跨供应商补缺，原供应商缺缓存价时被另一个供应商的同名价替代。 | 原始供应商命名空间进入来源证据及价格身份；候选沿用已声明的供应商，只允许同身份的完整备源。身份未知时不猜测供应商，不用最低价消除歧义。 |
| 中 | 价格预览先把数值 JSON 解码为 float64，再进入 Decimal，引入第二个精度入口。 | 预览通过精确 JSON 数字解码器读取，仍执行必填参数校验。回归覆盖 `0.7 × 3 + 0.9 × 1` 和 `0.9007199254740993` 数量。 |
| 中 | Realtime 上游明确报告输入、输出为零，音频细分却为 10 时，归一化将输入改成 10，继续当作完整实际用量。 | 完整非估算观察保留明确零值；细分大于该方向总量直接失败。本地估算仍可从细分生成缺失总量，两者不混用。 |
| 中 | Realtime 只检查顶层 `input_audio_transcription`，嵌套 `audio.input.transcription` 可穿过校验；未知工具类型也可能被原文转发。 | 两种转写入口均要求独立计量合同；当前不支持时拒绝。Realtime 只允许客户端执行的函数声明，已配置收费函数及其他工具仍须具备受支持的计量入口，固定费不替它们补合同。 |
| 中 | Realtime 无条件要求单独音频 token 价格，纯固定费或按次价在预留前被错误拒绝；图片准入误用未注册的 `img_i` 名称。 | 纯固定请求价覆盖常规 token 维度，混合 token 价格不获得豁免；工具及物理单位仍单独检查。图片使用注册维度 `img`，已定价输入可准入。 |
| 高 | Realtime 完成一轮后又发送下一轮生成或音频输入，连接中断仍可能把上一轮完整用量当作整个会话终态。 | 记录未完成的生成与输入；完整响应消费对应的待处理项，残余工作或混入估算时保留预留。重复响应去重，同时拒绝同 ID 的冲突用量或终态。 |
| 中 | 空 usage 对象与明确零计数在部分 Responses、Realtime、音频和 SSE 路径中无法区分。 | DTO 保存字段存在状态；缺失计数不是完整事实，明确零仍保留。转换链传递观察证据而不改变协议输出字段。 |
| 中 | 图片报价只冻结重组后的数量和质量，可能丢失最终发送的 service_tier；工具 ID 别名冲突可能掩盖重复事件。 | 图片使用实际序列化参数冻结档位；工具调用统一登记 ID、call ID 和索引，冲突身份进入对账，不重复计费。 |
| 中 | 渠道探测有单独的用量组装逻辑，收紧终态合同后无法正常结算完整响应。 | 探测调用与普通中继相同的文本计价、终态和固定费处理入口；保留真实报价与尝试校验。 |

最终事实统一使用 `Usage.ReadyForSettlement()`：完整、非估算、非非法、存在来源，且业务终态为 `completed`。只有管理员给出金额和说明，仍不足以把未知用量变成可扣款事实；明确人工放弃收费的释放操作保持可用。

## 通过的检查

- Decimal 字面量、数值属性和 schema 数量保持原始精度；拒绝浮点金额输入、默认内置函数、动态字段、间接函数引用和系统时钟读取。
- 所有表达式分支共用依赖分析，零系数和未执行分支仍接受完整性校验；除法固定 36 位小数，金额只在最终 quota 转换时执行一次 half-up。
- `0.7 × 3 + 0.9 × 1` 得到 3 微美元与 2 quota；整数中间结果不会通过机器整数回绕。
- Quote 哈希、价格哈希、来源版本和冻结日历参与重放；未知或估算用量保持预留，不因断流直接结束收费。
- 任务回归覆盖缺失事实、估算、部分用量、未知终态及费用篡改，均不产生最终消费；真实数据库发布的价格用于资金测试 fixture。
- 最终事务再次检查保存的用量、金额分项、版本与尝试，防止准备阶段之后证据损坏仍被扣款。
- 来源归档按正文去重，刷新后旧版本仍可取回原文；正文损坏、伪造字段、伪造计划和并发发布冲突都拒绝，手工价格不被自动同步覆盖。每次同步统一解析后分批发布，避免对整份目录逐模型重复解析。
- 单来源无快照或刷新失败不阻断合格备源；候选独立覆盖请求需要的维度，不拼接不同来源的价格。服务档没有默认倍率，多阈值冲突不自行组合。
- Midjourney 使用真实发布价格测试终态收费；缺失完成证据时人工金额不能扣费，结算事件写入失败后仍保留可重放证据，正确金额可幂等恢复。
- Realtime 真实 WebSocket 网关回归覆盖 `flat_fee` 和 `calls` 两种固定请求价，连续两轮音频响应只产生一笔 3 quota 结算；准入边界覆盖嵌套转写、未知工具、图片价格和明确零值。

验证命令与结果：

```text
go test ./pkg/billingexpr ./pkg/billing ./model -count=1 -timeout 60s
PASS: billingexpr 0.337s, billing 5.940s, model 28.711s

go test ./pkg/jsplugin -count=1 -timeout 60s
PASS: 8.609s

go test ./relay/channel/task/jsplugin -count=1 -timeout 60s
PASS: 6.637s

go test ./router -run '^TestGatewaySunoContinuationKeepsOwnerAndPrivateTaskID$' -count=1 -timeout 60s
PASS: 0.220s
```

前一轮 service 验证记录中的 `TestTextMissingQuoteCannotUseOldRatioOrPrice` 失败是旧错误文案断言不一致，调用仍然拒绝缺少 Quote 的计费，未发现该测试对应的运行时绕过。

来源专项测试通过 `pkg/autopricing`、价格模型和自动同步用例，覆盖原文真实性、同名供应商、可选能力、来源隔离、活动版本 CAS、生效窗口、手工保护及离线内置目录。`TestAutomaticBillingPriceContractsAcrossDatabases` 的 SQLite 分支通过；MySQL、PostgreSQL 未配置 DSN 时仍明确跳过，不计为完成。

后续修复的定向验证：

```text
go test ./service -run 'Test(Midjourney|TaskPluginProcessExitRecovery)' -count=1 -timeout 60s
PASS: service 9.886s

go test ./pkg/billing ./service ./relay/helper ./router -run 'Test(PricingSupport|FixedRequestCoverage|RealtimeProviderZero|RealtimeUsageRejects|RealtimeAccumulates|RealtimeFixedRequestPrice|RealtimeImageInput|GatewayRealtimeFixedPrice|RealtimeCannotEnable|PricingRequirements)' -count=1 -timeout 60s
PASS: billing 5.716s, service 14.202s, helper 19.861s, router 17.115s

python -m unittest scripts.tests.test_ren2hub_matrix
PASS: 17 tests
```

根仓库矩阵已把自动价格合同列为必须有 SQLite、MySQL、PostgreSQL 实际结果的任务，仅顶层测试通过不能代替三库证据。新增的资金与报价 guard 也纳入执行集合；它们自身使用 SQLite fixture，不冒充三库测试。

主线全项目测试暴露的旧夹具和探测入口问题已逐项修复。受影响的 service、router、billing、plugins 全包重新运行通过（26.861s、38.284s、0.072s、2.479s）。Controller 按完整库存分为 9 个互斥批次、每批限制 60 秒，403 项顶层测试中 400 通过、3 因外部环境缺失跳过；额外数据库子项跳过 8 项。之后修改的渠道探测相关用例单独补测通过。RelayKit 已独立在 `GOWORK=off` 下构建并验证 DTO/转换测试。跳过项不视为验收通过。

## 尚未由本次复审验证

MySQL 和 PostgreSQL 的数据库矩阵用例没有配置独立 DSN，保持明确跳过；SQLite 已实测。自动来源证据与上述 WebSocket 场景已有定向回归，公开价格及完整 HTTP/SSE/WebSocket、媒体协议矩阵仍由集成工作流继续验收。三个全新部署环境、备份恢复及旧业务数据删除尚需独立的运行验收，不能用本报告或健康检查替代。
