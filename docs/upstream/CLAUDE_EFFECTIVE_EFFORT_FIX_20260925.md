# Claude 消息级 effort 本地兼容修复

修复基线 `563866a0c54d42540eb179fb52c7143de697d14c`，上游范围保持 `c2b7a9a9e0b548c2051a949fceabb59029adcb49`。本轮关闭消息级 effort 未进入冻结报价，以及跨协议转换静默丢失配置的两项审计发现。原始失败复现保存在忽略目录 `.local-tests/AUDIT_C2B7A9A9E_20260925.md` 及其列出的日志。

## 实现

- RelayKit 新增只读 `ClaudeRequest.ResolveEffectiveEffort()`，消息级设置等待下一个 user 激活并持续；尾部待生效配置不覆盖当前值，缺省不猜默认值，非法结构和角色返回路径错误。原 `GetEfforts()` 继续只读顶层，原消息和 JSON 不被改写。
- 计价依 DTO 类型或实际发送协议选择解析器，最终 Claude effort 同时进入冻结 Quote 与日志。覆盖、删除和重试重新计算当前尝试；allow 转换后按目标请求计价。显式 thinking/预算属性保留，旧 Quote 和恢复结算不重新定价。
- 消息级 output_config 的存在性（含空对象和 null）产生 `claude_message_output_config_not_portable`。原生继续保留，safe/strict 拒绝跨协议，allow 记录诊断而不近似映射。经过 Chat 再回 Claude 的路径也在首次协议边界检查，不因最终格式相同绕过保护。
- 原生字段及 beta 转发策略沿用上游，不自动添加 beta、不新增模型能力白名单。覆盖生成非法 effort 的类型化错误返回 400，其他通道配置错误保留既有分类。

具体协议及计价规则见 [rc.40 协议合同](RC40_PROTOCOL_CONTRACT.md)、[计价契约](../../pkg/billingexpr/expr.md)和 [RelayKit](../../relaykit/README.md)。没有数据库迁移、价格格式或 HTTP 参数变化。

## 正式回归

| 边界 | 回归位置与覆盖 |
| --- | --- |
| 解析 | `relaykit/dto/claude_effort_test.go`：顶层、激活时序、连续与尾部切换、空值、非法输入、原值与复制隔离 |
| 计价及参数覆盖 | `relay/helper/claude_effort_billing_test.go`、`relay/common/claude_effort_override_test.go`：已知协议、属性/日志一致、删除清理、显式 thinking、旧 Quote 不变 |
| 转换 | `relaykit/relayconvert/claude_output_config_test.go`：24 组合、字段存在性、直接/兼容/API ID/多步入口、计划后请求变化、6 组合往返 |
| 实际 HTTP 计价 | `router/gateway_claude_effort_billing_test.go`：24 个子场景，含 DTO/透传、覆盖、非法请求零资金、渠道失败重试和冻结报价恢复 |
| HTTP 预览与转换 | `router/gateway_claude_output_config_conversion_test.go`：12 个子场景，原生/三目标及三策略，预览与执行一致；拒绝零发送零资金，allow 日志含诊断 |
| 原生供应商边界 | `relay/channel/claude/native_output_config_test.go`：12 个子场景，Claude/AWS/Vertex × DTO/透传 × JSON/流式，验证 beta 和可重放请求体 |

资金回归采用测试价格 `v1:p * (attr("effort") == "high" ? 10 : 1)`，顶层 medium、已激活消息 high、1000 输入 token 应结算 0.01 USD / 5000 quota。HTTP 实测检查实际发送、报价、余额、累计消费、预留归零和日志；不是 Anthropic 官方价格对比。

## 本地验证

Windows，`GOMAXPROCS=2`，测试使用 `-p 2 -timeout 60s -count=1`。日志位于 `.local-tests/claude-effort-fix/`。

| 检查 | 结果 | 日志 |
| --- | --- | --- |
| RelayKit 全包、GOWORK=off | 9 个测试包、287 个顶层测试、865 个测试/子测试事件通过，无失败或跳过 | `relaykit-final.jsonl` |
| 受影响根包 | relay/common/helper、Claude/AWS/Vertex、billing/billingexpr 共 8 包；391 个顶层测试、1673 个测试事件通过，无失败或跳过 | `root-affected-final.jsonl` |
| router 全包 | 165 个顶层测试、1528 个测试事件通过；17 个需独立 SDK/负载环境的旧用例跳过，不计为通过 | `router-final.jsonl` |
| 根模块与 RelayKit vet/build | 均退出 0；RelayKit 禁用 workspace | `root-vet.log`、`root-build.log`、`relaykit-vet.log`、`relaykit-build.log` |

修改前宿主与转换红测，以及覆盖非法 effort 500、多步返回原协议绕过的失败记录保留。新增正式回归已经转绿，不用原整包通过掩盖新路径遗漏。测试只使用临时数据库和模拟上游，不调用商业供应商。

服务器固定提交测试、三库检查和源码同步结果由根工作区验收记录承载。本地结果不代替服务器结果；本轮不更新常驻应用镜像。
