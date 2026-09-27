# 2026-09-27 计费核心实现复审

本次复审覆盖 Decimal 表达式、冻结 Quote、计量单位、普通请求与任务的资金结算边界，以及插件用量的精度边界。审计比较起点为 `c178744028`，包含当前实现的已提交和未提交修改。该报告不代表全项目部署验收完成。

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

最终事实统一使用 `Usage.ReadyForSettlement()`：完整、非估算、非非法、存在来源，且业务终态为 `completed`。只有管理员给出金额和说明，仍不足以把未知用量变成可扣款事实；明确人工放弃收费的释放操作保持可用。

## 通过的检查

- Decimal 字面量、数值属性和 schema 数量保持原始精度；拒绝浮点金额输入、默认内置函数、动态字段、间接函数引用和系统时钟读取。
- 所有表达式分支共用依赖分析，零系数和未执行分支仍接受完整性校验；除法固定 36 位小数，金额只在最终 quota 转换时执行一次 half-up。
- `0.7 × 3 + 0.9 × 1` 得到 3 微美元与 2 quota；整数中间结果不会通过机器整数回绕。
- Quote 哈希、价格哈希、来源版本和冻结日历参与重放；未知或估算用量保持预留，不因断流直接结束收费。
- 任务回归覆盖缺失事实、估算、部分用量、未知终态及费用篡改，均不产生最终消费；真实数据库发布的价格用于资金测试 fixture。
- 最终事务再次检查保存的用量、金额分项、版本与尝试，防止准备阶段之后证据损坏仍被扣款。

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

## 尚未由本次复审验证

MySQL 和 PostgreSQL 的数据库矩阵用例没有配置独立 DSN，保持明确跳过；SQLite 已实测。公开价格、自动来源证据、完整 HTTP/SSE/WebSocket 与媒体协议矩阵由其他工作流继续验收。三个全新部署环境、备份恢复及旧业务数据删除尚需独立的运行验收，不能用本报告或健康检查替代。
